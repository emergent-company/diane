import SwiftUI

/// Relay Nodes view — shows registered Diane nodes with online status, mode, version, tools.
/// Master nodes are always sorted to the top. Node info is always visible; MCP Tools is collapsible.
struct RelayNodesView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration

    @State private var nodes: [RelayNode] = []
    @State private var expandedTools: Set<String> = []
    @State private var nodeTools: [String: [MCPToolInfo]] = [:]
    @State private var loadingTools: Set<String> = []
    @State private var isLoading = false
    @State private var error: String? = nil

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let err = error {
                    ErrorBannerView(message: err) {
                        Task { await load() }
                    }
                }

                if isLoading && nodes.isEmpty {
                    VStack(spacing: 12) {
                        ProgressView()
                        Text("Loading relay nodes…")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.top, 60)
                } else if nodes.isEmpty {
                    EmptyStateView(
                        title: "No Connected Nodes",
                        icon: "server.rack",
                        description: "No Diane nodes are currently registered."
                    )
                    .padding(.top, 60)
                } else {
                    // Summary header
                    summaryHeader

                    // Per-node cards
                    ForEach(nodes) { node in
                        nodeCard(node)
                    }
                }
            }
            .padding()
        }
        .navigationTitle("Relay Nodes")
        .task { await load() }
    }

    // MARK: - Summary Header

    private var summaryHeader: some View {
        let masterCount = nodes.filter { $0.mode == "master" }.count
        let slaveCount = nodes.filter { $0.mode == "slave" }.count
        let onlineCount = nodes.filter { $0.online }.count

        return HStack(spacing: 12) {
            Label("\(onlineCount)/\(nodes.count) nodes", systemImage: "server.rack")
                .font(.subheadline)
                .fontWeight(.medium)

            if masterCount > 0 {
                Text("● \(masterCount) master")
                    .font(.caption)
                    .foregroundStyle(.green)
            }
            if slaveCount > 0 {
                Text("● \(slaveCount) slave")
                    .font(.caption)
                    .foregroundStyle(.blue)
            }

            Spacer()

            Button("Refresh") {
                Task { await load() }
            }
            .font(.caption)
            .buttonStyle(.borderless)
        }
        .padding(Design.Padding.sectionHeader)
        .background(Color.primary.opacity(0.04))
        .cornerRadius(8)
    }

    // MARK: - Node Card

    private func nodeCard(_ node: RelayNode) -> some View {
        let toolsExpanded = expandedTools.contains(node.instanceID)
        let isLoadingTools = loadingTools.contains(node.instanceID)
        let tools = nodeTools[node.instanceID] ?? []

        return VStack(alignment: .leading, spacing: 0) {
            // ── Header Row (always visible; no toggle) ──
            HStack(spacing: Design.Spacing.sm) {
                // Mode badge (colored)
                modeBadge(node.mode)

                VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                    HStack(spacing: Design.Spacing.xs) {
                        Circle()
                            .fill(node.online ? Color.green : Color.gray.opacity(0.4))
                            .frame(width: 7, height: 7)
                        Text(node.hostname ?? node.instanceID)
                            .font(.subheadline)
                            .fontWeight(.semibold)
                            .lineLimit(1)
                    }

                    HStack(spacing: Design.Spacing.sm) {
                        if let ver = node.version {
                            Text(ver)
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        if let count = node.toolCount {
                            Text("\(count) tool\(count == 1 ? "" : "s")")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        if let connected = node.connectedAt {
                            Text(formatTime(connected))
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }

                Spacer()
            }
            .padding(Design.Padding.sectionHeader)

            Divider().padding(.horizontal, 12)

            // ── Node Info Section (always visible) ──
            VStack(alignment: .leading, spacing: Design.Spacing.xs) {
                HStack {
                    Text("Node Info")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .foregroundStyle(.secondary)
                        .textCase(.uppercase)
                    Spacer()
                }
                .padding(.horizontal, 12)
                .padding(.top, Design.Spacing.sm)

                if let uptime = node.uptime, !uptime.isEmpty {
                    infoRow(label: "Uptime", value: formatTime(uptime))
                }
                if let provider = node.provider, !provider.isEmpty {
                    infoRow(label: "Provider", value: provider)
                }
                if let relayActive = node.relayActive {
                    infoRow(label: "Relay", value: relayActive ? "Active" : "Inactive",
                            valueColor: relayActive ? .green : .secondary)
                }
                if let botActive = node.botActive {
                    infoRow(label: "Discord Bot", value: botActive ? "Active" : "Inactive",
                            valueColor: botActive ? .green : .secondary)
                }
                if let healthy = node.healthy {
                    infoRow(label: "Health", value: healthy ? "Healthy" : "Unhealthy",
                            valueColor: healthy ? .green : .red)
                }
            }

            Divider().padding(.horizontal, 12)

            // ── MCP Tools Section (collapsible) ──
            VStack(alignment: .leading, spacing: 0) {
                Button(action: {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        if toolsExpanded {
                            expandedTools.remove(node.instanceID)
                        } else {
                            expandedTools.insert(node.instanceID)
                            if nodeTools[node.instanceID] == nil {
                                Task { await loadTools(node: node) }
                            }
                        }
                    }
                }) {
                    HStack {
                        Text("MCP Tools")
                            .font(.caption)
                            .fontWeight(.semibold)
                            .foregroundStyle(.secondary)
                            .textCase(.uppercase)
                        if !tools.isEmpty {
                            Text("(\(tools.count))")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                        Spacer()
                        if isLoadingTools {
                            ProgressView().controlSize(.mini)
                        }
                        Image(systemName: toolsExpanded ? "chevron.down" : "chevron.right")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, Design.Spacing.sm)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)

                if toolsExpanded {
                    if isLoadingTools {
                        HStack {
                            Spacer()
                            ProgressView("Loading tools…")
                                .controlSize(.small)
                                .padding(Design.Padding.sectionHeader)
                            Spacer()
                        }
                    } else if tools.isEmpty {
                        Text("No tools registered on this node")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                            .italic()
                            .padding(.horizontal, 12)
                            .padding(.bottom, 8)
                    } else {
                        ForEach(tools) { tool in
                            VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                                Text(tool.name)
                                    .font(.caption)
                                    .fontWeight(.medium)
                                    .monospaced()
                                if let desc = tool.description, !desc.isEmpty {
                                    Text(desc)
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                        .lineLimit(2)
                                }
                            }
                            .padding(.horizontal, 12)
                            .padding(.vertical, 4)
                        }
                        .padding(.bottom, 8)
                    }
                }
            }
        }
        .cardStyle(cornerRadius: Design.CornerRadius.medium)
    }

    // MARK: - Mode Badge

    /// Shows master/slave mode badge from graph config.
    private func modeBadge(_ mode: String?) -> some View {
        switch mode {
        case "master":
            return AnyView(
                HStack(spacing: Design.Spacing.xs) {
                    Circle()
                        .fill(Color.green)
                        .frame(width: 7, height: 7)
                    Text("Master")
                        .font(.caption2)
                        .fontWeight(.semibold)
                        .foregroundStyle(.green)
                }
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.green.opacity(0.1))
                .cornerRadius(4)
            )
        case "slave":
            return AnyView(
                HStack(spacing: Design.Spacing.xs) {
                    Circle()
                        .fill(Color.blue)
                        .frame(width: 7, height: 7)
                    Text("Slave")
                        .font(.caption2)
                        .fontWeight(.semibold)
                        .foregroundStyle(.blue)
                }
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.blue.opacity(0.1))
                .cornerRadius(4)
            )
        default:
            return AnyView(
                HStack(spacing: Design.Spacing.xs) {
                    Ellipse()
                        .fill(Color.secondary)
                        .frame(width: 7, height: 5)
                    Text("Node")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.primary.opacity(0.05))
                .cornerRadius(4)
            )
        }
    }

    // MARK: - Helpers

    private func formatTime(_ iso: String) -> String {
        DateUtils.formatTimestamp(iso)
    }

    /// A labeled info row for the node info section.
    private func infoRow(label: String, value: String, valueColor: Color = .primary) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 80, alignment: .leading)
            Text(value)
                .font(.caption)
                .fontWeight(.medium)
                .foregroundStyle(valueColor)
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 2)
    }

    /// Sort nodes so master is first, then slave, then others. Within same mode, sort by hostname.
    private func sortedNodes(_ nodes: [RelayNode]) -> [RelayNode] {
        nodes.sorted { a, b in
            let orderA = modeOrder(a.mode)
            let orderB = modeOrder(b.mode)
            if orderA != orderB { return orderA < orderB }
            return (a.hostname ?? a.instanceID) < (b.hostname ?? b.instanceID)
        }
    }

    private func modeOrder(_ mode: String?) -> Int {
        switch mode {
        case "master": return 0
        case "slave":  return 1
        default:       return 2
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        do {
            let raw = try await dianeAPI.fetchRelayNodes()
            nodes = sortedNodes(raw)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    @MainActor
    private func loadTools(node: RelayNode) async {
        loadingTools.insert(node.instanceID)
        do {
            let tools = try await dianeAPI.fetchNodeTools(instanceID: node.instanceID)
            nodeTools[node.instanceID] = tools
        } catch {
            nodeTools[node.instanceID] = []
        }
        loadingTools.remove(node.instanceID)
    }
}

// MARK: - Previews

#Preview {
    RelayNodesView()
        .environmentObject(AppState())
        .environmentObject(DianeAPIClient())
        .environmentObject(ServerConfiguration())
        .frame(width: 800, height: 600)
}
