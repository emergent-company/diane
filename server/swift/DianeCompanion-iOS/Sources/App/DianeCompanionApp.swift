import SwiftUI
import DianeShared
import Sentry

@main
struct DianeCompanionApp: App {
    @State private var config = ServerConfiguration()
    @State private var cloudClient = EmergentAPIClient()
    @State private var showConfigSheet = false

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
                .environment(\.cloudClient, cloudClient)
                .task { await startup() }
                .sheet(isPresented: $showConfigSheet) {
                    SettingsView()
                        .environment(\.config, config)
                        .environment(\.cloudClient, cloudClient)
                }
        }
    }

    private func startup() async {
        // 0. Start network monitoring
        NetworkMonitor.shared.start()

        // 1. Connect to Memory Platform
        cloudClient.configure(
            baseURL: MemoryPlatform.defaultURL,
            apiKey: config.apiKey,
            projectID: config.projectID
        )

        // 2. If no API key, show config sheet
        if !config.isConfigured && !CommandLine.arguments.contains("UITesting") {
            showConfigSheet = true
            return
        }
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
