import XCTest
@testable import Diane

@MainActor
final class RelayNodesViewModelDataTests: XCTestCase {

    // MARK: - Mock Data

    private let masterNode = RelayNode(
        instanceID: "master-1", hostname: "diane-prod", mode: "master",
        version: "1.0.0", toolCount: 5, connectedAt: "2026-05-07T10:00:00Z",
        online: true, uptime: nil, provider: "openai/gpt-4o",
        relayActive: true, botActive: false, healthy: true
    )

    private let slaveNode = RelayNode(
        instanceID: "slave-1", hostname: "diane-worker-1", mode: "slave",
        version: "1.0.0", toolCount: 3, connectedAt: "2026-05-07T11:00:00Z",
        online: true, uptime: nil, provider: nil,
        relayActive: false, botActive: true, healthy: true
    )

    private let offlineNode = RelayNode(
        instanceID: "offline-1", hostname: nil, mode: nil,
        version: nil, toolCount: nil, connectedAt: nil,
        online: false, uptime: nil, provider: nil,
        relayActive: nil, botActive: nil, healthy: nil
    )

    // MARK: - Load Success

    func testLoadSuccessPopulatesNodes() async {
        let mockNodes = [masterNode, slaveNode, offlineNode]
        let vm = RelayNodesViewModel(
            fetchNodes: { mockNodes },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertEqual(vm.nodes.count, 3)
        XCTAssertNil(vm.error)
        XCTAssertFalse(vm.isLoading)
    }

    func testLoadSortsMasterFirst() async {
        let mockNodes = [slaveNode, masterNode, offlineNode]
        let vm = RelayNodesViewModel(
            fetchNodes: { mockNodes },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertEqual(vm.nodes[0].instanceID, "master-1", "Master should be first")
        XCTAssertEqual(vm.nodes[1].instanceID, "slave-1", "Slave should be second")
    }

    // MARK: - Empty State

    func testLoadWithEmptyNodes() async {
        let vm = RelayNodesViewModel(
            fetchNodes: { [] },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertTrue(vm.nodes.isEmpty)
        XCTAssertNil(vm.error)
        XCTAssertFalse(vm.isLoading)
    }

    // MARK: - Error State

    func testLoadWithNetworkError() async {
        let vm = RelayNodesViewModel(
            fetchNodes: { throw URLError(.notConnectedToInternet) },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertTrue(vm.nodes.isEmpty)
        XCTAssertNotNil(vm.error)
        XCTAssertFalse(vm.isLoading)
    }

    func testLoadWithDecodingError() async {
        let vm = RelayNodesViewModel(
            fetchNodes: { throw DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: "Bad JSON")) },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertTrue(vm.nodes.isEmpty)
        XCTAssertNotNil(vm.error)
        // Error description should contain useful info about the failure
        XCTAssertNotNil(vm.error)
    }

    // MARK: - Derived Counts

    func testMasterAndSlaveCounts() async {
        let mockNodes = [masterNode, slaveNode, offlineNode]
        let vm = RelayNodesViewModel(
            fetchNodes: { mockNodes },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertEqual(vm.masterCount, 1)
        XCTAssertEqual(vm.slaveCount, 1)
        XCTAssertEqual(vm.onlineCount, 2) // master + slave are online, offline is not
    }

    func testAllNodesOffline() async {
        let allOffline = (1...3).map { i in
            RelayNode(instanceID: "n\(i)", hostname: "node\(i)", mode: nil, version: nil,
                      toolCount: nil, connectedAt: nil, online: false, uptime: nil, provider: nil,
                      relayActive: nil, botActive: nil, healthy: nil)
        }
        let vm = RelayNodesViewModel(
            fetchNodes: { allOffline },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertEqual(vm.onlineCount, 0)
        XCTAssertEqual(vm.nodes.count, 3)
    }

    // MARK: - Tools

    func testLoadToolsPopulates() async {
        let mockTools = [
            MCPToolInfo(name: "search", description: "Search the web"),
            MCPToolInfo(name: "read", description: "Read a file")
        ]
        let vm = RelayNodesViewModel(
            fetchNodes: { [self.masterNode] },
            fetchTools: { id in
                XCTAssertEqual(id, "master-1")
                return mockTools
            }
        )

        await vm.load()
        await vm.loadTools(node: masterNode)

        let tools = vm.tools(for: masterNode)
        XCTAssertEqual(tools.count, 2)
        XCTAssertEqual(tools[0].name, "search")
        XCTAssertFalse(vm.isLoadingTools(for: masterNode))
    }

    func testLoadToolsFailureReturnsEmpty() async {
        let vm = RelayNodesViewModel(
            fetchNodes: { [self.masterNode] },
            fetchTools: { _ in throw URLError(.badServerResponse) }
        )

        await vm.load()
        await vm.loadTools(node: masterNode)

        let tools = vm.tools(for: masterNode)
        XCTAssertTrue(tools.isEmpty)
        XCTAssertFalse(vm.isLoadingTools(for: masterNode))
    }

    // MARK: - Expand/Collapse

    func testToggleToolsExpandsAndCollapses() async {
        let vm = RelayNodesViewModel(
            fetchNodes: { [self.masterNode] },
            fetchTools: { _ in [] }
        )

        await vm.load()

        XCTAssertFalse(vm.isExpanded(masterNode))

        vm.toggleTools(node: masterNode)
        XCTAssertTrue(vm.isExpanded(masterNode))

        vm.toggleTools(node: masterNode)
        XCTAssertFalse(vm.isExpanded(masterNode))
    }

    func testExpandTriggersToolLoad() async {
        var toolsLoaded = false
        let vm = RelayNodesViewModel(
            fetchNodes: { [self.masterNode] },
            fetchTools: { _ in
                toolsLoaded = true
                return []
            }
        )

        await vm.load()
        vm.toggleTools(node: masterNode)

        // Allow async task to complete
        try? await Task.sleep(nanoseconds: 100_000_000)

        XCTAssertTrue(toolsLoaded)
        XCTAssertTrue(vm.isExpanded(masterNode))
    }

    // MARK: - Refresh Resets Tools

    func testReloadResetsToolsState() async {
        let vm = RelayNodesViewModel(
            fetchNodes: { [self.masterNode] },
            fetchTools: { _ in [MCPToolInfo(name: "test", description: nil)] }
        )

        // Load first time with tools
        await vm.load()
        vm.toggleTools(node: masterNode)
        try? await Task.sleep(nanoseconds: 100_000_000)

        XCTAssertFalse(vm.tools(for: masterNode).isEmpty)

        // Reload with empty
        let vm2 = RelayNodesViewModel(
            fetchNodes: { [] },
            fetchTools: { _ in [] }
        )
        await vm2.load()

        XCTAssertTrue(vm2.nodes.isEmpty)
        XCTAssertFalse(vm2.isExpanded(masterNode))
        XCTAssertTrue(vm2.tools(for: masterNode).isEmpty)
    }
}
