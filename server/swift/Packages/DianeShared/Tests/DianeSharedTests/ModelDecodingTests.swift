import Testing
import Foundation
@testable import DianeShared

// MARK: - Model Decoding Tests

struct ModelDecodingTests {

    // MARK: - DianeSession

    @Test func decodeSessionFlat() throws {
        let json = """
        {
            "id": "sess-123",
            "title": "Test Session",
            "status": "active",
            "message_count": 5,
            "created_at": "2025-01-15T10:30:00Z",
            "updated_at": "2025-01-15T11:30:00Z",
            "agent_name": "Assistant"
        }
        """.data(using: .utf8)!

        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        #expect(session.id == "sess-123")
        #expect(session.title == "Test Session")
        #expect(session.status == "active")
        #expect(session.messageCount == 5)
        #expect(session.agentName == "Assistant")
    }

    @Test func decodeSessionMinimal() throws {
        let json = """
        {
            "id": "sess-456",
            "title": null
        }
        """.data(using: .utf8)!

        let session = try JSONDecoder().decode(DianeSession.self, from: json)
        #expect(session.id == "sess-456")
        #expect(session.title == nil)
        #expect(session.status == nil)
    }

    // MARK: - DianeMessage

    @Test func decodeMessage() throws {
        let json = """
        {
            "id": "msg-001",
            "role": "user",
            "content": "Hello, world!",
            "created_at": "2025-01-15T10:30:00Z"
        }
        """.data(using: .utf8)!

        let message = try JSONDecoder().decode(DianeMessage.self, from: json)
        #expect(message.id == "msg-001")
        #expect(message.role == "user")
        #expect(message.content == "Hello, world!")
        #expect(message.toolCalls == nil)
    }

    @Test func decodeMessageWithToolCalls() throws {
        let json = """
        {
            "id": "msg-002",
            "role": "assistant",
            "content": "",
            "tool_calls": [
                {
                    "name": "get_weather",
                    "arguments": "{\\"city\\": \\"NYC\\"}",
                    "result": "{\\"temp\\": 72}"
                }
            ]
        }
        """.data(using: .utf8)!

        let message = try JSONDecoder().decode(DianeMessage.self, from: json)
        #expect(message.id == "msg-002")
        #expect(message.role == "assistant")
        #expect(message.toolCalls?.count == 1)
        #expect(message.toolCalls?.first?.name == "get_weather")
    }

    // MARK: - StreamChatEvent

    @Test func decodeStreamChatEvent() throws {
        let json = """
        {
            "type": "token",
            "content": "Hello",
            "session_id": "sess-123",
            "run_id": "run-456"
        }
        """.data(using: .utf8)!

        let event = try JSONDecoder().decode(StreamChatEvent.self, from: json)
        #expect(event.type == "token")
        #expect(event.content == "Hello")
        #expect(event.sessionID == "sess-123")
        #expect(event.runID == "run-456")
    }

    // MARK: - AgentDef

    @Test func decodeAgentDef() throws {
        let json = """
        {
            "id": "def-001",
            "name": "Code Assistant",
            "description": "Helps with code",
            "model": "gpt-4",
            "provider": "openai",
            "tools": ["read_file", "write_file"],
            "max_tokens": 4096,
            "temperature": 0.7,
            "is_active": true
        }
        """.data(using: .utf8)!

        let def = try JSONDecoder().decode(AgentDef.self, from: json)
        #expect(def.id == "def-001")
        #expect(def.name == "Code Assistant")
        #expect(def.model == "gpt-4")
        #expect(def.tools?.count == 2)
        #expect(def.isActive == true)
    }

    // MARK: - MCPServer

    @Test func decodeMCPServer() throws {
        let json = """
        {
            "id": "mcp-001",
            "name": "Filesystem",
            "url": "http://localhost:3001",
            "status": "connected",
            "enabled": true
        }
        """.data(using: .utf8)!

        let server = try JSONDecoder().decode(MCPServer.self, from: json)
        #expect(server.id == "mcp-001")
        #expect(server.name == "Filesystem")
        #expect(server.status == "connected")
        #expect(server.enabled == true)
    }

