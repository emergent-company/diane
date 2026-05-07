import XCTest
@testable import Diane

// MARK: - EmergentAPIClient Tests

@MainActor
final class EmergentAPIClientTests: XCTestCase {

    func testErrorDescriptions() {
        XCTAssertEqual(
            EmergentAPIError.notConfigured.errorDescription,
            "Server URL not configured"
        )
        XCTAssertEqual(
            EmergentAPIError.unauthorized.errorDescription,
            "Unauthorized — check your API key"
        )
        XCTAssertEqual(
            EmergentAPIError.serverError(500).errorDescription,
            "Server error (500)"
        )
        XCTAssertEqual(
            EmergentAPIError.notFound("/projects").errorDescription,
            "Not found: /projects"
        )
        XCTAssertEqual(
            EmergentAPIError.network("timeout").errorDescription,
            "Network error: timeout"
        )
        XCTAssertEqual(
            EmergentAPIError.decodingFailed("bad key").errorDescription,
            "Decoding failed: bad key"
        )
    }

    func testConfigureWithEmptyURL() {
        let client = EmergentAPIClient()
        client.configure(serverURL: "", apiKey: "")
        // Should not crash — validated through behavior
    }

    // MARK: - API Response Model Decoding

    func testServerStatusDecoding() throws {
        let json = """
        {"ok": true, "version": "1.38.5", "started_at": "2026-05-07T10:00:00Z", "server_url": "http://localhost:8890", "project_id": "proj-1"}
        """.data(using: .utf8)!
        let status = try JSONDecoder().decode(DianeAPIClient.ServerStatus.self, from: json)
        XCTAssertTrue(status.ok)
        XCTAssertEqual(status.version, "1.38.5")
        XCTAssertEqual(status.serverURL, "http://localhost:8890")
        XCTAssertEqual(status.projectID, "proj-1")
    }

    func testSessionsResponseDecoding() throws {
        let json = """
        {"items": [
            {"id": "s1", "title": "Test Session", "status": "active", "message_count": 5, "total_tokens": 500, "created_at": "2026-05-01T00:00:00Z", "updated_at": "2026-05-01T01:00:00Z"}
        ]}
        """.data(using: .utf8)!
        struct Response: Decodable { let items: [DianeSession] }
        let resp = try JSONDecoder().decode(Response.self, from: json)
        XCTAssertEqual(resp.items.count, 1)
        XCTAssertEqual(resp.items.first?.title, "Test Session")
        XCTAssertEqual(resp.items.first?.messageCount, 5)
    }
}

// MARK: - MCP Server Model Tests

final class MCPServerModelTests: XCTestCase {

    func testMCPServerStdioDecoding() throws {
        let json = """
        {"name": "filesystem", "enabled": true, "type": "stdio", "command": "/usr/bin/npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"], "timeout": 30}
        """.data(using: .utf8)!
        let server = try JSONDecoder().decode(MCPServer.self, from: json)
        XCTAssertEqual(server.name, "filesystem")
        XCTAssertTrue(server.enabled)
        XCTAssertEqual(server.type, "stdio")
        XCTAssertEqual(server.command, "/usr/bin/npx")
        XCTAssertEqual(server.args?.count, 3)  // -y, server-filesystem, /tmp
        XCTAssertEqual(server.timeout, 30)
        XCTAssertEqual(server.id, "filesystem")  // name-based
    }

    func testMCPServerHTTPDecoding() throws {
        let json = """
        {"name": "sentry", "enabled": true, "type": "http", "url": "http://localhost:9090/mcp", "timeout": 60}
        """.data(using: .utf8)!
        let server = try JSONDecoder().decode(MCPServer.self, from: json)
        XCTAssertEqual(server.type, "http")
        XCTAssertEqual(server.url, "http://localhost:9090/mcp")
        XCTAssertNil(server.command)
        XCTAssertNil(server.args)
    }

    func testMCPToolDecoding() throws {
        let json = """
        {"id": "tool-1", "name": "read_file", "description": "Read a file from disk"}
        """.data(using: .utf8)!
        let tool = try JSONDecoder().decode(MCPTool.self, from: json)
        XCTAssertEqual(tool.name, "read_file")
        XCTAssertEqual(tool.description, "Read a file from disk")
    }
}

