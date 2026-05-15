import XCTest
@testable import Diane

/// Comprehensive edge case tests for model decoding and formatting.
///
/// Covers vulnerabilities the existing ModelEdgeCaseTests don't:
/// - Timezone offsets (not just Z)
/// - Control characters in content
/// - NaN / Infinity in JSON numbers
/// - Null vs empty arrays (`[]` vs `null`)
/// - Duplicate values in comma-separated lists
/// - Very deep nesting in tool arguments
/// - Far-future / far-past dates
/// - Very large payloads (10K items)
/// - Duplicate session IDs
@MainActor
final class EdgeCaseDecodingTests: XCTestCase {

    let decoder: JSONDecoder = {
        let d = JSONDecoder()
        return d
    }()

    // MARK: - Timezone Handling

    func testSessionWithPositiveTimezoneOffset() throws {
        let json = """
        {"id":"sess-1","title":"Test","status":"active","created_at":"2026-05-07T23:30:00+02:00"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "sess-1")
        // createdAt should decode with timezone
        XCTAssertNotNil(session.createdAt)
    }

    func testSessionWithNegativeTimezoneOffset() throws {
        let json = """
        {"id":"sess-2","title":"Test","status":"active","created_at":"2026-05-07T23:30:00-05:00"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "sess-2")
    }

