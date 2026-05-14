import Foundation

// MARK: - Emergent API Client

public final class EmergentAPIClient: @unchecked Sendable {
    public let http: HTTPClient

    public var baseURL: String {
        get { http.baseURL }
        set { http.baseURL = newValue }
    }

    public var apiKey: String {
        get { http.defaultHeaders["Authorization"] ?? "" }
        set { http.defaultHeaders["Authorization"] = newValue.hasPrefix("Bearer ") ? newValue : "Bearer \(newValue)" }
    }

    // MARK: - Init

    public init(baseURL: String = "", apiKey: String = "") {
        self.http = HTTPClient(baseURL: baseURL, timeoutForRequest: 30, timeoutForResource: 60)
        if !apiKey.isEmpty {
            self.apiKey = apiKey
        }
        http.defaultHeaders["Content-Type"] = "application/json"
    }

    // MARK: - Configuration

    public func configure(baseURL: String, apiKey: String) {
        self.baseURL = baseURL
        self.apiKey = apiKey
    }

    public var isConfigured: Bool {
        !baseURL.isEmpty && !apiKey.isEmpty
    }

    // MARK: - Convenience JSON helpers

    private func jsonDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        return decoder
    }

    private func getJSON<T: Decodable>(_ path: String) async throws -> T {
        let data = try await http.get(path)
        return try jsonDecoder().decode(T.self, from: data)
    }

    private func postJSON<B: Encodable, R: Decodable>(_ path: String, body: B) async throws -> R {
        let data = try JSONEncoder().encode(body)
        let responseData = try await http.post(path, body: data)
        return try jsonDecoder().decode(R.self, from: responseData)
    }

    private func postJSONEmptyBody<R: Decodable>(_ path: String) async throws -> R {
        let responseData = try await http.post(path, body: nil)
        return try jsonDecoder().decode(R.self, from: responseData)
    }

    // MARK: - Projects

    public func fetchProjects() async throws -> [Project] {
        struct ProjectsResponse: Decodable, Sendable {
            let projects: [Project]
        }
        let response: APIListResponse<Project> = try await getJSON("/api/projects")
        return response.data
    }

    public func fetchProjectStats() async throws -> ProjectStats {
        try await getJSON("/api/stats")
    }

    public func fetchProject(id: String) async throws -> Project {
        try await getJSON("/api/projects/\(id)")
    }

    // MARK: - Sessions

    public func fetchSessions(projectID: String? = nil, limit: Int = 50, offset: Int = 0) async throws -> [DianeSession] {
        var path = "/api/sessions?limit=\(limit)&offset=\(offset)"
        if let pid = projectID { path += "&project_id=\(pid)" }
        let response: APIListResponse<DianeSession> = try await getJSON(path)
        return response.data
    }

    public func fetchSession(id: String) async throws -> DianeSession {
        try await getJSON("/api/sessions/\(id)")
    }

    public func deleteSession(id: String) async throws {
        let _: Data = try await http.delete("/api/sessions/\(id)")
    }

    // MARK: - Messages

    public func fetchMessages(sessionID: String) async throws -> [DianeMessage] {
        let response: APIListResponse<DianeMessage> = try await getJSON("/api/sessions/\(sessionID)/messages")
        return response.data
    }

    // MARK: - Chat / Streaming

    public func sendMessageStream(
        sessionID: String,
        content: String,
        agentID: String? = nil
    ) -> AsyncThrowingStream<StreamChatEvent, Error> {
        var body: [String: Any] = [
            "content": content,
            "session_id": sessionID,
        ]
        if let agentID { body["agent_id"] = agentID }

        let payload = try! JSONSerialization.data(withJSONObject: body)

        return http.streamSSE(path: "/api/chat/stream", body: payload)
    }

    public func sendMessage(
        sessionID: String,
        content: String,
        agentID: String? = nil
    ) async throws -> StreamChatEvent {
        let stream = sendMessageStream(sessionID: sessionID, content: content, agentID: agentID)
        var finalEvent: StreamChatEvent?
        for try await event in stream {
            if event.type == "done" || event.type == "error" {
                finalEvent = event
                break
            }
        }
        guard let event = finalEvent else {
            throw HTTPError.network("No completion event received")
        }
        return event
    }

    // MARK: - Agents

    public func fetchAgents() async throws -> [AgentInfo] {
        let response: APIListResponse<AgentInfo> = try await getJSON("/api/agents")
        return response.data
    }

    public func fetchAgentDefs() async throws -> [AgentDef] {
        struct AgentDefsResponse: Decodable, Sendable {
            let definitions: [AgentDef]
        }
        let response: APIListResponse<AgentDef> = try await getJSON("/api/agents/definitions")
        return response.data
    }

    public func fetchAgentDetail(id: String) async throws -> AgentDetail {
        try await getJSON("/api/agents/\(id)")
    }

    // MARK: - Diagnostics

    public func fetchDiagnostics() async throws -> HealthStatus {
        try await getJSON("/api/health")
    }

    // MARK: - Search

    public func searchObjects(query: String, type: String? = nil) async throws -> [AnySearchResult] {
        var path = "/api/search?q=\(query.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? query)"
        if let type { path += "&type=\(type)" }
        struct SearchResponse: Decodable, Sendable {
            let results: [AnySearchResult]
        }
        let response: SearchResponse = try await getJSON(path)
        return response.results
    }

    // MARK: - MCP Servers

    public func fetchMCPServers() async throws -> [MCPServer] {
        let response: APIListResponse<MCPServer> = try await getJSON("/api/mcp/servers")
        return response.data
    }

    public func fetchMCPTools(serverID: String) async throws -> [MCPToolInfo] {
        let response: APIListResponse<MCPToolInfo> = try await getJSON("/api/mcp/servers/\(serverID)/tools")
        return response.data
    }

    // MARK: - Relay Nodes

    public func fetchRelayNodes() async throws -> [RelayNode] {
        let response: APIListResponse<RelayNode> = try await getJSON("/api/relay/nodes")
        return response.data
    }

    // MARK: - Workers

    public func fetchWorkers() async throws -> [Worker] {
        let response: APIListResponse<Worker> = try await getJSON("/api/workers")
        return response.data
    }

    // MARK: - Documents

    public func fetchDocuments(projectID: String) async throws -> [Document] {
        let response: APIListResponse<Document> = try await getJSON("/api/projects/\(projectID)/documents")
        return response.data
    }

    // MARK: - Schema

    public func fetchSchema() async throws -> SchemaResponse {
        try await getJSON("/api/schema")
    }
}

// MARK: - API Response Wrapper

public struct APIListResponse<T: Decodable & Sendable>: Decodable, Sendable {
    public let data: [T]
    public let total: Int?
    public let page: Int?
    public let pageSize: Int?

    enum CodingKeys: String, CodingKey {
        case data, total, page
        case pageSize = "page_size"
    }
}

// MARK: - AnySearchResult (polymorphic search result)

public struct AnySearchResult: Codable, Sendable, Identifiable {
    public let id: String
    public let type: String
    public let title: String?
    public let subtitle: String?
    public let score: Double?
    public let metadata: [String: String]?

    enum CodingKeys: String, CodingKey {
        case id, type, title, subtitle, score, metadata
    }

    public init(id: String, type: String, title: String? = nil, subtitle: String? = nil,
                score: Double? = nil, metadata: [String: String]? = nil) {
        self.id = id; self.type = type; self.title = title; self.subtitle = subtitle
        self.score = score; self.metadata = metadata
    }
}
