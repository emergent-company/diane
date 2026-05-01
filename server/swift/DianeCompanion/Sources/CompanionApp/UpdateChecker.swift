import Foundation
import AppKit

@MainActor
final class UpdateChecker: ObservableObject {
    @Published private(set) var updateAvailable = false
    @Published private(set) var currentVersion: String?
    @Published private(set) var latestVersion: String?
    @Published private(set) var isChecking = false
    @Published private(set) var isUpdating = false
    @Published private(set) var updateOutput: String = ""
    @Published private(set) var downloadProgress: Double = 0

    weak var statusMonitor: StatusMonitor?
    weak var cliManager: CLIManager?

    private let repoOwner    = "emergent-company"
    private let repoName     = "diane"
    private let checkInterval: TimeInterval = 300 // 5 minutes
    private nonisolated(unsafe) var timer: Timer?
    private var hasStarted = false
    private var releaseData: GitHubRelease?

    deinit { timer?.invalidate() }

    // MARK: - Public

    func start() async {
        guard !hasStarted else { return }
        hasStarted = true

        if let appVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String {
            currentVersion = appVersion
        } else {
            currentVersion = "unknown"
        }

        await checkForUpdates()
        timer = Timer.scheduledTimer(withTimeInterval: checkInterval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in await self?.checkForUpdates() }
        }
    }

    func checkForUpdates() async {
        logDebug("UpdateChecker: Starting checkForUpdates", category: "Updates")
        isChecking = true
        defer { isChecking = false }

        // Fetch recent releases and pick the highest semver — don't rely on
        // GitHub's /releases/latest endpoint which returns by publish date,
        // not semver order.
        guard let url = URL(string: "https://api.github.com/repos/\(repoOwner)/\(repoName)/releases?per_page=20") else { return }

        do {
            var request = URLRequest(url: url)
            request.setValue("application/vnd.github.v3+json", forHTTPHeaderField: "Accept")
            request.timeoutInterval = 10

            let (data, response) = try await URLSession.shared.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                logError("UpdateChecker: Did not receive a valid HTTP response.", category: "Updates")
                return
            }
            guard http.statusCode == 200 else {
                logError("UpdateChecker: GitHub API call failed with status code \(http.statusCode).", category: "Updates")
                return
            }

            let releases = try JSONDecoder().decode([GitHubRelease].self, from: data)
            guard let latest = releases.max(by: { a, b in isOlderVersion(a.tagName, than: b.tagName) }) else {
                logError("UpdateChecker: No releases found", category: "Updates")
                return
            }

            releaseData = latest
            latestVersion = latest.tagName

            let installed = currentVersion ?? "0.0.0"

            if installed == "unknown" || installed == "dev" {
                updateAvailable = true
                logInfo("UpdateChecker: Update available (installed version \(installed) is dev/unknown).", category: "Updates")
            } else {
                updateAvailable = isOlderVersion(installed, than: latest.tagName)
                if updateAvailable {
                    logInfo("UpdateChecker: Update available: \(installed) -> \(latest.tagName).", category: "Updates")
                } else {
                    logInfo("UpdateChecker: No update available. Current version: \(installed).", category: "Updates")
                }
            }
        } catch {
            logDebug("UpdateChecker: checkForUpdates failed: \(error.localizedDescription)", category: "Updates")
        }
    }

    /// Actually download the DMG, install it, and relaunch the app
    func performUpdate() {
        guard !isUpdating else { return }
        guard let release = releaseData else {
            logError("UpdateChecker: No release data available", category: "Updates")
            return
        }

        // Find DMG asset
        guard let dmgAsset = release.assets?.first(where: { $0.name.hasSuffix(".dmg") && $0.name.hasPrefix("Diane-") }),
              let dmgURL = URL(string: dmgAsset.browserDownloadUrl) else {
            logError("UpdateChecker: No DMG asset found in release", category: "Updates")
            // Fallback: open release page
            if let url = URL(string: release.htmlUrl) {
                NSWorkspace.shared.open(url)
            }
            return
        }

        Task {
            await performDMGUpdate(dmgURL: dmgURL, version: release.tagName)
        }
    }

    // MARK: - DMG Download & Install

    /// Install using a post-termination script so macOS lets us replace the running app bundle.
    /// Steps: download → mount → create installer script → terminate → script copies + relaunches
    private func performDMGUpdate(dmgURL: URL, version: String) async {
        isUpdating = true
        updateOutput = "Downloading \(version)…"
        logInfo("UpdateChecker: Starting DMG download from \(dmgURL)", category: "Updates")

        do {
            // Step 1: Download DMG to temp directory
            let tempDir = FileManager.default.temporaryDirectory
            let dmgPath = tempDir.appendingPathComponent("Diane-\(version).dmg")

            // Clean up any previous temp file
            try? FileManager.default.removeItem(at: dmgPath)

            let (_, _) = try await downloadWithProgress(from: dmgURL, to: dmgPath)
            updateOutput = "Download complete. Installing…"
            logInfo("UpdateChecker: DMG downloaded to \(dmgPath.path)", category: "Updates")

            // Step 2: Mount DMG to find the .app name
            let mountPoint = tempDir.appendingPathComponent("diane-update-mount")
            try? FileManager.default.removeItem(at: mountPoint)

            _ = try await runCommand("/usr/bin/hdiutil", arguments: [
                "attach", dmgPath.path,
                "-mountpoint", mountPoint.path,
                "-nobrowse", "-quiet"
            ])

            let mountedApps = try FileManager.default.contentsOfDirectory(at: mountPoint, includingPropertiesForKeys: nil)
            guard let dmgApp = mountedApps.first(where: { $0.pathExtension == "app" }) else {
                throw UpdateError("No .app found in mounted DMG")
            }
            let appName = dmgApp.lastPathComponent

            // Step 3: Write a post-termination installer script
            let scriptPath = tempDir.appendingPathComponent("diane-installer.sh")
            let appPath = "/Applications/\(appName)"

            let script = """
#!/bin/bash
sleep 2
# Mount DMG
/usr/bin/hdiutil attach "\(dmgPath.path)" -mountpoint "\(mountPoint.path)" -nobrowse -quiet
sleep 1
# Remove old app (app is now terminated so this will work)
rm -rf "\(appPath)"
# Copy new app
cp -R "\(mountPoint.path)/\(appName)" "\(appPath)"
# Detach DMG
/usr/bin/hdiutil detach "\(mountPoint.path)" -quiet
# Relaunch
open -n -a "\(appPath)"
# Clean up DMG
rm -f "\(dmgPath.path)"
rm -f "\(scriptPath.path)"
"""
            try script.write(to: scriptPath, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: scriptPath.path)

            updateOutput = "Installing… (will relaunch)"
            logInfo("UpdateChecker: Launching post-termination installer script", category: "Updates")

            // Step 4: Launch installer script as a truly detached background process
            let installer = Process()
            installer.executableURL = URL(fileURLWithPath: "/bin/bash")
            installer.arguments = [scriptPath.path]
            installer.standardOutput = FileHandle.nullDevice
            installer.standardError = FileHandle.nullDevice
            try installer.run()

            // Step 5: This process must terminate NOW so macOS lets us replace the bundle
            logInfo("UpdateChecker: Terminating for update", category: "Updates")
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
                NSApplication.shared.terminate(nil)
            }

        } catch {
            logError("UpdateChecker: Update failed: \(error.localizedDescription)", category: "Updates")
            updateOutput = "Update failed: \(error.localizedDescription)"
            isUpdating = false

            // Report update failure
            reportError(
                title: "Auto-update failed",
                body: "Failed to download/install Diane update.\n\nError: \(error.localizedDescription)\nVersion: \(releaseData?.tagName ?? "?")",
                severity: "medium",
                category: "Updates",
                labels: "update"
            )

            // Fallback: open release page so user can manually install
            if let url = URL(string: releaseData?.htmlUrl ?? "https://github.com/\(repoOwner)/\(repoName)/releases/latest") {
                NSWorkspace.shared.open(url)
            }
        }
    }

    /// Download a file and report progress
    private func downloadWithProgress(from url: URL, to destination: URL) async throws -> (URL, URLResponse) {
        let session = URLSession(configuration: .default)
        // Simple download without progress delegate for now
        let (tempURL, response) = try await session.download(from: url)

        // Move to our destination
        try FileManager.default.moveItem(at: tempURL, to: destination)
        downloadProgress = 1.0

        return (destination, response)
    }

    private func runCommand(_ path: String, arguments: [String]) async throws -> String {
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global().async {
                let process = Process()
                process.executableURL = URL(fileURLWithPath: path)
                process.arguments = arguments

                let pipe = Pipe()
                process.standardOutput = pipe
                process.standardError = pipe

                do {
                    try process.run()
                    process.waitUntilExit()

                    let data = pipe.fileHandleForReading.readDataToEndOfFile()
                    let output = String(data: data, encoding: .utf8) ?? ""

                    if process.terminationStatus == 0 {
                        continuation.resume(returning: output)
                    } else {
                        continuation.resume(throwing: UpdateError("Command failed (\(process.terminationStatus)): \(output)"))
                    }
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    // MARK: - Version comparison

    private func isOlderVersion(_ v1: String, than v2: String) -> Bool {
        let parts1 = versionParts(v1)
        let parts2 = versionParts(v2)
        for i in 0..<max(parts1.count, parts2.count) {
            let a = i < parts1.count ? parts1[i] : 0
            let b = i < parts2.count ? parts2[i] : 0
            if a < b { return true }
            if a > b { return false }
        }
        return false
    }

    private func versionParts(_ v: String) -> [Int] {
        let stripped = v.hasPrefix("v") ? String(v.dropFirst()) : v
        let numeric = stripped.components(separatedBy: "-").first ?? stripped
        return numeric.split(separator: ".").compactMap { Int($0) }
    }
}

// MARK: - GitHub API models

private struct GitHubRelease: Decodable {
    let tagName: String
    let htmlUrl: String
    let assets: [GitHubAsset]?

    enum CodingKeys: String, CodingKey {
        case tagName = "tag_name"
        case htmlUrl = "html_url"
        case assets
    }
}

private struct GitHubAsset: Decodable {
    let name: String
    let browserDownloadUrl: String

    enum CodingKeys: String, CodingKey {
        case name
        case browserDownloadUrl = "browser_download_url"
    }
}

// MARK: - Previews

#if DEBUG
extension UpdateChecker {
    /// Create an instance pre-configured for preview canvases.
    /// Can only live in this file because `@Published private(set)` properties
    /// are file-private for writes.
    static func forPreviews(
        updateAvailable: Bool = false,
        currentVersion: String? = nil,
        latestVersion: String? = nil,
        isUpdating: Bool = false,
        updateOutput: String = ""
    ) -> UpdateChecker {
        let checker = UpdateChecker()
        checker.updateAvailable = updateAvailable
        if let cv = currentVersion { checker.currentVersion = cv }
        if let lv = latestVersion { checker.latestVersion = lv }
        checker.isUpdating = isUpdating
        checker.updateOutput = updateOutput
        return checker
    }
}
#endif

// MARK: - Error

private struct UpdateError: Error, LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}
