import Foundation

/// Manages the `diane serve --api-port 8890` process lifecycle.
///
/// Startup strategy (in order):
///   1. Check if already reachable (fast path)
///   2. Try launchd — install plist, bootstrap, wait for response
///   3. Fall back to direct child process management
///
/// On subsequent launches, launchd auto-starts serve at login
/// and survives SSH disconnects, reboots, and crashes.
@MainActor
final class APIServerManager: ObservableObject {

    @Published private(set) var isRunning = false
    @Published private(set) var lastError: String?

    private var process: Process?
    private var apiClient: DianeAPIClient?
    private nonisolated(unsafe) var healthCheckTimer: Timer?
    private weak var healthCheckClient: DianeAPIClient?

    /// Whether launchd is managing the serve process (preferred path)
    private var usingLaunchd = false

    // Circuit breaker state (for direct process fallback only)
    private var restartCount = 0
    private var firstRestartTime: Date? = nil
    private static let maxRestarts = 3
    private static let circuitBreakerWindow: TimeInterval = 60

    private static let plistLabel = "com.emergent-company.diane-serve"

    /// Old plist labels from previous versions that used a separate relay process.
    /// These conflict with the current in-process relay architecture.
    private static let oldRelayPlists = [
        "com.emergent.diane.slave",
        "com.diane.relay",
        "ai.diane.relay",
    ]

    /// Whether old-plist cleanup has been performed this session
    private static var hasCleanedUpOldPlists = false

    func configure(apiClient: DianeAPIClient) {
        self.apiClient = apiClient
    }

    // MARK: - Public API

    /// Ensure the local API is running. Starts it if not already reachable.
    func ensureRunning(dianeAPI: DianeAPIClient) async {
        // Clean up orphaned launchd plists from previous Diane versions
        Self.cleanupOldLaunchdPlists()

        // Fast path: already reachable
        if await dianeAPI.checkReachability() {
            AppLogger.shared.info("Local Diane API already running", category: "APIServer")
            isRunning = true
            lastError = nil
            scheduleHealthCheck(dianeAPI: dianeAPI)
            return
        }

        // Try launchd first — install plist + bootstrap if needed
        if await tryLaunchd(dianeAPI: dianeAPI) {
            AppLogger.shared.info("diane serve started via launchd", category: "APIServer")
            isRunning = true
            lastError = nil
            usingLaunchd = true
            scheduleHealthCheck(dianeAPI: dianeAPI)
            return
        }

        AppLogger.shared.warning("launchd not available — falling back to direct process management", category: "APIServer")
        await startDirectProcess(dianeAPI: dianeAPI)
    }

    /// Stop diane serve — uses launchd bootout if plist is loaded, otherwise terminates child process.
    func stop() {
        healthCheckTimer?.invalidate()
        healthCheckTimer = nil

        if usingLaunchd {
            stopLaunchd()
        } else if let proc = process, proc.isRunning {
            logInfo("Stopping diane serve process (PID \(proc.processIdentifier))", category: "APIServer")
            proc.terminate()
        }
        process = nil
        isRunning = false
        usingLaunchd = false
    }

    deinit {
        healthCheckTimer?.invalidate()
        process?.terminate()
    }

    // MARK: - Launchd Strategy

    /// Try to manage diane serve via launchd. Installs plist + bootstraps if needed.
    /// Returns true if serve becomes reachable after launchd setup.
    private func tryLaunchd(dianeAPI: DianeAPIClient) async -> Bool {
        let plistPath = launchdPlistPath()

        // If the plist is already bootstrapped, just kickstart the service
        if isLaunchdLoaded() {
            AppLogger.shared.info("launchd plist already loaded — kickstarting", category: "APIServer")
            kickstartLaunchd()
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            return await dianeAPI.checkReachability()
        }

        // Find the binary path for the plist
        guard let binaryPath = findDianeBinary()?.path else {
            AppLogger.shared.warning("No diane binary found — cannot install launchd plist", category: "APIServer")
            return false
        }

        // Install/update the plist
        guard installLaunchdPlist(binaryPath: binaryPath) else {
            AppLogger.shared.warning("Failed to install launchd plist", category: "APIServer")
            return false
        }

        // Bootstrap the service
        guard bootstrapLaunchd(plistPath: plistPath) else {
            AppLogger.shared.warning("Failed to bootstrap launchd service", category: "APIServer")
            return false
        }

        AppLogger.shared.info("launchd service bootstrapped — waiting for serve to respond", category: "APIServer")
        try? await Task.sleep(nanoseconds: 3_000_000_000)
        return await dianeAPI.checkReachability()
    }

