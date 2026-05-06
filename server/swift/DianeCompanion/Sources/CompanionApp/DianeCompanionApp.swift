import SwiftUI
import Sentry

@main
struct DianeCompanionApp: App {

    @StateObject private var statusMonitor  = StatusMonitor()
    @StateObject private var updateChecker  = UpdateChecker()
    @StateObject private var serverConfig   = ServerConfiguration()
    @StateObject private var cliManager     = CLIManager()
    @StateObject private var appState       = AppState()
    @StateObject private var dianeAPI       = DianeAPIClient()
    @StateObject private var apiClient      = EmergentAPIClient()
    @StateObject private var apiServer      = APIServerManager()
    @StateObject private var selfTestManager = SelfTestManager()
    @State private var hasStarted           = false

    init() {
        SentrySDK.start { options in
            options.dsn = "https://d18f08c868e65e24ce766257453eccd6@o4511344463839232.ingest.de.sentry.io/4511344490446928"
            options.debug = false
            options.sendDefaultPii = true
            options.tracesSampleRate = 1.0
        }

        // Attach a persistent device UUID to all Sentry events for session correlation.
        SentrySDK.configureScope { scope in
            let deviceIDKey = "com.diane.sentry_device_id"
            if let stored = UserDefaults.standard.string(forKey: deviceIDKey) {
                let user = User(userId: stored)
                user.email = "maciej@kucharz.net"
                user.username = "mcj"
                scope.setUser(user)
            } else {
                let uuid = UUID().uuidString
                UserDefaults.standard.set(uuid, forKey: deviceIDKey)
                let user = User(userId: uuid)
                user.email = "maciej@kucharz.net"
                user.username = "mcj"
                scope.setUser(user)
            }
        }

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
        // Main application window — shows onboarding when not configured,
        // or full sidebar + content when configured and connected.
        Window("Diane", id: "main") {
            MainWindowView()
                .sentryView("MainWindow")
                .environmentObject(appState)
                .environmentObject(apiClient)
                .environmentObject(statusMonitor)
                .environmentObject(serverConfig)
                .environmentObject(dianeAPI)
                .environmentObject(updateChecker)
                .environmentObject(selfTestManager)
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
                .environmentObject(selfTestManager)
                .task { await startIfNeeded() }
        } label: {
            Image(systemName: menuBarIconName)
                .symbolRenderingMode(.hierarchical)
        }
        .menuBarExtraStyle(.window)
    }

    @MainActor
    private func startIfNeeded() async {
        guard !hasStarted else { return }
        hasStarted = true

        AppLogger.shared.info("App startup sequence beginning", category: "App")

        // Send any crash reports from previous sessions
        ErrorReporter.shared.sendPendingReports()

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

        // Post-upgrade self-test: runs after upgrade detection
        if let version = updateChecker.currentVersion {
            await selfTestManager.checkPostUpgrade(installedVersion: version)
            await selfTestManager.runIfPending()
        }
    }
}
