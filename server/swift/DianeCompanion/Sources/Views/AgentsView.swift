import SwiftUI

/// Agents view — lists agent definitions from the Memory Platform.
struct AgentsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var agents: [AgentDef] = []
    @State private var selectedAgent: AgentDef? = nil
    @State private var isLoading = false
    @State private var error: String? = nil

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
        .task { await load() }
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
        HStack(spacing: 10) {
            Image(systemName: agentIcon(agent.flowType))
                .font(.system(size: 12))
                .foregroundStyle(agentColor(agent.flowType))
                .frame(width: 20, height: 20)

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(agent.name)
                        .font(.subheadline)
                        .fontWeight(.medium)
                        .lineLimit(1)
                    if agent.isDefault {
                        Text("default")
                            .font(.system(size: 9, weight: .medium))
                            .foregroundStyle(.blue)
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(Color.blue.opacity(0.1))
                            .cornerRadius(3)
                    }
                    flowBadge(agent.flowType)
                }
                HStack(spacing: 8) {
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
                        Text(relativeTimestamp(date))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
        }
        .padding(.vertical, 3)
    }

    private func flowBadge(_ flow: String) -> some View {
        Text(flow.isEmpty ? "chat" : flow)
            .font(.system(size: 9, weight: .medium))
            .foregroundStyle(agentColor(flow))
            .padding(.horizontal, 4)
            .padding(.vertical, 1)
            .background(agentColor(flow).opacity(0.1))
            .cornerRadius(3)
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

    private func agentDetailPanel(_ agent: AgentDef) -> some View {
        List {
            Section("Agent Info") {
                detailRow(label: "Name", value: agent.name)
                detailRow(label: "Flow Type", value: agent.flowType.isEmpty ? "chat" : agent.flowType)
                detailRow(label: "Visibility", value: agent.visibility)
                detailRow(label: "Tool Count", value: "\(agent.toolCount)")
                if agent.isDefault {
                    HStack {
                        Text("Default Agent")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                        Image(systemName: "checkmark.seal.fill")
                            .foregroundStyle(.blue)
                    }
                }
            }

            if let desc = agent.description, !desc.isEmpty {
                Section("Description") {
                    Text(desc)
                        .font(.caption)
                }
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
        .listStyle(.plain)
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 70, alignment: .leading)
            Text(value)
                .font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)
            Spacer()
        }
    }

    // MARK: - Helpers

    private func relativeTimestamp(_ dateStr: String) -> String {
        DateUtils.formatTimestamp(dateStr)
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