    /// Path to the user-level launchd plist
    private func launchdPlistPath() -> String {
        return NSHomeDirectory() + "/Library/LaunchAgents/" + Self.plistLabel + ".plist"
    }

    /// Check if the launchd service is currently loaded
    private func isLaunchdLoaded() -> Bool {
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        proc.arguments = ["print", "gui/\(getuid())/\(Self.plistLabel)"]
        let pipe = Pipe()
        proc.standardOutput = pipe
        proc.standardError = pipe
        do {
            try proc.run()
            proc.waitUntilExit()
            return proc.terminationStatus == 0
        } catch {
            return false
        }
    }

    /// Generate and install the launchd plist with correct paths for this machine
    private func installLaunchdPlist(binaryPath: String) -> Bool {
        let home = NSHomeDirectory()
        let plistContent = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
            <key>Label</key>
            <string>\(Self.plistLabel)</string>
            <key>ProgramArguments</key>
            <array>
                <string>\(binaryPath)</string>
                <string>serve</string>
                <string>--api-port</string>
                <string>8890</string>
            </array>
            <key>RunAtLoad</key>
            <true/>
            <key>KeepAlive</key>
            <true/>
            <key>WatchPath</key>
            <array>
                <string>\(binaryPath)</string>
            </array>
            <key>StandardOutPath</key>
            <string>\(home)/Library/Logs/diane-serve.log</string>
            <key>StandardErrorPath</key>
            <string>\(home)/Library/Logs/diane-serve.log</string>
            <key>EnvironmentVariables</key>
            <dict>
                <key>PATH</key>
                <string>/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
            </dict>
            <key>WorkingDirectory</key>
            <string>\(home)</string>
            <key>ProcessType</key>
            <string>Background</string>
        </dict>
        </plist>
        """

        let launchAgentsDir = home + "/Library/LaunchAgents"
        try? FileManager.default.createDirectory(atPath: launchAgentsDir, withIntermediateDirectories: true)

        let plistURL = URL(fileURLWithPath: launchdPlistPath())
        do {
            try plistContent.write(to: plistURL, atomically: true, encoding: .utf8)
            AppLogger.shared.info("Installed launchd plist: \(plistURL.path)", category: "APIServer")
            return true
        } catch {
            AppLogger.shared.error("Failed to write launchd plist: \(error.localizedDescription)", category: "APIServer")
            return false
        }
    }

    /// Bootstrap (load) the launchd service from the plist
    private func bootstrapLaunchd(plistPath: String) -> Bool {
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        proc.arguments = ["bootstrap", "gui/\(getuid())", plistPath]
        let pipe = Pipe()
        proc.standardOutput = pipe
        proc.standardError = pipe
        do {
            try proc.run()
            proc.waitUntilExit()
            if proc.terminationStatus != 0 {
                let data = try? pipe.fileHandleForReading.readToEnd()
                let msg = data.flatMap { String(data: $0, encoding: .utf8) } ?? ""
                AppLogger.shared.warning("launchctl bootstrap failed (exit \(proc.terminationStatus)): \(msg)", category: "APIServer")
            }
            return proc.terminationStatus == 0
        } catch {
            AppLogger.shared.error("Failed to run launchctl bootstrap: \(error.localizedDescription)", category: "APIServer")
            return false
        }
    }

    /// Kickstart the launchd service (restart if already loaded)
    private func kickstartLaunchd() {
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        proc.arguments = ["kickstart", "-kp", "gui/\(getuid())/\(Self.plistLabel)"]
        do {
            try proc.run()
            proc.waitUntilExit()
        } catch {
            AppLogger.shared.warning("Failed to kickstart launchd service: \(error.localizedDescription)", category: "APIServer")
        }
    }

    /// Bootout (unload) the launchd service
    private func stopLaunchd() {
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        proc.arguments = ["bootout", "gui/\(getuid())/\(Self.plistLabel)"]
        do {
            try proc.run()
            proc.waitUntilExit()
            logInfo("launchd service booted out", category: "APIServer")
        } catch {
            logWarning("Failed to bootout launchd service: \(error.localizedDescription)", category: "APIServer")
        }
    }

    // MARK: - Old Plist Cleanup

    /// Boot out and delete old relay plists from prior Diane versions.
    /// Runs once per app launch to prevent duplicate relay processes.
    private static func cleanupOldLaunchdPlists() {
        guard !hasCleanedUpOldPlists else { return }
        hasCleanedUpOldPlists = true

        let uid = getuid()
        let fileManager = FileManager.default
        let launchAgentsDir = fileManager.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents")

        for label in oldRelayPlists {
            // Boot out from launchd (ignore errors — service may not be loaded)
            let bootout = Process()
            bootout.executableURL = URL(fileURLWithPath: "/bin/launchctl")
            bootout.arguments = ["bootout", "gui/\(uid)/\(label)"]
            let nullPipe = Pipe()
            bootout.standardOutput = nullPipe
            bootout.standardError = nullPipe
            do {
                try bootout.run()
                bootout.waitUntilExit()
                if bootout.terminationStatus == 0 {
                    AppLogger.shared.info("Booted out old launchd service: \(label)", category: "APIServer")
                }
            } catch {
                AppLogger.shared.debug("launchctl bootout for \(label) failed (expected if not loaded): \(error.localizedDescription)", category: "APIServer")
            }

            // Delete the plist file if it still exists
            let plistURL = launchAgentsDir.appendingPathComponent("\(label).plist")
            if fileManager.fileExists(atPath: plistURL.path) {
                do {
                    try fileManager.removeItem(at: plistURL)
                    AppLogger.shared.info("Deleted old launchd plist: \(plistURL.lastPathComponent)", category: "APIServer")
                } catch {
                    AppLogger.shared.warning("Failed to delete old plist \(plistURL.lastPathComponent): \(error.localizedDescription)", category: "APIServer")
                }
            }
        }
    }

    // MARK: - Direct Process Fallback

    /// Start diane serve as a direct child process (fallback when launchd is unavailable).
    private func startDirectProcess(dianeAPI: DianeAPIClient) async {
        guard let dianeURL = findDianeBinary() else {
            lastError = "No diane binary found in app bundle, ~/.diane/bin/, or PATH"
            AppLogger.shared.error(lastError!, category: "APIServer")
            return
        }

        AppLogger.shared.info("Starting diane serve --api-port 8890 from \(dianeURL.path)", category: "APIServer")

        let proc = Process()
        proc.executableURL = dianeURL

        if !FileManager.default.isExecutableFile(atPath: dianeURL.path) {
            let msg = "Diane binary is not executable: \(dianeURL.path)"
            AppLogger.shared.error(msg, category: "APIServer")
            lastError = msg
            return
        }

        proc.arguments = ["serve", "--api-port", "8890"]

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        proc.standardOutput = stdoutPipe
        proc.standardError = stderrPipe

        let stderrHandle = stderrPipe.fileHandleForReading
        stderrHandle.readabilityHandler = { handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            if let output = String(data: data, encoding: .utf8), !output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                AppLogger.shared.error("diane serve stderr: \(output.trimmingCharacters(in: .whitespacesAndNewlines))", category: "APIServer")
            }
        }

        let stdoutHandle = stdoutPipe.fileHandleForReading
        stdoutHandle.readabilityHandler = { handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            if let output = String(data: data, encoding: .utf8), !output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                AppLogger.shared.debug("diane serve stdout: \(output.trimmingCharacters(in: .whitespacesAndNewlines))", category: "APIServer")
            }
        }

        proc.terminationHandler = { [weak self] proc in
            stdoutHandle.readabilityHandler = nil
            stderrHandle.readabilityHandler = nil

            let remainingStderr = try? stderrHandle.readToEnd()
            if let data = remainingStderr, let output = String(data: data, encoding: .utf8), !output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                AppLogger.shared.error("diane serve final stderr: \(output.trimmingCharacters(in: .whitespacesAndNewlines))", category: "APIServer")
            }

            let exitCode = proc.terminationStatus
            AppLogger.shared.warning("diane serve exited (code \(exitCode))", category: "APIServer")

            Task { @MainActor in
                guard let self else { return }

                let now = Date()
                if let first = self.firstRestartTime {
                    if now.timeIntervalSince(first) > Self.circuitBreakerWindow {
                        self.restartCount = 0
                        self.firstRestartTime = nil
                    }
                }

                self.restartCount += 1
                if self.firstRestartTime == nil {
                    self.firstRestartTime = now
                }

                guard self.restartCount <= Self.maxRestarts else {
                    let msg = "diane serve terminated \(self.restartCount) times in \(Self.circuitBreakerWindow)s — stopping auto-restart (exit code \(exitCode))"
                    AppLogger.shared.error(msg, category: "APIServer")
                    self.lastError = msg
                    self.isRunning = false
                    return
                }

                AppLogger.shared.warning("diane serve process terminated (restart \(self.restartCount)/\(Self.maxRestarts), exit \(exitCode)) — restarting in 3s", category: "APIServer")
                self.isRunning = false
                try? await Task.sleep(nanoseconds: 3_000_000_000)
                if let client = self.apiClient {
                    await self.ensureRunning(dianeAPI: client)
                }
            }
        }

        do {
            try proc.run()
            self.process = proc
            self.isRunning = true
            self.lastError = nil
            AppLogger.shared.info("diane serve process started (PID \(proc.processIdentifier))", category: "APIServer")
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            let reachable = await dianeAPI.checkReachability()
            AppLogger.shared.info("Local API reachable after start: \(reachable)", category: "APIServer")
            if !reachable {
                AppLogger.shared.warning("diane serve started but API not yet responding", category: "APIServer")
            }
            scheduleHealthCheck(dianeAPI: dianeAPI)
        } catch {
            let msg = "Failed to start diane serve: \(error.localizedDescription)"
            AppLogger.shared.error(msg, category: "APIServer")
            lastError = msg
            isRunning = false
        }
    }

    // MARK: - Health Check

    /// Periodically check if diane serve is still reachable.
    /// Restarts via kickstart (launchd) or child respawn (fallback).
    private func scheduleHealthCheck(dianeAPI: DianeAPIClient) {
        healthCheckTimer?.invalidate()
        healthCheckClient = dianeAPI
        healthCheckTimer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            Task { @MainActor in
                guard let client = self.healthCheckClient else { return }
                if !(await client.checkReachability()) {
                    AppLogger.shared.warning("Health check: local API unreachable — restarting", category: "APIServer")
                    self.isRunning = false
                    self.process = nil
                    if self.usingLaunchd {
                        // kickstart tells launchd to restart the service
                        self.kickstartLaunchd()
                        try? await Task.sleep(nanoseconds: 3_000_000_000)
                        if await client.checkReachability() {
                            self.isRunning = true
                            return
                        }
                    }
                    // Fall through to full restart (launchd or direct)
                    await self.ensureRunning(dianeAPI: client)
                }
            }
        }
    }

    // MARK: - Binary Discovery

    /// Find the diane binary by checking bundled path first, then fallback locations.
    /// Order: app bundle → ~/.diane/bin/diane → PATH discovery
    private func findDianeBinary() -> URL? {
        if let bundled = Bundle.main.url(forResource: "diane", withExtension: nil),
           FileManager.default.isExecutableFile(atPath: bundled.path) {
            AppLogger.shared.info("Found diane binary in app bundle", category: "APIServer")
            return bundled
        }

        let homeDiane = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".diane/bin/diane")
        if FileManager.default.isExecutableFile(atPath: homeDiane.path) {
            AppLogger.shared.info("Found diane binary at ~/.diane/bin/diane", category: "APIServer")
            return homeDiane
        }

        let whichProc = Process()
        whichProc.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        whichProc.arguments = ["which", "diane"]
        let whichPipe = Pipe()
        whichProc.standardOutput = whichPipe
        do {
            try whichProc.run()
            whichProc.waitUntilExit()
            if whichProc.terminationStatus == 0 {
                let data = whichPipe.fileHandleForReading.readDataToEndOfFile()
                if let path = String(data: data, encoding: .utf8)?
                    .trimmingCharacters(in: .whitespacesAndNewlines),
                   !path.isEmpty,
                   FileManager.default.isExecutableFile(atPath: path) {
                    AppLogger.shared.info("Found diane binary on PATH: \(path)", category: "APIServer")
                    return URL(fileURLWithPath: path)
                }
            }
        } catch {
            AppLogger.shared.warning("Failed to run 'which diane': \(error.localizedDescription)", category: "APIServer")
        }

        return nil
    }
}
