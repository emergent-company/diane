import SwiftUI

/// Agents view — lists agent definitions from the Memory Platform.
/// The detail panel is a live form: edit fields directly, save persists
/// to the right backend (override config for built-in, direct PATCH for user-defined).
struct AgentsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var agents: [AgentDef] = []
    @State private var selectedAgent: AgentDef? = nil
    @State private var agentDetail: AgentDetail? = nil
    @State private var overrideConfig: AgentOverrideConfig? = nil

    // Editable form state
    @State private var editSystemPrompt: String = ""
    @State private var editSkills: String = ""
    @State private var editModelName: String = ""
    @State private var editModelProvider: String = ""
    @State private var editTemperature: String = ""
    @State private var editMaxTokens: String = ""
    @State private var editMaxSteps: String = ""
    @State private var editTimeout: String = ""
    @State private var editVisibility: String = "project"
    @State private var editDisabled: Bool = false
    @State private var editSandboxEnabled: Bool = false
    @State private var editHasChanges: Bool = false

    @State private var isLoading = false
    @State private var isLoadingDetail = false
    @State private var isSaving = false
    @State private var error: String?
    @State private var saveStatus: String?

    // Provider/model picker state
    @State private var availableProviders: [OrgProviderConfig] = []
    @State private var availableModels: [ProviderModel] = []
    @State private var isLoadingProviders = false

    // Sheets
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

    private var isBuiltIn: Bool {
        guard let agent = selectedAgent else { return false }
        return builtInAgentNames.contains(agent.name)
    }

    var body: some View {
        SplitListDetailView(
            emptyTitle: "Select an Agent",
            emptyIcon: "brain.head.profile",
            emptyDescription: "Select an agent to view and edit its configuration.",
            listContent: { agentsList },
            detailContent: {
                if let _ = selectedAgent {
                    agentEditorPanel
                } else {
                    EmptyStateView(
                        title: "Select an Agent",
                        icon: "brain.head.profile",
                        description: "Select an agent to view and edit its configuration."
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
                    Text("Disable built-in agent \"\(agent.name)\"? It will be skipped during seeding.")
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
                .foregroundStyle(StatusColors.agentFlow(agent.flowType))
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
            .badgeStyle(color: StatusColors.agentFlow(flow))
    }

    func agentColor(_ flow: String) -> Color { StatusColors.agentFlow(flow) }

    func agentIcon(_ flow: String) -> String {
        switch flow.lowercased() {
        case "chat", "": return "message"
        case "agent":    return "brain.head.profile"
        case "chain":    return "link"
        case "workflow": return "arrow.triangle.branch"
        default:         return "gearshape"
        }
    }

    // MARK: - Agent Editor (Inline Form)

    @ViewBuilder
    private var agentEditorPanel: some View {
        if isLoadingDetail {
            VStack {
                Spacer()
                LoadingStateView(message: "Loading agent details…")
                Spacer()
            }
        } else {
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        editorForm
                    }
                }

                Divider()

                // Action bar: save + clone + delete
                HStack(spacing: 8) {
                    if isSaving {
                        ProgressView()
                            .scaleEffect(0.7)
                            .frame(width: 16)
                    }
                    if let status = saveStatus {
                        Text(status)
                            .font(.caption)
                            .foregroundStyle(status.contains("✅") ? .green : .secondary)
                    }
                    if editHasChanges {
                        Button("Revert") {
                            populateForm(from: agentDetail, override: overrideConfig)
                        }
                        .buttonStyle(.borderless)
                    }

                    Spacer()

                    if editHasChanges || true {
                        Button("Save Changes") {
                            Task { await saveChanges() }
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(isSaving)
                    }

                    Button("Clone") {
                        if let agent = selectedAgent {
                            cloneName = agent.name + "-copy"
                            showCloneSheet = true
                        }
                    }
                    .buttonStyle(.bordered)

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

    @ViewBuilder
    private var editorForm: some View {
        if let agent = selectedAgent {
            // Header
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(agent.name)
                        .font(.title2)
                        .fontWeight(.semibold)
                    Spacer()
                    if agent.isDefault {
                        Text("default")
                            .font(.caption).fontWeight(.medium)
                            .badgeStyle(color: .blue)
                    }
                    if isBuiltIn {
                        Text("built-in")
                            .font(.caption).fontWeight(.medium)
                            .badgeStyle(color: .secondary)
                    }
                    if overrideConfig?.disabled == true {
                        Text("DISABLED")
                            .font(.caption).fontWeight(.bold)
                            .foregroundStyle(.red)
                    }
                    flowBadge(agent.flowType)
                }

                if let desc = agent.description, !desc.isEmpty {
                    Text(desc)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 8)

            // ── System Prompt ──
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text("System Prompt")
                        .font(.caption).foregroundStyle(.secondary)
                    TextEditor(text: $editSystemPrompt)
                        .font(.system(.caption, design: .monospaced))
                        .frame(minHeight: 100)
                        .border(Color.secondary.opacity(0.3))
                        .overlay(alignment: .topTrailing) {
                            Text("\(editSystemPrompt.count) chars")
                                .font(.system(size: 9))
                                .foregroundStyle(.tertiary)
                                .padding(4)
                        }
                }
                .padding(.horizontal)
                .padding(.vertical, 6)
            }

            Divider()

            // ── Skills ──
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text("Skills")
                            .font(.caption).foregroundStyle(.secondary)
                        Text("(comma-separated)")
                            .font(.caption2).foregroundStyle(.tertiary)
                    }
                    TextField("skill1, skill2", text: $editSkills)
                        .textFieldStyle(.roundedBorder)
                }
                .padding(.horizontal)
                .padding(.vertical, 6)
            }

            Divider()

            // ── Tools (read-only display) ──
            if let tools = agentDetail?.tools, !tools.isEmpty {
                Section {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Tools (\(tools.count))")
                            .font(.caption).foregroundStyle(.secondary)
                        FlowLayout(spacing: 4) {
                            ForEach(tools, id: \.self) { tool in
                                Text(tool)
                                    .font(.system(size: 9, design: .monospaced))
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Color.secondary.opacity(0.1))
                                    .cornerRadius(4)
                            }
                        }
                    }
                    .padding(.horizontal)
                    .padding(.vertical, 6)
                }
                Divider()
            }

            // ── Model ──
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Model")
                        .font(.caption).foregroundStyle(.secondary)

                    HStack(spacing: 12) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Provider").font(.caption2).foregroundStyle(.tertiary)
                            if isLoadingProviders || availableProviders.isEmpty {
                                TextField("deepseek", text: $editModelProvider)
                                    .textFieldStyle(.roundedBorder)
                                    .disabled(isLoadingProviders)
                            } else {
                                Picker("", selection: $editModelProvider) {
                                    Text("(none)").tag("")
                                    ForEach(availableProviders) { p in
                                        Text(providerDisplayName(p.provider)).tag(p.provider)
                                    }
                                }
                                .labelsHidden()
                                .onChange(of: editModelProvider) { _, newVal in
                                    guard !newVal.isEmpty else {
                                        availableModels = []
                                        editModelName = ""
                                        return
                                    }
                                    Task {
                                        availableModels = (try? await apiClient.fetchProviderModels(provider: newVal)) ?? []
                                        if !availableModels.contains(where: { $0.modelName == editModelName }) {
                                            editModelName = ""
                                        }
                                    }
                                }
                            }
                        }
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Name").font(.caption2).foregroundStyle(.tertiary)
                            if isLoadingProviders || availableModels.isEmpty {
                                TextField("deepseek-v4-flash", text: $editModelName)
                                    .textFieldStyle(.roundedBorder)
                            } else {
                                Picker("", selection: $editModelName) {
                                    Text("(auto)").tag("")
                                    ForEach(availableModels) { m in
                                        Text(m.displayName ?? m.modelName).tag(m.modelName)
                                    }
                                }
                                .labelsHidden()
                            }
                        }
                    }

                    HStack(spacing: 12) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Temperature").font(.caption2).foregroundStyle(.tertiary)
                            TextField("0.7", text: $editTemperature)
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 80)
                        }
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Max Tokens").font(.caption2).foregroundStyle(.tertiary)
                            TextField("4096", text: $editMaxTokens)
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 100)
                        }
                        Spacer()
                    }
                }
                .padding(.horizontal)
                .padding(.vertical, 6)
            }

            Divider()

            // ── Runtime Limits ──
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Runtime")
                        .font(.caption).foregroundStyle(.secondary)

                    HStack(spacing: 12) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Max Steps").font(.caption2).foregroundStyle(.tertiary)
                            TextField("50", text: $editMaxSteps)
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 80)
                        }
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Timeout (s)").font(.caption2).foregroundStyle(.tertiary)
                            TextField("300", text: $editTimeout)
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 80)
                        }
                        Spacer()
                    }

                    HStack(spacing: 12) {
                        Text("Visibility").font(.caption).foregroundStyle(.secondary)
                        Picker("", selection: $editVisibility) {
                            Text("project").tag("project")
                            Text("org").tag("org")
                            Text("private").tag("private")
                        }
                        .labelsHidden()
                        .frame(width: 120)
                        Spacer()
                    }
                }
                .padding(.horizontal)
                .padding(.vertical, 6)
            }

            // ── Built-in Only Options ──
            if isBuiltIn {
                Divider()
                Section {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Override Options")
                            .font(.caption).foregroundStyle(.secondary)

                        Toggle(isOn: $editDisabled) {
                            HStack {
                                Text("Disabled")
                                Text("(agent won't be seeded)")
                                    .font(.caption2).foregroundStyle(.tertiary)
                            }
                        }

                        Toggle(isOn: $editSandboxEnabled) {
                            HStack {
                                Text("Sandbox Enabled")
                                Text("(isolated execution environment)")
                                    .font(.caption2).foregroundStyle(.tertiary)
                            }
                        }
                    }
                    .padding(.horizontal)
                    .padding(.vertical, 6)
                }
            }

            // ── Timestamps ──
            if let detail = agentDetail {
                Divider()
                Section {
                    HStack(spacing: 16) {
                        if let created = detail.createdAt {
                            VStack(alignment: .leading) {
                                Text("Created").font(.caption2).foregroundStyle(.tertiary)
                                Text(DateUtils.formatTimestamp(created))
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                        }
                        if let updated = detail.updatedAt {
                            VStack(alignment: .leading) {
                                Text("Updated").font(.caption2).foregroundStyle(.tertiary)
                                Text(DateUtils.formatTimestamp(updated))
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                        }
                        Spacer()
                    }
                    .padding(.horizontal)
                    .padding(.vertical, 4)
                }
            }

            // spacer for bottom padding
            Color.clear.frame(height: 8)
        }
    }

    // MARK: - Save Logic

    private func saveChanges() async {
        guard let agent = selectedAgent else { return }
        isSaving = true
        saveStatus = nil

        if isBuiltIn {
            await saveBuiltInOverride(agent)
        } else {
            await saveUserDefinedAgent(agent)
        }

        isSaving = false
    }

    private func saveBuiltInOverride(_ agent: AgentDef) async {
        var oc = AgentOverrideConfig(agentName: agent.name)

        // Only include fields that differ from defaults
        if !editSystemPrompt.isEmpty { oc.systemPrompt = editSystemPrompt }
        let skillList = parseCommaList(editSkills)
        if !skillList.isEmpty { oc.skills = skillList }
        if !editModelProvider.isEmpty { oc.modelProvider = editModelProvider }
        if !editModelName.isEmpty { oc.modelName = editModelName }
        if let t = Double(editTemperature), t != 0 { oc.modelTemperature = t }
        if let mt = Int(editMaxTokens), mt > 0 { oc.modelMaxTokens = mt }
        if let ms = Int(editMaxSteps), ms > 0 { oc.maxSteps = ms }
        if let to = Int(editTimeout), to > 0 { oc.timeout = to }
        if !editVisibility.isEmpty { oc.visibility = editVisibility }
        oc.sandboxEnabled = editSandboxEnabled
        if editDisabled { oc.disabled = true }

        do {
            try await dianeAPI.saveAgentOverride(name: agent.name, override: oc)
            _ = try? await dianeAPI.seedAgents()
            saveStatus = "✅ Saved as override"
            await loadDetail()
        } catch {
            saveStatus = "❌ \(error.localizedDescription)"
        }
    }

    private func saveUserDefinedAgent(_ agent: AgentDef) async {
        var changes: [String: Any] = [:]

        if editSystemPrompt != (agentDetail?.systemPrompt ?? "") {
            changes["system_prompt"] = editSystemPrompt
        }

        let currentSkills = agentDetail?.skills ?? []
        let newSkills = parseCommaList(editSkills)
        if newSkills != currentSkills {
            changes["skills"] = newSkills
        }

        if let ms = Int(editMaxSteps), ms > 0 { changes["max_steps"] = ms }
        if let to = Int(editTimeout), to > 0 { changes["default_timeout"] = to }

        let modelNameVal: Any = editModelName.isEmpty ? NSNull() : editModelName
        changes["model"] = ["name": modelNameVal]

        changes["visibility"] = editVisibility

        do {
            try await dianeAPI.updateAgent(name: agent.name, changes: changes)
            saveStatus = "✅ Saved"
            await loadDetail()
        } catch {
            saveStatus = "❌ \(error.localizedDescription)"
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

        do {
            agentDetail = try await dianeAPI.fetchAgentDetail(name: agent.name)
        } catch {
            agentDetail = nil
        }

        // Fetch override if built-in
        if isBuiltIn {
            overrideConfig = try? await dianeAPI.fetchAgentOverride(name: agent.name)
        } else {
            overrideConfig = nil
        }

        populateForm(from: agentDetail, override: overrideConfig)
        editHasChanges = false
        saveStatus = nil

        // Fetch available providers from MP for picker
        isLoadingProviders = true
        let projectID = serverConfig.projectID.isEmpty
            ? (appState.selectedProject?.id ?? "")
            : serverConfig.projectID
        if !projectID.isEmpty {
            availableProviders = (try? await apiClient.fetchProjectProviderConfigs(projectID: projectID)) ?? []
            // Fetch models for the currently selected provider
            let currentProvider = overrideConfig?.modelProvider ?? agentDetail?.model?.provider ?? ""
            if !currentProvider.isEmpty {
                availableModels = (try? await apiClient.fetchProviderModels(provider: currentProvider)) ?? []
            }
        }
        isLoadingProviders = false

        isLoadingDetail = false
    }

    private func populateForm(from detail: AgentDetail?, override: AgentOverrideConfig?) {
        let sp = override?.systemPrompt ?? detail?.systemPrompt ?? ""
        editSystemPrompt = sp

        let skills = override?.skills ?? detail?.skills ?? []
        editSkills = skills.joined(separator: ", ")

        editModelProvider = override?.modelProvider ?? detail?.model?.provider ?? ""
        editModelName = override?.modelName ?? detail?.model?.name ?? ""
        editTemperature = override.flatMap { $0.modelTemperature.map { String($0) } } ?? ""
        editMaxTokens = override.flatMap { $0.modelMaxTokens.map { String($0) } } ?? ""
        editMaxSteps = override.flatMap { $0.maxSteps.map { String($0) } } ?? detail.flatMap { $0.maxSteps.map { String($0) } } ?? ""
        editTimeout = override.flatMap { $0.timeout.map { String($0) } } ?? detail.flatMap { $0.defaultTimeout.map { String($0) } } ?? ""
        editVisibility = override?.visibility ?? detail?.visibility ?? "project"
        editDisabled = override?.disabled ?? false
        editSandboxEnabled = override?.sandboxEnabled ?? false
    }

    func parseCommaList(_ str: String) -> [String] {
        str.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    }

    private func providerDisplayName(_ provider: String) -> String {
        switch provider {
        case "google":          return "Google AI"
        case "google-vertex":   return "Vertex AI"
        case "deepseek":        return "DeepSeek"
        case "openai-compatible": return "OpenAI Compatible"
        default:                return provider
        }
    }
}

// MARK: - FlowLayout (simple wrapping layout for tool tags)

struct FlowLayout: Layout {
    var spacing: CGFloat = 4

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout Void) -> CGSize {
        let width = proposal.width ?? 0
        var height: CGFloat = 0
        var x: CGFloat = 0
        var y: CGFloat = 0
        var maxHeight: CGFloat = 0

        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x + size.width > width {
                x = 0
                y += maxHeight + spacing
                maxHeight = 0
            }
            maxHeight = max(maxHeight, size.height)
            x += size.width + spacing
            height = y + maxHeight
        }
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout Void) {
        var x = bounds.minX
        var y = bounds.minY
        var maxHeight: CGFloat = 0

        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x + size.width > bounds.maxX {
                x = bounds.minX
                y += maxHeight + spacing
                maxHeight = 0
            }
            view.place(at: CGPoint(x: x, y: y), proposal: .unspecified)
            maxHeight = max(maxHeight, size.height)
            x += size.width + spacing
        }
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
