import XCTest
@testable import Diane

/// Response shape snapshot tests — verify every field of every endpoint.
///
/// These catch silent contract violations: API changes response shape
/// but Swift model isn't updated, causing empty states with no error.
@MainActor
final class LiveAPIResponseShapeTests: XCTestCase {

    var client: DianeAPIClient!

    override func setUp() async throws {
        client = DianeAPIClient(baseURL: "http://127.0.0.1:8890")
    }

    private func requireServer() async throws {
        let reachable = await client.checkReachability()
        if !reachable {
            throw XCTSkip("diane serve not running on 127.0.0.1:8890")
        }
    }

    // MARK: - Sessions

    func testSessionsResponseFullShape() async throws {
        try await requireServer()
        let sessions = try await client.fetchSessions()

        for s in sessions {
            // ID — must never be empty
            XCTAssertFalse(s.id.isEmpty, "Session ID must not be empty")

            // Status — must be known status
            if let status = s.status, !status.isEmpty {
                let validStatuses = ["active", "running", "paused", "idle",
                                     "completed", "closed", "done", "error", "failed"]
                if !validStatuses.contains(status.lowercased()) {
                    print("  ⚠ Session \(s.id) has unknown status: \(status)")
                }
            }

            // Message count — if present, must be >= 0
            if let count = s.messageCount {
                XCTAssertGreaterThanOrEqual(count, 0, "messageCount must be >= 0")
            }
            // Total tokens — if present, must be >= 0
            if let tokens = s.totalTokens {
                XCTAssertGreaterThanOrEqual(tokens, 0, "totalTokens must be >= 0")
            }
            // Timestamps — must be valid ISO8601
            if let createdAt = s.createdAt {
                XCTAssertNotNil(ISO8601DateFormatter().date(from: createdAt),
                                "createdAt must be ISO8601: \(createdAt)")
            }
            if let updatedAt = s.updatedAt {
                XCTAssertNotNil(ISO8601DateFormatter().date(from: updatedAt),
                                "updatedAt must be ISO8601: \(updatedAt)")
            }
        }
    }

    // MARK: - Messages

    func testMessagesResponseFullShape() async throws {
        try await requireServer()
        let sessions = try await client.fetchSessions()
        guard let first = sessions.first else {
            throw XCTSkip("No sessions available")
        }

        let messages = try await client.fetchSessionMessages(sessionID: first.id)

        for m in messages {
            XCTAssertFalse(m.id.isEmpty, "Message ID must not be empty")

            let validRoles = ["user", "assistant", "system", "tool"]
            if !validRoles.contains(m.role.lowercased()) {
                print("  ⚠ Message \(m.id) has unknown role: \(m.role)")
            }

            // Sequence number — if present, must be >= 0
            if let seq = m.sequenceNumber {
                XCTAssertGreaterThanOrEqual(seq, 0, "sequenceNumber must be >= 0")
            }

            // Token count — if present, must be >= 0
            if let tokens = m.tokenCount {
                XCTAssertGreaterThanOrEqual(tokens, 0, "tokenCount must be >= 0")
            }

            // Created at — if present, must be ISO8601
            if let createdAt = m.createdAt {
                XCTAssertNotNil(ISO8601DateFormatter().date(from: createdAt),
                                "createdAt must be ISO8601: \(createdAt)")
            }

            // Tool calls — if present, validate shape
            if let calls = m.toolCalls {
                for call in calls {
                    XCTAssertFalse(call.id.isEmpty, "Tool call ID must not be empty")
                    XCTAssertFalse(call.name.isEmpty, "Tool call name must not be empty")
                }
            }
        }
    }

    // MARK: - Agents

