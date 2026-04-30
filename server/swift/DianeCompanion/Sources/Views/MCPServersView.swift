import SwiftUI

/// MCP Servers view — reads from Diane's local API (served by `diane serve`) or remote fallback.
struct MCPServersView: View {
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
        SplitListDetailView(
            emptyTitle: "Select a Server",
            emptyIcon: "plug",
            emptyDescription: "Select an MCP server to inspect its configuration and tools.",
            listContent: { serversList },
            detailContent: {
                if let server = selectedServer {
                    serverDetailPanel(server)
                } else {
                    EmptyStateView(
                        title: "Select a Server",
                        icon: "plug",
                        description: "Select an MCP server to inspect its configuration and tools."
                    )
                }
            }
        )
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
        MCPServerDetailView(server: server, dianeAPI: dianeAPI)
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
            logWarning("Local API failed: \(localError.localizedDescription), trying remote...", category: "MCPView")
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
                    RelayNode(instanceID: r.instanceID ?? r.id, hostname: r.nodeName, mode: nil, version: nil, toolCount: r.toolCount, connectedAt: r.connectedAt, online: false)
                }
                nodeError = nil
            } catch {
                nodeError = error.localizedDescription
            }
        }
        isLoadingNodes = false
    }
}

// MARK: - Server Detail View with Tools/Prompts Tabs

private struct MCPServerDetailView: View {
    let server: MCPServer
    let dianeAPI: DianeAPIClient

    @State private var selectedTab: DetailTab = .connection
    @State private var tools: [MCPTool] = []
    @State private var prompts: [MCPPrompt] = []
    @State private var isLoadingTools = false
    @State private var isLoadingPrompts = false
    @State private var toolsError: String? = nil
    @State private var promptsError: String? = nil

    private enum DetailTab: String, CaseIterable {
        case connection = "Connection"
        case tools = "Tools"
        case prompts = "Prompts"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
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

            // Tab bar
            Picker("", selection: $selectedTab) {
                ForEach(DetailTab.allCases, id: \.self) { tab in
                    Text(tab.rawValue).tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal, 12)
            .padding(.vertical, 8)

            Divider()

            // Tab content
            ScrollView {
                switch selectedTab {
                case .connection:
                    connectionContent
                case .tools:
                    toolsContent
                case .prompts:
                    promptsContent
                }
            }
        }
        .task(id: server.name) {
            await loadTools()
            await loadPrompts()
        }
    }

    // MARK: - Connection Tab

    @ViewBuilder
    private var connectionContent: some View {
        VStack(alignment: .leading, spacing: 0) {
            connectionRow(label: "Type", value: server.type.uppercased())
            if let url = server.url, !url.isEmpty {
                connectionRow(label: "URL", value: url)
            }
            if let cmd = server.command {
                connectionRow(label: "Command", value: cmd)
            }
            if let args = server.args, !args.isEmpty {
                connectionRow(label: "Args", value: args.joined(separator: " "))
            }
            if let timeout = server.timeout, timeout > 0 {
                connectionRow(label: "Timeout", value: "\(timeout)s")
            }

            if let env = server.env, !env.isEmpty {
                Divider().padding(.horizontal, 12)
                Text("Environment (\(env.count))")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.top, 8)
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
                    .padding(.horizontal, 12)
                    .padding(.vertical, 2)
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func connectionRow(label: String, value: String) -> some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 60, alignment: .leading)
            Text(value)
                .font(.system(.caption, design: .monospaced))
                .lineLimit(nil)
                .textSelection(.enabled)
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 3)
    }

    // MARK: - Tools Tab

    @ViewBuilder
    private var toolsContent: some View {
        VStack(alignment: .leading, spacing: 4) {
            if isLoadingTools {
                HStack {
                    Spacer()
                    ProgressView("Loading tools…")
                        .controlSize(.small)
                        .padding(20)
                    Spacer()
                }
            } else if let err = toolsError {
                HStack {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Retry") { Task { await loadTools() } }
                        .font(.caption)
                        .buttonStyle(.borderless)
                }
                .padding(12)
            } else if tools.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "wrench.adjustable")
                        .font(.title2)
                        .foregroundStyle(.tertiary)
                    Text("No tools registered")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text("This server exposes no MCP tools.")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity)
                .padding(24)
            } else {
                Text("\(tools.count) tool\(tools.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.top, 8)
                ForEach(tools) { tool in
                    toolRow(tool)
                }
            }
        }
    }

    private func toolRow(_ tool: MCPTool) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Image(systemName: "wrench.adjustable")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(tool.name)
                    .font(.system(.caption, design: .monospaced))
                    .fontWeight(.medium)
            }
            if let desc = tool.description, !desc.isEmpty {
                Text(desc)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
    }

    // MARK: - Prompts Tab

    @ViewBuilder
    private var promptsContent: some View {
        VStack(alignment: .leading, spacing: 4) {
            if isLoadingPrompts {
                HStack {
                    Spacer()
                    ProgressView("Loading prompts…")
                        .controlSize(.small)
                        .padding(20)
                    Spacer()
                }
            } else if let err = promptsError {
                HStack {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Retry") { Task { await loadPrompts() } }
                        .font(.caption)
                        .buttonStyle(.borderless)
                }
                .padding(12)
            } else if prompts.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "text.bubble")
                        .font(.title2)
                        .foregroundStyle(.tertiary)
                    Text("No prompts registered")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text("This server exposes no MCP prompts.")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity)
                .padding(24)
            } else {
                Text("\(prompts.count) prompt\(prompts.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.top, 8)
                ForEach(prompts) { prompt in
                    promptRow(prompt)
                }
            }
        }
    }

    private func promptRow(_ prompt: MCPPrompt) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Image(systemName: "text.bubble")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(prompt.name)
                    .font(.system(.caption, design: .monospaced))
                    .fontWeight(.medium)
            }
            if let desc = prompt.description, !desc.isEmpty {
                Text(desc)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
            }
            if let args = prompt.arguments, !args.isEmpty {
                HStack(spacing: 4) {
                    ForEach(args) { arg in
                        Text(arg.name)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(Color.primary.opacity(0.05))
                            .cornerRadius(3)
                    }
                }
                .padding(.top, 2)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
    }

    // MARK: - Data Loading

    @MainActor
    private func loadTools() async {
        isLoadingTools = true
        toolsError = nil
        do {
            tools = try await dianeAPI.fetchMCPTools(serverName: server.name)
        } catch {
            toolsError = error.localizedDescription
            tools = []
        }
        isLoadingTools = false
    }

    @MainActor
    private func loadPrompts() async {
        isLoadingPrompts = true
        promptsError = nil
        do {
            prompts = try await dianeAPI.fetchMCPPrompts(serverName: server.name)
        } catch {
            promptsError = error.localizedDescription
            prompts = []
        }
        isLoadingPrompts = false
    }
}

// MARK: - Previews

#Preview {
    MCPServersView()
        .environmentObject(AppState())
        .environmentObject(ServerConfiguration())
        .environmentObject(DianeAPIClient())
        .environmentObject(EmergentAPIClient())
        .frame(width: 800, height: 600)
}
