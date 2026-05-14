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
        .task { await autoRefreshLoop() }
    }

    /// Refreshes server list every 30 seconds while the view is visible.
    private func autoRefreshLoop() async {
        while !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 30_000_000_000)
            guard !Task.isCancelled else { break }
            await load()
        }
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
                .refreshable { await load() }
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
                .fill(server.statusColor)
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 2) {
                Text(server.name)
                    .font(.subheadline)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    Text(server.type.uppercased())
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    Text(server.statusLabel)
                        .font(.caption2)
                        .foregroundStyle(server.statusColor)
                }
            }
            Spacer()
        }
        .padding(.vertical, 2)
    }

    // MARK: - Server Detail Panel

    private func serverDetailPanel(_ server: MCPServer) -> some View {
        MCPServerDetailView(server: server, dianeAPI: dianeAPI, nodes: nodes)
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
            // Retry once after brief delay — local diane serve may still be starting
            logWarning("Local API failed: \(localError.localizedDescription), retrying in 2s...", category: "MCPView")
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            do {
                servers = try await dianeAPI.fetchMCPServers()
                error = nil
            } catch {
                // MCP servers are local-only configuration — remote fallback won't work
                // (the remote /api/admin/mcp-servers endpoint requires admin:read scope
                // which client tokens don't have)
                self.error = "Cannot load MCP servers: local diane serve not reachable. Try restarting the app."
            }
            await loadNodes()
        }
        isLoading = false
    }

    /// Create a user-friendly error message from MCP API failures.
    /// Detects Memory Platform 502/500 and returns a clear message.
    private func formatMCPError(_ local: Error, remoteError: Error) -> String {
        // Check if the underlying issue is MP being unreachable
        let localDesc = local.localizedDescription.lowercased()
        let remoteDesc = remoteError.localizedDescription.lowercased()
        let isMPDown = localDesc.contains("502") || localDesc.contains("mp returned") ||
                       remoteDesc.contains("502") || remoteDesc.contains("bad gateway")

        if isMPDown {
            return "Memory Platform unavailable (502). Try again later."
        }
        return "Cannot load MCP servers: \(remoteError.localizedDescription)"
    }

    /// Format a nodes fetch error into a user-friendly message.
    private func formatMCPNodeError(_ error: Error) -> String {
        let desc = error.localizedDescription.lowercased()
        if desc.contains("502") || desc.contains("mp returned") || desc.contains("bad gateway") {
            return "Memory Platform unavailable (502). Cannot load relay nodes."
        }
        return "Failed to load relay nodes: \(error.localizedDescription)"
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
                    RelayNode(instanceID: r.instanceID ?? r.id, hostname: r.nodeName, mode: nil, version: nil, toolCount: r.toolCount, connectedAt: r.connectedAt, online: false, uptime: nil, provider: nil, relayActive: nil, botActive: nil, healthy: nil)
                }
                nodeError = nil
            } catch {
                nodeError = formatMCPNodeError(error)
            }
        }
        isLoadingNodes = false
    }
}

// MARK: - Server Detail View with Tools/Prompts Tabs

private struct MCPServerDetailView: View {
    let server: MCPServer
    let dianeAPI: DianeAPIClient
    let nodes: [RelayNode]

    @State private var selectedTab: DetailTab = .connection
    @State private var tools: [MCPTool] = []
    @State private var prompts: [MCPPrompt] = []
    @State private var isLoadingTools = false
    @State private var isLoadingPrompts = false
    @State private var toolsError: String? = nil
    @State private var promptsError: String? = nil

    // Logs state
    @State private var logs: [MCPServerLogEntry] = []
    @State private var isLoadingLogs = false
    @State private var logsError: String? = nil

    // Auth state
    @State private var authStatus: String? = nil
    @State private var authIsLoading = false
    @State private var authError: String? = nil

    // Scope state
    @State private var newScope: String = ""
    @State private var isUpdatingScope = false
    @State private var scopeUpdateError: String? = nil

