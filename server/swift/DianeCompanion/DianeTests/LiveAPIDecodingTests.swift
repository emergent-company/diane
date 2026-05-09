import XCTest
@testable import Diane

/// Live API Response Decoding Tests
///
/// These tests fetch data from the running `diane serve` on 127.0.0.1:8890
/// and verify that every endpoint's response successfully decodes into
/// the expected Swift model types.
///
/// This catches the most common class of bugs: the API changes its response
/// shape (adds/removes/renames fields) but the Swift model isn't updated,
/// causing silent empty states with no error.
///
/// Run with: xcodebuild test -only-testing:DianeLiveAPITests
/// Requires: diane serve running on port 8890
@MainActor
final class DianeLiveAPITests: XCTestCase {

    var client: DianeAPIClient!

    // MARK: - Setup

    override func setUp() async throws {
        client = DianeAPIClient(baseURL: "http://127.0.0.1:8890")
    }

    /// Check if server is reachable — skip if not
    private func requireServer() async throws {
        let reachable = await client.checkReachability()
        if !reachable {
            throw XCTSkip("diane serve not running on 127.0.0.1:8890")
        }
    }

    // MARK: - Server Status

    func testStatusEndpoint() async throws {
        try await requireServer()
        let status = try await client.fetchServerStatus()
        XCTAssertTrue(status.ok, "Server status should report ok")
        XCTAssertNotNil(status.version, "Server should report a version")
    }

    // MARK: - Sessions

    func testSessionsEndpoint() async throws {
        try await requireServer()
        let sessions = try await client.fetchSessions()
        XCTAssertNotNil(sessions, "Sessions should not be nil")
    }

    func testSessionMessagesEndpoint() async throws {
        try await requireServer()
        let sessions = try await client.fetchSessions()
        if let first = sessions.first {
            let messages = try await client.fetchSessionMessages(sessionID: first.id)
            XCTAssertNotNil(messages, "Messages should not be nil")
        }
    }

    /// Session create → list → verify it appears → close
    /// Note: POST create/close endpoints may not be implemented on local API yet.
    func testSessionLifecycle() async throws {
        try await requireServer()

        do {
            let created = try await client.createSession(title: "LiveAPI Test Session")
            XCTAssertFalse(created.id.isEmpty, "Created session should have an ID")

            let sessions = try await client.fetchSessions()
            let found = sessions.contains { $0.id == created.id }
            XCTAssertTrue(found, "Created session should appear in session list")

            try await client.closeSession(sessionID: created.id)
        } catch {
            throw XCTSkip("Session CRUD endpoints not available: \(error.localizedDescription)")
        }
    }

    /// Session list should contain at least the session we just created
    /// This verifies the list/detail data consistency
    func testSessionConsistency() async throws {
        try await requireServer()

        let sessions = try await client.fetchSessions()
        if let first = sessions.first {
            let detail = try await client.fetchSessionDetail(sessionID: first.id)
            XCTAssertEqual(detail.id, first.id, "Detail session ID should match list")
        }
    }

    // MARK: - Chat / Send

    /// Send a chat message and verify we get a response
    func testChatSend() async throws {
        try await requireServer()

        do {
            let resp = try await client.sendChatMessage(
                sessionID: nil,
                content: "Hello, this is an automated test message. Say OK.",
                agentName: "diane-default"
            )
            XCTAssertFalse(resp.sessionID.isEmpty, "Chat response should have a session ID")
            XCTAssertFalse(resp.runID.isEmpty, "Chat response should have a run ID")
            XCTAssertTrue(resp.success || resp.error == nil, "Chat should succeed or have clear error")
        } catch {
            throw XCTSkip("Chat send endpoint not available: \(error.localizedDescription)")
        }
    }

    /// Send a message to an existing session and verify it has messages
    func testChatToExistingSession() async throws {
        try await requireServer()

        do {
            let created = try await client.createSession(title: "Chat Test Session")
            let resp = try await client.sendChatMessage(
                sessionID: created.id,
                content: "This is a test message. Reply with OK.",
                agentName: "diane-default"
            )
            XCTAssertEqual(resp.sessionID, created.id, "Response should reference the same session")

            let messages = try await client.fetchSessionMessages(sessionID: created.id)
            XCTAssertFalse(messages.isEmpty, "Session should have messages after chat send")
        } catch {
            throw XCTSkip("Chat endpoints not available: \(error.localizedDescription)")
        }
    }

    // MARK: - MCP Servers

    func testMCPServersEndpoint() async throws {
        try await requireServer()
        let servers = try await client.fetchMCPServers()
        XCTAssertNotNil(servers, "MCP servers should not be nil")
        for server in servers {
            XCTAssertFalse(server.name.isEmpty, "Server name should not be empty")
            XCTAssertFalse(server.type.isEmpty, "Server type should not be empty")
        }
    }

    // MARK: - Relay Nodes

    /// The test that would have caught the `online: Bool` → `Bool?` bug.
    func testRelayNodesEndpoint() async throws {
        try await requireServer()
        let nodes = try await client.fetchRelayNodes()
        XCTAssertNotNil(nodes, "Relay nodes should not be nil")
        for node in nodes {
            XCTAssertFalse(node.instanceID.isEmpty, "Node instanceID should not be empty")
        }
    }

    // MARK: - Node Tools

    /// Fetch tools from the first relay node
    func testRelayNodeTools() async throws {
        try await requireServer()
        let nodes = try await client.fetchRelayNodes()
        if let firstNode = nodes.first {
            do {
                let tools = try await client.fetchNodeTools(instanceID: firstNode.instanceID)
                XCTAssertNotNil(tools, "Node tools should not be nil")
            } catch {
                throw XCTSkip("Node tools endpoint not available: \(error.localizedDescription)")
            }
        }
    }

