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

    func configure(apiClient: DianeAPIClient) {
        self.apiClient = apiClient
    }

    /// Ensure the local API is running. Starts it if not already reachable.
    func ensureRunning(dianeAPI: DianeAPIClient) async {
        // First check if it's already running
        if await dianeAPI.checkReachability() {
            logger.info("Local Diane API already running.")
            isRunning = true
            return
        }

        guard let bundledURL = Bundle.main.url(forResource: "diane", withExtension: nil) else {
            let msg = "No bundled diane binary found in app bundle"
            logger.warning("\(msg)")
            lastError = msg
            return
        }

        logger.info("Starting diane serve --api-port 8890 from \(bundledURL.path)")

        let proc = Process()
        proc.executableURL = bundledURL
        proc.arguments = ["serve", "--api-port", "8890"]
        proc.standardOutput = Pipe()
        proc.standardError = Pipe()

        // Set up termination handler for auto-restart
        proc.terminationHandler = { [weak self] _ in
            Task { @MainActor in
                self?.logger.warning("diane serve process terminated — restarting in 3s")
                self?.isRunning = false
                try? await Task.sleep(nanoseconds: 3_000_000_000)
                if let client = self?.apiClient {
                    await self?.ensureRunning(dianeAPI: client)
                }
            }
        }

        do {
            try proc.run()
            self.process = proc
            self.isRunning = true
            self.lastError = nil
            // Wait a moment for it to start
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            let reachable = await dianeAPI.checkReachability()
            logger.info("Local API reachable after start: \(reachable)")
            if !reachable {
                logger.warning("diane serve started but API not yet responding")
            }
        } catch {
            let msg = "Failed to start diane serve: \(error.localizedDescription)"
            logger.error("\(msg)")
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