// MARK: - Session Model Tests

final class SessionModelTests: XCTestCase {

    func testDianeSessionFlatDecoding() throws {
        let json = """
        {"id": "session-abc", "key": "conv-123", "title": "Test conversation", "status": "active", "message_count": 5, "total_tokens": 1500, "created_at": "2026-04-28T10:00:00Z", "updated_at": "2026-04-28T12:00:00Z"}
        """.data(using: .utf8)!
        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "session-abc")
        XCTAssertEqual(session.title, "Test conversation")
        XCTAssertEqual(session.status, "active")
        XCTAssertEqual(session.messageCount, 5)
        XCTAssertEqual(session.totalTokens, 1500)
    }

    func testDianeSessionGraphDecoding() throws {
        let json = """
        {"entity_id": "session-graph-1", "key": "conv-456", "created_at": "2026-04-28T10:00:00Z", "properties": {"title": "Graph session", "status": "active", "message_count": 3, "total_tokens": 500}}
        """.data(using: .utf8)!
        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "session-graph-1")
        XCTAssertEqual(session.title, "Graph session")
        XCTAssertEqual(session.messageCount, 3)
    }

    func testDianeMessageDecoding() throws {
        let json = """
        {"id": "msg-1", "role": "user", "content": "Hello", "sequence_number": 1, "token_count": 10}
        """.data(using: .utf8)!
        let msg = try JSONDecoder().decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.role, "user")
        XCTAssertEqual(msg.content, "Hello")
        XCTAssertEqual(msg.sequenceNumber, 1)
        XCTAssertNil(msg.toolCalls)
    }

    func testDianeMessageWithToolCalls() throws {
        let json = """
        {"id": "msg-2", "role": "assistant", "content": "Let me search.", "sequence_number": 2, "token_count": 25, "tool_calls": [{"id": "call_1", "name": "web_search", "arguments": "{\\\"query\\\": \\\"test\\\"}"}], "reasoning_content": "I should search first.", "created_at": "2026-04-28T10:00:05Z"}
        """.data(using: .utf8)!
        let msg = try JSONDecoder().decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.role, "assistant")
        XCTAssertEqual(msg.toolCalls?.count, 1)
        XCTAssertEqual(msg.toolCalls?.first?.name, "web_search")
        XCTAssertEqual(msg.reasoningContent, "I should search first.")
    }

    func testDianeMessageGraphDecoding() throws {
        let json = """
        {"entity_id": "msg-graph-1", "created_at": "2026-04-28T10:00:05Z", "properties": {"role": "assistant", "content": "Here is the result.", "sequence_number": 3, "toolCalls": [{"id": "call_x", "name": "get_weather", "arguments": {"city": "London"}}], "reasoningContent": "Thinking..."}}
        """.data(using: .utf8)!
        let msg = try JSONDecoder().decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.id, "msg-graph-1")
        XCTAssertEqual(msg.role, "assistant")
    }
}

// MARK: - Agent Model Tests

final class AgentModelTests: XCTestCase {

    func testAgentDefDecoding() throws {
        let json = """
        {"id": "agent-1", "name": "diane-default", "description": "Default agent", "flow_type": "pipeline", "visibility": "public", "is_default": true, "tool_count": 5, "created_at": "2026-01-01T00:00:00Z"}
        """.data(using: .utf8)!
        let agent = try JSONDecoder().decode(AgentDef.self, from: json)
        XCTAssertEqual(agent.name, "diane-default")
        XCTAssertEqual(agent.flowType, "pipeline")
        XCTAssertEqual(agent.isDefault, true)
        XCTAssertEqual(agent.toolCount, 5)
    }

    func testAgentOverrideConfigDecoding() throws {
        let json = """
        {"agent_name": "diane-default", "model_name": "gpt-4o", "model_provider": "openai", "skills": ["web_search"], "model_temperature": 0.7, "visibility": "private"}
        """.data(using: .utf8)!
        let config = try JSONDecoder().decode(AgentOverrideConfig.self, from: json)
        XCTAssertEqual(config.modelName, "gpt-4o")
        XCTAssertEqual(config.modelProvider, "openai")
        XCTAssertEqual(config.skills?.count, 1)
        XCTAssertEqual(config.modelTemperature, 0.7)
    }

