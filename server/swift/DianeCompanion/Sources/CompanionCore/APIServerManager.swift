import Foundation
import OSLog

/// Manages the `diane serve --api-port 8890` process lifecycle.
/// Starts it on launch, monitors it, and auto-restarts on crash.
/// Runs as a child of the companion app for TCC visibility.
@MainActor
final class APIServerManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "APIServer")

    @Published private(set) var isRunning = false
    @Published private(set) var lastError: String?

    private var process: Process?
    private var apiClient: DianeAPIClient?

    // Circuit breaker state
    private var restartCount = 0
    private var firstRestartTime: Date? = nil
    private static let maxRestarts = 3
    private static let circuitBreakerWindow: TimeInterval = 60

    func configure(apiClient: DianeAPIClient) {
        self.apiClient = apiClient
    }

    /// Ensure the local API is running. Starts it if not already reachable.
    func ensureRunning(dianeAPI: DianeAPIClient) async {
        // First check if it's already running
        if await dianeAPI.checkReachability() {
            AppLogger.shared.info("Local Diane API already running", category: "APIServer")
            isRunning = true
            return
        }

        guard let bundledURL = Bundle.main.url(forResource: "diane", withExtension: nil) else {
            let msg = "No bundled diane binary found in app bundle"
            AppLogger.shared.error(msg, category: "APIServer")
            lastError = msg
            return
        }

        AppLogger.shared.info("Starting diane serve --api-port 8890 from \(bundledURL.path)", category: "APIServer")

        let proc = Process()
        proc.executableURL = bundledURL

        // Check if the binary is actually executable
        if !FileManager.default.isExecutableFile(atPath: bundledURL.path) {
            let msg = "Bundled diane binary is not executable: \(bundledURL.path)"
            AppLogger.shared.error(msg, category: "APIServer")
            lastError = msg
            return
        }

        proc.arguments = ["serve", "--api-port", "8890"]

        // Capture stdout and stderr for diagnostics
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        proc.standardOutput = stdoutPipe
        proc.standardError = stderrPipe

        // Read stderr asynchronously — this captures crash output
        let stderrHandle = stderrPipe.fileHandleForReading
        stderrHandle.readabilityHandler = { handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            if let output = String(data: data, encoding: .utf8), !output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                AppLogger.shared.error("diane serve stderr: \(output.trimmingCharacters(in: .whitespacesAndNewlines))", category: "APIServer")
            }
        }

        // Read stdout asynchronously for startup info
        let stdoutHandle = stdoutPipe.fileHandleForReading
        stdoutHandle.readabilityHandler = { handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            if let output = String(data: data, encoding: .utf8), !output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                AppLogger.shared.debug("diane serve stdout: \(output.trimmingCharacters(in: .whitespacesAndNewlines))", category: "APIServer")
            }
        }

        // Set up termination handler with circuit breaker for auto-restart
        proc.terminationHandler = { [weak self] proc in
            // Clean up pipe handlers
            stdoutHandle.readabilityHandler = nil
            stderrHandle.readabilityHandler = nil

            // Capture remaining buffered output
            let remainingStderr = try? stderrHandle.readToEnd()
            if let data = remainingStderr, let output = String(data: data, encoding: .utf8), !output.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                AppLogger.shared.error("diane serve final stderr: \(output.trimmingCharacters(in: .whitespacesAndNewlines))", category: "APIServer")
            }

            let exitCode = proc.terminationStatus
            AppLogger.shared.warning("diane serve exited (code \(exitCode))", category: "APIServer")

            Task { @MainActor in
                guard let self else { return }

                // Circuit breaker: check restart rate within window
                let now = Date()
                if let first = self.firstRestartTime {
                    if now.timeIntervalSince(first) > Self.circuitBreakerWindow {
                        // Window expired — reset counter
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
            // Wait a moment for it to start
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            let reachable = await dianeAPI.checkReachability()
            AppLogger.shared.info("Local API reachable after start: \(reachable)", category: "APIServer")
            if !reachable {
                AppLogger.shared.warning("diane serve started but API not yet responding", category: "APIServer")
            }
        } catch {
            let msg = "Failed to start diane serve: \(error.localizedDescription)"
            AppLogger.shared.error(msg, category: "APIServer")
            lastError = msg
            isRunning = false
        }
    }

    /// Stop the diane serve process
    func stop() {
        guard let proc = process, proc.isRunning else { return }
        logger.info("Stopping diane serve process")
        proc.terminate()
        process = nil
        isRunning = false
    }

    deinit {
        process?.terminate()
    }
}