    func testSessionWithUTCMinusZero() throws {
        let json = """
        {"id":"sess-3","title":"Test","status":"active","created_at":"2026-05-07T23:30:00+00:00"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "sess-3")
    }

    func testMessageWithTimezoneTimestamp() throws {
        let json = """
        {"id":"msg-1","role":"user","content":"Hello","created_at":"2026-05-07T20:30:00+02:00"}
        """.data(using: .utf8)!
        let msg = try decoder.decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.id, "msg-1")
        XCTAssertEqual(msg.content, "Hello")
    }

    // MARK: - Control Characters in Content

    func testMessageWithNewlinesInContent() throws {
        let json = """
        {"id":"msg-1","role":"user","content":"Line 1\\nLine 2\\nLine 3"}
        """.data(using: .utf8)!
        let msg = try decoder.decode(DianeMessage.self, from: json)
        XCTAssertTrue(msg.content.contains("\n"))
    }

    func testMessageWithTabsInContent() throws {
        let json = """
        {"id":"msg-2","role":"assistant","content":"Col1\\tCol2\\tCol3"}
        """.data(using: .utf8)!
        let msg = try decoder.decode(DianeMessage.self, from: json)
        XCTAssertTrue(msg.content.contains("\t"))
    }

    func testMessageWithNullByteInContent() throws {
        // JSON allows \\u0000
        let json = """
        {"id":"msg-3","role":"system","content":"Hello\\\\u0000World"}
        """.data(using: .utf8)!
        let msg = try decoder.decode(DianeMessage.self, from: json)
        // Content should contain the literal \\u0000 or decoded null byte
        // Either way, should not crash
        XCTAssertNotNil(msg)
    }

    // MARK: - NaN / Infinity in JSON

    func testSessionWithNaNTotalTokens() throws {
        let json = """
        {"id":"sess-1","title":"Test","status":"active","total_tokens":"NaN"}
        """.data(using: .utf8)!
        // JSONDecoder with default non-conforming float strategy should throw
        // But since totalTokens is Int?, it should be nil
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "sess-1")
        // totalTokens should be nil (failed to parse "NaN" as Int)
        XCTAssertNil(session.totalTokens)
    }

    func testMessageWithUnreasonableTokenCount() throws {
        let json = """
        {"id":"msg-1","role":"user","content":"Hi","token_count":999999999999}
        """.data(using: .utf8)!
        let msg = try decoder.decode(DianeMessage.self, from: json)
        XCTAssertGreaterThan(msg.tokenCount ?? 0, 0)
    }

    func testCostWithInfinity() throws {
        // Formatting function must not crash on infinity
        let result = formatCost(Double.infinity)
        // Should produce something (likely "inf" or similar)
        XCTAssertFalse(result.isEmpty)
    }

    func testFormatTokenCountWithMaxInt() throws {
        let result = formatTokenCount(Int.max)
        XCTAssertFalse(result.isEmpty)
    }

    // MARK: - Null vs Empty Arrays

    func testNullSessionsList() throws {
        let json = """
        {"sessions":null}
        """.data(using: .utf8)!
        struct NullSessionsResponse: Codable {
            let sessions: [DianeSession]?
        }
        let resp = try decoder.decode(NullSessionsResponse.self, from: json)
        XCTAssertNil(resp.sessions, "null should decode as nil, not empty array")
    }

    func testEmptySessionsList() throws {
        let json = """
        {"sessions":[]}
        """.data(using: .utf8)!
        struct EmptySessionsResponse: Codable {
            let sessions: [DianeSession]
        }
        let resp = try decoder.decode(EmptySessionsResponse.self, from: json)
        XCTAssertTrue(resp.sessions.isEmpty, "[] should decode as empty array")
    }

    func testNullAgentToolsList() throws {
        let json = """
        {"name":"test-agent","description":"Test","flow_type":"chat","visibility":"project","is_default":false,"tool_count":0,"tools":null}
        """.data(using: .utf8)!
        struct AgentWithNullTools: Codable {
            let name: String
            let tools: [String]?
        }
        let agent = try decoder.decode(AgentWithNullTools.self, from: json)
        XCTAssertNil(agent.tools)
    }

    // MARK: - Duplicate Values in Comma Lists

    func testAgentsViewParseCommaListWithDuplicates() throws {
        let view = AgentsView()
        let result = view.parseCommaList("a,b,a,c,b")
        // Current behavior: returns ["a", "b", "a", "c", "b"] (no dedup)
        // This is a potential issue — duplicates shown in UI
        XCTAssertEqual(result, ["a", "b", "a", "c", "b"])
    }

    // MARK: - Deep Nesting

    func testDeeplyNestedToolCallArgs() throws {
        // Build deeply nested JSON string as escaped string for tool arguments
        var inner = "{\"level0\":true}"
        for i in 1...10 {
            inner = "{\"level\(i)\":\(inner)}"
        }
        // arguments field is a String — escape it for JSON
        let escaped = inner.replacingOccurrences(of: "\"", with: "\\\"")
        let json = """
        {"id":"tool-1","name":"deep_func","arguments":"\(escaped)"}
        """.data(using: .utf8)!
        let toolCall = try decoder.decode(ToolCall.self, from: json)
        XCTAssertEqual(toolCall.id, "tool-1")
        // formatToolArgs with deeply nested JSON should not crash
        let view = SessionsView()
        let formatted = view.formatToolArgs(toolCall.arguments ?? "")
        XCTAssertFalse(formatted.isEmpty)
    }

    // MARK: - Very Large Payload

    func testVeryLongAgentName() throws {
        let longName = String(repeating: "x", count: 10_000)
        let json = """
        {"id":"agent-1","name":"\(longName)","description":"Test","flow_type":"chat","visibility":"project","is_default":false,"tool_count":0}
        """.data(using: .utf8)!
        let agent = try decoder.decode(AgentDef.self, from: json)
        XCTAssertEqual(agent.name.count, 10_000)
    }

    // MARK: - Duplicate Session IDs

    func testDuplicateSessionIDs() throws {
        let json = """
        [
            {"id":"dupe-1","title":"First","status":"active"},
            {"id":"dupe-2","title":"Second","status":"active"},
            {"id":"dupe-1","title":"Duplicate","status":"paused"},
            {"id":"unique-3","title":"Third","status":"active"}
        ]
        """.data(using: .utf8)!
        let sessions = try decoder.decode([DianeSession].self, from: json)
        let ids = sessions.map(\.id)
        // Verify duplicates exist
        let duplicateIDs = Dictionary(grouping: ids, by: { $0 }).filter { $1.count > 1 }
        XCTAssertEqual(duplicateIDs.count, 1)
        XCTAssertEqual(duplicateIDs["dupe-1"]?.count, 2)
    }

    // MARK: - Missing Required Fields

    func testSessionWithoutId() throws {
        let json = """
        {"title":"No ID","status":"active"}
        """.data(using: .utf8)!
        // Should fail - id is required
        XCTAssertThrowsError(try decoder.decode(DianeSession.self, from: json))
    }

    func testMessageWithoutRequiredRole() throws {
        let json = """
        {"id":"msg-1","content":"No role"}
        """.data(using: .utf8)!
        // role defaults to "" (empty string) via custom decoder
        let msg = try decoder.decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.role, "", "Missing role should default to empty string")
        XCTAssertEqual(msg.content, "No role")
    }

    // MARK: - Mixed Graph/Flat Format

    func testMixedSessionFormat() throws {
        // Session may use entity_id instead of id in some API responses
        let json = """
        {"entity_id":"graph-sess-1","title":"Graph Format","status":"active"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "graph-sess-1")
    }

