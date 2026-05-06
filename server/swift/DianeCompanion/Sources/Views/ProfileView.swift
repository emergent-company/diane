import SwiftUI

/// Profile view — shows identity card and API key management.
///
/// Task 9.1
struct ProfileView: View {
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var appState: AppState

    @State private var profile: UserProfile? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var isRotatingKey = false
    @State private var rotateError: String? = nil
    @State private var showRotateConfirmation = false

    var body: some View {
        HSplitView {
            // Left: Identity card
            identityPanel
                .frame(minWidth: 280)

            // Right: API Key management
            apiKeyPanel
                .frame(minWidth: 280)
        }
        .navigationTitle("Profile")
        .task { await load() }
    }

    // MARK: - Identity Panel

    @ViewBuilder
    private var identityPanel: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Profile")
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding(8)
            }

            if isLoading && profile == nil {
                LoadingStateView(message: "Loading profile…")
            } else if let p = profile {
                List {
                    Section("Identity") {
                        HStack {
                            // Avatar placeholder
                            ZStack {
                                Circle()
                                    .fill(Color.accentColor.opacity(0.2))
                                    .frame(width: 48, height: 48)
                                Text(initials(for: p.name))
                                    .font(.headline)
                                    .foregroundStyle(Color.accentColor)
                            }
                            VStack(alignment: .leading, spacing: 4) {
                                Text(p.name ?? "Unknown")
                                    .font(.subheadline)
                                    .fontWeight(.semibold)
                                if let email = p.email {
                                    Text(email)
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                        .padding(.vertical, 4)

                        if let role = p.role {
                            profileRow(label: "Role", value: role)
                        }
                        profileRow(label: "User ID", value: p.id)
                    }
                }
                .listStyle(.plain)
            } else if error == nil {
                EmptyStateView(
                    title: "No Profile",
                    icon: "person.circle",
                    description: "Could not load profile information."
                )
            }

            Spacer()
        }
    }

    // MARK: - API Key Panel

    @ViewBuilder
    private var apiKeyPanel: some View {
        VStack(spacing: 0) {
            HStack {
                Text("API Key Management")
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            if let p = profile {
                List {
                    Section("Active Key") {
                        if let key = p.apiKey {
                            // Show only last 4 chars masked
                            profileRow(label: "Key", value: maskedKey(key))
                        } else {
                            Text("No API key configured")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }

                        if let created = p.apiKeyCreatedAt {
                            profileRow(label: "Created", value: created)
                        }
                        if let lastUsed = p.apiKeyLastUsed {
                            profileRow(label: "Last Used", value: lastUsed)
                        }
                    }

                    Section {
                        if let err = rotateError {
                            Text(err)
                                .font(.caption)
                                .foregroundStyle(.red)
                        }

                        Button {
                            showRotateConfirmation = true
                        } label: {
                            if isRotatingKey {
                                HStack {
                                    ProgressView().controlSize(.mini)
                                    Text("Rotating…")
                                }
                            } else {
                                Label("Rotate API Key", systemImage: "arrow.2.circlepath")
                            }
                        }
                        .disabled(isRotatingKey)
                        .confirmationDialog(
                            "Rotate API Key?",
                            isPresented: $showRotateConfirmation,
                            titleVisibility: .visible
                        ) {
                            Button("Rotate Key", role: .destructive) {
                                Task { await rotateAPIKey() }
                            }
                            Button("Cancel", role: .cancel) {}
                        } message: {
                            Text("Your current API key will be invalidated. Any clients using it will need to be updated.")
                        }
                    }
                }
                .listStyle(.plain)
            } else if !isLoading {
                EmptyStateView(
                    title: "No Key Info",
                    icon: "key",
                    description: "Load profile to manage your API key."
                )
            }

            Divider()
            HStack {
                Text(profile != nil ? "Authenticated" : "Not authenticated")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
    }

    // MARK: - Helpers

    private func profileRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 70, alignment: .leading)
            Text(value)
                .font(.system(.caption, design: .monospaced))
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }

    private func initials(for name: String?) -> String {
        guard let name, !name.isEmpty else { return "?" }
        let parts = name.split(separator: " ")
        if parts.count >= 2 {
            return "\(parts[0].prefix(1))\(parts[1].prefix(1))".uppercased()
        }
        return String(name.prefix(2)).uppercased()
    }

    private func maskedKey(_ key: String) -> String {
        let suffix = key.suffix(4)
        return "****\(suffix)"
    }

    @MainActor
    private func load() async {
        isLoading = true
        do {
            profile = try await apiClient.fetchUserProfile()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    @MainActor
    private func rotateAPIKey() async {
        isRotatingKey = true
        rotateError = nil
        // Rotate via POST /user/profile/rotate-key (endpoint may vary)
        do {
            let data = try await apiClient.post("/user/profile/rotate-key", body: Data())
            if let json = try? JSONDecoder().decode(UserProfile.self, from: data) {
                profile = json
            } else {
                // Reload profile after rotation
                await load()
            }
        } catch {
            rotateError = error.localizedDescription
        }
        isRotatingKey = false
    }
}
