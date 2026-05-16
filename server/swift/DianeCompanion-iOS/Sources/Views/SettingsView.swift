import SwiftUI
import DianeShared
import UserNotifications
import UIKit

// MARK: - Sound Picker

private let notificationSounds: [(name: String, id: String)] = [
    ("Default", "default"),
    ("None", "none"),
    ("Ambient", "ambient"),
    ("Bell", "bell"),
    ("Chime", "chime"),
    ("Ding", "ding"),
    ("Electronic", "electronic"),
    ("Flute", "flute"),
    ("Glass", "glass"),
    ("Guitar", "guitar"),
    ("Knock", "knock"),
    ("Marimba", "marimba"),
    ("Message", "message"),
    ("Note", "note"),
    ("Piano", "piano"),
    ("Pop", "pop"),
]

// MARK: - SettingsView

struct SettingsView: View {
    @Environment(\.config) private var config
    @Environment(\.cloudClient) private var cloudClient
    @Environment(\.dismiss) private var dismiss

    @State private var serverURL: String = ""
    @State private var dianeServerURL: String = ""
    @State private var apiKey: String = ""
    @State private var projectID: String = ""
    @State private var isTestingConnection = false
    @State private var connectionTestResult: ConnectionTestResult?
    @State private var pushEnabled = PushNotificationService.shared.isRegistered
    @State private var selectedSound: String = "default"

    private enum ConnectionTestResult: Equatable {
        case success
        case failure(String)
    }

    // MARK: - Body

