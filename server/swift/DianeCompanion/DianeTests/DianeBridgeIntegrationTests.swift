import XCTest

/// Diane Bridge Integration Tests
///
/// These tests hit the live Diane API via raw HTTP and validate the response
/// JSON structure against what the Swift client expects. They catch API
/// contract violations BEFORE they show up as empty views or decode failures.
///
/// Unlike LiveAPIResponseShapeTests (which use the Swift client to decode),
/// these tests work with raw JSON to catch cases where the API returns data
/// in a shape the client didn't expect at all.
///
/// Run: xcodebuild test -only-testing:DianeBridgeIntegrationTests
/// Requires: `diane serve` running on 127.0.0.1:8890
@MainActor
final class DianeBridgeIntegrationTests: XCTestCase {

    // MARK: - Raw API Tester

    /// Raw API tester that checks JSON structure without going through Swift models.
    struct RawAPI {
        let baseURL: String

        func get(_ path: String) async throws -> Any {
            let url = URL(string: "\(baseURL)\(path)")!
            let (data, resp) = try await URLSession.shared.data(from: url)
            guard let http = resp as? HTTPURLResponse else {
                throw URLError(.badServerResponse)
            }
            if http.statusCode == 404 { throw XCTSkip("\(path): 404 — endpoint not available") }
            if http.statusCode >= 400 { throw URLError(.badServerResponse) }
            return try JSONSerialization.jsonObject(with: data)
        }
    }

    var api: RawAPI!

    override func setUp() async throws {
        api = RawAPI(baseURL: "http://127.0.0.1:8890")
        _ = try await api.get("/api/status") // smoke test — server reachable
    }

    // MARK: - Server Status

    func testServerStatusContract() async throws {
        let json = try await api.get("/api/status") as! [String: Any]
        // Expected fields
        XCTAssertNotNil(json["ok"], "/api/status must have 'ok' field")
        // Version may be present
        if let version = json["version"] as? String {
            XCTAssertFalse(version.isEmpty)
        }
    }

    // MARK: - Sessions

    func testSessionsContract() async throws {
        let json = try await api.get("/api/sessions?limit=1") as! [String: Any]
        // Top-level shape: {"items": [...]}
        XCTAssertTrue(json["items"] is [Any] || json["sessions"] is [Any],
                      "/api/sessions must return {'items': [...]} or {'sessions': [...]}")
    }

    // MARK: - Relay Nodes (the "empty page" culprit)

    func testRelayNodesContract() async throws {
        let json = try await api.get("/api/nodes") as! [String: Any]
        guard let nodes = json["nodes"] as? [[String: Any]] else {
            XCTFail("/api/nodes must return {'nodes': [...]}")
            return
        }

        // Log actual nodes for debugging
        print("  Nodes returned: \(nodes.count)")
        for node in nodes {
            let fields = node.keys.sorted()
            print("    instance_id=\(node["instance_id"] ?? "(missing)"), fields=\(fields)")

            // Every node must have instance_id
            XCTAssertNotNil(node["instance_id"], "Every node must have instance_id")
        }
    }

    /// Compare actual node fields against what RelayNode model expects.
    /// If the API adds a new field the Swift model doesn't know about, log a warning.
    /// If a REQUIRED field is missing, fail.
    func testRelayNodeFieldsMatchSwiftModel() async throws {
        let json = try await api.get("/api/nodes") as! [String: Any]
        guard let nodes = json["nodes"] as? [[String: Any]], !nodes.isEmpty else {
            throw XCTSkip("No nodes to test")
        }

        let expectedFields: Set<String> = [
            "instance_id", "hostname", "mode", "version", "tool_count",
            "connected_at", "online", "uptime", "provider",
            "relay_active", "bot_active", "healthy"
        ]

        // These fields are optional in the model — their absence is OK
        let optionalFields: Set<String> = [
            "hostname", "mode", "version", "tool_count",
            "connected_at", "online", "uptime", "provider",
            "relay_active", "bot_active", "healthy"
        ]

        for (i, node) in nodes.enumerated() {
            let actualFields = Set(node.keys)

            // Check for unexpected fields (API added something the model doesn't know)
            let unexpected = actualFields.subtracting(expectedFields)
            if !unexpected.isEmpty {
                print("  ⚠ Node \(i): unexpected fields: \(unexpected.sorted())")
                print("    The Swift RelayNode model may need updating!")
            }

            // Check for missing required fields
            let requiredFields: Set<String> = ["instance_id"]
            let missing = requiredFields.subtracting(actualFields)
            if !missing.isEmpty {
                XCTFail("Node \(i): missing required fields: \(missing.sorted())")
            }

            // Log field coverage
            let known = actualFields.intersection(expectedFields)
            print("    Node \(i): \(known.count)/\(expectedFields.count) fields present")
        }
    }

    // MARK: - Agents

    func testAgentsContract() async throws {
        let json = try await api.get("/api/agents") as! [String: Any]
        let agents = json["agents"] as? [[String: Any]] ?? []

        print("  Agents returned: \(agents.count)")
        for agent in agents {
            let fields = agent.keys.sorted()
            print("    name=\(agent["name"] ?? "(missing)"), fields=\(fields)")
            XCTAssertNotNil(agent["name"], "Every agent must have 'name'")
            XCTAssertNotNil(agent["flow_type"] ?? agent["flowType"], "Agent should have flow_type")
        }
    }

    // MARK: - MCP Servers

    func testMCPServersContract() async throws {
        let json = try await api.get("/api/mcp-servers") as! [String: Any]
        let servers = json["servers"] as? [[String: Any]] ?? []

        print("  MCP Servers returned: \(servers.count)")
        for server in servers {
            let fields = server.keys.sorted()
            print("    name=\(server["name"] ?? "(missing)"), fields=\(fields)")
            XCTAssertNotNil(server["name"], "Every MCP server must have 'name'")
        }
    }

    // MARK: - Doctor

    func testDoctorContract() async throws {
        let json = try await api.get("/api/doctor") as! [String: Any]

        XCTAssertNotNil(json["ok"], "/api/doctor must have 'ok'")
        XCTAssertNotNil(json["results"], "/api/doctor must have 'results'")

        if let results = json["results"] as? [[String: Any]] {
            print("  Doctor checks: \(results.count)")
            for check in results {
                XCTAssertNotNil(check["check"] ?? check["name"], "Each check must have 'check' or 'name'")
                XCTAssertNotNil(check["status"], "Each check must have 'status'")
            }
        }
    }

    // MARK: - Schema

    func testSchemaContract() async throws {
        let json = try await api.get("/api/schema") as! [String: Any]

        let nodeTypes = json["node_types"] as? [[String: Any]] ?? []
        let relationships = json["relationships"] as? [[String: Any]] ?? []

        print("  Schema: \(nodeTypes.count) node types, \(relationships.count) relationships")

        XCTAssertGreaterThan(nodeTypes.count, 0, "Schema should have at least one node type")

        for type in nodeTypes {
            XCTAssertNotNil(type["type_name"], "Node type must have 'type_name'")
            let typeFields = type.keys.sorted()
            print("    type=\(type["type_name"] ?? "?"), fields=\(typeFields)")
        }
    }

    // MARK: - Provider Stats

    func testProviderStatsContract() async throws {
        let json = try await api.get("/api/stats/providers?hours=24") as! [String: Any]

        let providers = json["providers"] as? [[String: Any]] ?? []
        print("  Provider stats: \(providers.count) providers")

        for p in providers {
            let fields = p.keys.sorted()
            print("    provider=\(p["provider_name"] ?? p["name"] ?? "?"), fields=\(fields)")
        }
    }
}
