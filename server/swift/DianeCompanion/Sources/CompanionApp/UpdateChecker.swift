import Foundation
import AppKit
import OSLog

@MainActor
final class UpdateChecker: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Updates")
    @Published private(set) var updateAvailable = false
    @Published private(set) var currentVersion: String?
    @Published private(set) var latestVersion: String?
    @Published private(set) var isChecking = false
    @Published private(set) var isUpdating = false
    @Published private(set) var updateOutput: String = ""

    weak var statusMonitor: StatusMonitor?
    weak var cliManager: CLIManager?

    private let repoOwner    = "emergent-company"
    private let repoName     = "diane"
    private let checkInterval: TimeInterval = 3600
    private var timer: Timer?
    private var hasStarted = false
    private var downloadUrl: URL?
    private var releaseData: GitHubRelease?

    deinit { timer?.invalidate() }

    // MARK: - Public

    func start() async {
        guard !self.hasStarted else { return }
        self.hasStarted = true

        if let appVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String {
            self.currentVersion = appVersion
        } else {
            self.currentVersion = "unknown"
        }

        logger.info("UpdateChecker: current version = \(self.currentVersion ?? "nil")")

        await checkForUpdates()
        timer = Timer.scheduledTimer(withTimeInterval: self.checkInterval, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            Task { @MainActor in
                await self.checkForUpdates()
            }
        }
    }

    func checkForUpdates() async {
        logger.debug("UpdateChecker: Starting checkForUpdates")
        self.isChecking = true
        defer { self.isChecking = false }

        guard let url = URL(string: "https://api.github.com/repos/\(self.repoOwner)/\(self.repoName)/releases/latest") else { return }

        do {
            var request = URLRequest(url: url)
            request.setValue("application/vnd.github.v3+json", forHTTPHeaderField: "Accept")
            request.timeoutInterval = 10

            let (data, response) = try await URLSession.shared.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                logger.error("UpdateChecker: Did not receive a valid HTTP response.")
                return
            }
            guard http.statusCode == 200 else {
                logger.error("UpdateChecker: GitHub API call failed with status code \(http.statusCode).")
                return
            }

            let release = try JSONDecoder().decode(GitHubRelease.self, from: data)
            self.releaseData = release
            self.latestVersion = release.tagName

            if let htmlUrl = URL(string: release.htmlUrl) {
                self.downloadUrl = htmlUrl
            }

            let installed = self.currentVersion ?? "0.0.0"

            if installed == "unknown" || installed == "dev" || installed == "0.0.0-DEVELOPMENT" {
                self.updateAvailable = true
                logger.info("UpdateChecker: Update available (installed version \(installed) is dev/unknown).")
            } else {
                self.updateAvailable = isOlderVersion(installed, than: release.tagName)
                if self.updateAvailable {
                    logger.info("UpdateChecker: Update available: \(installed) -> \(release.tagName).")
                } else {
                    logger.info("UpdateChecker: No update available. Current version: \(installed).")
                }
            }
        } catch {
            logger.debug("UpdateChecker: checkForUpdates failed: \(error.localizedDescription)")
        }
        logger.debug("UpdateChecker: Finished checkForUpdates")
    }

    /// Download the latest DMG and install it in-place, then relaunch.
    func performUpdate() async {
        guard let release = self.releaseData, let version = self.latestVersion else {
            logger.error("UpdateChecker: No release data available")
            self.appendOutput("No release data available. Check again later.\n")
            return
        }

        self.isUpdating = true
        self.appendOutput("Starting update to \(version)...\n")

        // Find the DMG asset URL
        let dmgName = "Diane-\(version).dmg"
        guard let dmgAsset = release.assets.first(where: { $0.name == dmgName }),
              let dmgURL = URL(string: dmgAsset.browserDownloadURL) else {
            logger.error("UpdateChecker: DMG asset not found for \(version)")
            self.appendOutput("DMG asset not found for \(version).\n")
            self.isUpdating = false
            return
        }

        let currentAppURL = Bundle.main.bundleURL
        let tempDir = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("diane-update-\(UUID().uuidString)")
        let dmgPath = tempDir.appendingPathComponent(dmgName)
        let mountPoint = tempDir.appendingPathComponent("mount")

        do {
            try FileManager.default.createDirectory(at: tempDir, withIntermediateDirectories: true)

            // Download DMG
            self.appendOutput("Downloading \(dmgName)...\n")
            logger.info("UpdateChecker: Downloading DMG from \(dmgURL)")
            let (downloadURL, _) = try await URLSession.shared.download(from: dmgURL)
            try FileManager.default.moveItem(at: downloadURL, to: dmgPath)
            self.appendOutput("Downloaded (\(self.humanSize(dmgPath)))\n")

            // Mount DMG
            self.appendOutput("Mounting DMG...\n")
            try FileManager.default.createDirectory(at: mountPoint, withIntermediateDirectories: true)
            let mountOutput = try await self.shell("/usr/bin/hdiutil", args: ["attach", dmgPath.path, "-mountpoint", mountPoint.path, "-nobrowse", "-quiet"])
            logger.info("UpdateChecker: Mounted DMG: \(mountOutput)")

            // Find the .app inside the mounted volume
            let contents = try FileManager.default.contentsOfDirectory(at: mountPoint, includingPropertiesForKeys: nil)
            guard let newAppURL = contents.first(where: { $0.pathExtension == "app" }) else {
                self.appendOutput("No .app found in DMG.\n")
                throw UpdateError.appNotFound
            }

            self.appendOutput("Installing new version over current app...\n")

            // Copy new app over current app (works even for running apps on macOS)
            let destination = currentAppURL.deletingLastPathComponent().appendingPathComponent(newAppURL.lastPathComponent)
            _ = try await self.shell("/usr/bin/ditto", args: [newAppURL.path, destination.path])

            self.appendOutput("✅ Update installed to \(destination.path)\n")

            // Unmount DMG
            try await self.shell("/usr/bin/hdiutil", args: ["detach", mountPoint.path, "-quiet", "-force"])
            try? FileManager.default.removeItem(at: tempDir)

            // Relaunch and quit
            self.appendOutput("Relaunching app...\n")
            let config = NSWorkspace.OpenConfiguration()
            config.activates = true
            try? await Task.sleep(nanoseconds: 500_000_000)
            NSWorkspace.shared.openApplication(at: destination, configuration: config) { _, _ in
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    NSApplication.shared.terminate(nil)
                }
            }
            self.isUpdating = false
        } catch {
            logger.error("UpdateChecker: Update failed: \(error.localizedDescription)")
            self.appendOutput("❌ Update failed: \(error.localizedDescription)\n")
            try? FileManager.default.removeItem(at: tempDir)
            self.isUpdating = false
        }
    }

    // MARK: - Helpers

    private func appendOutput(_ text: String) {
        self.updateOutput += text
    }

    private func humanSize(_ url: URL) -> String {
        guard let attrs = try? FileManager.default.attributesOfItem(atPath: url.path),
              let size = attrs[.size] as? Int64 else { return "?" }
        let mb = Double(size) / 1_000_000
        return String(format: "%.1f MB", mb)
    }

    @discardableResult
    private func shell(_ path: String, args: [String]) async throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = args
        let outputPipe = Pipe()
        process.standardOutput = outputPipe
        process.standardError = Pipe()
        try process.run()
        process.waitUntilExit()
        let data = outputPipe.fileHandleForReading.readDataToEndOfFile()
        return String(data: data, encoding: .utf8) ?? ""
    }

    // MARK: - Version comparison

    /// Returns true if `v1` is strictly older than `v2` (both semver, optional "v" prefix)
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

// MARK: - Errors

enum UpdateError: Error, LocalizedError {
    case appNotFound
    case downloadFailed(String)

    var errorDescription: String? {
        switch self {
        case .appNotFound: return "Application bundle not found in DMG"
        case .downloadFailed(let msg): return "Download failed: \(msg)"
        }
    }
}

// MARK: - GitHub API models

private struct GitHubRelease: Decodable {
    let tagName: String
    let htmlUrl: String
    let assets: [GitHubAsset]

    enum CodingKeys: String, CodingKey {
        case tagName = "tag_name"
        case htmlUrl = "html_url"
        case assets
    }
}

private struct GitHubAsset: Decodable {
    let name: String
    let browserDownloadURL: String

    enum CodingKeys: String, CodingKey {
        case name
        case browserDownloadURL = "browser_download_url"
    }
}
