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
        HSplitView {
            agentsList
                .frame(minWidth: 220, idealWidth: 350)

            if let agent = selectedAgent {
                agentDetailPanel(agent)
                    .frame(minWidth: 220, idealWidth: 350)
            } else {
                EmptyStateView(
                    title: "Select an Agent",
                    icon: "brain.head.profile",
                    description: "Select an agent definition to view its configuration."
                )
                .frame(minWidth: 220, idealWidth: 350)
            }
        }
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
                    detailRow(label: "Created", value: created)
                }
                if let updated = agent.updatedAt {
                    detailRow(label: "Updated", value: updated)
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
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = formatter.date(from: dateStr)
            ?? ISO8601DateFormatter().date(from: dateStr) else {
            return dateStr
        }
        let interval = -date.timeIntervalSinceNow
        switch interval {
        case ..<60:      return "just now"
        case ..<3600:    return "\(Int(interval / 60))m ago"
        case ..<86400:   return "\(Int(interval / 3600))h ago"
        case ..<172800:  return "yesterday"
        case ..<604800:  return "\(Int(interval / 86400))d ago"
        default:         return "\(Int(interval / 604800))w ago"
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        do {
            agents = try await dianeAPI.fetchAgentDefs()
            if agents.isEmpty {
                // Fall back to remote API
                agents = try await apiClient.fetchAgentDefs(projectID: serverConfig.projectID)
            }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}
