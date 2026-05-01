import Foundation

/// Client for Diane's local companion API (served by `diane serve` on 127.0.0.1:8890).
///
/// This is the preferred data source for the companion app — it uses the
/// same data paths as the diane CLI (Memory Bridge for sessions, local
/// config for MCP servers, Memory Platform relay for nodes).
@MainActor
final class DianeAPIClient: ObservableObject {
    private let session: URLSession
    private let baseURL: String

    @Published private(set) var isReachable: Bool = false

    init(baseURL: String = "http://127.0.0.1:8890") {
        self.baseURL = baseURL
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 10
        session = URLSession(configuration: config)
    }

    // MARK: - Health / Server Status

    struct ServerStatus: Codable, Sendable {
        let ok: Bool
        let version: String?
        let startedAt: String?
        let serverURL: String?
        let projectID: String?

        enum CodingKeys: String, CodingKey {
            case ok
            case version
            case startedAt = "started_at"
            case serverURL = "server_url"
            case projectID = "project_id"
        }
    }

    func fetchServerStatus() async throws -> ServerStatus {
        let data = try await get("/api/status")
        return try JSONDecoder().decode(ServerStatus.self, from: data)
    }

    func checkReachability() async -> Bool {
        guard let url = URL(string: "\(baseURL)/api/status") else { return false }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.timeoutInterval = 3
        do {
            let (data, resp) = try await session.data(for: request)
            guard let http = resp as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
                isReachable = false
                return false
            }
            // Parse to confirm structure
            if let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               json["ok"] as? Bool == true {
                isReachable = true
                return true
            }
            isReachable = false
            return false
        } catch {
            isReachable = false
            return false
        }
    }

    // MARK: - Sessions

    /// Log a snippet of response data when JSON decoding fails, so we can debug API mismatches.
    private func logDecodeFailure<T>(_ type: T.Type, data: Data, context: String) {
        let prefix = String(data: data.prefix(1024), encoding: .utf8) ?? "<non-utf8>"
        logWarning("JSON decode failed for \(context) — expected \(T.self). Response prefix: \(prefix)", category: "DianeAPI")
    }

    func fetchSessions(status: String? = nil) async throws -> [DianeSession] {
        var path = "/api/sessions"
        if let s = status {
            path += "?status=\(s)"
        }
        let data = try await get(path)
        struct Response: Decodable { let items: [DianeSession]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        logDecodeFailure([DianeSession].self, data: data, context: "fetchSessions")
        return (try? JSONDecoder().decode([DianeSession].self, from: data)) ?? []
    }

    func fetchSessionMessages(sessionID: String) async throws -> [DianeMessage] {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let data = try await get("/api/sessions/\(encoded)/messages")
        struct Response: Decodable { let items: [DianeMessage]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        logDecodeFailure([DianeMessage].self, data: data, context: "fetchSessionMessages")
        return (try? JSONDecoder().decode([DianeMessage].self, from: data)) ?? []
    }

    func fetchSessionDetail(sessionID: String) async throws -> SessionDetailResponse {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let data = try await get("/api/sessions/\(encoded)")
        return try JSONDecoder().decode(SessionDetailResponse.self, from: data)
    }

    // MARK: - Chat Send

    /// Send a chat message and wait for the full agent response via the agent pipeline.
    func sendChatMessage(sessionID: String?, content: String, agentName: String = "diane-default") async throws -> ChatSendResponse {
        let body: [String: Any] = [
            "session_id": sessionID as Any,
            "content": content,
            "agent_name": agentName
        ]
        let jsonData = try JSONSerialization.data(withJSONObject: body)
        let data = try await post("/api/chat/send", body: jsonData, timeout: 180)
        return try JSONDecoder().decode(ChatSendResponse.self, from: data)
    }

    // MARK: - Session Write

    func createSession(title: String? = nil) async throws -> DianeSession {
        var body: Data? = nil
        if let t = title {
            body = try JSONEncoder().encode(["title": t])
        }
        let data = try await post("/api/sessions", body: body)
        return try JSONDecoder().decode(DianeSession.self, from: data)
    }

    func closeSession(sessionID: String) async throws {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        _ = try await delete("/api/sessions/\(encoded)")
    }

    // MARK: - MCP Servers

    func fetchMCPServers() async throws -> [MCPServer] {
        let data = try await get("/api/mcp-servers")
        struct Response: Decodable { let servers: [MCPServer]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.servers {
            return list
        }
        logDecodeFailure([MCPServer].self, data: data, context: "fetchMCPServers")
        return (try? JSONDecoder().decode([MCPServer].self, from: data)) ?? []
    }

    // MARK: - Relay Nodes

    func fetchRelayNodes() async throws -> [RelayNode] {
        let data = try await get("/api/nodes")
        struct Response: Decodable { let nodes: [RelayNode]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.nodes {
            return list
        }
        logDecodeFailure([RelayNode].self, data: data, context: "fetchRelayNodes")
        return (try? JSONDecoder().decode([RelayNode].self, from: data)) ?? []
    }

    // MARK: - MCP Tools & Prompts

    /// Fetch tools exposed by a specific MCP server.
    func fetchMCPTools(serverName: String) async throws -> [MCPTool] {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName
        let data = try await get("/api/mcp-servers/\(encoded)/tools")
        struct Response: Decodable { let tools: [MCPTool]?; let error: String? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data) {
            if let errMsg = resp.error {
                throw DianeAPIError.serverError(errMsg)
            }
            if let list = resp.tools {
                return list
            }
        }
        logDecodeFailure([MCPTool].self, data: data, context: "fetchMCPTools")
        return (try? JSONDecoder().decode([MCPTool].self, from: data)) ?? []
    }

    /// Fetch prompts exposed by a specific MCP server.
    func fetchMCPPrompts(serverName: String) async throws -> [MCPPrompt] {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName
        let data = try await get("/api/mcp-servers/\(encoded)/prompts")
        struct Response: Decodable { let prompts: [MCPPrompt]?; let error: String? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data) {
            if let errMsg = resp.error {
                throw DianeAPIError.serverError(errMsg)
            }
            if let list = resp.prompts {
                return list
            }
        }
        return (try? JSONDecoder().decode([MCPPrompt].self, from: data)) ?? []
    }

    // MARK: - MCP Server CRUD

    /// Toggle an MCP server's enabled/disabled state.
    func toggleMCPServer(serverName: String) async throws -> Bool {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName
        let data = try await post("/api/mcp-servers/toggle/\(encoded)", body: nil)
        struct Response: Decodable { let ok: Bool?; let enabled: Bool? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data) {
            return resp.enabled ?? false
        }
        return false
    }

    /// Save (add or update) an MCP server configuration.
    func saveMCPServer(_ server: MCPServer) async throws {
        let body = try JSONEncoder().encode(server)
        _ = try await post("/api/mcp-servers/store", body: body)
    }

    /// Delete an MCP server configuration.
    func deleteMCPServer(serverName: String) async throws {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName
        _ = try await post("/api/mcp-servers/delete/\(encoded)", body: nil)
    }

    // MARK: - Stats

    func fetchAgentStats(hours: Int = 24) async throws -> AgentStatsResponse {
        let data = try await get("/api/stats?hours=\(hours)")
        return try JSONDecoder().decode(AgentStatsResponse.self, from: data)
    }

    func fetchProviderStats(hours: Int = 24) async throws -> ProviderStatsResponse {
        let data = try await get("/api/stats/providers?hours=\(hours)")
        return try JSONDecoder().decode(ProviderStatsResponse.self, from: data)
    }

    func fetchProjectProviders() async throws -> [ProjectProviderInfo] {
        let data = try await get("/api/providers")
        struct Response: Decodable { let providers: [ProjectProviderInfo]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.providers {
            return list
        }
        return []
    }

    func fetchGraphObjectStats() async throws -> GraphObjectStatsResponse {
        let data = try await get("/api/stats/objects")
        return try JSONDecoder().decode(GraphObjectStatsResponse.self, from: data)
    }

    // MARK: - Graph Schema

    /// Fetch the embedded graph schema definitions (object types + relationships).
    func fetchGraphSchema() async throws -> SchemaResponse {
        let data = try await get("/api/schema")
        return try JSONDecoder().decode(SchemaResponse.self, from: data)
    }

    /// Fetch recent objects of a given schema type from the project's memory graph.
    func fetchSchemaObjects(typeName: String, limit: Int = 20) async throws -> SchemaObjectsResponse {
        let data = try await get("/api/schema/objects/\(typeName)?limit=\(limit)")
        return try JSONDecoder().decode(SchemaObjectsResponse.self, from: data)
    }

    // MARK: - Agent Definitions

    func fetchAgentDefs() async throws -> [AgentDef] {
        let data = try await get("/api/agents")
        struct Response: Decodable { let agents: [AgentDef]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.agents {
            return list
        }
        logDecodeFailure([AgentDef].self, data: data, context: "fetchAgentDefs")
        return []
    }

    // MARK: - Doctor Check

    /// Run the diane doctor diagnostics via the local API.
    func fetchDoctorReport() async throws -> DoctorResponse {
        let data = try await get("/api/doctor")
        return try JSONDecoder().decode(DoctorResponse.self, from: data)
    }

    // MARK: - Relay Nodes

    func fetchNodeTools(instanceID: String) async throws -> [MCPToolInfo] {
        let encoded = instanceID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? instanceID
        let data = try await get("/api/nodes/\(encoded)/tools")
        struct Response: Decodable { let tools: [MCPToolInfo]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.tools {
            return list
        }
        logDecodeFailure([MCPToolInfo].self, data: data, context: "fetchNodeTools")
        return []
    }

    // MARK: - HTTP

    private func get(_ path: String) async throws -> Data {
        guard let url = URL(string: "\(baseURL)\(path)") else {
            throw DianeAPIError.invalidURL(path)
        }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.timeoutInterval = 10

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw DianeAPIError.network("No HTTP response")
        }
        guard (200...299).contains(http.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? ""
            throw DianeAPIError.httpError(http.statusCode, body)
        }
        return data
    }

    private func post(_ path: String, body: Data?, timeout: TimeInterval? = nil) async throws -> Data {
        guard let url = URL(string: "\(baseURL)\(path)") else {
            throw DianeAPIError.invalidURL(path)
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.timeoutInterval = timeout ?? 10
        if let b = body {
            request.httpBody = b
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw DianeAPIError.network("No HTTP response")
        }
        guard (200...299).contains(http.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? ""
            throw DianeAPIError.httpError(http.statusCode, body)
        }
        return data
    }

    private func delete(_ path: String) async throws -> Data {
        guard let url = URL(string: "\(baseURL)\(path)") else {
            throw DianeAPIError.invalidURL(path)
        }
        var request = URLRequest(url: url)
        request.httpMethod = "DELETE"
        request.timeoutInterval = 10

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw DianeAPIError.network("No HTTP response")
        }
        guard (200...299).contains(http.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? ""
            throw DianeAPIError.httpError(http.statusCode, body)
        }
        return data
    }
}

enum DianeAPIError: Error, LocalizedError {
    case invalidURL(String)
    case network(String)
    case httpError(Int, String)
    case serverError(String)

    var errorDescription: String? {
        switch self {
        case .invalidURL(let p): return "Invalid URL: \(p)"
        case .network(let msg):  return "Network error: \(msg)"
        case .httpError(let c, let b): return "HTTP \(c): \(b)"
        case .serverError(let msg): return "Server error: \(msg)"
        }
    }
}

// MARK: - Relay Node Model

struct RelayNode: Identifiable, Codable, Hashable, Sendable {
    let instanceID: String
    let hostname: String?
    let mode: String?          // "master" or "slave" (from graph config)
    let version: String?
    let toolCount: Int?
    let connectedAt: String?
    let online: Bool           // whether node has an active relay connection

    var id: String { instanceID }

    enum CodingKeys: String, CodingKey {
        case instanceID = "instance_id"
        case hostname, mode, version
        case toolCount = "tool_count"
        case connectedAt = "connected_at"
        case online
    }

    func hash(into hasher: inout Hasher) { hasher.combine(instanceID) }
    static func == (lhs: RelayNode, rhs: RelayNode) -> Bool { lhs.instanceID == rhs.instanceID }
}

struct MCPToolInfo: Identifiable, Codable, Sendable {
    let name: String
    let description: String?

    var id: String { name }
}