    func testAgentDefResponseDecoding() throws {
        let json = """
        {"agents": [{"id": "a1", "name": "agent-1", "flow_type": "pipeline", "visibility": "public", "is_default": false, "tool_count": 3}]}
        """.data(using: .utf8)!
        struct Response: Decodable { let agents: [AgentDef] }
        let resp = try JSONDecoder().decode(Response.self, from: json)
        XCTAssertEqual(resp.agents.count, 1)
        XCTAssertEqual(resp.agents.first?.name, "agent-1")
    }
}

// MARK: - Graph Node Model Tests

final class NodeModelTests: XCTestCase {

    func testRelayNodeDecoding() throws {
        let json = """
        {"instance_id": "node-1", "hostname": "mcj-mini", "version": "1.38.5", "online": true, "healthy": true, "tool_count": 12, "mode": "master", "connected_at": "2026-05-07T10:00:00Z"}
        """.data(using: .utf8)!
        let node = try JSONDecoder().decode(RelayNode.self, from: json)
        XCTAssertEqual(node.hostname, "mcj-mini")
        XCTAssertTrue(node.online)
        XCTAssertEqual(node.healthy, true)
        XCTAssertEqual(node.toolCount, 12)
    }

    func testNodesResponseDecoding() throws {
        let json = """
        {"nodes": [{"instance_id": "n1", "online": true, "healthy": true}]}
        """.data(using: .utf8)!
        struct Response: Decodable { let nodes: [RelayNode] }
        let resp = try JSONDecoder().decode(Response.self, from: json)
        XCTAssertEqual(resp.nodes.count, 1)
    }
}

// MARK: - Schema Model Tests

final class SchemaModelTests: XCTestCase {

    func testSchemaNodeTypeDecoding() throws {
        let json = """
        {"type_name": "session", "label": "Session", "description": "A conversation session", "namespace": "memory", "properties": [{"name": "title", "type": "string", "description": "Session title"}], "object_count": 42, "relationship_count": 7}
        """.data(using: .utf8)!
        let type = try JSONDecoder().decode(SchemaNodeType.self, from: json)
        XCTAssertEqual(type.typeName, "session")
        XCTAssertEqual(type.label, "Session")
        XCTAssertEqual(type.properties.count, 1)
        XCTAssertEqual(type.objectCount, 42)
    }

    func testSchemaResponseDecoding() throws {
        let json = """
        {"node_types": [{"type_name": "session", "label": "Session", "description": "desc", "namespace": "memory", "properties": [], "object_count": 0, "relationship_count": 0}], "relationships": [{"name": "belongs_to", "label": "Belongs To", "inverse_label": "Has", "description": "Message belongs to session", "source_type": "message", "target_type": "session"}]}
        """.data(using: .utf8)!
        let resp = try JSONDecoder().decode(SchemaResponse.self, from: json)
        XCTAssertEqual(resp.nodeTypes.count, 1)
        XCTAssertEqual(resp.relationships.count, 1)
        XCTAssertEqual(resp.nodeTypes.first?.typeName, "session")
    }

    func testSchemaRelationshipDecoding() throws {
        let json = """
        {"name": "belongs_to", "label": "Belongs To", "inverse_label": "Has", "description": "Message belongs to a session", "source_type": "message", "target_type": "session"}
        """.data(using: .utf8)!
        let rel = try JSONDecoder().decode(SchemaRelationship.self, from: json)
        XCTAssertEqual(rel.name, "belongs_to")
        XCTAssertEqual(rel.label, "Belongs To")
        XCTAssertEqual(rel.sourceType, "message")
        XCTAssertEqual(rel.targetType, "session")
    }
}

// MARK: - Component Tests

final class ComponentTests: XCTestCase {

    func testStatCardViewProperties() {
        let card = StatCardView(title: "Sessions", value: "42", icon: "message")
        XCTAssertEqual(card.title, "Sessions")
        XCTAssertEqual(card.value, "42")
        XCTAssertEqual(card.icon, "message")
    }

    func testStatusBadgeFromStatus() {
        let active = StatusBadgeView(status: "active")
        XCTAssertEqual(active.text, "Active")
        XCTAssertEqual(active.color, .green)

        let error = StatusBadgeView(status: "error")
        XCTAssertEqual(error.text, "Error")
        XCTAssertEqual(error.color, .red)

        let completed = StatusBadgeView(status: "completed")
        XCTAssertEqual(completed.text, "Completed")
        XCTAssertEqual(completed.color, .gray)
    }
}

