import SwiftUI
import OSLog

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
    @StateObject private var apiServer      = APIServerManager()
    @State private var hasStarted           = false

    init() {
        AppLogger.shared.info("Diane Companion app launching", category: "App")
        // Log environment info for crash diagnostics
        let sysInfo = ProcessInfo.processInfo
        AppLogger.shared.debug("macOS \(sysInfo.operatingSystemVersionString), \(sysInfo.processName) v\(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?")", category: "App")
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

        AppLogger.shared.info("App startup sequence beginning", category: "App")

        updateChecker.statusMonitor = statusMonitor
        updateChecker.cliManager = cliManager
        statusMonitor.configure(from: serverConfig)

        // Configuration
        AppLogger.shared.info("Server URL: \(serverConfig.serverURL)", category: "App")
        AppLogger.shared.debug("API key set: \(!serverConfig.apiKey.isEmpty)", category: "App")

        // Configure the API client from persisted server settings
        apiClient.configure(serverURL: serverConfig.serverURL, apiKey: serverConfig.apiKey)

        // Configure the API server manager and ensure local diane serve is running
        apiServer.configure(apiClient: dianeAPI)
        AppLogger.shared.info("Ensuring local diane serve is running", category: "App")
        await apiServer.ensureRunning(dianeAPI: dianeAPI)

        // Check reachability after trying to start
        let reachable = await dianeAPI.checkReachability()
        AppLogger.shared.info("Local Diane API reachable: \(reachable)", category: "App")
        if !reachable {
            AppLogger.shared.warning("Local API not reachable — will use remote fallback", category: "App")
        }

        await updateChecker.start()
        AppLogger.shared.info("App startup complete", category: "App")
    }
}
