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
    @Published var autoUpdateEnabled = true {
        didSet { UserDefaults.standard.set(autoUpdateEnabled, forKey: "autoUpdateEnabled") }
    }
    @Published private(set) var previousVersion: String?
    @Published private(set) var rollbackAvailable = false

    weak var statusMonitor: StatusMonitor?
    weak var cliManager: CLIManager?

    private let repoOwner    = "emergent-company"
    private let repoName     = "diane"
    private let checkInterval: TimeInterval = 300 // 5 minutes
    private nonisolated(unsafe) var timer: Timer?
    private var hasStarted = false
    private var releaseData: GitHubRelease?
    private var shouldAutoUpdate = false
    private let dianeDir: String

    init() {
        self.dianeDir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".diane").path
    }

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

        autoUpdateEnabled = UserDefaults.standard.object(forKey: "autoUpdateEnabled") as? Bool ?? true

        // Check for rollback availability
        checkRollbackAvailability()

        // Migrate: clean up stale Diane.v*.backup.app from /Applications/ (pre-v1.38.52 storage)
        cleanupStaleApplicationsBackups()

        await checkForUpdates()
        timer = Timer.scheduledTimer(withTimeInterval: checkInterval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in await self?.checkForUpdates() }
        }
    }

    func checkForUpdates() async {
        logDebug("UpdateChecker: Starting checkForUpdates", category: "Updates")
        isChecking = true
        defer { isChecking = false }

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

            // Safety check: if version is the project default, version injection is broken.
            // Prevent auto-update loop — without this, the app compares "1.0" against
            // "v1.38.x", thinks an update is always available, and re-installs forever.
            if installed == "1.0" {
                logError("UpdateChecker: Version reports as '1.0' — version injection appears broken. Use manual download to reinstall.", category: "Updates")
                return
            }

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

            // Auto-update if enabled and update is available
            if updateAvailable && autoUpdateEnabled && !isUpdating {
                logInfo("UpdateChecker: Auto-update enabled — starting download", category: "Updates")
                shouldAutoUpdate = true
                performUpdate()
            }

            // Check for CLI-triggered DMG update (written by diane upgrade --auto or serve background check)
            checkDMGTrigger()
        } catch {
            logDebug("UpdateChecker: checkForUpdates failed: \(error.localizedDescription)", category: "Updates")
        }
    }

    /// Check for a DMG trigger file written by the CLI upgrade mechanism.
    /// When the CLI (running alongside the companion) detects a new version,
    /// it writes ~/.diane/diane.dmg-trigger instead of replacing the embedded binary.
    /// The companion picks this up and performs the full DMG-based app update.
    private func checkDMGTrigger() {
        let triggerPath = (dianeDir as NSString).appendingPathComponent("diane.dmg-trigger")
        guard FileManager.default.fileExists(atPath: triggerPath) else { return }

        do {
            let data = try Data(contentsOf: URL(fileURLWithPath: triggerPath))
            if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
               let version = json["version"] as? String,
               let available = json["available"] as? Bool,
               available {

                logInfo("UpdateChecker: DMG trigger found for \(version)", category: "Updates")

                // Only proceed if we have a newer version or no version info
                let installed = currentVersion ?? "0.0.0"
                
                // Safety check: skip DMG trigger update if version injection is broken
                if installed == "1.0" {
                    logError("UpdateChecker: DMG trigger skipped — version injection appears broken (reports '1.0').", category: "Updates")
                    try? FileManager.default.removeItem(atPath: triggerPath)
                    return
                }
                
                if installed == "unknown" || installed == "dev" || isOlderVersion(installed, than: version) {
                    latestVersion = version
                    updateAvailable = true
                    releaseData = nil // Will re-fetch from GitHub for DMG URL

                    logInfo("UpdateChecker: Triggered DMG update to \(version)", category: "Updates")

                    // Clear trigger so it doesn't re-trigger
                    try? FileManager.default.removeItem(atPath: triggerPath)

                    if autoUpdateEnabled && !isUpdating {
                        shouldAutoUpdate = true
                        performUpdate()
                    }
                } else {
                    // Already on this or newer version — clean up stale trigger
                    try? FileManager.default.removeItem(atPath: triggerPath)
                }
            } else {
                // Invalid trigger — clean up
                try? FileManager.default.removeItem(atPath: triggerPath)
            }
        } catch {
            logError("UpdateChecker: Failed to read DMG trigger: \(error.localizedDescription)", category: "Updates")
            try? FileManager.default.removeItem(atPath: triggerPath)
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
            logError("UpdateChecker: No DMG asset found in release \(release.tagName) — will retry on next check", category: "Updates")
            return
        }

        Task {
            await performDMGUpdate(dmgURL: dmgURL, version: release.tagName)
        }
    }

    /// Revert to the previous app version's backup
    func performRollback() {
        guard let prevVersion = previousVersion else {
            updateOutput = "No backup available for rollback"
            return
        }

        let backupPath = appBackupPath(version: prevVersion)
        guard FileManager.default.fileExists(atPath: backupPath.path) else {
            updateOutput = "Backup not found at \(backupPath.path)"
            rollbackAvailable = false
            return
        }

        isUpdating = true
        updateOutput = "Rolling back to \(prevVersion)…"

        Task {
            do {
                // Create installer script that swaps backup → active, then relaunches
                let tempDir = FileManager.default.temporaryDirectory
                let scriptPath = tempDir.appendingPathComponent("diane-rollback.sh")
                let appPath = "/Applications/Diane.app"

                let script = """
#!/bin/bash
sleep 2
# Remove current app
rm -rf "\(appPath)"
# Copy backup to active location
cp -R "\(backupPath.path)" "\(appPath)"
# Fix permissions
chmod -R a=u+rX "\(appPath)"
# Clean up backup file
rm -rf "\(backupPath.path)"
# Relaunch
open -n -a "\(appPath)"
# Clean up script
rm -f "\(scriptPath.path)"
"""
                try script.write(to: scriptPath, atomically: true, encoding: .utf8)
                try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: scriptPath.path)

                updateOutput = "Rolling back… (will relaunch)"
                logInfo("UpdateChecker: Launching rollback installer script", category: "Updates")

                let installer = Process()
                installer.executableURL = URL(fileURLWithPath: "/bin/bash")
                installer.arguments = [scriptPath.path]
                installer.standardOutput = FileHandle.nullDevice
                installer.standardError = FileHandle.nullDevice
                try installer.run()

                // Terminate the current app so macOS lets us replace the bundle
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
                    NSApplication.shared.terminate(nil)
                }
            } catch {
                logError("UpdateChecker: Rollback failed: \(error.localizedDescription)", category: "Updates")
                updateOutput = "Rollback failed: \(error.localizedDescription)"
                isUpdating = false
            }
        }
    }

    // MARK: - DMG Download & Install

    /// Install using a post-termination script so macOS lets us replace the running app bundle.
    /// Steps: backup → download → mount → create installer script → terminate → script copies + relaunches
    private func performDMGUpdate(dmgURL: URL, version: String) async {
        isUpdating = true
        updateOutput = shouldAutoUpdate ? "Auto-updating to \(version)…" : "Downloading \(version)…"
        logInfo("UpdateChecker: Starting DMG download from \(dmgURL)", category: "Updates")

        do {
            // Step 0: Backup current app before installing
            updateOutput = "Backing up current app…"
            backupCurrentApp()

            // Step 1: Download DMG to temp directory
            let tempDir = FileManager.default.temporaryDirectory
            let dmgPath = tempDir.appendingPathComponent("Diane-\(version).dmg")

            // Clean up any previous temp file
            try? FileManager.default.removeItem(at: dmgPath)

            let (_, _) = try await downloadWithProgress(from: dmgURL, to: dmgPath)
            updateOutput = shouldAutoUpdate ? "Download complete. Installing…" : "Download complete. Installing…"
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
# Fix permissions
chmod -R a=u+rX "\(appPath)"
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
            shouldAutoUpdate = false

            // Report update failure
            reportError(
                title: "Auto-update failed",
                body: "Failed to download/install Diane update.\n\nError: \(error.localizedDescription)\nVersion: \(releaseData?.tagName ?? "?")",
                severity: "medium",
                category: "Updates",
                labels: "update"
            )

            // Never open browser — log and let the next poll cycle retry
            logError("UpdateChecker: Auto-update failed for \(releaseData?.tagName ?? "?"): \(error.localizedDescription)", category: "Updates")
            shouldAutoUpdate = false
        }
    }

    // MARK: - Backup & Rollback

    /// Backup the current app bundle before installing an update.
    /// Stores in ~/.diane/backups/ instead of /Applications/ to avoid cluttering
    /// the Applications folder with stale .backup.app bundles.
    private func backupCurrentApp() {
        let appPath = "/Applications/Diane.app"
        guard let version = currentVersion, version != "unknown" else { return }

        let backupPath = appBackupPath(version: version)
        let fm = FileManager.default

        // Create backups directory
        try? fm.createDirectory(at: backupPath.deletingLastPathComponent(),
                                withIntermediateDirectories: true)

        // Remove old backup if exists
        try? fm.removeItem(at: backupPath)

        do {
            try fm.copyItem(atPath: appPath, toPath: backupPath.path)
            previousVersion = version
            rollbackAvailable = true
            logInfo("UpdateChecker: Backed up \(appPath) → \(backupPath.path)", category: "Updates")
        } catch {
            logError("UpdateChecker: Backup failed: \(error.localizedDescription)", category: "Updates")
        }
    }

    /// Check if a backup exists for rollback.
    private func checkRollbackAvailability() {
        let fm = FileManager.default
        let backupDir = backupDirURL()

        // Check all backups in ~/.diane/backups/
        guard let backups = try? fm.contentsOfDirectory(at: backupDir,
                                                         includingPropertiesForKeys: nil) else {
            rollbackAvailable = false
            return
        }

        for backup in backups {
            if backup.lastPathComponent.hasPrefix("Diane.v") && backup.lastPathComponent.hasSuffix(".backup.app") {
                let ver = backup.lastPathComponent
                    .replacingOccurrences(of: "Diane.v", with: "")
                    .replacingOccurrences(of: ".backup.app", with: "")
                previousVersion = ver
                rollbackAvailable = true
                logInfo("UpdateChecker: Rollback available: \(ver)", category: "Updates")
                return
            }
        }
        rollbackAvailable = false
    }

    /// Clean up stale backup(s) in case the same version or older exist.
    /// Called after a successful update or rollback.
    private func cleanupBackup(version: String? = nil) {
        let fm = FileManager.default
        if let ver = version {
            let path = appBackupPath(version: ver)
            try? fm.removeItem(at: path)
            logInfo("UpdateChecker: Cleaned up backup for \(ver)", category: "Updates")
        }
    }

    private func backupDirURL() -> URL {
        return URL(fileURLWithPath: dianeDir).appendingPathComponent("backups")
    }

    /// Remove any Diane.v*.backup.app left in /Applications/ from old versions.
    private func cleanupStaleApplicationsBackups() {
        let fm = FileManager.default
        guard let apps = try? fm.contentsOfDirectory(atPath: "/Applications") else { return }
        for app in apps where app.hasPrefix("Diane.v") && app.hasSuffix(".backup.app") {
            let path = "/Applications/\(app)"
            try? fm.removeItem(atPath: path)
            logInfo("UpdateChecker: Cleaned up stale backup from /Applications/: \(app)", category: "Updates")
        }
    }

    private func appBackupPath(version: String) -> URL {
        let cleaned = version.hasPrefix("v") ? String(version.dropFirst()) : version
        return backupDirURL().appendingPathComponent("Diane.v\(cleaned).backup.app")
    }

    /// Download a file and report progress
    private func downloadWithProgress(from url: URL, to destination: URL) async throws -> (URL, URLResponse) {
        let session = URLSession(configuration: .default)
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