    private enum DetailTab: String, CaseIterable {
        case connection = "Connection"
        case tools = "Tools"
        case prompts = "Prompts"
        case logs = "Logs"
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
                            .fill(server.statusColor)
                            .frame(width: 7, height: 7)
                        Text(server.statusLabel)
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
                Group {
                    switch selectedTab {
                    case .connection:
                        connectionContent
                    case .tools:
                        toolsContent
                    case .prompts:
                        promptsContent
                    case .logs:
                        logsContent
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task(id: server.name) {
            await loadTools()
            await loadPrompts()
        }
        .onAppear {
            newScope = server.scope ?? "all"
        }
    }

    // MARK: - Scope Update

    @MainActor
    private func updateScope() async {
        scopeUpdateError = nil
        isUpdatingScope = true
        do {
            try await dianeAPI.updateMCPServerScope(serverName: server.name, scope: newScope)
            isUpdatingScope = false
        } catch {
            scopeUpdateError = error.localizedDescription
            isUpdatingScope = false
        }
    }

    // MARK: - Connection Tab

    @ViewBuilder
    private var connectionContent: some View {
        VStack(alignment: .leading, spacing: 0) {
            connectionRow(label: "Type", value: server.type.uppercased())
            connectionRow(label: "Bound To", value: server.scopeLabel)
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

            // Node binding (scope) picker
            Divider().padding(.horizontal, 12)
            VStack(alignment: .leading, spacing: 6) {
                Text("Node Binding")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.top, 4)

                Picker("Scope", selection: $newScope) {
                    Text("All nodes").tag("all")
                    Text("All slave nodes").tag("slave:*")
                    ForEach(nodes.filter { $0.mode != "master" }) { node in
                        Text("\(node.hostname ?? node.instanceID)").tag("instance:\(node.instanceID)")
                    }
                }
                .pickerStyle(.radioGroup)
                .padding(.horizontal, 12)
                .disabled(isUpdatingScope)

                if isUpdatingScope {
                    HStack(spacing: 4) {
                        ProgressView().controlSize(.mini)
                        Text("Updating...")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.horizontal, 12)
                } else if scopeUpdateError != nil {
                    HStack(spacing: 4) {
                        Image(systemName: "exclamationmark.triangle")
                            .foregroundStyle(.orange)
                            .font(.caption)
                        Text(scopeUpdateError!)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.horizontal, 12)
                }

                Button("Update") {
                    Task { await updateScope() }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
                .disabled(isUpdatingScope || newScope == server.scope ?? "all")
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

            // Authentication section for HTTP servers
            if server.type.lowercased() == "http" || server.type.lowercased() == "streamable-http" || server.type.lowercased() == "sse" {
                Divider().padding(.horizontal, 12)
                authSection
            }
        }
        .padding(.vertical, 4)
    }

    // MARK: - Authentication Section

    @ViewBuilder
    private var authSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Authentication")
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 12)
                .padding(.top, 4)

            if let status = authStatus {
                HStack(spacing: 6) {
                    if authIsLoading {
                        ProgressView().controlSize(.mini)
                    }
                    Text(status)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 12)
            }

            if let err = authError {
                HStack(spacing: 6) {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                        .font(.caption)
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 12)
            }

            if !authIsLoading {
                Button {
                    Task { await startAuth() }
                } label: {
                    Label("Re-authenticate", systemImage: "person.badge.key")
                        .font(.caption)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
            } else {
                Button {} label: {
                    Label("Authenticating...", systemImage: "person.badge.key")
                        .font(.caption)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .disabled(true)
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
            }
        }
    }

    @MainActor
    private func startAuth() async {
        authIsLoading = true
        authError = nil
        authStatus = nil

        do {
            let (status, authURL) = try await dianeAPI.startMCPServerAuth(serverName: server.name)
            guard status == "pending", let urlStr = authURL, let url = URL(string: urlStr) else {
                authError = "Failed to start authentication (status: \(status))"
                authIsLoading = false
                return
            }

            // Open the browser
            NSWorkspace.shared.open(url)

            authStatus = "Waiting for browser authorization..."

            // Poll for completion (every 2 seconds, up to 5 minutes)
            let pollStart = Date()
            while Date().timeIntervalSince(pollStart) < 300 {
                try await Task.sleep(nanoseconds: 2_000_000_000)
                let (checkStatus, expiresAt, checkError) = try await dianeAPI.checkMCPServerAuthStatus(serverName: server.name)
                if checkStatus == "completed" {
                    if let exp = expiresAt, !exp.isEmpty {
                        authStatus = "Authenticated (expires: \(exp))"
                    } else {
                        authStatus = "Authenticated"
                    }
                    authIsLoading = false
                    return
                }
                if checkStatus == "failed" {
                    authError = checkError ?? "Authentication failed"
                    authIsLoading = false
                    return
                }
            }
            authError = "Authentication timed out"
        } catch {
            authError = error.localizedDescription
        }
        authIsLoading = false
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
                            .badgeStyle(color: .secondary)
                    }
                }
                .padding(.top, 2)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
    }