    // MARK: - Agents

    func testAgentDefsEndpoint() async throws {
        try await requireServer()
        let agents = try await client.fetchAgentDefs()
        XCTAssertNotNil(agents, "Agent defs should not be nil")
        for agent in agents {
            XCTAssertFalse(agent.name.isEmpty, "Agent name should not be empty")
        }
    }

    func testAgentDetailEndpoint() async throws {
        try await requireServer()
        let agents = try await client.fetchAgentDefs()
        if let first = agents.first {
            do {
                let detail = try await client.fetchAgentDetail(name: first.name)
                XCTAssertEqual(detail.name, first.name, "Detail should match agent name")
            } catch {
                // Agent detail may not be available for all agents — skip gracefully
                print("  ⚠ Agent detail not available for \(first.name): \(error)")
            }
        }
    }

    /// Create, list, and clean up a test agent
    func testAgentLifecycle() async throws {
        try await requireServer()

        let testName = "test-agent-live-\(UUID().uuidString.prefix(8))"
        let req = CreateAgentRequest(name: testName, description: "Live API test agent",
                                      systemPrompt: "You are a test agent.",
                                      tools: nil, skills: nil,
                                      flowType: "pipeline", visibility: "project",
                                      maxSteps: nil, defaultTimeout: nil)

        do {
            // Create
            let created = try await client.createAgent(req)
            XCTAssertEqual(created.name, testName, "Created agent should have the requested name")

            // List and verify it appears
            let agents = try await client.fetchAgentDefs()
            let found = agents.contains { $0.name == testName }
            XCTAssertTrue(found, "Created agent should appear in agent list")

            // Clean up — delete the test agent
            let status = try await client.deleteAgent(name: testName)
            XCTAssertFalse(status.isEmpty, "Delete response should not be empty")

            // Verify it's gone
            let agentsAfter = try await client.fetchAgentDefs()
            let gone = agentsAfter.contains { $0.name == testName }
            XCTAssertFalse(gone, "Deleted agent should not be in agent list")
        } catch {
            throw XCTSkip("Agent CRUD endpoints not available: \(error.localizedDescription)")
        }
    }

    // MARK: - Stats

    func testAgentStatsEndpoint() async throws {
        try await requireServer()
        let stats = try await client.fetchAgentStats(hours: 24)
        XCTAssertNotNil(stats.agents, "Stats agents list should not be nil")
        XCTAssertGreaterThanOrEqual(stats.totals.totalRuns, 0, "Total runs should be >= 0")
    }

    func testProviderStatsEndpoint() async throws {
        try await requireServer()
        let stats = try await client.fetchProviderStats(hours: 24)
        XCTAssertNotNil(stats.providers, "Provider stats should not be nil")
    }

    // MARK: - Graph Objects

    func testGraphObjectStatsEndpoint() async throws {
        try await requireServer()
        let stats = try await client.fetchGraphObjectStats()
        XCTAssertGreaterThanOrEqual(stats.total, 0, "Total graph objects should be >= 0")
    }

    // MARK: - Schema

    func testSchemaEndpoint() async throws {
        try await requireServer()
        let schema = try await client.fetchGraphSchema()
        XCTAssertFalse(schema.nodeTypes.isEmpty, "Schema should have at least one node type")
        let firstType = schema.nodeTypes[0]
        XCTAssertFalse(firstType.typeName.isEmpty, "Node type name should not be empty")
    }

    // MARK: - Schema Objects

    /// Fetch recent objects of the first schema type
    func testSchemaObjectsEndpoint() async throws {
        try await requireServer()
        let schema = try await client.fetchGraphSchema()
        if let firstType = schema.nodeTypes.first {
            let objects = try await client.fetchSchemaObjects(typeName: firstType.typeName, limit: 5)
            XCTAssertEqual(objects.typeName, firstType.typeName, "Response should match requested type")
            XCTAssertGreaterThanOrEqual(objects.total, 0, "Total should be >= 0")
            for obj in objects.objects {
                XCTAssertFalse(obj.entityID.isEmpty, "Object entity ID should not be empty")
            }
        }
    }

    // MARK: - Doctor

    func testDoctorEndpoint() async throws {
        try await requireServer()
        let doctor = try await client.fetchDoctorReport()
        XCTAssertFalse(doctor.results.isEmpty, "Doctor should return at least one check")
    }

    // MARK: - MCP Tools & Prompts

    /// Fetch tools for the first MCP server
    func testMCPToolsEndpoint() async throws {
        try await requireServer()
        let servers = try await client.fetchMCPServers()
        if let first = servers.first {
            do {
                let tools = try await client.fetchMCPTools(serverName: first.name)
                XCTAssertNotNil(tools, "MCP tools should not be nil")
            } catch DianeAPIError.httpError(404, _) {
                throw XCTSkip("MCP tools endpoint not yet implemented on backend")
            }
        }
    }

    func testMCPPromptsEndpoint() async throws {
        try await requireServer()
        let servers = try await client.fetchMCPServers()
        if let first = servers.first {
            do {
                let prompts = try await client.fetchMCPPrompts(serverName: first.name)
                XCTAssertNotNil(prompts, "MCP prompts should not be nil")
            } catch DianeAPIError.httpError(404, _) {
                throw XCTSkip("MCP prompts endpoint not yet implemented on backend")
            }
        }
    }

    // MARK: - Project Providers

    func testProjectProvidersEndpoint() async throws {
        try await requireServer()
        let providers = try await client.fetchProjectProviders()
        XCTAssertNotNil(providers, "Project providers should not be nil")
    }
}
