import SwiftUI
import DianeShared
import Sentry

@main
struct DianeCompanionApp: App {
    @State private var config = ServerConfiguration()
    @State private var apiClient = RemoteDianeAPIClient()
    @State private var cloudClient = EmergentAPIClient()
    @State private var showConfigSheet = false

    @Environment(\.scenePhase) private var scenePhase

    init() {
        SentrySDK.start { options in
            options.dsn = "https://d18f08c868e65e24ce766257453eccd6@o4511344463839232.ingest.de.sentry.io/4511344490446928"
            options.debug = false
            options.sendDefaultPii = true
            options.tracesSampleRate = 1.0
        }
        #if DEBUG
        print("[Diane] Sentry SDK initialized")
        #endif
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environment(\.config, config)
                .environment(\.apiClient, apiClient)
                .environment(\.cloudClient, cloudClient)
                .task { await startup() }
                .sheet(isPresented: $showConfigSheet) {
                    SettingsView()
                        .environment(\.config, config)
                        .environment(\.apiClient, apiClient)
                }
                .onChange(of: scenePhase) { _, newPhase in
                    handleScenePhase(newPhase)
                }
        }
    }

    private func startup() async {
        // 0. Start network monitoring
        NetworkMonitor.shared.start()

        // 1. Configure clients
        apiClient.configure(baseURL: config.serverURL, apiKey: config.apiKey)
        cloudClient.configure(baseURL: MemoryPlatform.defaultURL, apiKey: config.apiKey)

        // 2. If no API key, show config sheet
        if !config.isConfigured && !CommandLine.arguments.contains("UITesting") {
            showConfigSheet = true
            return
        }

        // 3. Register as phone node
        try? await NodeRegistrationService.shared.register(apiClient: cloudClient)

        // 4. Register for push notifications
        PushNotificationService.shared.register()

        // 5. Clear badge on launch
        BadgeManager.shared.clearBadge()
    }

    private func handleScenePhase(_ newPhase: ScenePhase) {
        switch newPhase {
        case .active:
            BadgeManager.shared.clearBadge()
            // Send heartbeat on foreground
            Task {
                try? await NodeRegistrationService.shared.sendHeartbeat(apiClient: cloudClient)
            }
        case .background:
            Task {
                // Send final heartbeat with background status
                guard NodeRegistrationService.shared.status == .registered else { return }
                try? await sendBackgroundHeartbeat()
            }
        case .inactive:
            break
        @unknown default:
            break
        }
    }

    private func sendBackgroundHeartbeat() async throws {
        let body = try JSONSerialization.data(withJSONObject: ["status": "background"])
        _ = try await cloudClient.http.put(
            "/api/nodes/\(NodeRegistrationService.shared.instanceID)/heartbeat",
            body: body
        )
    }
}

// MARK: - Environment Keys

struct ConfigKey: EnvironmentKey {
    static let defaultValue = ServerConfiguration()
}

struct APIClientKey: EnvironmentKey {
    static let defaultValue = RemoteDianeAPIClient()
}

struct CloudClientKey: EnvironmentKey {
    static let defaultValue = EmergentAPIClient()
}

extension EnvironmentValues {
    var config: ServerConfiguration {
        get { self[ConfigKey.self] }
        set { self[ConfigKey.self] = newValue }
    }
    var apiClient: RemoteDianeAPIClient {
        get { self[APIClientKey.self] }
        set { self[APIClientKey.self] = newValue }
    }
    var cloudClient: EmergentAPIClient {
        get { self[CloudClientKey.self] }
        set { self[CloudClientKey.self] = newValue }
    }
}

// MARK: - Memory Platform Defaults

enum MemoryPlatform {
    static let defaultURL = "https://memory.emergent-company.ai"
}
