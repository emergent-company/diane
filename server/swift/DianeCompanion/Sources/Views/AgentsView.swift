import SwiftUI

/// Agents view — lists configured agents with a detail/edit panel.
///
/// Configuration task 8.1
struct AgentsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var agents: [Agent] = []
    @State private var selectedAgent: Agent? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var isSaving = false
    @State private var saveError: String? = nil

    // Editable fields for the detail form
    @State private var editName: String = ""
    @State private var editSchedule: String = ""
    @State private var editPrompt: String = ""
    @State private var editIsActive: Bool = true

    var body: some View {
        HSplitView {
            agentsList
                .frame(minWidth: 280)

            if let agent = selectedAgent {
                agentEditForm(agent)
                    .frame(minWidth: 300)
            } else {
                EmptyStateView(
                    title: "Select an Agent",
                    icon: "cpu",
                    description: "Select an agent to view or edit its configuration."
                )
                .frame(minWidth: 300)
            }
        }
        .navigationTitle("Agents")
        .task { await load() }
        .onChange(of: appState.selectedProject) { _ in
            Task { await load() }
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
                    icon: "cpu",
                    description: "No agents have been configured."
                )
            } else {
                List(agents, selection: $selectedAgent) { agent in
                    agentRow(agent)
                        .tag(agent)
                }
                .listStyle(.plain)
                .onChange(of: selectedAgent) { agent in
                    if let a = agent {
                        editName = a.name
                        editSchedule = a.schedule ?? ""
                        editPrompt = a.prompt ?? ""
                        editIsActive = a.isActive
                        saveError = nil
                    }
                }
            }

            Divider()
            HStack {
                Text("\(agents.count) agent\(agents.count == 1 ? "" : "s") configured")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
    }

    private func agentRow(_ agent: Agent) -> some View {
        HStack(spacing: 8) {
            Circle()
                .fill(agent.isActive ? Color.green : Color.red)
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 2) {
                Text(agent.name)
                    .font(.subheadline)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let trigger = agent.triggerType {
                        Text(trigger)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    Text(agent.isActive ? "Active" : "Paused")
                        .font(.caption2)
                        .foregroundStyle(agent.isActive ? .green : .red)
                }
            }
            Spacer()
        }
        .padding(.vertical, 2)
    }

    // MARK: - Agent Edit Form

    private func agentEditForm(_ agent: Agent) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                Text("Edit Agent")
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
                Toggle("Active", isOn: $editIsActive)
                    .toggleStyle(.switch)
                    .controlSize(.small)
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            if let err = saveError {
                ErrorBannerView(message: err)
                    .padding(8)
            }

            Form {
                Section("Identity") {
                    LabeledContent("Name") {
                        TextField("Agent name", text: $editName)
                            .textFieldStyle(.plain)
                    }
                    LabeledContent("Trigger") {
                        Text(agent.triggerType ?? "—")
                            .foregroundStyle(.secondary)
                    }
                }

                if agent.triggerType == "cron" || agent.triggerType == "schedule" {
                    Section("Schedule") {
                        LabeledContent("Cron") {
                            TextField("0 9 * * *", text: $editSchedule)
                                .textFieldStyle(.plain)
                                .font(.system(.body, design: .monospaced))
                        }
                    }
                }

                Section("Prompt") {
                    TextEditor(text: $editPrompt)
                        .font(.system(.body, design: .monospaced))
                        .frame(minHeight: 100)
                }

                if let caps = agent.capabilities, !caps.isEmpty {
                    Section("Capabilities") {
                        ForEach(caps, id: \.self) { cap in
                            Label(cap, systemImage: "checkmark.circle.fill")
                                .font(.caption)
                                .foregroundStyle(.green)
                        }
                    }
                }
            }
            .formStyle(.grouped)

            Divider()

            HStack {
                Spacer()
                if isSaving {
                    ProgressView().controlSize(.small)
                } else {
                    Button("Save") {
                        Task { await saveAgent(agent) }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(editName.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            .padding(12)
        }
    }

    @MainActor
    private func saveAgent(_ original: Agent) async {
        isSaving = true
        saveError = nil
        let updated = Agent(
            id: original.id,
            name: editName.trimmingCharacters(in: .whitespaces),
            triggerType: original.triggerType,
            schedule: editSchedule.isEmpty ? nil : editSchedule,
            prompt: editPrompt.isEmpty ? nil : editPrompt,
            isActive: editIsActive,
            capabilities: original.capabilities
        )
        do {
            let saved = try await apiClient.updateAgent(updated)
            if let idx = agents.firstIndex(where: { $0.id == saved.id }) {
                agents[idx] = saved
            }
            selectedAgent = saved
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
    }

    @MainActor
    private func load() async {
        guard let projectID = appState.selectedProject?.id else {
            error = "No project selected"
            return
        }
        isLoading = true
        do {
            agents = try await apiClient.fetchAgents(projectID: projectID)
            error = nil
        } catch EmergentAPIError.notFound {
            // Agents feature may not be enabled in standalone mode — show empty state
            agents = []
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}