    func testAgentsResponseFullShape() async throws {
        try await requireServer()
        let agents = try await client.fetchAgentDefs()

        for a in agents {
            XCTAssertFalse(a.id.isEmpty, "Agent ID must not be empty")
            XCTAssertFalse(a.name.isEmpty, "Agent name must not be empty")

            // Flow type
            let validFlows = ["chat", "agent", "chain", "workflow", "pipeline"]
            if !validFlows.contains(a.flowType.lowercased()) {
                print("  ⚠ Agent \(a.name) has unknown flow: \(a.flowType)")
            }

            XCTAssertGreaterThanOrEqual(a.toolCount, 0, "toolCount must be >= 0")

            // Timestamps — if present, must be ISO8601
            if let createdAt = a.createdAt {
                XCTAssertNotNil(ISO8601DateFormatter().date(from: createdAt),
                                "createdAt must be ISO8601: \(createdAt)")
            }
        }
    }

    // MARK: - Relay Nodes

    func testRelayNodesResponseFullShape() async throws {
        try await requireServer()
        let nodes = try await client.fetchRelayNodes()

        for n in nodes {
            XCTAssertFalse(n.instanceID.isEmpty, "Node instanceID must not be empty")
            if let hostname = n.hostname { XCTAssertFalse(hostname.isEmpty, "Node hostname must not be empty") }

            let validModes = ["master", "slave", "standalone"]
            if let mode = n.mode, !validModes.contains(mode.lowercased()) {
                print("  ⚠ Node \(n.instanceID) has unknown mode: \(mode)")
            }
        }
    }

    // MARK: - MCP Servers

    func testMCPServersResponseFullShape() async throws {
        try await requireServer()
        let servers = try await client.fetchMCPServers()

        for s in servers {
            XCTAssertFalse(s.name.isEmpty, "MCP server name must not be empty")
            // enabled is Bool — just verify it doesn't crash
            _ = s.enabled
        }
    }

    // MARK: - Doctor

    func testDoctorResponseFullShape() async throws {
        try await requireServer()
        let doctor = try await client.fetchDoctorReport()

        XCTAssertFalse(doctor.results.isEmpty, "Doctor must return at least one check")

        for check in doctor.results {
            XCTAssertFalse(check.check.isEmpty, "Doctor check name must not be empty")
            let validStatuses = ["ok", "warning", "error"]
            XCTAssertTrue(validStatuses.contains(check.status),
                          "Doctor check status must be one of \(validStatuses): got '\(check.status)'")
        }
    }

    // MARK: - Schema

    func testSchemaResponseFullShape() async throws {
        try await requireServer()
        let schema = try await client.fetchGraphSchema()

        XCTAssertFalse(schema.nodeTypes.isEmpty, "Schema must have at least one node type")

        for nodeType in schema.nodeTypes {
            XCTAssertFalse(nodeType.typeName.isEmpty, "Node type name must not be empty")
            XCTAssertFalse(nodeType.label.isEmpty, "Node type label must not be empty")
            XCTAssertGreaterThanOrEqual(nodeType.objectCount, 0, "objectCount must be >= 0")
            XCTAssertGreaterThanOrEqual(nodeType.relationshipCount, 0, "relationshipCount must be >= 0")
        }
    }

    // MARK: - Provider Stats

    func testProviderStatsResponseFullShape() async throws {
        try await requireServer()
        let stats = try await client.fetchProviderStats(hours: 24)

        for p in stats.providers {
            XCTAssertFalse(p.providerName.isEmpty, "Provider name must not be empty")
            XCTAssertFalse(p.modelName.isEmpty, "Model name must not be empty")
            XCTAssertGreaterThanOrEqual(p.totalRuns, 0, "totalRuns must be >= 0")
            XCTAssertGreaterThanOrEqual(p.successRuns, 0, "successRuns must be >= 0")
            XCTAssertGreaterThanOrEqual(p.errorRuns, 0, "errorRuns must be >= 0")
            XCTAssertGreaterThanOrEqual(p.totalCostUsd, 0, "totalCostUsd must be >= 0")
            // UInt64 — verify no overflow
            XCTAssertGreaterThanOrEqual(p.totalInputTokens, 0, "totalInputTokens must be >= 0")
            XCTAssertGreaterThanOrEqual(p.totalOutputTokens, 0, "totalOutputTokens must be >= 0")
        }
    }
}
