import SwiftUI
import DianeShared

struct SystemView: View {
    @Environment(\.cloudClient) private var cloudClient

    @State private var sessions: [DianeSession] = []
    @State private var servers: [MCPServer] = []
    @State private var agents: [AgentDef] = []
    @State private var health: String?
    @State private var isLoading = true
    @State private var error: String?

    var body: some View {
        Group {
            if isLoading {
                ProgressView("Loading system info...")
                    .frame(maxHeight: .infinity)
            } else if let err = error {
                ContentUnavailableView(
                    "Could Not Load System Info",
                    systemImage: "antenna.radiowaves.left.and.right",
                    description: Text(err)
                )
            } else {
                Form {
                    // Connection Section
                    Section("Connection") {
                        HStack {
                            Label("Server", systemImage: "externaldrive")
                            Spacer()
                            Text(cloudClient.baseURL.isEmpty ? "Not configured" : cloudClient.baseURL)
                                .font(.caption)
                                .foregroundColor(.secondary)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }

                        HStack {
                            Label("Cloud API", systemImage: "cloud")
                            Spacer()
                            Text(cloudClient.baseURL.isEmpty ? "Not configured" : cloudClient.baseURL)
                                .font(.caption)
                                .foregroundColor(.secondary)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }

                        if let healthStr = health {
                            HStack {
                                Label("Health", systemImage: "heart")
                                Spacer()
                                HStack(spacing: 4) {
                                    Circle()
                                        .fill(healthStr == "ok" ? Color.green : Color.red)
                                        .frame(width: 8, height: 8)
                                    Text(healthStr.capitalized)
                                        .font(.subheadline)
                                        .foregroundColor(.secondary)
                                }
                            }
                        }
                    }

                    // Counts Section
                    Section("Counts") {
                        StatRow(
                            icon: "message.fill",
                            iconColor: .accentColor,
                            label: "Sessions",
                            count: sessions.count
                        )
                        StatRow(
                            icon: "brain.head.profile",
                            iconColor: .purple,
                            label: "Agents",
                            count: agents.count
                        )
                        StatRow(
                            icon: "server.rack",
                            iconColor: .orange,
                            label: "MCP Servers",
                            count: servers.count
                        )
                    }

                    // Device Info Section
                    Section("Device") {
                        HStack {
                            Label("Model", systemImage: "iphone")
                            Spacer()
                            Text(UIDevice.current.model)
                                .foregroundColor(.secondary)
                        }
                        HStack {
                            Label("iOS Version", systemImage: "gear")
                            Spacer()
                            Text(UIDevice.current.systemVersion)
                                .foregroundColor(.secondary)
                        }
                        HStack {
                            Label("Name", systemImage: "person.text.rectangle")
                            Spacer()
                            Text(UIDevice.current.name)
                                .foregroundColor(.secondary)
                                .lineLimit(1)
                        }
                        if !NodeRegistrationService.shared.instanceID.isEmpty {
                            HStack {
                                Label("Node ID", systemImage: "antenna.radiowaves.left.and.right")
                                Spacer()
                                Text(NodeRegistrationService.shared.instanceID)
                                    .font(.caption.monospaced())
                                    .foregroundColor(.secondary)
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle("Status")
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        isLoading = true
        error = nil
        do {
            // Fetch counts in parallel
            async let sessionsTask = cloudClient.fetchSessions()
            async let agentsTask = cloudClient.fetchAgentDefs()
            async let serversTask = cloudClient.fetchMCPServers()

            (sessions, agents, servers) = try await (
                sessionsTask,
                agentsTask,
                serversTask
            )
            health = "ok"
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - Stat Row

struct StatRow: View {
    let icon: String
    let iconColor: Color
    let label: String
    let count: Int

    var body: some View {
        HStack {
            Label(label, systemImage: icon)
                .foregroundColor(iconColor)
            Spacer()
            Text("\(count)")
                .font(.title3.monospaced())
                .foregroundColor(.primary)
                .fontWeight(.semibold)
        }
    }
}