    // MARK: - Logs Tab

    @ViewBuilder
    private var logsContent: some View {
        VStack(alignment: .leading, spacing: 4) {
            if isLoadingLogs {
                HStack {
                    Spacer()
                    ProgressView("Loading logs…")
                        .controlSize(.small)
                        .padding(20)
                    Spacer()
                }
            } else if let err = logsError {
                HStack {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Retry") { Task { await loadLogs() } }
                        .font(.caption)
                        .buttonStyle(.borderless)
                }
                .padding(12)
            } else if logs.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "doc.text")
                        .font(.title2)
                        .foregroundStyle(.tertiary)
                    Text("No logs")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text("Log entries appear here after the server processes requests.")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity)
                .padding(24)
            } else {
                Text("\(logs.count) log\(logs.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.top, 8)

                ScrollViewReader { proxy in
                    List(logs) { entry in
                        logRow(entry)
                    }
                    .listStyle(.plain)
                    .onChange(of: logs.count) { _ in
                        withAnimation { proxy.scrollTo(logs.last?.id, anchor: .bottom) }
                    }
                }
            }
        }
        .onAppear { Task { await loadLogs() } }
        .onChange(of: selectedTab) { newTab in
            if newTab == .logs { Task { await loadLogs() } }
        }
    }

    private func logRow(_ entry: MCPServerLogEntry) -> some View {
        HStack(alignment: .top, spacing: 8) {
            Text(formatLogTime(entry.time))
                .font(.system(.caption2, design: .monospaced))
                .foregroundStyle(.tertiary)
                .frame(width: 80, alignment: .trailing)
            Text(entry.message)
                .font(.system(.caption, design: .monospaced))
                .lineLimit(nil)
                .textSelection(.enabled)
        }
        .padding(.vertical, 2)
        .padding(.horizontal, 8)
        .listRowInsets(EdgeInsets())
        .listRowSeparator(.visible)
    }

    private func formatLogTime(_ iso: String) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: iso) {
            let timeFormatter = DateFormatter()
            timeFormatter.dateFormat = "HH:mm:ss"
            return timeFormatter.string(from: date)
        }
        formatter.formatOptions = [.withInternetDateTime]
        if let date = formatter.date(from: iso) {
            let timeFormatter = DateFormatter()
            timeFormatter.dateFormat = "HH:mm:ss"
            return timeFormatter.string(from: date)
        }
        return String(iso.suffix(8).prefix(8))
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

    @MainActor
    private func loadLogs() async {
        isLoadingLogs = true
        logsError = nil
        do {
            logs = try await dianeAPI.fetchMCPLogs(serverName: server.name)
        } catch {
            logsError = error.localizedDescription
            logs = []
        }
        isLoadingLogs = false
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

