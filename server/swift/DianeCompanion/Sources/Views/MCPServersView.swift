import SwiftUI
import OSLog

/// MCP Servers view — reads from Diane's local API (served by `diane serve`) or remote fallback.
struct MCPServersView: View {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "MCPView")
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var servers: [MCPServer] = []
    @State private var selectedServer: MCPServer? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var nodes: [RelayNode] = []
    @State private var isLoadingNodes = false
    @State private var nodeError: String? = nil

    var body: some View {
        HSplitView {
            serversList
                .frame(minWidth: 200, idealWidth: 400, maxWidth: .infinity)

            if let server = selectedServer {
                serverDetailPanel(server)
                    .frame(minWidth: 200, idealWidth: 400, maxWidth: .infinity)
            } else {
                EmptyStateView(
                    title: "Select a Server",
                    icon: "plug",
                    description: "Select an MCP server to inspect its configuration."
                )
                .frame(minWidth: 200, idealWidth: 400, maxWidth: .infinity)
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
                    description: "No MCP servers configured. Add them to ~/.diane/mcp-servers.json"
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
                Text("Connected Relay Nodes")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                    .textCase(.uppercase)
                Spacer()
                if isLoadingNodes {
                    ProgressView().controlSize(.mini)
                }
                Button("Refresh") {
                    Task { await loadNodes() }
                }
                .font(.caption2)
                .buttonStyle(.borderless)
            }

            if let err = nodeError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            } else if nodes.isEmpty {
                Text("No active relay nodes")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .italic()
            } else {
                ForEach(nodes) { node in
                    HStack(spacing: 6) {
                        Circle()
                            .fill(Color.green)
                            .frame(width: 6, height: 6)
                        Text(node.hostname ?? node.instanceID)
                            .font(.caption)
                            .lineLimit(1)
                        Spacer()
                        if let count = node.toolCount {
                            Text("\(count) tool\(count == 1 ? "" : "s")")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Server Row

    private func serverRow(_ server: MCPServer) -> some View {
        HStack(spacing: 8) {
            Circle()
                .fill(server.enabled ? Color.green : Color.secondary)
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 2) {
                Text(server.name)
                    .font(.subheadline)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    Text(server.type.uppercased())
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    Text(server.enabled ? "Enabled" : "Disabled")
                        .font(.caption2)
                        .foregroundStyle(server.enabled ? Color.green : Color.secondary)
                }
            }
            Spacer()
        }
        .padding(.vertical, 2)
    }

    // MARK: - Server Detail Panel

    private func serverDetailPanel(_ server: MCPServer) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text(server.name)
                        .font(.subheadline)
                        .fontWeight(.semibold)
                    HStack(spacing: 6) {
                        Circle()
                            .fill(server.enabled ? Color.green : Color.secondary)
                            .frame(width: 7, height: 7)
                        Text(server.enabled ? "Enabled" : "Disabled")
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
                    detailRow(label: "Type", value: server.type.uppercased())
                    if let url = server.url, !url.isEmpty {
                        detailRow(label: "URL", value: url)
                    }
                    if let cmd = server.command {
                        detailRow(label: "Command", value: cmd)
                    }
                    if let args = server.args, !args.isEmpty {
                        detailRow(label: "Args", value: args.joined(separator: " "))
                    }
                    if let timeout = server.timeout, timeout > 0 {
                        detailRow(label: "Timeout", value: "\(timeout)s")
                    }
                }

                if let env = server.env, !env.isEmpty {
                    Section("Environment (\(env.count))") {
                        ForEach(Array(env.keys.sorted()), id: \.self) { key in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(key)
                                    .font(.caption)
                                    .fontWeight(.medium)
                                Text(env[key] ?? "")
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                            }
                            .padding(.vertical, 2)
                        }
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

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        do {
            // Always try local API first — it's fresh for each request
            servers = try await dianeAPI.fetchMCPServers()
            await loadNodes()
            error = nil
        } catch let localError {
            // Fall back to remote API if local fails
            logger.warning("Local API failed: \(localError.localizedDescription), trying remote...")
            do {
                servers = try await apiClient.fetchMCPServers(projectID: serverConfig.projectID)
                await loadNodes()
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
        isLoading = false
    }

    @MainActor
    private func loadNodes() async {
        isLoadingNodes = true
        do {
            // Always try local API first
            nodes = try await dianeAPI.fetchRelayNodes()
            nodeError = nil
        } catch {
            do {
                let relaySessions = try await apiClient.fetchRelaySessions(projectID: serverConfig.projectID)
                nodes = relaySessions.map { r in
                    RelayNode(instanceID: r.instanceID ?? r.id, hostname: r.nodeName, version: nil, toolCount: r.toolCount, connectedAt: r.connectedAt)
                }
                nodeError = nil
            } catch {
                nodeError = error.localizedDescription
            }
        }
        isLoadingNodes = false
    }
}
