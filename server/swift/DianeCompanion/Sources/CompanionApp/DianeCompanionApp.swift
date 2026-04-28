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
    @StateObject private var apiClient      = EmergentAPIClient()
    @State private var hasStarted           = false

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
        // Main application window (task 5.x)
        Window("Diane", id: "main") {
            MainWindowView()
                .environmentObject(appState)
                .environmentObject(apiClient)
                .environmentObject(statusMonitor)
                .environmentObject(serverConfig)
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

        // Dedicated settings window opened via openWindow(id: "settings").
        Window("Diane Settings", id: "settings") {
            SettingsView()
                .environmentObject(statusMonitor)
                .environmentObject(serverConfig)
                .environmentObject(apiClient)
        }
        .windowResizability(.contentSize)
        .defaultPosition(.center)

        // Document content viewer — opened via openWindow(id: "document-content").
        // The document to display is stored on AppState.contentViewDocument.
        Window("Document Content", id: "document-content") {
            DocumentContentView()
                .environmentObject(appState)
                .environmentObject(apiClient)
        }
        .windowStyle(.titleBar)
        .windowToolbarStyle(.unified)
        .defaultSize(width: 1000, height: 680)
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

        await updateChecker.start()
    }
}
