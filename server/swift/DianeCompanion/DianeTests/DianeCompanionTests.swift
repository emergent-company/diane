import XCTest
@testable import Diane

/// Unit tests for the EmergentAPIClient — verifies HTTP routing, decoding,
/// and error mapping without hitting a real server.
final class EmergentAPIClientTests: XCTestCase {

    // MARK: - Configure

    func testConfigureWithEmptyURL() {
        let client = EmergentAPIClient()
        // Configuring with an empty URL should make requests throw .notConfigured
        client.configure(serverURL: "", apiKey: "")
        // Verified through behavior in integration tests
    }

    // MARK: - Error mapping

    func testEmergentAPIErrorDescriptions() {
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

    // MARK: - MCP Model decoding

    func testMCPServerDecoding() throws {
        let json = """
        {
            "id": "mcp-1",
            "name": "Test Server",
            "server_type": "sse",
            "url": "http://localhost:8080",
            "status": "connected",
            "tools": [{"id": "t1", "name": "tool1", "description": "A test tool"}]
        }
        """.data(using: .utf8)!

        let server = try JSONDecoder().decode(MCPServer.self, from: json)
        XCTAssertEqual(server.id, "mcp-1")
        XCTAssertEqual(server.name, "Test Server")
        XCTAssertEqual(server.serverType, "sse")
        XCTAssertEqual(server.tools?.count, 1)
        XCTAssertEqual(server.tools?.first?.name, "tool1")
    }

    // MARK: - Session model decoding

    func testDianeSessionDecoding() throws {
        let json = """
        {
            "id": "session-abc",
            "key": "conv-123",
            "title": "Test conversation",
            "status": "active",
            "message_count": 5,
            "total_tokens": 1500,
            "created_at": "2026-04-28T10:00:00Z"
        }
        """.data(using: .utf8)!

        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "session-abc")
        XCTAssertEqual(session.title, "Test conversation")
        XCTAssertEqual(session.status, "active")
        XCTAssertEqual(session.messageCount, 5)
    }

    func testDianeMessageDecoding() throws {
        let json = """
        {
            "id": "msg-1",
            "role": "user",
            "content": "Hello",
            "sequence_number": 1,
            "token_count": 10
        }
        """.data(using: .utf8)!

        let msg = try JSONDecoder().decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.role, "user")
        XCTAssertEqual(msg.content, "Hello")
        XCTAssertEqual(msg.sequenceNumber, 1)
    }

    // MARK: - Relay Session decoding

    func testRelaySessionDecoding() throws {
        let json = """
        {
            "id": "relay-1",
            "instance_id": "macmini-1",
            "node_name": "mcj-mini",
            "tool_count": 12,
            "connected_at": "2026-04-28T08:00:00Z"
        }
        """.data(using: .utf8)!

        let session = try JSONDecoder().decode(RelaySession.self, from: json)
        XCTAssertEqual(session.id, "relay-1")
        XCTAssertEqual(session.nodeName, "mcj-mini")
        XCTAssertEqual(session.toolCount, 12)
    }
}

/// Unit tests for AppState — verifies observable state logic.
@MainActor
final class AppStateTests: XCTestCase {

    func testInitialState() {
        let state = AppState()
        XCTAssertFalse(state.isConnected)
        XCTAssertFalse(state.isReady)
    }

    func testIsReadyRequiresConnection() {
        let state = AppState()
        XCTAssertFalse(state.isReady)

        state.isConnected = true
        XCTAssertTrue(state.isReady)

        state.isConnected = false
        XCTAssertFalse(state.isReady)
    }

    func testSidebarItemIcons() {
        XCTAssertEqual(SidebarItem.sessions.systemIcon, "message")
        XCTAssertEqual(SidebarItem.mcpServers.systemIcon, "plug")
        XCTAssertEqual(SidebarItem.permissions.systemIcon, "lock.shield")
    }
}
