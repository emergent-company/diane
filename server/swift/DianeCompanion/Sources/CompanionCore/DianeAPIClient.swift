import Foundation
import Sentry

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

    /// Fetch agent runs associated with a session.
    /// GET /api/sessions/{id}/runs
    func fetchSessionRuns(sessionID: String) async throws -> SessionRunsResponse {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let data = try await get("/api/sessions/\(encoded)/runs")
        return try JSONDecoder().decode(SessionRunsResponse.self, from: data)
    }

    /// Fetch todos for a session.
    /// GET /api/sessions/{id}/todos
    func fetchSessionTodos(sessionID: String) async throws -> [SessionTodoItem] {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let data = try await get("/api/sessions/\(encoded)/todos")
        // The API returns {"items": [...]}
        let container = try JSONDecoder().decode([String: [SessionTodoItem]].self, from: data)
        return container["items"] ?? []
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

    /// Append a message to a session's message history.
    /// POST /api/sessions/{id}/messages
    func appendSessionMessage(sessionID: String, role: String, content: String) async throws -> String? {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let body: [String: String] = ["role": role, "content": content]
        let jsonData = try JSONEncoder().encode(body)
        let data = try await post("/api/sessions/\(encoded)/messages", body: jsonData)
        struct Response: Decodable { let ok: Bool?; let id: String? }
        let resp = try JSONDecoder().decode(Response.self, from: data)
        return resp.id
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

    // MARK: - MCP Server Authentication

    func startMCPServerAuth(serverName: String) async throws -> (status: String, authURL: String?) {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName
        let data = try await post("/api/mcp-servers/\(encoded)/auth", body: nil, timeout: 30)
        struct Response: Decodable {
            let status: String
            let auth_url: String?
            let server: String
            let error: String?
        }
        let resp = try JSONDecoder().decode(Response.self, from: data)
        return (resp.status, resp.auth_url)
    }

    func checkMCPServerAuthStatus(serverName: String) async throws -> (status: String, expiresAt: String?, error: String?) {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName
        let data = try await get("/api/mcp-servers/\(encoded)/auth-status")
        struct Response: Decodable {
            let status: String
            let server: String
            let auth_url: String?
            let error: String?
            let expires_at: String?
        }
        let resp = try JSONDecoder().decode(Response.self, from: data)
        return (resp.status, resp.expires_at, resp.error)
    }

    func updateMCPServerScope(serverName: String, scope: String) async throws {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName
        let body = try JSONEncoder().encode(["scope": scope])
        _ = try await put("/api/mcp-servers/\(encoded)/scope", body: body)
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

    /// Fetch recent log entries for a specific MCP server.
    /// Tries the local MCP log HTTP endpoint first (port 18990, served by mcp serve),
    /// falls back to the diane serve local API endpoint.
    func fetchMCPLogs(serverName: String) async throws -> [MCPServerLogEntry] {
        let encoded = serverName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? serverName

        // Try the dedicated MCP log HTTP server first (runs in diane mcp serve)
        let logURL = "http://127.0.0.1:18990/logs/\(encoded)"
        if let url = URL(string: logURL) {
            do {
                let (data, _) = try await URLSession.shared.data(from: url)
                struct LogResponse: Decodable { let logs: [MCPServerLogEntry]? }
                if let resp = try? JSONDecoder().decode(LogResponse.self, from: data), let logs = resp.logs {
                    return logs
                }
            } catch {
                // Log server not running — try local API fallback
            }
        }

        // Fallback: try via diane serve local API endpoint
        let data = try await get("/api/mcp-servers/\(encoded)/logs")
        struct FallbackResponse: Decodable { let logs: [MCPServerLogEntry]? }
        if let resp = try? JSONDecoder().decode(FallbackResponse.self, from: data), let logs = resp.logs {
            return logs
        }
        return (try? JSONDecoder().decode([MCPServerLogEntry].self, from: data)) ?? []
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
        if let list = try? JSONDecoder().decode([ProjectProviderInfo].self, from: data) {
            return list
        }
        // Both decode attempts failed — this is a protocol mismatch bug
        let body = String(data: data, encoding: .utf8) ?? "(non-UTF8)"
        logError("fetchProjectProviders: failed to decode response: \(body)", category: "APIClient")
        throw DianeAPIError.decodingError("providers")
    }

    // MARK: - Provider Config CRUD

    /// Upsert a project-level provider config.
    /// PUT /api/v1/projects/{projectId}/providers/{provider}
    func saveProviderConfig(projectID: String, provider: String,
                            apiKey: String? = nil,
                            baseURL: String? = nil,
                            generativeModel: String? = nil,
                            embeddingModel: String? = nil) async throws {
        struct Req: Encodable {
            let apiKey: String?
            let baseUrl: String?
            let generativeModel: String?
            let embeddingModel: String?
        }
        let body = try JSONEncoder().encode(Req(
            apiKey: apiKey,
            baseUrl: baseURL,
            generativeModel: generativeModel,
            embeddingModel: embeddingModel
        ))
        let projPath = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
        let provPath = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        _ = try await put("/api/v1/projects/\(projPath)/providers/\(provPath)", body: body)
    }

    /// Delete a project-level provider config.
    /// DELETE /api/v1/projects/{projectId}/providers/{provider}
    func deleteProviderConfig(projectID: String, provider: String) async throws {
        let projPath = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
        let provPath = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        _ = try await delete("/api/v1/projects/\(projPath)/providers/\(provPath)")
    }

    /// Fetches projects from the local API proxy.
    /// GET /api/projects
    func fetchProjects() async throws -> [Project] {
        let data = try await get("/api/projects")
        struct Response: Decodable { let projects: [Project]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.projects {
            return list
        }
        if let list = try? JSONDecoder().decode([Project].self, from: data) {
            return list
        }
        let body = String(data: data, encoding: .utf8) ?? "(raw)"
        logError("fetchProjects: decode failure: \(body)", category: "APIClient")
        throw DianeAPIError.decodingError("projects")
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

    /// Fetch full detail for a single agent by name.
    func fetchAgentDetail(name: String) async throws -> AgentDetail {
        let encoded = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? name
        let data = try await get("/api/agents/\(encoded)")
        return try JSONDecoder().decode(AgentDetail.self, from: data)
    }

    // MARK: - Agent CRUD (Create, Update, Delete, Clone)

    /// Create a new user-defined agent.
    func createAgent(_ req: CreateAgentRequest) async throws -> AgentDef {
        let body = try JSONEncoder().encode(req)
        let data = try await post("/api/agents", body: body, timeout: 15)
        struct Response: Decodable { let name: String?; let id: String? }
        _ = try? JSONDecoder().decode(Response.self, from: data)
        // Return the agent list to get the full def
        let agents = try await fetchAgentDefs()
        guard let created = agents.first(where: { $0.name == req.name }) else {
            throw DianeAPIError.serverError("Agent created but not found in listing")
        }
        return created
    }

    /// Update a user-defined agent definition.
    func updateAgent(name: String, changes: [String: Any]) async throws {
        let encoded = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? name
        let body = try JSONSerialization.data(withJSONObject: changes)
        _ = try await patch("/api/agents/\(encoded)", body: body)
    }

    /// Delete a user-defined agent or disable a built-in agent.
    func deleteAgent(name: String) async throws -> String {
        let encoded = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? name
        let data = try await delete("/api/agents/\(encoded)")
        struct Response: Decodable { let status: String? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let s = resp.status {
            return s
        }
        return "deleted"
    }

    /// Clone an agent as a new user-defined agent.
    func cloneAgent(name: String, newName: String) async throws -> String {
        let encoded = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? name
        let body = try JSONEncoder().encode(CloneAgentRequest(name: newName))
        let data = try await post("/api/agents/\(encoded)/clone", body: body, timeout: 15)
        struct Response: Decodable { let name: String?; let status: String? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let n = resp.name {
            return n
        }
        return newName
    }

    // MARK: - Agent Override Config

    /// Fetch the override config for a built-in agent.
    func fetchAgentOverride(name: String) async throws -> AgentOverrideConfig? {
        let encoded = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? name
        let data = try await get("/api/agents/\(encoded)/override")
        struct Response: Decodable { let overrides: AgentOverrideConfig? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data) {
            return resp.overrides
        }
        // Try direct decode
        return try? JSONDecoder().decode(AgentOverrideConfig.self, from: data)
    }

    /// Save (upsert) an override config for a built-in agent.
    func saveAgentOverride(name: String, override: AgentOverrideConfig) async throws {
        let encoded = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? name
        let body = try JSONEncoder().encode(override)
        _ = try await put("/api/agents/\(encoded)/override", body: body)
    }

    /// Remove the override config for a built-in agent (restores built-in defaults).
    func deleteAgentOverride(name: String) async throws {
        let encoded = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? name
        _ = try await delete("/api/agents/\(encoded)/override")
    }

    /// Trigger re-seed of built-in agents with current graph config.
    func seedAgents() async throws -> Int {
        let data = try await post("/api/agents/seed", body: nil, timeout: 60)
        struct Response: Decodable { let count: Int?; let status: String? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data) {
            return resp.count ?? 0
        }
        return 0
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

    /// Capture an HTTP response in Sentry — adds breadcrumb + captures errors.
    private func captureSentinel(method: String, path: String, status: Int, body: String) {
        let breadcrumb = Breadcrumb()
        breadcrumb.category = "http"
        breadcrumb.type = "http"
        breadcrumb.data = [
            "method": method,
            "url": "\(baseURL)\(path)",
            "status_code": status,
            "response_body": String(body.prefix(500)),
        ]
        SentrySDK.addBreadcrumb(breadcrumb)

        if status >= 400 {
            // Use key names that avoid Sentry's built-in PII scrubbing patterns.
            // Keys like "path" and "response" in NSError userInfo are automatically
            // filtered by the SDK regardless of sendDefaultPii=true.
            let bodyPrefix = String(body.prefix(2000))
            let error = NSError(
                domain: "DianeAPIError",
                code: status,
                userInfo: [
                    NSLocalizedDescriptionKey: "HTTP \(status) \(method) \(path) — \(bodyPrefix.prefix(200))",
                    "method": method,
                    "apiPath": path,
                    "responseBody": bodyPrefix,
                ]
            )
            SentrySDK.capture(error: error)
        }
    }

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
            captureSentinel(method: "GET", path: path, status: http.statusCode, body: body)
            throw DianeAPIError.httpError(http.statusCode, body)
        }
        return data
    }

    func post(_ path: String, body: Data?, timeout: TimeInterval? = nil) async throws -> Data {
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
            captureSentinel(method: "POST", path: path, status: http.statusCode, body: body)
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
            captureSentinel(method: "DELETE", path: path, status: http.statusCode, body: body)
            throw DianeAPIError.httpError(http.statusCode, body)
        }
        return data
    }

    private func put(_ path: String, body: Data?, timeout: TimeInterval? = nil) async throws -> Data {
        guard let url = URL(string: "\(baseURL)\(path)") else {
            throw DianeAPIError.invalidURL(path)
        }
        var request = URLRequest(url: url)
        request.httpMethod = "PUT"
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
            let bodyStr = String(data: data, encoding: .utf8) ?? ""
            captureSentinel(method: "PUT", path: path, status: http.statusCode, body: bodyStr)
            throw DianeAPIError.httpError(http.statusCode, bodyStr)
        }
        return data
    }

    private func patch(_ path: String, body: Data?, timeout: TimeInterval? = nil) async throws -> Data {
        guard let url = URL(string: "\(baseURL)\(path)") else {
            throw DianeAPIError.invalidURL(path)
        }
        var request = URLRequest(url: url)
        request.httpMethod = "PATCH"
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
            let bodyStr = String(data: data, encoding: .utf8) ?? ""
            captureSentinel(method: "PATCH", path: path, status: http.statusCode, body: bodyStr)
            throw DianeAPIError.httpError(http.statusCode, bodyStr)
        }
        return data
    }
}

enum DianeAPIError: Error, LocalizedError {
    case invalidURL(String)
    case network(String)
    case httpError(Int, String)
    case serverError(String)
    case decodingError(String)

    var errorDescription: String? {
        switch self {
        case .invalidURL(let p): return "Invalid URL: \(p)"
        case .network(let msg):  return "Network error: \(msg)"
        case .httpError(let c, let b): return "HTTP \(c): \(b)"
        case .serverError(let msg): return "Server error: \(msg)"
        case .decodingError(let target): return "Decoding failed: \(target)"
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
    let online: Bool?          // whether node has an active relay connection
    let uptime: String?        // ISO 8601 — process start time
    let provider: String?      // e.g. "deepseek/deepseek-v4-flash"
    let relayActive: Bool?     // MCP relay connected
    let botActive: Bool?       // Discord bot connected
    let healthy: Bool?         // overall health

    var id: String { instanceID }

    enum CodingKeys: String, CodingKey {
        case instanceID = "instance_id"
        case hostname, mode, version
        case toolCount = "tool_count"
        case connectedAt = "connected_at"
        case online, uptime, provider
        case relayActive = "relay_active"
        case botActive = "bot_active"
        case healthy
    }

    func hash(into hasher: inout Hasher) { hasher.combine(instanceID) }
    static func == (lhs: RelayNode, rhs: RelayNode) -> Bool { lhs.instanceID == rhs.instanceID }
}

struct MCPToolInfo: Identifiable, Codable, Sendable {
    let name: String
    let description: String?

    var id: String { name }
}
