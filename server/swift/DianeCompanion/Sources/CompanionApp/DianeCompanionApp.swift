import SwiftUI
import OSLog
import AppKit

@main
struct DianeCompanionApp: App {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "App")

    @StateObject private var statusMonitor  = StatusMonitor()
    @StateObject private var updateChecker  = UpdateChecker()
    @StateObject private var serverConfig   = ServerConfiguration()
    @StateObject private var cliManager     = CLIManager()
    @StateObject private var appState       = AppState()
    @StateObject private var dianeAPI       = DianeAPIClient()
    @StateObject private var apiClient      = EmergentAPIClient()
    @State private var hasStarted           = false
    @State private var dianeProcess: Process?

    init() {
        logger.info("Diane is launching.")
    }

    private var menuBarIconName: String {
        switch statusMonitor.connectionState {
        case .unknown:      return "brain"
        case .connected:    return "brain.head.profile"
        case .disconnected: return "brain"
        case .error:        return "brain.head.profile.fill"
        }
    }

    var body: some Scene {
        // Main application window
        Window("Diane", id: "main") {
            MainWindowView()
                .environmentObject(appState)
                .environmentObject(apiClient)
                .environmentObject(statusMonitor)
                .environmentObject(serverConfig)
                .environmentObject(dianeAPI)
                .task { await startIfNeeded() }
        }
        .windowStyle(.titleBar)
        .windowToolbarStyle(.unified)
        .defaultSize(width: 1100, height: 700)
        .defaultPosition(.center)

        MenuBarExtra {
            MenuBarView()
                .environmentObject(statusMonitor)
                .environmentObject(updateChecker)
                .environmentObject(serverConfig)
                .environmentObject(cliManager)
                .environmentObject(appState)
                .environmentObject(apiClient)
                .task { await startIfNeeded() }
        } label: {
            Image(systemName: menuBarIconName)
                .symbolRenderingMode(.hierarchical)
        }
        .menuBarExtraStyle(.window)

        // Dedicated settings window
        Window("Diane Settings", id: "settings") {
            SettingsView()
                .environmentObject(statusMonitor)
                .environmentObject(serverConfig)
                .environmentObject(apiClient)
        }
        .windowResizability(.contentSize)
        .defaultPosition(.center)
    }

    @MainActor
    private func startIfNeeded() async {
        guard !hasStarted else { return }
        hasStarted = true

        updateChecker.statusMonitor = statusMonitor
        updateChecker.cliManager = cliManager
        statusMonitor.configure(from: serverConfig)

        // Configure the API client from persisted server settings
        apiClient.configure(serverURL: serverConfig.serverURL, apiKey: serverConfig.apiKey)

        // Ensure local diane serve is running with API port
        await ensureLocalAPIRunning()

        // Check if the local Diane API is reachable
        let reachable = await dianeAPI.checkReachability()
        logger.info("Local Diane API reachable: \(reachable)")
        if !reachable {
            logger.info("Local API not reachable — will use remote fallback")
        }

        await updateChecker.start()
    }

    /// Ensure `diane serve --api-port 8890` is running using the bundled binary.
    /// Starts it as a child process of the companion app (TCC visibility).
    @MainActor
    private func ensureLocalAPIRunning() async {
        // First check if it's already running
        if await dianeAPI.checkReachability() {
            logger.info("Local Diane API already running.")
            return
        }

        guard let bundledURL = Bundle.main.url(forResource: "diane", withExtension: nil) else {
            logger.warning("No bundled diane binary found — cannot start local API.")
            return
        }

        logger.info("Starting diane serve --api-port 8890 from \(bundledURL.path)")
        
        let process = Process()
        process.executableURL = bundledURL
        process.arguments = ["serve", "--api-port", "8890"]
        process.standardOutput = Pipe()
        process.standardError = Pipe()
        
        // Set up termination handler for auto-restart
        process.terminationHandler = { [weak self] proc in
            Task { @MainActor in
                self?.logger.warning("diane serve process terminated (reason: \(proc.terminationReason.rawValue)) — restarting in 3s")
                try? await Task.sleep(nanoseconds: 3_000_000_000)
                await self?.ensureLocalAPIRunning()
            }
        }

        do {
            try process.run()
            self.dianeProcess = process
            // Wait a moment for it to start
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            let reachable = await dianeAPI.checkReachability()
            logger.info("Local API reachable after start: \(reachable)")
        } catch {
            logger.error("Failed to start diane serve: \(error.localizedDescription)")
        }
    }
}
