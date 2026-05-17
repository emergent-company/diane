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

    @State private var apiKey: String = ""
    @State private var projectID: String = ""
    @State private var isTestingConnection = false
    @State private var connectionTestResult: ConnectionTestResult?
    @State private var pushEnabled = PushNotificationService.shared.isRegistered
    @State private var selectedSound = "default"

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
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Save") { saveConfig() }
                    .disabled(!hasChanges)
            }
        }
        .onAppear {
            apiKey = config.apiKey
            projectID = config.projectID
        }
    }

    // MARK: - Has Changes

    private var hasChanges: Bool {
        apiKey != config.apiKey
        || projectID != config.projectID
    }

    // MARK: - Connection Section

    private var connectionSection: some View {
        Section {
            SecureField("API Key", text: $apiKey)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            TextField("Project ID", text: $projectID)
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
                    .disabled(apiKey.trimmingCharacters(in: .whitespaces).isEmpty)
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
            Text("Configure your API key and project ID to connect to the Memory Platform.")
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

        } header: {
            Label("Device", systemImage: "iphone")
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
                } else {
                    PushNotificationService.shared.unregister()
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
                Text("Distribution")
                Spacer()
                Text(distributionLabel)
                    .foregroundColor(.secondary)
                    .font(.caption)
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
        config.serverURL = MemoryPlatform.defaultURL
        config.apiKey = apiKey.trimmingCharacters(in: .whitespaces)
        config.projectID = projectID.trimmingCharacters(in: .whitespaces)
        cloudClient.configure(
            baseURL: MemoryPlatform.defaultURL,
            apiKey: config.apiKey,
            projectID: config.projectID
        )
        dismiss()
    }

    private func testConnection() async {
        guard !apiKey.trimmingCharacters(in: .whitespaces).isEmpty else {
            connectionTestResult = .failure("API Key is empty")
            return
        }

        isTestingConnection = true
        connectionTestResult = nil

        // Temporarily configure the client with the entered values
        let savedKey = config.apiKey
        let savedPID = config.projectID
        config.apiKey = apiKey.trimmingCharacters(in: .whitespaces)
        cloudClient.configure(
            baseURL: MemoryPlatform.defaultURL,
            apiKey: config.apiKey
        )

        do {
            // Test MP connectivity via projects endpoint
            let _ = try await cloudClient.fetchProjects()
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
        config.apiKey = savedKey
        config.projectID = savedPID
        cloudClient.configure(
            baseURL: MemoryPlatform.defaultURL,
            apiKey: savedKey,
            projectID: savedPID
        )
        isTestingConnection = false
    }

    // MARK: - App Version Info

    private var appVersion: String {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0"
    }

    private var appBuild: String {
        Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "1"
    }

    private var distributionLabel: String {
        #if DEBUG
        return "Debug (Xcode)"
        #else
        let receiptURL = Bundle.main.value(forKey: "appStoreReceiptURL") as? URL
        if receiptURL?.lastPathComponent == "sandboxReceipt" {
            return "TestFlight"
        }
        return "App Store"
        #endif
    }
}

// MARK: - Preview

#Preview {
    NavigationStack {
        SettingsView()
    }
}
