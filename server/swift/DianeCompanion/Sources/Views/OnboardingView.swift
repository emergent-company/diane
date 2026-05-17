import SwiftUI

/// Full-screen onboarding shown when the user has not configured their API key yet.
/// The server URL is always http://localhost:8890 on macOS — the bundled `diane serve`.
struct OnboardingView: View {
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var apiKeyDraft: String = ""
    @State private var isAPIKeyVisible: Bool = false
    @State private var savedConfirmation: Bool = false

    var body: some View {
        VStack(spacing: 0) {
            Spacer()

            // ── Welcome header ──
            VStack(spacing: Design.Spacing.sm) {
                Image(systemName: "brain.head.profile")
                    .font(.system(size: 48))
                    .foregroundStyle(.tint)

                Text("Welcome to Diane")
                    .font(.title)
                    .fontWeight(.semibold)

                Text("Connect to your Memory Platform to get started.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding(.bottom, Design.Spacing.xl)

            // ── Configuration form ──
            VStack(alignment: .leading, spacing: Design.Spacing.lg) {
                // API Key
                GroupBox("Authentication") {
                    VStack(alignment: .leading, spacing: Design.Spacing.sm) {
                        HStack {
                            if isAPIKeyVisible {
                                TextField("Account API key", text: $apiKeyDraft)
                                    .textFieldStyle(.roundedBorder)
                                    .font(.system(.body, design: .monospaced))
                                    .onSubmit { saveSettings() }
                            } else {
                                SecureField("Account API key", text: $apiKeyDraft)
                                    .textFieldStyle(.roundedBorder)
                                    .font(.system(.body, design: .monospaced))
                                    .onSubmit { saveSettings() }
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
                    .padding(Design.Padding.card - 8)
                }

                // Save button
                HStack {
                    if savedConfirmation {
                        Label("Saved", systemImage: "checkmark.circle.fill")
                            .font(.caption)
                            .foregroundStyle(.green)
                            .accessibilityIdentifier("Saved")
                    }
                    Spacer()
                    Button("Save") { saveSettings() }
                        .buttonStyle(.borderedProminent)
                        .disabled(apiKeyDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                        .keyboardShortcut(.return, modifiers: .command)
                }
            }
            .frame(width: 420)

            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .onAppear {
            apiKeyDraft = serverConfig.apiKey
        }
    }

    // MARK: - Save

    private func saveSettings() {
        let trimmedKey = apiKeyDraft.trimmingCharacters(in: .whitespacesAndNewlines)

        // macOS companion always connects to the local diane serve daemon.
        serverConfig.serverURL = "http://localhost:8890"
        serverConfig.apiKey = trimmedKey
        statusMonitor.configure(from: serverConfig)
        apiClient.configure(serverURL: serverConfig.serverURL, apiKey: trimmedKey)

        // Flash "Saved" confirmation
        savedConfirmation = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            savedConfirmation = false
        }
    }
}
