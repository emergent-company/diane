import SwiftUI

/// Agents view — lists agent definitions from the Memory Platform,
/// with full detail panel showing tools, skills, model config, and runtime settings.
/// Supports editing overrides for built-in agents, creating/cloning/deleting agents.
struct AgentsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var agents: [AgentDef] = []
    @State private var selectedAgent: AgentDef? = nil
    @State private var agentDetail: AgentDetail? = nil
    @State private var overrideConfig: AgentOverrideConfig? = nil
    @State private var isLoading = false
    @State private var isLoadingDetail = false
    @State private var error: String? = nil

    // Sheets
    @State private var showOverrideEditor = false
    @State private var showCreateSheet = false
    @State private var showCloneSheet = false
    @State private var cloneName: String = ""
    @State private var showDeleteConfirm = false

    // Built-in agent names from the Go registry
    private let builtInAgentNames: Set<String> = [
        "diane-default", "diane-researcher", "diane-agent-creator",
        "diane-schema-designer", "diane-mcp-manager", "diane-session-extractor",
        "diane-entity-extractor", "diane-graph-merger", "diane-codebase",
        "diane-dreamer", "diane-skill-monitor"
    ]

    var body: some View {
        SplitListDetailView(
            emptyTitle: "Select an Agent",
            emptyIcon: "brain.head.profile",
            emptyDescription: "Select an agent definition to view its configuration.",
            listContent: { agentsList },
            detailContent: {
                if let agent = selectedAgent {
                    agentDetailPanel(agent)
                } else {
                    EmptyStateView(
                        title: "Select an Agent",
                        icon: "brain.head.profile",
                        description: "Select an agent definition to view its configuration."
                    )
                }
            }
        )
        .navigationTitle("Agents")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("New Agent") { showCreateSheet = true }
            }
        }
        .task { await load() }
        .sheet(isPresented: $showOverrideEditor) {
            if let agent = selectedAgent {
                AgentOverrideEditorView(
                    agentName: agent.name,
                    agentDetail: agentDetail,
                    existingOverride: overrideConfig,
                    onSave: { oc in
                        try? await dianeAPI.saveAgentOverride(name: agent.name, override: oc)
                        _ = try? await dianeAPI.seedAgents()
                        await loadDetail()
                    },
                    onDelete: {
                        try? await dianeAPI.deleteAgentOverride(name: agent.name)
                        _ = try? await dianeAPI.seedAgents()
                        overrideConfig = nil
                        await loadDetail()
                    },
                    onClose: { showOverrideEditor = false }
                )
            }
        }
        .sheet(isPresented: $showCreateSheet) {
            AgentCreateView(
                onCreate: { req in
                    _ = try await dianeAPI.createAgent(req)
                    showCreateSheet = false
                    await load()
                },
                onClose: { showCreateSheet = false }
            )
        }
        .alert("Clone Agent", isPresented: $showCloneSheet) {
            TextField("New name", text: $cloneName)
            Button("Clone") {
                Task {
                    if let agent = selectedAgent {
                        let name = cloneName.trimmingCharacters(in: .whitespaces)
                        guard !name.isEmpty else { return }
                        _ = try? await dianeAPI.cloneAgent(name: agent.name, newName: name)
                        cloneName = ""
                        await load()
                    }
                }
            }
            Button("Cancel", role: .cancel) { cloneName = "" }
        } message: {
            if let agent = selectedAgent {
                Text("Create a copy of \"\(agent.name)\" as a new user-defined agent.")
            }
        }
        .alert("Delete Agent", isPresented: $showDeleteConfirm) {
            Button("Delete", role: .destructive) {
                Task {
                    if let agent = selectedAgent {
                        let status = try? await dianeAPI.deleteAgent(name: agent.name)
                        if status == "disabled" || status == "deleted" {
                            selectedAgent = nil
                            agentDetail = nil
                            overrideConfig = nil
                            await load()
                        }
                    }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            if let agent = selectedAgent {
                if builtInAgentNames.contains(agent.name) {
                    Text("Disable built-in agent \"\(agent.name)\"? It will be skipped during seeding. Re-enable by removing the override.")
                } else {
                    Text("Delete agent \"\(agent.name)\"? This cannot be undone.")
                }
            }
        }
    }

    // MARK: - Agents List

    @ViewBuilder
    private var agentsList: some View {
        VStack(spacing: 0) {
            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding(8)
            }

            if isLoading && agents.isEmpty {
                LoadingStateView(message: "Loading agents…")
            } else if agents.isEmpty {
                EmptyStateView(
                    title: "No Agents",
                    icon: "brain.head.profile",
                    description: "No agent definitions found."
                )
            } else {
                List(agents, selection: $selectedAgent) { agent in
                    agentRow(agent)
                        .tag(agent)
                        .onChange(of: selectedAgent?.id) { _ in
                            Task { await loadDetail() }
                        }
                }
                .listStyle(.plain)
            }

            Divider()
            HStack {
                Text("\(agents.count) agent\(agents.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Button("Refresh") { Task { await load() } }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
    }

    private func agentRow(_ agent: AgentDef) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            Image(systemName: agentIcon(agent.flowType))
                .font(.system(size: Design.IconSize.small))
                .foregroundStyle(agentColor(agent.flowType))
                .frame(width: 20, height: 20)

            VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                HStack(spacing: Design.Spacing.xs) {
                    Text(agent.name)
                        .font(.subheadline)
                        .fontWeight(.medium)
                        .lineLimit(1)
                    if agent.isDefault {
                        Text("default")
                            .font(.system(size: Design.IconSize.tiny, weight: .medium))
                            .badgeStyle(color: .blue)
                    }
                    flowBadge(agent.flowType)
                    if builtInAgentNames.contains(agent.name) {
                        Text("built-in")
                            .font(.system(size: 8, weight: .medium))
                            .badgeStyle(color: .secondary)
                    }
                }
                HStack(spacing: Design.Spacing.sm) {
                    if let desc = agent.description, !desc.isEmpty {
                        Text(desc)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    Spacer()
                    HStack(spacing: 3) {
                        Image(systemName: "wrench")
                            .font(.system(size: 9))
                        Text("\(agent.toolCount)")
                            .font(.caption2)
                    }
                    .foregroundStyle(.tertiary)
                    if let date = agent.updatedAt ?? agent.createdAt {
                        Text(DateUtils.formatTimestamp(date))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
        }
        .padding(.vertical, Design.Spacing.xxs)
        .contentShape(Rectangle())
        .onTapGesture {
            selectedAgent = agent
            Task { await loadDetail() }
        }
    }

    private func flowBadge(_ flow: String) -> some View {
        Text(flow.isEmpty ? "chat" : flow)
            .font(.system(size: Design.IconSize.tiny, weight: .medium))
            .badgeStyle(color: agentColor(flow))
    }

    private func agentColor(_ flow: String) -> Color {
        switch flow.lowercased() {
        case "chat", "": return .green
        case "agent":    return .purple
        case "chain":    return .orange
        case "workflow": return .blue
        default:         return .secondary
        }
    }

    private func agentIcon(_ flow: String) -> String {
        switch flow.lowercased() {
        case "chat", "": return "message"
        case "agent":    return "brain.head.profile"
        case "chain":    return "link"
        case "workflow": return "arrow.triangle.branch"
        default:         return "gearshape"
        }
    }

    // MARK: - Agent Detail Panel

    @ViewBuilder
    private func agentDetailPanel(_ agent: AgentDef) -> some View {
        if isLoadingDetail {
            VStack {
                Spacer()
                LoadingStateView(message: "Loading agent details…")
                Spacer()
            }
        } else if let detail = agentDetail {
            detailContent(detail)
        } else {
            fallbackDetail(agent)
        }
    }

    @ViewBuilder
    private func detailContent(_ detail: AgentDetail) -> some View {
        VStack(spacing: 0) {
            List {
                // ── Identity ──
                Section("Agent") {
                    HStack {
                        Text(detail.name)
                            .font(.title2)
                            .fontWeight(.semibold)
                        Spacer()
                        if detail.isDefault {
                            Text("default")
                                .font(.caption)
                                .fontWeight(.medium)
                                .badgeStyle(color: .blue)
                        }
                        if builtInAgentNames.contains(detail.name) {
                            Text("built-in")
                                .font(.caption)
                                .fontWeight(.medium)
                                .badgeStyle(color: .secondary)
                        }
                        flowBadge(detail.flowType)
                    }

                    if let desc = detail.description, !desc.isEmpty {
                        Text(desc)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }

                    detailRow(label: "Visibility", value: detail.visibility)
                    if let dm = detail.dispatchMode, !dm.isEmpty {
                        detailRow(label: "Dispatch", value: dm)
                    }

                    if let oc = overrideConfig {
                        HStack {
                            Text("Overrides Active")
                                .font(.caption)
                                .foregroundStyle(.orange)
                            if oc.disabled == true {
                                Text("DISABLED")
                                    .font(.caption)
                                    .fontWeight(.bold)
                                    .foregroundStyle(.red)
                            }
                        }
                    }
                }

                // ── Runtime Limits ──
                Section("Runtime") {
                    if let ms = detail.maxSteps {
                        detailRow(label: "Max Steps", value: "\(ms)")
                    }
                    if let to = detail.defaultTimeout {
                        detailRow(label: "Timeout", value: "\(to)s")
                    }
                }

                // ── Model ──
                Section("Model") {
                    if let model = detail.model {
                        if let name = model.name, !name.isEmpty {
                            detailRow(label: "Name", value: name)
                        } else {
                            Text("Project default")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        if let temp = model.temperature {
                            detailRow(label: "Temperature", value: String(format: "%.2f", temp))
                        }
                        if let mt = model.maxTokens {
                            detailRow(label: "Max Tokens", value: "\(mt)")
                        }
                    } else {
                        Text("Project default (no override)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                // ── Tools ──
                Section {
                    if let tools = detail.tools, !tools.isEmpty {
                        ForEach(tools, id: \.self) { tool in
                            HStack(spacing: 6) {
                                Image(systemName: "wrench.fill")
                                    .font(.system(size: 9))
                                    .foregroundStyle(.secondary)
                                Text(tool)
                                    .font(.system(.caption, design: .monospaced))
                            }
                        }
                    } else {
                        Text("No tools configured")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                } header: {
                    HStack {
                        Text("Tools")
                        if detail.toolCount > 0 {
                            Text("(\(detail.toolCount))")
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                // ── Skills ──
                Section {
                    if let skills = detail.skills, !skills.isEmpty {
                        ForEach(skills, id: \.self) { skill in
                            HStack(spacing: 6) {
                                Image(systemName: "book.fill")
                                    .font(.system(size: 9))
                                    .foregroundStyle(.secondary)
                                Text(skill)
                                    .font(.system(.caption, design: .monospaced))
                            }
                        }
                    } else {
                        Text("None")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                } header: {
                    HStack {
                        Text("Skills")
                        if let s = detail.skills {
                            Text("(\(s.count))")
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                // ── System Prompt ──
                if let sp = detail.systemPrompt, !sp.isEmpty {
                    Section("System Prompt (\(sp.count) chars)") {
                        Text(sp)
                            .font(.system(.caption2, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineLimit(nil)
                            .textSelection(.enabled)
                    }
                }

                // ── Timestamps ──
                Section("Timestamps") {
                    if let created = detail.createdAt {
                        detailRow(label: "Created", value: DateUtils.formatTimestamp(created))
                    }
                    if let updated = detail.updatedAt {
                        detailRow(label: "Updated", value: DateUtils.formatTimestamp(updated))
                    }
                }
            }
            .listStyle(.sidebar)

            // ── Action Bar ──
            VStack(spacing: 0) {
                Divider()
                HStack(spacing: 8) {
                    if builtInAgentNames.contains(detail.name) {
                        Button(overrideConfig != nil ? "Edit Override" : "Override") {
                            showOverrideEditor = true
                        }
                        .buttonStyle(.bordered)
                        .help("Override built-in agent configuration via graph config")
                    } else {
                        Button("Edit") {
                            // For user-defined agents — show edit sheet (simplified)
                            Task {
                                // Future: build an edit sheet for user-defined agents
                            }
                        }
                        .buttonStyle(.bordered)
                    }

                    Button("Clone") {
                        cloneName = detail.name + "-copy"
                        showCloneSheet = true
                    }
                    .buttonStyle(.bordered)

                    Spacer()

                    Button("Delete", role: .destructive) {
                        showDeleteConfirm = true
                    }
                    .buttonStyle(.bordered)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }
        }
    }

    /// Fallback detail panel using summary-only AgentDef when detail fetch fails.
    private func fallbackDetail(_ agent: AgentDef) -> some View {
        VStack(spacing: 0) {
            List {
                Section("Agent") {
                    HStack {
                        Text(agent.name)
                            .font(.title2)
                            .fontWeight(.semibold)
                        Spacer()
                        if agent.isDefault {
                            Text("default")
                                .font(.caption)
                                .fontWeight(.medium)
                                .badgeStyle(color: .blue)
                        }
                        if builtInAgentNames.contains(agent.name) {
                            Text("built-in")
                                .font(.caption)
                                .fontWeight(.medium)
                                .badgeStyle(color: .secondary)
                        }
                        flowBadge(agent.flowType)
                    }

                    if let desc = agent.description, !desc.isEmpty {
                        Text(desc)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }

                    detailRow(label: "Visibility", value: agent.visibility)
                    detailRow(label: "Tool Count", value: "\(agent.toolCount)")
                }

                Section("Timestamps") {
                    if let created = agent.createdAt {
                        detailRow(label: "Created", value: DateUtils.formatTimestamp(created))
                    }
                    if let updated = agent.updatedAt {
                        detailRow(label: "Updated", value: DateUtils.formatTimestamp(updated))
                    }
                }
            }
            .listStyle(.sidebar)

            // Action bar even in fallback
            VStack(spacing: 0) {
                Divider()
                HStack(spacing: 8) {
                    if builtInAgentNames.contains(agent.name) {
                        Button(overrideConfig != nil ? "Edit Override" : "Override") {
                            showOverrideEditor = true
                        }
                        .buttonStyle(.bordered)
                    }
                    Button("Clone") {
                        cloneName = agent.name + "-copy"
                        showCloneSheet = true
                    }
                    .buttonStyle(.bordered)
                    Spacer()
                    Button("Delete", role: .destructive) {
                        showDeleteConfirm = true
                    }
                    .buttonStyle(.bordered)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }
        }
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 90, alignment: .leading)
            Text(value)
                .font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)
            Spacer()
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        agents = (try? await dianeAPI.fetchAgentDefs()) ?? []
        if agents.isEmpty {
            agents = (try? await apiClient.fetchAgentDefs(projectID: serverConfig.projectID)) ?? []
        }
        isLoading = false
    }

    @MainActor
    private func loadDetail() async {
        guard let agent = selectedAgent else { return }
        isLoadingDetail = true
        agentDetail = nil
        overrideConfig = nil

        // Fetch detail
        do {
            agentDetail = try await dianeAPI.fetchAgentDetail(name: agent.name)
        } catch {
            agentDetail = nil
        }

        // Fetch override (if built-in)
        if builtInAgentNames.contains(agent.name) {
            overrideConfig = try? await dianeAPI.fetchAgentOverride(name: agent.name)
        }

        isLoadingDetail = false
    }
}

// MARK: - Previews

#Preview {
    AgentsView()
        .environmentObject(AppState())
        .environmentObject(DianeAPIClient())
        .environmentObject(ServerConfiguration())
        .environmentObject(EmergentAPIClient())
        .frame(width: 800, height: 600)
}
