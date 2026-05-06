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
        logger.debug("UpdateChecker: Starting checkForUpdates")
        isChecking = true
        defer { isChecking = false }

        guard let url = URL(string: "https://api.github.com/repos/\(repoOwner)/\(repoName)/releases/latest") else { return }

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
            latestVersion = release.tagName
            if let htmlUrl = URL(string: release.htmlUrl) {
                self.downloadUrl = htmlUrl
            }

            let installed = currentVersion ?? "0.0.0"
            
            if installed == "unknown" || installed == "dev" {
                updateAvailable = true
                logger.info("UpdateChecker: Update available (installed version \(installed) is dev/unknown).")
            } else {
                updateAvailable = isOlderVersion(installed, than: release.tagName)
                if updateAvailable {
                    logger.info("UpdateChecker: Update available: \(installed) -> \(release.tagName).")
                } else {
                    logger.info("UpdateChecker: No update available. Current version: \(installed).")
                }
            }
        } catch {
            // Silently fail
            logger.debug("UpdateChecker: checkForUpdates failed: \(error.localizedDescription)")
        }
        logger.debug("UpdateChecker: Finished checkForUpdates")
    }

    /// Run `diane upgrade` via the bundled CLI to perform a real upgrade.
    /// The CLI handles binary download, DMG install, app termination, and relaunch.
    func performUpdate() {
        guard let bundledCLI = Bundle.main.url(forResource: "diane", withExtension: nil) else {
            // Fallback: open releases page
            if let url = downloadUrl {
                NSWorkspace.shared.open(url)
            }
            return
        }

        isUpdating = true
        updateOutput += "⬆️  Starting upgrade via bundled CLI...\n"

        let process = Process()
        process.executableURL = bundledCLI
        process.arguments = ["upgrade"]

        // Pipe "y" to stdin so the companion app prompt is auto-confirmed
        let stdinPipe = Pipe()
        process.standardInput = stdinPipe
        let stdoutPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = Pipe()

        // Detach: we fire-and-forget because `diane upgrade` will pkill Diane
        // and launch a detached install script. The calling process may be killed.
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            do {
                try process.run()
                // Auto-confirm the "Update companion app now? [Y/n]" prompt
                stdinPipe.fileHandleForWriting.write("y\n".data(using: .utf8)!)
                stdinPipe.fileHandleForWriting.closeFile()

                process.waitUntilExit()

                let data = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
                if let output = String(data: data, encoding: .utf8) {
                    DispatchQueue.main.async {
                        self?.updateOutput += output
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    self?.updateOutput += "❌ Upgrade failed: \(error.localizedDescription)\n"
                }
            }
        }
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

// MARK: - GitHub API models

private struct GitHubRelease: Decodable {
    let tagName: String
    let htmlUrl: String

    enum CodingKeys: String, CodingKey {
        case tagName = "tag_name"
        case htmlUrl = "html_url"
    }
}
