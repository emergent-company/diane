import SwiftUI
import ServiceManagement
import AppKit

struct SettingsView: View {
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var selfTestManager: SelfTestManager

    @State private var urlDraft: String = ""
    @State private var apiKeyDraft: String = ""
    @State private var urlError: String? = nil
    @State private var isAPIKeyVisible: Bool = false
    @State private var savedConfirmation: Bool = false

    // Connection test state
    @State private var testState: TestState = .idle

    enum TestState: Equatable {
        case idle
        case testing
        case success(String)
        case failure(String)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {

            // Server URL
            GroupBox("Server") {
                VStack(alignment: .leading, spacing: 8) {
                    TextField("https://your-server:8080", text: $urlDraft)
                        .textFieldStyle(.roundedBorder)
                        .onSubmit { saveSettings() }
                        .onChange(of: urlDraft) { testState = .idle }

                    if let error = urlError {
                        Label(error, systemImage: "exclamationmark.circle")
                            .font(.caption)
                            .foregroundStyle(.red)
                    } else {
                        Text("HTTP or HTTPS URL of your Memory Platform server, including port if needed.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(4)
            }

            // API Key
            GroupBox("Authentication") {
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        if isAPIKeyVisible {
                            TextField("Account API key", text: $apiKeyDraft)
                                .textFieldStyle(.roundedBorder)
                                .font(.system(.body, design: .monospaced))
                                .onSubmit { saveSettings() }
                                .onChange(of: apiKeyDraft) { testState = .idle }
                        } else {
                            SecureField("Account API key", text: $apiKeyDraft)
                                .textFieldStyle(.roundedBorder)
                                .font(.system(.body, design: .monospaced))
                                .onSubmit { saveSettings() }
                                .onChange(of: apiKeyDraft) { testState = .idle }
                        }
                        Button {
                            isAPIKeyVisible.toggle()
                        } label: {
                            Image(systemName: isAPIKeyVisible ? "eye.slash" : "eye")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                        .help(isAPIKeyVisible ? "Hide API key" : "Show API key")
                    }
                    Text("Your Memory Platform account API key. Leave blank for unauthenticated servers.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(4)
            }

            // Test connection + result
            GroupBox("Connection") {
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Button {
                            Task { await testConnection() }
                        } label: {
                            HStack(spacing: 6) {
                                if case .testing = testState {
                                    ProgressView()
                                        .controlSize(.small)
                                } else {
                                    Image(systemName: "antenna.radiowaves.left.and.right")
                                }
                                Text("Test Connection")
                            }
                        }
                        .disabled(urlDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || testState == .testing)

                        Spacer()

                        // Inline result badge
                        switch testState {
                        case .idle:
                            EmptyView()
                        case .testing:
                            EmptyView()
                        case .success(let msg):
                            Label(msg, systemImage: "checkmark.circle.fill")
                                .font(.caption)
                                .foregroundStyle(.green)
                        case .failure(let msg):
                            Label(msg, systemImage: "xmark.circle.fill")
                                .font(.caption)
                                .foregroundStyle(.red)
                                .lineLimit(2)
                        }
                    }
                    Text("Tests the current URL and API key without saving.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(4)
            }

            // Save button
            HStack {
                Spacer()
                if savedConfirmation {
                    Label("Saved", systemImage: "checkmark.circle.fill")
                        .font(.caption)
                        .foregroundStyle(.green)
                }
                Button("Save") { saveSettings() }
                    .buttonStyle(.borderedProminent)
                    .disabled(urlDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .keyboardShortcut(.return, modifiers: .command)
            }

            // Launch at Login
            GroupBox("Startup") {
                VStack(alignment: .leading, spacing: 8) {
                    Toggle("Launch at Login", isOn: $serverConfig.launchAtLogin)
                        .onChange(of: serverConfig.launchAtLogin) { _, newValue in
                            applyLaunchAtLogin(newValue)
                        }
                    Text("Automatically start Diane when you log in.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(4)
            }

            // Self-Test
            GroupBox("Self-Test") {
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Button {
                            Task { await selfTestManager.run() }
                        } label: {
                            HStack(spacing: 6) {
                                if case .running = selfTestManager.state {
                                    ProgressView()
                                        .controlSize(.small)
                                } else {
                                    Image(systemName: "stethoscope")
                                }
                                Text("Run Self-Test")
                            }
                        }
                        .disabled(selfTestManager.state == .running)

                        Spacer()

                        switch selfTestManager.state {
                        case .idle:
                            EmptyView()
                        case .running:
                            Text("Running…")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        case .completed(let report):
                            HStack(spacing: 4) {
                                Image(systemName: report.failed > 0 ? "xmark.circle.fill" : "checkmark.circle.fill")
                                    .foregroundStyle(report.failed > 0 ? .red : .green)
                                Text("\(report.passed)/\(report.total) passed")
                                    .font(.caption)
                            }
                        case .failed(let error):
                            Label(error, systemImage: "exclamationmark.triangle.fill")
                                .font(.caption)
                                .foregroundStyle(.orange)
                                .lineLimit(1)
                        }
                    }

                    if case .completed(let report) = selfTestManager.state {
                        VStack(alignment: .leading, spacing: 3) {
                            ForEach(report.checks) { check in
                                HStack(spacing: 4) {
                                    switch check.status {
                                    case "pass":
                                        Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                                    case "fail":
                                        Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
                                    case "warn":
                                        Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
                                    case "skip":
                                        Image(systemName: "minus.circle.fill").foregroundStyle(.secondary)
                                    default:
                                        Image(systemName: "questionmark.circle").foregroundStyle(.secondary)
                                    }
                                    Text(check.name)
                                        .font(.caption)
                                        .fontWeight(check.isFailed ? .semibold : .regular)
                                    if let detail = check.detail {
                                        Text(detail)
                                            .font(.caption2)
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                    }
                                }
                            }
                        }
                        .padding(.leading, 4)
                    }

                    Text("Runs diane doctor --json to check config, SDK, session CRUD, agents, and more.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(4)
            }

            Spacer()
        }
        .padding(20)
        .frame(width: 420, height: 560)
        .onAppear {
            urlDraft = serverConfig.serverURL
            apiKeyDraft = serverConfig.apiKey
        }
    }

    // MARK: - Test Connection

    @MainActor
    private func testConnection() async {
        let trimmedURL = urlDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedKey = apiKeyDraft.trimmingCharacters(in: .whitespacesAndNewlines)

        guard !trimmedURL.isEmpty,
              let base = URL(string: trimmedURL),
              let scheme = base.scheme,
              ["http", "https"].contains(scheme.lowercased()),
              base.host != nil else {
            testState = .failure("Invalid URL")
            return
        }

        testState = .testing

        let healthURL = base.appendingPathComponent("health")
        var request = URLRequest(url: healthURL)
        request.httpMethod = "GET"
        request.timeoutInterval = 8
        if !trimmedKey.isEmpty {
            // Match CLI auth logic: emt_* tokens use Bearer auth; standalone keys use X-API-Key.
            if trimmedKey.hasPrefix("emt_") {
                request.setValue("Bearer \(trimmedKey)", forHTTPHeaderField: "Authorization")
            } else {
                request.setValue(trimmedKey, forHTTPHeaderField: "X-API-Key")
            }
        }

        do {
            let session = URLSession(configuration: .ephemeral)
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                testState = .failure("No HTTP response")
                return
            }
            if (200...299).contains(http.statusCode) {
                // Try to extract version from health payload
                struct HealthPayload: Decodable {
                    let version: String?
                    let status: String?
                }
                let version = (try? JSONDecoder().decode(HealthPayload.self, from: data))?.version
                testState = .success(version.map { "Connected — v\($0)" } ?? "Connected")
            } else if http.statusCode == 401 || http.statusCode == 403 {
                testState = .failure("Unauthorized — check API key")
            } else {
                testState = .failure("HTTP \(http.statusCode)")
            }
        } catch let urlError as URLError {
            switch urlError.code {
            case .timedOut:             testState = .failure("Timed out")
            case .cannotConnectToHost:  testState = .failure("Cannot connect to host")
            case .cannotFindHost:       testState = .failure("Host not found")
            case .notConnectedToInternet: testState = .failure("No internet connection")
            default:                    testState = .failure(urlError.localizedDescription)
            }
        } catch {
            testState = .failure(error.localizedDescription)
        }
    }

    // MARK: - Save

    private func saveSettings() {
        let trimmedURL = urlDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedKey = apiKeyDraft.trimmingCharacters(in: .whitespacesAndNewlines)

        if !trimmedURL.isEmpty {
            guard let url = URL(string: trimmedURL),
                  let scheme = url.scheme,
                  ["http", "https"].contains(scheme.lowercased()),
                  url.host != nil else {
                urlError = "Please enter a valid HTTP or HTTPS URL."
                return
            }
        }

        urlError = nil
        serverConfig.serverURL = trimmedURL
        serverConfig.apiKey = trimmedKey
        statusMonitor.configure(from: serverConfig)
        apiClient.configure(serverURL: trimmedURL, apiKey: trimmedKey)

        // Flash "Saved" confirmation
        savedConfirmation = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            savedConfirmation = false
        }
    }

    private func applyLaunchAtLogin(_ enabled: Bool) {
        do {
            if enabled {
                try SMAppService.mainApp.register()
            } else {
                try SMAppService.mainApp.unregister()
            }
        } catch {
            print("Launch at login error: \(error)")
        }
    }
}