// MARK: - AppState Tests

@MainActor
final class AppStateTests: XCTestCase {

    func testInitialState() {
        let state = AppState()
        XCTAssertFalse(state.isConnected)
        XCTAssertFalse(state.isReady)
        XCTAssertEqual(state.selectedSidebarItem, .dashboard)
    }

    func testIsReadyRequiresConnection() {
        let state = AppState()
        state.isConnected = true
        XCTAssertTrue(state.isReady)
        state.isConnected = false
        XCTAssertFalse(state.isReady)
    }

    func testSidebarItemIcons() {
        XCTAssertEqual(SidebarItem.sessions.systemIcon, "message")
        XCTAssertEqual(SidebarItem.dashboard.systemIcon, "chart.bar.fill")
        XCTAssertEqual(SidebarItem.agents.systemIcon, "brain.head.profile")
    }

    func testSidebarItemAllCases() {
        XCTAssertEqual(SidebarItem.allCases.count, 9)
        XCTAssertEqual(SidebarItem.allCases.first, .dashboard)
        XCTAssertEqual(SidebarItem.allCases.last, .system)
    }
}

// MARK: - Data Validation Tests

final class DataValidationTests: XCTestCase {

    /// Verify that the API response shape expected by each view matches
    /// the actual model definitions (regression guard against rename drift).

    func testSessionResponseHasExpectedFields() {
        // The SessionsView expects: items array with id, title, status, message_count
        let json = """
        {"items": [{"id": "x", "title": "y", "status": "active", "message_count": 1, "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}]}
        """.data(using: .utf8)!
        XCTAssertNoThrow(try JSONDecoder().decode([String: [DianeSession]].self, from: json))
    }

    func testAgentsResponseHasExpectedFields() {
        // The AgentsView expects: agents array with id, name, flow_type
        let json = """
        {"agents": [{"id": "a1", "name": "agent-1", "flow_type": "pipeline", "visibility": "public", "is_default": false, "tool_count": 3}]}
        """.data(using: .utf8)!
        struct Response: Decodable { let agents: [AgentDef] }
        XCTAssertNoThrow(try JSONDecoder().decode(Response.self, from: json))
    }

    func testMCPServersResponseHasExpectedFields() {
        // The MCPServersView expects: servers array with name, enabled, type
        let json = """
        {"servers": [{"name": "fs", "enabled": true, "type": "stdio"}]}
        """.data(using: .utf8)!
        struct Response: Decodable { let servers: [MCPServer] }
        XCTAssertNoThrow(try JSONDecoder().decode(Response.self, from: json))
    }

    func testNodesResponseHasExpectedFields() {
        // The RelayNodesView expects: nodes array with instance_id, online, healthy
        let json = """
        {"nodes": [{"instance_id": "n1", "hostname": "mini", "online": true, "healthy": true}]}
        """.data(using: .utf8)!
        struct Response: Decodable { let nodes: [RelayNode] }
        XCTAssertNoThrow(try JSONDecoder().decode(Response.self, from: json))
    }

    func testStatsResponseHasExpectedFields() {
        // The Dashboard/StatsView expects: AgentStatsResponse with agents array
        let json = """
        {"agents": [{"agent_name": "default", "total_runs": 0, "success_runs": 0, "error_runs": 0, "avg_duration_ms": 0, "avg_step_count": 0, "avg_tool_calls": 0, "avg_input_tokens": 0, "avg_output_tokens": 0, "total_duration_ms": 0, "total_input_tokens": 0, "total_output_tokens": 0, "total_cost_usd": 0, "avg_cost_usd": 0, "success_rate": 0}], "totals": {"total_runs": 100, "total_success": 95, "total_errors": 5, "total_duration_ms": 0, "total_input_tokens": 0, "total_output_tokens": 0, "total_cost_usd": 0, "overall_avg_duration_ms": 0, "overall_success_rate": 0}, "hours": 24}
        """.data(using: .utf8)!
        XCTAssertNoThrow(try JSONDecoder().decode(AgentStatsResponse.self, from: json))
    }
}