    // MARK: - Project

    @Test func decodeProject() throws {
        let json = """
        {
            "id": "proj-001",
            "name": "My Project",
            "description": "A test project",
            "status": "active",
            "agent_count": 3,
            "session_count": 42
        }
        """.data(using: .utf8)!

        let project = try JSONDecoder().decode(Project.self, from: json)
        #expect(project.id == "proj-001")
        #expect(project.name == "My Project")
        #expect(project.agentCount == 3)
        #expect(project.sessionCount == 42)
    }

    // MARK: - RelayNode

    @Test func decodeRelayNode() throws {
        let json = """
        {
            "id": "node-001",
            "name": "us-east-1",
            "url": "https://relay.example.com",
            "status": "online",
            "node_type": "relay",
            "region": "us-east-1",
            "connected_agents": 5,
            "load": 0.65
        }
        """.data(using: .utf8)!

        let node = try JSONDecoder().decode(RelayNode.self, from: json)
        #expect(node.id == "node-001")
        #expect(node.name == "us-east-1")
        #expect(node.status == "online")
        #expect(node.connectedAgents == 5)
    }

    // MARK: - Schema Types

    @Test func decodeSchemaType() throws {
        let json = """
        {
            "name": "Session",
            "fields": [
                {"name": "id", "type": "String", "required": true},
                {"name": "title", "type": "String", "required": false}
            ],
            "description": "A chat session"
        }
        """.data(using: .utf8)!

        let type = try JSONDecoder().decode(SchemaType.self, from: json)
        #expect(type.name == "Session")
        #expect(type.fields?.count == 2)
        #expect(type.fields?.first?.name == "id")
        #expect(type.fields?.first?.required == true)
    }

    // MARK: - DateUtils

    @Test func testParseISO8601() throws {
        let date = DateUtils.parseISO8601("2025-01-15T10:30:00Z")
        #expect(date != nil)

        let dateWithFractional = DateUtils.parseISO8601("2025-01-15T10:30:00.123Z")
        #expect(dateWithFractional != nil)

        let nilDate = DateUtils.parseISO8601(nil)
        #expect(nilDate == nil)

        let badDate = DateUtils.parseISO8601("not-a-date")
        #expect(badDate == nil)
    }

    // MARK: - NumberFormatting

    @Test func testCompactFormatting() throws {
        #expect(NumberFormatting.compact(999) == "999")
        #expect(NumberFormatting.compact(1500) == "1.5K")
        #expect(NumberFormatting.compact(2_500_000) == "2.5M")
        #expect(NumberFormatting.compact(1_500_000_000) == "1.5B")
    }

    @Test func testFormatted() throws {
        #expect(NumberFormatting.formatted(1234567) == "1,234,567")
    }

    // MARK: - ViewFormatting

    @Test func testMessagePreview() throws {
        #expect(ViewFormatting.messagePreview("Hello World") == "Hello World")
        #expect(ViewFormatting.messagePreview(nil) == "Empty message")
        #expect(ViewFormatting.messagePreview("") == "Empty message")
        #expect(ViewFormatting.messagePreview("Hello\nWorld") == "Hello World")
    }

    @Test func testTruncate() throws {
        #expect(ViewFormatting.truncate("Hello World", maxLength: 5) == "Hello...")
        #expect(ViewFormatting.truncate("Hi", maxLength: 5) == "Hi")
    }

    @Test func testPluralize() throws {
        #expect(ViewFormatting.pluralize(1, singular: "message") == "message")
        #expect(ViewFormatting.pluralize(2, singular: "message") == "messages")
    }

    // MARK: - StatusColors

    @Test func testStatusColor() throws {
        // Just verify these don't crash
        _ = StatusColors.statusColor("active")
        _ = StatusColors.statusColor("error")
        _ = StatusColors.statusColor("offline")
        _ = StatusColors.statusColor(nil)
    }
}