    // MARK: - Unicode Edge Cases

    func testSessionWithEmojiTitle() throws {
        let json = """
        {"id":"sess-emoji","title":"🔥 Test Session 🚀","status":"active"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertEqual(session.title, "🔥 Test Session 🚀")
    }

    func testAgentWithUnicodeDescription() throws {
        let json = """
        {"id":"agent-u","name":"幸子","description":"日本語の説明","flow_type":"chat","visibility":"project","is_default":false,"tool_count":0}
        """.data(using: .utf8)!
        let agent = try decoder.decode(AgentDef.self, from: json)
        XCTAssertEqual(agent.name, "幸子")
        XCTAssertEqual(agent.description, "日本語の説明")
    }

    func testMessageWithMixedScripts() throws {
        let json = """
        {"id":"msg-multi","role":"user","content":"Hello 世界 مرحبا 😊"}
        """.data(using: .utf8)!
        let msg = try decoder.decode(DianeMessage.self, from: json)
        XCTAssertEqual(msg.content, "Hello 世界 مرحبا 😊")
    }

    // MARK: - Very Old / Very New Dates

    func testSessionWithUnixEpochDate() throws {
        let json = """
        {"id":"sess-old","title":"Old","status":"active","created_at":"1970-01-01T00:00:00Z"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertNotNil(session.createdAt)
        // friendlyDate should handle epoch
        let view = SystemView()
        let friendly = view.friendlyDate(session.createdAt ?? "")
        XCTAssertFalse(friendly.isEmpty)
    }

    func testSessionWithYear2038Date() throws {
        let json = """
        {"id":"sess-2038","title":"Future","status":"active","created_at":"2038-01-19T03:14:07Z"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertNotNil(session.createdAt)
    }

    func testSessionWithDistantFutureDate() throws {
        let json = """
        {"id":"sess-future","title":"Way Future","status":"active","created_at":"2099-12-31T23:59:59Z"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertNotNil(session.createdAt)
    }

    // MARK: - Ultra-Short Session IDs

    func testSessionWithTinyID() throws {
        let json = """
        {"id":"x","title":"Tiny","status":"active"}
        """.data(using: .utf8)!
        let session = try decoder.decode(DianeSession.self, from: json)
        XCTAssertEqual(session.id, "x")
    }

    func testSessionIDShortFormWithTinyID() throws {
        let view = SessionsView()
        // sessionIDShortForm should not crash on very short IDs
        let short = view.sessionIDShortForm("x")
        XCTAssertEqual(short, "x")
    }

    // MARK: - Edge Cases for Comma-List Parsing

    func testAgentsViewParseCommaListWithSpacesAroundCommas() throws {
        let view = AgentsView()
        // Mixed spacing
        let result = view.parseCommaList(" a , b ,c, d ")
        XCTAssertEqual(result, ["a", "b", "c", "d"])
    }

    func testAgentsViewParseCommaListWithCommasOnly() throws {
        let view = AgentsView()
        // Only commas, no values
        let result = view.parseCommaList(",,,")
        XCTAssertTrue(result.isEmpty)
    }

    // MARK: - Formatting Edge Cases

    func testFormatToolArgsWithEmptyObject() throws {
        let view = SessionsView()
        let result = view.formatToolArgs("{}")
        XCTAssertEqual(result, "{}")
    }

    func testFormatToolArgsWithNull() throws {
        let view = SessionsView()
        let result = view.formatToolArgs("null")
        XCTAssertEqual(result, "null")
    }

    func testFormatToolArgsWithArrayOfPrimitives() throws {
        let view = SessionsView()
        let result = view.formatToolArgs("[1, 2, 3]")
        XCTAssertTrue(result.contains("1"))
        XCTAssertTrue(result.contains("2"))
    }

    func testFormatDurationWithMaxDouble() throws {
        let result = formatDuration(Double.greatestFiniteMagnitude)
        XCTAssertFalse(result.isEmpty)
    }

    func testFormatDurationWithNegative() throws {
        let result = formatDuration(-1000)
        // Should handle gracefully — negative ms
        XCTAssertFalse(result.isEmpty)
    }

    func testFormatCountWithNegative() throws {
        let result = formatCount(-500)
        // Current implementation: runs through the switch, probably prints raw
        XCTAssertFalse(result.isEmpty)
    }

    func testFormatCostWithZero() throws {
        let result = formatCost(0)
        // Zero cost
        XCTAssertFalse(result.isEmpty)
    }

    func testFormatCostWithTinyValue() throws {
        let result = formatCost(0.000001)
        // Very small value — should not throw
        XCTAssertFalse(result.isEmpty)
    }

    // MARK: - Provider Model Decoding

    func testProjectProviderInfoDecoding() throws {
        // Must match the local API response shape (camelCase keys)
        let json = """
        [
            {
                "provider": "deepseek",
                "baseUrl": "https://api.deepseek.com",
                "generativeModel": "deepseek-chat",
                "embeddingModel": "gemini-embedding-2-preview"
            },
            {
                "provider": "google",
                "baseUrl": null,
                "generativeModel": "gemini-3.1-flash-lite-preview",
                "embeddingModel": null
            }
        ]
        """.data(using: .utf8)!
        let providers = try decoder.decode([ProjectProviderInfo].self, from: json)
        XCTAssertEqual(providers.count, 2)
        XCTAssertEqual(providers[0].provider, "deepseek")
        XCTAssertEqual(providers[0].baseUrl, "https://api.deepseek.com")
        XCTAssertEqual(providers[0].generativeModel, "deepseek-chat")
        XCTAssertEqual(providers[0].embeddingModel, "gemini-embedding-2-preview")
        XCTAssertEqual(providers[1].provider, "google")
        XCTAssertNil(providers[1].baseUrl)
        XCTAssertEqual(providers[1].generativeModel, "gemini-3.1-flash-lite-preview")
        XCTAssertNil(providers[1].embeddingModel)
    }

    func testProjectProviderInfoDecodingWrapped() throws {
        // Must match the local API's /api/providers response: {"providers": [...]}
        let json = """
        {
            "providers": [
                {
                    "provider": "deepseek",
                    "baseUrl": "https://api.deepseek.com",
                    "generativeModel": "deepseek-chat",
                    "embeddingModel": "gemini-embedding-2-preview"
                }
            ]
        }
        """.data(using: .utf8)!
        struct Response: Decodable { let providers: [ProjectProviderInfo]? }
        let resp = try decoder.decode(Response.self, from: json)
        XCTAssertNotNil(resp.providers)
        XCTAssertEqual(resp.providers?.count, 1)
    }
}
