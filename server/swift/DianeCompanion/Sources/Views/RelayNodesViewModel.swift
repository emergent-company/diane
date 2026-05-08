import Foundation
import SwiftUI
import Observation

/// ViewModel for RelayNodesView — holds all state and loading logic.
///
/// Dependencies are injected via closures so tests can use mock data
/// without protocols or subclassing.
@MainActor
@Observable
final class RelayNodesViewModel {
    var nodes: [RelayNode] = []
    var error: String?
    var isLoading = false
    var expandedTools: Set<String> = []
    var nodeTools: [String: [MCPToolInfo]] = [:]
    var loadingTools: Set<String> = []

    private let fetchNodes: () async throws -> [RelayNode]
    private let fetchTools: (String) async throws -> [MCPToolInfo]

    init(
        fetchNodes: @escaping () async throws -> [RelayNode],
        fetchTools: @escaping (String) async throws -> [MCPToolInfo]
    ) {
        self.fetchNodes = fetchNodes
        self.fetchTools = fetchTools
    }

    /// Load all relay nodes, sorted (master first).
    func load() async {
        isLoading = true
        do {
            let raw = try await fetchNodes()
            nodes = ViewFormatting.sortedNodes(raw)
            error = nil
        } catch {
            self.error = error.localizedDescription
            nodes = []
        }
        isLoading = false
    }

    /// Load MCP tools for a specific node.
    func loadTools(node: RelayNode) async {
        loadingTools.insert(node.instanceID)
        do {
            let tools = try await fetchTools(node.instanceID)
            nodeTools[node.instanceID] = tools
        } catch {
            nodeTools[node.instanceID] = []
        }
        loadingTools.remove(node.instanceID)
    }

    /// Toggle expanded state and load tools if needed.
    func toggleTools(node: RelayNode) {
        if expandedTools.contains(node.instanceID) {
            expandedTools.remove(node.instanceID)
        } else {
            expandedTools.insert(node.instanceID)
            if nodeTools[node.instanceID] == nil {
                Task { await loadTools(node: node) }
            }
        }
    }

    // MARK: - Derived Values

    var masterCount: Int { nodes.filter { $0.mode == "master" }.count }
    var slaveCount: Int { nodes.filter { $0.mode == "slave" }.count }
    var onlineCount: Int { nodes.filter { $0.online == true }.count }

    func tools(for node: RelayNode) -> [MCPToolInfo] { nodeTools[node.instanceID] ?? [] }
    func isLoadingTools(for node: RelayNode) -> Bool { loadingTools.contains(node.instanceID) }
    func isExpanded(_ node: RelayNode) -> Bool { expandedTools.contains(node.instanceID) }
}
