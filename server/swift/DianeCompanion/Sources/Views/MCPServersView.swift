import SwiftUI

/// MCP Servers view — list registered MCP servers and inspect their tools.
///
/// Configuration task 8.2
struct MCPServersView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration

    @State private var servers: [MCPServer] = []
    @State private var selectedServer: MCPServer? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var relaySessions: [RelaySession] = []
    @State private var isLoadingRelays = false
    @State private var relayError: String? = nil

    var body: some View {
        HSplitView {
            serversList
                .frame(minWidth: 280)

            if let server = selectedServer {
                serverDetailPanel(server)
                    .frame(minWidth: 280)
            } else {
                EmptyStateView(
                    title: "Select a Server",
                    icon: "plug",
                    description: "Select an MCP server to inspect its tools."
                )
                .frame(minWidth: 280)
            }
        }
        .navigationTitle("MCP Servers")
        .task { await load() }
    }

    // MARK: - Servers List

    @ViewBuilder
    private var serversList: some View {
        VStack(spacing: 0) {
            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding(8)
            }

            if isLoading && servers.isEmpty {
                LoadingStateView(message: "Loading MCP servers…")
            } else if servers.isEmpty {
                EmptyStateView(
                    title: "No MCP Servers",
                    icon: "plug",
                    description: "No MCP servers have been configured."
                )
            } else {
                List(servers, selection: $selectedServer) { server in
                    serverRow(server)
                        .tag(server)
                }
                .listStyle(.plain)
            }

            Divider()
            HStack {
                Text("\(servers.count) MCP server\(servers.count == 1 ? "" : "s") configured")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Button("Refresh") { Task { await load() } }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            
            Divider()
            
            // Relay Nodes section
            relayNodesSection
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
        }
    }

    // MARK: - Relay Nodes Section

    @ViewBuilder
    private var relayNodesSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Active Relay Nodes")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                    .textCase(.uppercase)
                Spacer()
                if isLoadingRelays {
                    ProgressView().controlSize(.mini)
                }
                Button("Refresh") {
                    Task { await loadRelays() }
                }
                .font(.caption2)
                .buttonStyle(.borderless)
            }
            
            if let err = relayError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            } else if relaySessions.isEmpty {
                Text("No active relay nodes")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .italic()
            } else {
                ForEach(relaySessions) { relay in
                    HStack(spacing: 6) {
                        Circle()
                            .fill(Color.green)
                            .frame(width: 6, height: 6)
                        Text(relay.nodeName ?? relay.instanceID ?? "Unknown")
                            .font(.caption)
                            .lineLimit(1)
                        Spacer()
                        if let count = relay.toolCount {
                            Text("\(count) tools")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }

    private func serverRow(_ server: MCPServer) -> some View {
        HStack(spacing: 8) {
            Circle()
                .fill(serverStatusColor(server.status))
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 2) {
                Text(server.name)
                    .font(.subheadline)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let type = server.serverType {
                        Text(type.uppercased())
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    Text(serverStatusLabel(server.status))
                        .font(.caption2)
                        .foregroundStyle(serverStatusColor(server.status))
                }
            }
            Spacer()
        }
        .padding(.vertical, 2)
    }

    private func serverStatusColor(_ status: String?) -> Color {
        switch status?.lowercased() {
        case "online", "connected": return .green
        case "offline", "disconnected": return .red
        default: return .secondary
        }
    }

    private func serverStatusLabel(_ status: String?) -> String {
        status?.capitalized ?? "Unknown"
    }

    // MARK: - Server Detail Panel

    private func serverDetailPanel(_ server: MCPServer) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text(server.name)
                        .font(.subheadline)
                        .fontWeight(.semibold)
                    HStack(spacing: 6) {
                        Circle()
                            .fill(serverStatusColor(server.status))
                            .frame(width: 7, height: 7)
                        Text(serverStatusLabel(server.status))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            List {
                Section("Connection") {
                    if let type = server.serverType {
                        detailRow(label: "Type", value: type.uppercased())
                    }
                    if let url = server.url {
                        detailRow(label: "URL", value: url)
                    }
                }

                if let tools = server.tools, !tools.isEmpty {
                    Section("Tools (\(tools.count))") {
                        ForEach(tools) { tool in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(tool.name)
                                    .font(.caption)
                                    .fontWeight(.medium)
                                if let desc = tool.description {
                                    Text(desc)
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }
                            }
                            .padding(.vertical, 2)
                        }
                    }
                } else {
                    Section("Tools") {
                        Text("No tools available")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .listStyle(.plain)
        }
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 60, alignment: .leading)
            Text(value)
                .font(.system(.caption, design: .monospaced))
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }

    @MainActor
    private func load() async {
        guard !serverConfig.projectID.isEmpty else {
            error = "No project configured in Settings"
            return
        }
        isLoading = true
        do {
            servers = try await apiClient.fetchMCPServers(projectID: serverConfig.projectID)
            await loadRelays()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    @MainActor
    private func loadRelays() async {
        guard !serverConfig.projectID.isEmpty else {
            return
        }
        isLoadingRelays = true
        do {
            relaySessions = try await apiClient.fetchRelaySessions(projectID: serverConfig.projectID)
            relayError = nil
        } catch {
            relayError = error.localizedDescription
        }
        isLoadingRelays = false
    }
}