    var body: some View {
        Form {
            connectionSection
            deviceSection
            notificationsSection
            aboutSection
        }
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Save") { saveConfig() }
                    .disabled(!hasChanges)
            }
        }
        .onAppear {
            serverURL = config.serverURL
            dianeServerURL = config.dianeServerURL
            apiKey = config.apiKey
            projectID = config.projectID
        }
    }

    // MARK: - Has Changes

    private var hasChanges: Bool {
        serverURL != config.serverURL
        || dianeServerURL != config.dianeServerURL
        || apiKey != config.apiKey
        || projectID != config.projectID
    }

    // MARK: - Connection Section

    private var connectionSection: some View {
        Section {
            TextField("Memory Platform URL", text: $serverURL)
                .textContentType(.URL)
                .keyboardType(.URL)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            TextField("Diane Server URL", text: $dianeServerURL)
                .textContentType(.URL)
                .keyboardType(.URL)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            SecureField("API Key", text: $apiKey)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            TextField("Project ID (optional)", text: $projectID)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            // Test Connection
            HStack {
                if isTestingConnection {
                    HStack(spacing: DesignTokens.spacingSM) {
                        ProgressView()
                            .scaleEffect(0.8)
                        Text("Testing connection...")
                            .font(.subheadline)
                            .foregroundColor(.secondary)
                    }
                } else {
                    Button(action: { Task { await testConnection() } }) {
                        HStack(spacing: DesignTokens.spacingSM) {
                            Text("Test Connection")
                                .font(.subheadline)
                            Spacer()
                            if let result = connectionTestResult {
                                switch result {
                                case .success:
                                    Image(systemName: "checkmark.circle.fill")
                                        .foregroundColor(.green)
                                case .failure:
                                    Image(systemName: "xmark.circle.fill")
                                        .foregroundColor(.red)
                                }
                            }
                        }
                    }
                    .disabled(serverURL.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }

            if case .failure(let msg) = connectionTestResult {
                Text(msg)
                    .font(.caption)
                    .foregroundColor(.red)
            }
        } header: {
            Label("Connection", systemImage: "antenna.radiowaves.left.and.right")
        } footer: {
            Text("Configure your Diane server connection. The server URL should point to your running Diane instance.")
        }
    }

    // MARK: - Device Section

    private var deviceSection: some View {
        Section {
            HStack {
                Text("Device Name")
                Spacer()
                Text(UIDevice.current.name)
                    .foregroundColor(.secondary)
            }

            HStack {
                Text("Model")
                Spacer()
                Text(UIDevice.current.model)
                    .foregroundColor(.secondary)
            }

            HStack {
                Text("iOS Version")
                Spacer()
                Text(UIDevice.current.systemVersion)
                    .foregroundColor(.secondary)
            }

            if !NodeRegistrationService.shared.instanceID.isEmpty {
                HStack {
                    Text("Node ID")
                    Spacer()
                    Text(NodeRegistrationService.shared.instanceID)
                        .foregroundColor(.secondary)
                        .font(.caption.monospaced())
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }

            HStack {
                Text("Node Status")
                Spacer()
                HStack(spacing: DesignTokens.spacingXS) {
                    Circle()
                        .fill(nodeStatusColor)
                        .frame(width: 8, height: 8)
                    Text(NodeRegistrationService.shared.status.rawValue.capitalized)
                        .foregroundColor(.secondary)
                }
            }

            if let token = PushNotificationService.shared.token, !token.isEmpty {
                HStack {
                    Text("Push Token")
                    Spacer()
                    Text(PushNotificationService.shared.token ?? "")
                        .foregroundColor(.secondary)
                        .font(.caption2.monospaced())
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }

        } header: {
            Label("Device", systemImage: "iphone")
        }
    }

    private var nodeStatusColor: Color {
        switch NodeRegistrationService.shared.status {
        case .registered: return .green
        case .unregistered: return .orange
        case .failed: return .red
        }
    }

    // MARK: - Notifications Section

    private var notificationsSection: some View {
        Section {
            Toggle(isOn: $pushEnabled) {
                HStack(spacing: DesignTokens.spacingSM) {
                    Image(systemName: "bell.badge")
                        .foregroundColor(.accentColor)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Push Notifications")
                            .font(.body)
                        Text("Receive alerts from Diane agents")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                }
            }
            .onChange(of: pushEnabled) { _, newValue in
                if newValue {
                    PushNotificationService.shared.register()
                }
            }

            if pushEnabled {
                Picker(selection: $selectedSound) {
                    ForEach(notificationSounds, id: \.id) { sound in
                        Text(sound.name).tag(sound.id)
                    }
                } label: {
                    HStack(spacing: DesignTokens.spacingSM) {
                        Image(systemName: "speaker.wave.2")
                            .foregroundColor(.accentColor)
                        Text("Notification Sound")
                    }
                }
            }
        } header: {
            Label("Notifications", systemImage: "bell")
        } footer: {
            Text("Enable push notifications to receive updates from your Diane agents even when the app is in the background.")
        }
    }

    // MARK: - About Section

    private var aboutSection: some View {
        Section {
            HStack {
                Text("Version")
                Spacer()
                Text(appVersion)
                    .foregroundColor(.secondary)
            }

            HStack {
                Text("Build")
                Spacer()
                Text(appBuild)
                    .foregroundColor(.secondary)
                    .font(.caption.monospaced())
            }

            HStack {
                Text("Platform")
                Spacer()
                Text("iOS \(UIDevice.current.systemVersion)")
                    .foregroundColor(.secondary)
            }

            HStack {
                Text("SDK")
                Spacer()
                Text("Swift 6")
                    .foregroundColor(.secondary)
            }
        } header: {
            Label("About", systemImage: "info.circle")
        } footer: {
            Text("Diane Companion — iOS Client")
        }
    }

    // MARK: - Actions

    private func saveConfig() {
        config.serverURL = serverURL.trimmingCharacters(in: .whitespaces)
        config.dianeServerURL = dianeServerURL.trimmingCharacters(in: .whitespaces)
        config.apiKey = apiKey.trimmingCharacters(in: .whitespaces)
        config.projectID = projectID.trimmingCharacters(in: .whitespaces)
        cloudClient.configure(baseURL: config.serverURL, apiKey: config.apiKey)
        dismiss()
    }

    private func testConnection() async {
        let testURL = serverURL.trimmingCharacters(in: .whitespaces)
        let testDianeURL = dianeServerURL.trimmingCharacters(in: .whitespaces)
        let effectiveDianeURL = testDianeURL.isEmpty ? testURL : testDianeURL

        guard !testURL.isEmpty && !effectiveDianeURL.isEmpty else {
            connectionTestResult = .failure("Server URLs are empty")
            return
        }

        isTestingConnection = true
        connectionTestResult = nil

        // Temporarily configure the client with the entered values
        let savedURL = config.serverURL
        let savedDianeURL = config.dianeServerURL
        let savedKey = config.apiKey
        config.serverURL = testURL
        config.dianeServerURL = testDianeURL
        config.apiKey = apiKey.trimmingCharacters(in: .whitespaces)
        cloudClient.configure(baseURL: testURL, apiKey: apiKey)

        do {
            // Test Memory Platform connectivity
            let _ = try await cloudClient.fetchSessions()
            connectionTestResult = .success
        } catch {
            let message: String
            if let httpErr = error as? HTTPError {
                message = httpErr.localizedDescription
            } else {
                message = error.localizedDescription
            }
            connectionTestResult = .failure(message)
        }

        // Restore saved config
        config.serverURL = savedURL
        config.dianeServerURL = savedDianeURL
        config.apiKey = savedKey
        cloudClient.configure(baseURL: savedURL, apiKey: savedKey)
        isTestingConnection = false
    }

    // MARK: - App Version Info

    private var appVersion: String {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0"
    }

    private var appBuild: String {
        Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "1"
    }
}

// MARK: - UIDevice Push Token Extension

extension PushNotificationService {
    var isEmpty: Bool {
        token == nil || token?.isEmpty == true
    }
}

// MARK: - Preview

#Preview {
    NavigationStack {
        SettingsView()
    }
}
