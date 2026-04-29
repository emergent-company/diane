import SwiftUI
import OSLog

/// Relay Nodes view — shows connected MCP relay instances from the Memory Platform.
struct RelayNodesView: View {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "RelayNodes")
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var nodes: [RelayNode] = []
    @State private var isLoading = false
    @State private var error: String? = nil

    var body: some View {
        HSplitView {
            nodesList
                .frame(minWidth: 280)

            if let selected = selectedNode {
                nodeDetail(selected)
                    .frame(minWidth: 280)
            } else {
                EmptyStateView(
                    title: "Select a Node",
                    icon: "antenna.radiowaves.left.and.right",
                    description: "Select a relay node to see its connection details."
                )
                .frame(minWidth: 280)
            }
        }
        .navigationTitle("Relay Nodes")
        .task { await load() }
    }

    @State private var selectedNode: RelayNode? = nil

    // MARK: - Nodes List

    @ViewBuilder
    private var nodesList: some View {
        VStack(spacing: 0) {
            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding(8)
            }

            if isLoading && nodes.isEmpty {
                LoadingStateView(message: "Loading relay nodes…")
            } else if nodes.isEmpty {
                EmptyStateView(
                    title: "No Relay Nodes",
                    icon: "antenna.radiowaves.left.and.right",
                    description: "No relay connections active. Start 'diane serve' or 'diane mcp relay' on a node."
                )
            } else {
                List(nodes, selection: $selectedNode) { node in
                    nodeRow(node)
                        .tag(node)
                }
                .listStyle(.plain)
            }

            Divider()
            HStack {
                Text("\(nodes.count) node\(nodes.count == 1 ? "" : "s") connected")
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

    private func nodeRow(_ node: RelayNode) -> some View {
        HStack(spacing: 8) {
            Circle()
                .fill(Color.green)
                .frame(width: 7, height: 7)

            VStack(alignment: .leading, spacing: 2) {
                Text(node.hostname ?? node.instanceID)
                    .font(.subheadline)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    Text(node.instanceID)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    if let version = node.version {
                        Text("v\(version)")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
            Spacer()

            if let count = node.toolCount {
                HStack(spacing: 4) {
                    Image(systemName: "wrench")
                        .font(.caption2)
                        .foregroundStyle(.purple)
                    Text("\(count)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, 2)
    }

    // MARK: - Node Detail

    private func nodeDetail(_ node: RelayNode) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text(node.hostname ?? node.instanceID)
                        .font(.subheadline)
                        .fontWeight(.semibold)
                    HStack(spacing: 6) {
                        Circle()
                            .fill(Color.green)
                            .frame(width: 7, height: 7)
                        Text("Online")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            List {
                Section("Identity") {
                    detailRow(label: "Instance ID", value: node.instanceID)
                    if let host = node.hostname {
                        detailRow(label: "Hostname", value: host)
                    }
                }

                Section("Status") {
                    if let version = node.version {
                        detailRow(label: "Version", value: version)
                    }
                    if let count = node.toolCount {
                        detailRow(label: "Registered Tools", value: "\(count)")
                    }
                    if let connected = node.connectedAt {
                        detailRow(label: "Connected At", value: connected)
                    }
                }
            }
            .listStyle(.plain)
        }
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 100, alignment: .leading)
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
            nodes = try await dianeAPI.fetchRelayNodes()
            error = nil
        } catch {
            logger.warning("Local API failed: \(error.localizedDescription), trying remote...")
            do {
                let relaySessions = try await apiClient.fetchRelaySessions(projectID: serverConfig.projectID)
                nodes = relaySessions.map { r in
                    RelayNode(instanceID: r.instanceID ?? r.id, hostname: r.nodeName, version: nil, toolCount: r.toolCount, connectedAt: r.connectedAt)
                }
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
        isLoading = false
    }
}
