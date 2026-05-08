import SwiftUI

/// Relay Nodes view — shows registered Diane nodes with online status, mode, version, tools.
/// Master nodes are always sorted to the top. Node info is always visible; MCP Tools is collapsible.
struct RelayNodesView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration

    @State private var vm: RelayNodesViewModel?

    var body: some View {
        Group {
            if let vm {
                content(vm: vm)
            } else {
                ProgressView()
                    .task {
                        let newVM = RelayNodesViewModel(
                            fetchNodes: { [weak dianeAPI] in
                                guard let api = dianeAPI else { return [] }
                                return try await api.fetchRelayNodes()
                            },
                            fetchTools: { [weak dianeAPI] id in
                                guard let api = dianeAPI else { return [] }
                                return try await api.fetchNodeTools(instanceID: id)
                            }
                        )
                        vm = newVM
                        await newVM.load()
                    }
            }
        }
        .navigationTitle("Relay Nodes")
    }

    @ViewBuilder
    private func content(vm: RelayNodesViewModel) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let err = vm.error {
                    ErrorBannerView(message: err) {
                        Task { await vm.load() }
                    }
                }

                if vm.isLoading && vm.nodes.isEmpty {
                    VStack(spacing: 12) {
                        ProgressView()
                        Text("Loading relay nodes…")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.top, 60)
                } else if vm.nodes.isEmpty {
                    EmptyStateView(
                        title: "No Connected Nodes",
                        icon: "server.rack",
                        description: "No Diane nodes are currently registered."
                    )
                    .padding(.top, 60)
                } else {
                    summaryHeader(vm: vm)
                    ForEach(vm.nodes) { node in
                        nodeCard(vm: vm, node: node)
                    }
                }
            }
            .padding()
        }
    }

    // MARK: - Summary Header

    private func summaryHeader(vm: RelayNodesViewModel) -> some View {
        HStack(spacing: 12) {
            Label("\(vm.onlineCount)/\(vm.nodes.count) nodes", systemImage: "server.rack")
                .font(.subheadline)
                .fontWeight(.medium)

            if vm.masterCount > 0 {
                Text("● \(vm.masterCount) master")
                    .font(.caption)
                    .foregroundStyle(.green)
            }
            if vm.slaveCount > 0 {
                Text("● \(vm.slaveCount) slave")
                    .font(.caption)
                    .foregroundStyle(.blue)
            }

            Spacer()

            Button("Refresh") {
                Task { await vm.load() }
            }
            .font(.caption)
            .buttonStyle(.borderless)
        }
        .padding(Design.Padding.sectionHeader)
        .background(Color.primary.opacity(0.04))
        .cornerRadius(8)
    }

    // MARK: - Node Card

    private func nodeCard(vm: RelayNodesViewModel, node: RelayNode) -> some View {
        let toolsExpanded = vm.isExpanded(node)
        let isLoadingTools = vm.isLoadingTools(for: node)
        let tools = vm.tools(for: node)

        return VStack(alignment: .leading, spacing: 0) {
            // ── Header Row ──
            HStack(spacing: Design.Spacing.sm) {
                modeBadge(node.mode)

                VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                    HStack(spacing: Design.Spacing.xs) {
                        Circle()
                            .fill(node.online == true ? Color.green : Color.gray.opacity(0.4))
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
                            Text(DateUtils.formatTimestamp(connected))
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }

                Spacer()
            }
            .padding(Design.Padding.sectionHeader)

            Divider().padding(.horizontal, 12)

            // ── Node Info Section ──
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
                    infoRow(label: "Uptime", value: DateUtils.formatTimestamp(uptime))
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

            // ── MCP Tools Section ──
            VStack(alignment: .leading, spacing: 0) {
                Button(action: { vm.toggleTools(node: node) }) {
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
}
