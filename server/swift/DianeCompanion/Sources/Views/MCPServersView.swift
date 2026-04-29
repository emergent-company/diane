import SwiftUI
import OSLog

/// MCP Servers view — reads from Diane's local API (served by `diane serve`) or remote fallback.
/// Supports: tools/prompts inspection, enable/disable toggle, add/edit/delete servers.
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

    // Tools/prompts cache keyed by server name
    @State private var toolsCache: [String: [MCPTool]] = [:]
    @State private var promptsCache: [String: [MCPPrompt]] = [:]
    @State private var loadingTools: Set<String> = []
    @State private var loadingPrompts: Set<String> = []

    // Add/edit sheet
    @State private var showAddSheet = false
    @State private var editingServer: MCPServer? = nil

    var body: some View {
        HSplitView {
            serversList
                .frame(minWidth: 280)

            if let server = selectedServer {
                serverDetailPanel(server)
                    .frame(minWidth: 320)
            } else {
                EmptyStateView(
                    title: "Select a Server",
                    icon: "plug",
                    description: "Select an MCP server to inspect its configuration, tools, and prompts."
                )
                .frame(minWidth: 320)
            }
        }
        .navigationTitle("MCP Servers")
        .task { await load() }
        .sheet(isPresented: $showAddSheet) {
            AddMCPServerSheet(
                server: editingServer,
                onSave: { newServer in
                    await saveServer(newServer)
                }
            )
        }
    }

    // MARK: - Servers List

    @ViewBuilder
    private var serversList: some View {
        VStack(spacing: 0) {
            // Toolbar
            HStack {
                Button("Add Server") {
                    editingServer = nil
                    showAddSheet = true
                }
                .font(.caption)
                .buttonStyle(.borderedProminent)
                .controlSize(.small)

                Spacer()

                Button("Refresh") { Task { await load() } }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)

            Divider()

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
                    description: "No MCP servers configured. Click Add Server to create one."
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
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)

            Divider()

            relayNodesSection
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
        }
    }

    // MARK: - Server Row with Toggle

    private func serverRow(_ server: MCPServer) -> some View {
        HStack(spacing: 8) {
            Toggle(isOn: Binding(
                get: { server.enabled },
                set: { newValue in
                    Task { await toggleServer(server) }
                }
            )) {
                EmptyView()
            }
            .toggleStyle(.switch)
            .controlSize(.small)

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

    // MARK: - Server Detail Panel (Tabbed)

    private enum DetailTab: String, CaseIterable {
        case config = "Config"
        case tools = "Tools"
        case prompts = "Prompts"
    }

    @State private var selectedTab: DetailTab = .config

    private func serverDetailPanel(_ server: MCPServer) -> some View {
        VStack(spacing: 0) {
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

                Button("Edit") {
                    editingServer = server
                    showAddSheet = true
                }
                .font(.caption)
                .buttonStyle(.borderless)

                Button("Delete") {
                    Task { await deleteServer(server) }
                }
                .font(.caption)
                .foregroundStyle(.red)
                .buttonStyle(.borderless)
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            // Tab picker
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
            switch selectedTab {
            case .config:
                configTab(server)
            case .tools:
                toolsTab(server)
            case .prompts:
                promptsTab(server)
            }
        }
    }

    // MARK: - Config Tab

    private func configTab(_ server: MCPServer) -> some View {
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

    // MARK: - Tools Tab

    private func toolsTab(_ server: MCPServer) -> some View {
        Group {
            if loadingTools.contains(server.name) {
                LoadingStateView(message: "Loading tools…")
            } else if let tools = toolsCache[server.name], !tools.isEmpty {
                List(tools) { tool in
                    toolRow(tool)
                }
                .listStyle(.plain)
            } else if toolsCache.keys.contains(server.name) {
                EmptyStateView(
                    title: "No Tools",
                    icon: "wrench",
                    description: "This server has no tools or does not implement tools/list."
                )
            } else {
                EmptyStateView(
                    title: "Load Tools",
                    icon: "wrench",
                    description: "Click refresh to load this server's tools."
                )
                .toolbar {
                    ToolbarItem {
                        Button("Load") {
                            Task { await loadTools(for: server.name) }
                        }
                    }
                }
            }
        }
        .onAppear {
            if !toolsCache.keys.contains(server.name) && !loadingTools.contains(server.name) {
                Task { await loadTools(for: server.name) }
            }
        }
    }

    private func toolRow(_ tool: MCPTool) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: "wrench")
                    .font(.caption)
                    .foregroundStyle(.purple)
                Text(tool.name)
                    .font(.subheadline)
                    .fontWeight(.medium)
                    .lineLimit(1)
                Spacer()
            }

            if let desc = tool.description, !desc.isEmpty {
                Text(desc)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .padding(.vertical, 4)
    }

    // MARK: - Prompts Tab

    private func promptsTab(_ server: MCPServer) -> some View {
        Group {
            if loadingPrompts.contains(server.name) {
                LoadingStateView(message: "Loading prompts…")
            } else if let prompts = promptsCache[server.name], !prompts.isEmpty {
                List(prompts) { prompt in
                    promptRow(prompt)
                }
                .listStyle(.plain)
            } else if promptsCache.keys.contains(server.name) {
                EmptyStateView(
                    title: "No Prompts",
                    icon: "text.quote",
                    description: "This server has no prompts or does not implement prompts/list."
                )
            } else {
                EmptyStateView(
                    title: "Load Prompts",
                    icon: "text.quote",
                    description: "Click refresh to load this server's prompts."
                )
            }
        }
        .onAppear {
            if !promptsCache.keys.contains(server.name) && !loadingPrompts.contains(server.name) {
                Task { await loadPrompts(for: server.name) }
            }
        }
    }

    private func promptRow(_ prompt: MCPPrompt) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: "text.quote")
                    .font(.caption)
                    .foregroundStyle(.orange)
                Text(prompt.name)
                    .font(.subheadline)
                    .fontWeight(.medium)
                    .lineLimit(1)
                Spacer()
            }

            if let desc = prompt.description, !desc.isEmpty {
                Text(desc)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            if let args = prompt.arguments, !args.isEmpty {
                HStack(spacing: 4) {
                    ForEach(args, id: \.name) { arg in
                        Text(arg.name)
                            .font(.caption2)
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(arg.required == true ? Color.blue.opacity(0.1) : Color.secondary.opacity(0.1))
                            .cornerRadius(3)
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }

    // MARK: - Detail Row Helper

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
            servers = try await dianeAPI.fetchMCPServers()
            await loadNodes()
            error = nil
        } catch let localError {
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

    @MainActor
    private func loadTools(for serverName: String) async {
        loadingTools.insert(serverName)
        do {
            let tools = try await dianeAPI.fetchMCPTools(serverName: serverName)
            toolsCache[serverName] = tools
        } catch {
            logger.warning("Failed to load tools for \(serverName): \(error.localizedDescription)")
            toolsCache[serverName] = []
        }
        loadingTools.remove(serverName)
    }

    @MainActor
    private func loadPrompts(for serverName: String) async {
        loadingPrompts.insert(serverName)
        do {
            let prompts = try await dianeAPI.fetchMCPPrompts(serverName: serverName)
            promptsCache[serverName] = prompts
        } catch {
            logger.warning("Failed to load prompts for \(serverName): \(error.localizedDescription)")
            promptsCache[serverName] = []
        }
        loadingPrompts.remove(serverName)
    }

    // MARK: - CRUD Actions

    @MainActor
    private func toggleServer(_ server: MCPServer) async {
        do {
            _ = try await dianeAPI.toggleMCPServer(serverName: server.name)
            // Reload server list to reflect new state
            servers = try await dianeAPI.fetchMCPServers()
            // Clear caches — toggling may change available tools
            toolsCache.removeValue(forKey: server.name)
            promptsCache.removeValue(forKey: server.name)
        } catch {
            logger.error("Toggle failed for \(server.name): \(error.localizedDescription)")
        }
    }

    @MainActor
    private func saveServer(_ server: MCPServer) async {
        do {
            try await dianeAPI.saveMCPServer(server)
            await load()
            selectedServer = servers.first(where: { $0.name == server.name })
        } catch {
            logger.error("Save failed for \(server.name): \(error.localizedDescription)")
        }
    }

    @MainActor
    private func deleteServer(_ server: MCPServer) async {
        do {
            try await dianeAPI.deleteMCPServer(serverName: server.name)
            if selectedServer?.name == server.name {
                selectedServer = nil
            }
            await load()
        } catch {
            logger.error("Delete failed for \(server.name): \(error.localizedDescription)")
        }
    }
}

// MARK: - Add/Edit MCP Server Sheet

struct AddMCPServerSheet: View {
    @Environment(\.dismiss) private var dismiss

    let server: MCPServer?
    let onSave: (MCPServer) async -> Void

    @State private var name: String = ""
    @State private var type: String = "stdio"
    @State private var command: String = ""
    @State private var args: String = ""
    @State private var url: String = ""
    @State private var timeout: Int = 60
    @State private var enabled: Bool = true
    @State private var envEntries: [(id: UUID, key: String, value: String)] = []
    @State private var isSaving = false
    @State private var error: String? = nil

    private let serverTypes = ["stdio", "http", "streamable-http", "sse"]

    init(server: MCPServer?, onSave: @escaping (MCPServer) async -> Void) {
        self.server = server
        self.onSave = onSave

        _name = State(initialValue: server?.name ?? "")
        _type = State(initialValue: server?.type ?? "stdio")
        _command = State(initialValue: server?.command ?? "")
        _args = State(initialValue: server?.args?.joined(separator: " ") ?? "")
        _url = State(initialValue: server?.url ?? "")
        _timeout = State(initialValue: server?.timeout ?? 60)
        _enabled = State(initialValue: server?.enabled ?? true)
        if let env = server?.env {
            _envEntries = State(initialValue: env.map { (UUID(), $0.key, $0.value) })
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(server == nil ? "Add MCP Server" : "Edit MCP Server")
                    .font(.headline)
                Spacer()
                Button("Cancel") { dismiss() }
                    .buttonStyle(.borderless)
            }
            .padding()

            Divider()

            ScrollView {
                VStack(spacing: 12) {
                    Form {
                        TextField("Server Name", text: $name)
                            .textFieldStyle(.roundedBorder)

                        Picker("Type", selection: $type) {
                            ForEach(serverTypes, id: \.self) { t in
                                Text(t.uppercased()).tag(t)
                            }
                        }
                        .pickerStyle(.segmented)

                        if type == "stdio" {
                            TextField("Command", text: $command)
                                .textFieldStyle(.roundedBorder)
                            TextField("Arguments (space-separated)", text: $args)
                                .textFieldStyle(.roundedBorder)
                        } else {
                            TextField("URL", text: $url)
                                .textFieldStyle(.roundedBorder)
                        }

                        HStack {
                            Stepper("Timeout: \(timeout)s", value: $timeout, in: 0...300)
                        }

                        Toggle("Enabled", isOn: $enabled)
                    }
                    .padding(.horizontal)

                    // Environment variables
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Environment Variables")
                            .font(.subheadline)
                            .fontWeight(.medium)

                        ForEach(envEntries.indices, id: \.self) { idx in
                            HStack(spacing: 8) {
                                TextField("Key", text: $envEntries[idx].key)
                                    .textFieldStyle(.roundedBorder)
                                    .frame(width: 120)
                                TextField("Value", text: $envEntries[idx].value)
                                    .textFieldStyle(.roundedBorder)
                                Button {
                                    envEntries.remove(at: idx)
                                } label: {
                                    Image(systemName: "minus.circle.fill")
                                        .foregroundStyle(.red)
                                }
                                .buttonStyle(.borderless)
                            }
                        }

                        Button {
                            envEntries.append((UUID(), "", ""))
                        } label: {
                            Label("Add Variable", systemImage: "plus.circle")
                                .font(.caption)
                        }
                        .buttonStyle(.borderless)
                    }
                    .padding(.horizontal)

                    if let err = error {
                        Text(err)
                            .font(.caption)
                            .foregroundStyle(.red)
                            .padding(.horizontal)
                    }
                }
                .padding(.vertical)
            }

            Divider()

            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .buttonStyle(.bordered)
                Button("Save") {
                    Task { await save() }
                }
                .buttonStyle(.borderedProminent)
                .disabled(name.isEmpty || isSaving)
            }
            .padding()
        }
        .frame(width: 500, height: 500)
    }

    private func save() async {
        guard !name.isEmpty else { return }
        isSaving = true
        error = nil

        var env: [String: String]? = nil
        let nonEmpty = envEntries.filter { !$0.key.isEmpty }
        if !nonEmpty.isEmpty {
            env = [:]
            for entry in nonEmpty {
                env![entry.key] = entry.value
            }
        }

        var parsedArgs: [String]? = nil
        if !args.trimmingCharacters(in: .whitespaces).isEmpty {
            parsedArgs = args.components(separatedBy: " ").filter { !$0.isEmpty }
        }

        let newServer = MCPServer(
            name: name,
            enabled: enabled,
            type: type,
            url: type == "stdio" ? nil : url,
            command: type == "stdio" ? command : nil,
            args: type == "stdio" ? parsedArgs : nil,
            env: env,
            timeout: timeout
        )

        await onSave(newServer)
        isSaving = false
        dismiss()
    }
}
