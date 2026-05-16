import SwiftUI
import DianeShared

struct AgentsListView: View {
    @Environment(\.apiClient) private var apiClient

    @State private var agents: [AgentDef] = []
    @State private var selectedAgent: AgentDef?
    @State private var isLoading = true
    @State private var error: String?
    @State private var isOffline = false

    var body: some View {
        VStack(spacing: 0) {
            if isOffline { OfflineBanner() }

            Group {
                if isLoading && agents.isEmpty {
                    List {
                        ForEach(0..<5, id: \.self) { _ in
                            AgentRowPlaceholder()
                        }
                    }
                    .listStyle(.insetGrouped)
                } else if let err = error, agents.isEmpty {
                    ContentUnavailableView(
                        "Could Not Load Agents",
                        systemImage: "brain.head.profile",
                        description: Text(err)
                    )
                } else if agents.isEmpty {
                    ContentUnavailableView(
                        "No Agents",
                        systemImage: "brain.head.profile",
                        description: Text("No agent definitions found on this server.")
                    )
                } else {
                    List(agents) { agent in
                        NavigationLink(value: agent) {
                            AgentRow(agent: agent)
                        }
                    }
                    .listStyle(.insetGrouped)
                }
            }
        }
        .navigationTitle("Agents")
        .task { await load() }
        .refreshable { await load() }
        .navigationDestination(for: AgentDef.self) { agent in
            AgentDetailView(agent: agent)
            .sentryView("AgentsListView")
    }
    }

    private func load() async {
        isLoading = true
        error = nil
        isOffline = false
        do {
            agents = try await apiClient.fetchAgents()
            cacheAgents(agents)
        } catch {
            let cached = loadCachedAgents()
            if cached.isEmpty {
                self.error = error.localizedDescription
            } else {
                agents = cached
                isOffline = true
            }
        }
        isLoading = false
    }

    // MARK: - Simple UserDefaults Cache

    private func cacheAgents(_ agents: [AgentDef]) {
        if let data = try? JSONEncoder().encode(agents) {
            UserDefaults.standard.set(data, forKey: "cached-agents")
        }
    }

    private func loadCachedAgents() -> [AgentDef] {
        guard let data = UserDefaults.standard.data(forKey: "cached-agents"),
              let agents = try? JSONDecoder().decode([AgentDef].self, from: data) else {
            return []
        }
        return agents
    }
}

// MARK: - Agent Row

struct AgentRow: View {
    let agent: AgentDef

    var body: some View {
        HStack(spacing: DesignTokens.spacingMD) {
            // Icon
            Image(systemName: agentIcon(for: agent.name))
                .font(.title3)
                .foregroundColor(agentColor(for: agent.name))
                .frame(width: 32, height: 32)
                .background(agentColor(for: agent.name).opacity(0.1))
                .cornerRadius(DesignTokens.radiusMD)

            VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
                Text(agentDisplayName(agent.name))
                    .font(.body)
                    .lineLimit(1)

                if let model = agent.model {
                    Text(model)
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .lineLimit(1)
                }

                if let desc = agent.description, !desc.isEmpty {
                    Text(desc)
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .lineLimit(2)
                }
            }

            Spacer()

            if agent.isActive != false {
                Circle()
                    .fill(Color.green)
                    .frame(width: 8, height: 8)
            }
        }
        .padding(.vertical, DesignTokens.spacingXS)
    }

    private func agentIcon(for name: String) -> String {
        if name.contains("researcher") { return "magnifyingglass" }
        if name.contains("schema") { return "point.3.connected.trianglepath.dotted" }
        if name.contains("mcp") || name.contains("tool") { return "wrench.and.screwdriver" }
        if name.contains("codebase") || name.contains("coder") { return "chevron.left.forwardslash.chevron.right" }
        if name.contains("dreamer") { return "sparkles" }
        if name.contains("extractor") || name.contains("merger") { return "arrow.triangle.branch" }
        if name.contains("default") { return "brain.head.profile" }
        return "person.circle"
    }

    private func agentColor(for name: String) -> Color {
        if name.contains("researcher") { return .blue }
        if name.contains("schema") { return .purple }
        if name.contains("mcp") || name.contains("tool") { return .orange }
        if name.contains("codebase") || name.contains("coder") { return .green }
        if name.contains("dreamer") { return .indigo }
        if name.contains("extractor") || name.contains("merger") { return .teal }
        if name.contains("default") { return .accentColor }
        return .secondary
    }

    private func agentDisplayName(_ name: String) -> String {
        name.replacingOccurrences(of: "diane-", with: "")
            .replacingOccurrences(of: "-", with: " ")
            .capitalized
    }
}

// MARK: - Placeholder Row

struct AgentRowPlaceholder: View {
    var body: some View {
        HStack(spacing: DesignTokens.spacingMD) {
            Circle()
                .fill(Color.gray.opacity(0.3))
                .frame(width: 32, height: 32)

            VStack(alignment: .leading, spacing: 4) {
                Text("Agent Name")
                    .font(.body)
                Text("model-name")
                    .font(.caption)
            }
        }
        .redacted(reason: .placeholder)
    }
}

// MARK: - Agent Detail View

struct AgentDetailView: View {
    let agent: AgentDef

    var body: some View {
        Form {
            Section("Info") {
                DetailRow(label: "Name", value: agentDisplayName(agent.name))
                DetailRow(label: "ID", value: agent.id)
                if let desc = agent.description, !desc.isEmpty {
                    DetailRow(label: "Description", value: desc)
                }
                DetailRow(label: "Status", value: agent.isActive != false ? "Active" : "Inactive")
            }

            Section("Model") {
                if let model = agent.model {
                    DetailRow(label: "Model", value: model)
                } else {
                    DetailRow(label: "Model", value: "Default")
                }
                if let provider = agent.provider {
                    DetailRow(label: "Provider", value: provider)
                }
                if let temp = agent.temperature {
                    DetailRow(label: "Temperature", value: String(format: "%.1f", temp))
                }
                if let maxTokens = agent.maxTokens {
                    DetailRow(label: "Max Tokens", value: formatNumber(maxTokens))
                }
            }

            if let tools = agent.tools, !tools.isEmpty {
                Section("Tools (\(tools.count))") {
                    ForEach(tools, id: \.self) { tool in
                        Text(tool)
                            .font(.caption.monospaced())
                            .lineLimit(1)
                    }
                }
            }

            if let prompt = agent.systemPrompt, !prompt.isEmpty {
                Section("System Prompt") {
                    Text(prompt)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
        }
        .navigationTitle(agentDisplayName(agent.name))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func agentDisplayName(_ name: String) -> String {
        name.replacingOccurrences(of: "diane-", with: "")
            .replacingOccurrences(of: "-", with: " ")
            .capitalized
    }

    private func formatNumber(_ n: Int) -> String {
        if n >= 1000 { return "\(n / 1000)k" }
        return "\(n)"
    }
}

// MARK: - Reusable Detail Row

struct DetailRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .top) {
            Text(label)
                .foregroundColor(.secondary)
                .frame(width: 100, alignment: .leading)
            Spacer()
            Text(value)
                .multilineTextAlignment(.trailing)
        }
    }
}
