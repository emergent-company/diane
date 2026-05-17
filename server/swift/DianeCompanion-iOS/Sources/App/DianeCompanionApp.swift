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
            options.dsn = "https://2d01248f19ea419d943c89778b6f5c55@o4511344463839232.ingest.de.sentry.io/4511404771049552"
            options.debug = false
            options.sendDefaultPii = true
            options.tracesSampleRate = 0.2
            options.environment = Self.sentryEnvironment
            options.enabled = true
        }
        #if DEBUG
        print("[Diane] Sentry SDK initialized (env=\(Self.sentryEnvironment))")
        #endif
    }

    /// Tag events with the build context so simulator/TestFlight/App Store crashes
    /// are easy to filter in the Sentry dashboard. Sentry is always enabled in all
    /// of these — only the environment tag differs.
    private static var sentryEnvironment: String {
        #if targetEnvironment(simulator)
        return "simulator"
        #else
        if Bundle.main.appStoreReceiptURL?.lastPathComponent == "sandboxReceipt" {
            return "testflight"
        }
        #if DEBUG
        return "debug-device"
        #else
        return "production"
        #endif
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

        // 0b. Inject mock data for UI testing
        if CommandLine.arguments.contains("UITesting") {
            injectMockData()
        }

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

/// Inject mock session and messages into SessionCache for UI testing.
private func injectMockData() {
    let session = DianeSession(
        id: "mock-session-1",
        title: "Mock Chat",
        status: "active",
        messageCount: 3,
        runCount: 2,
        totalTokens: 161128,
        totalCostUsd: 0.0161368,
        lastRunStatus: "completed",
        createdAt: DateUtils.formatISO8601(Date().addingTimeInterval(-3600)),
        updatedAt: DateUtils.formatISO8601(),
        agentName: "diane-default"
    )
    SessionCache.shared.cacheSessions([session])

    let userMsg = DianeMessage(
        id: "msg-1",
        role: "user",
        content: "Hello, what can you do?",
        createdAt: DateUtils.formatISO8601(Date().addingTimeInterval(-1800))
    )

    let toolMsg = DianeMessage(
        id: "msg-2",
        role: "assistant",
        content: "I can help with various tasks like searching the web, reading files, and running code.",
        createdAt: DateUtils.formatISO8601(Date().addingTimeInterval(-1700)),
        toolCalls: [
            DianeMessage.ToolCall(
                name: "web_search",
                arguments: "{\"query\": \"current weather\"}",
                result: "Sunny, 72°F"
            ),
            DianeMessage.ToolCall(
                name: "read_file",
                arguments: "{\"path\": \"/tmp/test.txt\"}",
                result: "File contents: Hello world"
            )
        ],
        reasoningContent: "The user wants to know my capabilities. Let me list them."
    )
    SessionCache.shared.cacheMessages([userMsg, toolMsg], for: session.id)
}

struct ConfigKey: EnvironmentKey {
    static let defaultValue = ServerConfiguration()
}

struct CloudClientKey: EnvironmentKey {
    static let defaultValue = EmergentAPIClient()
}

extension EnvironmentValues {
    var config: ServerConfiguration {
        get { self[ConfigKey.self] }
        set { self[ConfigKey.self] = newValue }
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
