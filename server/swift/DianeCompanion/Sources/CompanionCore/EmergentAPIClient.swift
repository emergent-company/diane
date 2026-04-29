import Foundation
import OSLog

/// Lightweight HTTP client for Emergent REST API endpoints not yet
/// exposed via the EmergentKit Swift Package CGO bridge.
///
/// This covers: projects, stats, traces (extraction jobs), workers,
/// graph objects, agents, MCP servers, and user profile.
@MainActor
final class EmergentAPIClient: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "APIClient")

    private let session: URLSession
    private var baseURL: URL?
    private var apiKey: String = ""

    init() {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 15
        config.timeoutIntervalForResource = 30
        session = URLSession(configuration: config)
    }

    // MARK: - Configuration

    func configure(serverURL: String, apiKey: String) {
        self.apiKey = apiKey
        if serverURL.isEmpty {
            baseURL = nil
            logger.info("APIClient: server URL cleared")
        } else {
            baseURL = URL(string: serverURL)
            logger.info("APIClient: configured for \(serverURL, privacy: .public)")
        }
    }

    // MARK: - Projects

    func fetchProjects() async throws -> [Project] {
        struct Response: Decodable {
            let projects: [Project]?
        }
        let data = try await get("/api/projects")
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.projects {
            return list
        }
        return (try? JSONDecoder().decode([Project].self, from: data)) ?? []
    }

    // MARK: - Project Stats

    /// Assembles project stats from two endpoints:
    ///   - GET /api/type-registry/projects/{projectId}/stats  → object & type counts
    ///   - GET /api/documents?limit=1  (X-Project-ID)         → document total
    func fetchProjectStats(projectID: String) async throws -> ProjectStats {
        struct TypeRegistryStats: Decodable {
            let total_types: Int
            let enabled_types: Int
            let types_with_objects: Int
            let total_objects: Int
        }
        struct DocumentsResp: Decodable {
            let total: Int?
        }

        async let regData  = get("/api/type-registry/projects/\(projectID)/stats")
        async let docsData = get("/api/documents?limit=1", projectID: projectID)

        let (rd, dd) = try await (regData, docsData)
        let reg  = try decode(TypeRegistryStats.self, from: rd)
        let docs = (try? decode(DocumentsResp.self, from: dd))?.total ?? 0

        return ProjectStats(
            totalObjects:     reg.total_objects,
            totalTypes:       reg.total_types,
            enabledTypes:     reg.enabled_types,
            typesWithObjects: reg.types_with_objects,
            totalDocuments:   docs
        )
    }

    // MARK: - Traces (Extraction Jobs)

    func fetchTraces(projectID: String, limit: Int = 50) async throws -> [Trace] {
        struct Response: Decodable { let jobs: [Trace]? }
        let data = try await get("/api/monitoring/extraction-jobs?limit=\(limit)", projectID: projectID)
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.jobs {
            return list
        }
        return (try? JSONDecoder().decode([Trace].self, from: data)) ?? []
    }

    // MARK: - Graph Objects

    func searchObjects(projectID: String, query: String, limit: Int = 20) async throws -> [GraphObject] {
        let encoded = query.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? query
        let path = encoded.isEmpty
            ? "/api/graph/objects/search?limit=\(limit)"
            : "/api/graph/objects/search?q=\(encoded)&limit=\(limit)"
        let data = try await get(path, projectID: projectID)
        struct Response: Decodable { let objects: [GraphObject]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.objects {
            return list
        }
        return (try? JSONDecoder().decode([GraphObject].self, from: data)) ?? []
    }

    func fetchObject(id: String) async throws -> GraphObject {
        let data = try await get("/api/graph/objects/\(id)")
        return try decode(GraphObject.self, from: data)
    }

    // MARK: - Documents

    func searchDocuments(projectID: String, query: String, limit: Int = 20) async throws -> [Document] {
        let encoded = query.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? query
        let path = encoded.isEmpty
            ? "/api/documents?limit=\(limit)"
            : "/api/documents?q=\(encoded)&limit=\(limit)"
        let data = try await get(path, projectID: projectID)
        struct Response: Decodable { let documents: [Document]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.documents {
            return list
        }
        return (try? JSONDecoder().decode([Document].self, from: data)) ?? []
    }

    /// Fetch a single document by ID, including its full `content` field.
    func fetchDocument(projectID: String, documentID: String) async throws -> Document {
        let data = try await get("/api/documents/\(documentID)", projectID: projectID)
        return try decode(Document.self, from: data)
    }

    /// Fetch all chunks for a document.
    /// Note: the query param is camelCase `documentId` as required by the server.
    func fetchDocumentChunks(projectID: String, documentID: String) async throws -> [DocumentChunk] {
        let data = try await get("/api/chunks?documentId=\(documentID)", projectID: projectID)
        let resp = try decode(ChunksResponse.self, from: data)
        return resp.data
    }

    // MARK: - Query

    func executeQuery(projectID: String, query: String) async throws -> QueryResult {
        let body = try JSONEncoder().encode(["query": query])
        let data = try await post("/api/graph/search", body: body, projectID: projectID)
        return try decode(QueryResult.self, from: data)
    }

    // MARK: - Workers (uses /api/diagnostics — no dedicated workers endpoint)

    func fetchWorkers() async throws -> [Worker] {
        // The server has no /api/admin/workers endpoint.
        // Return an empty list; WorkersView shows diagnostics info instead.
        return []
    }

    func fetchDiagnostics() async throws -> ServerDiagnostics {
        let data = try await get("/api/diagnostics")
        return try decode(ServerDiagnostics.self, from: data)
    }

    // MARK: - Agents

    func fetchAgents(projectID: String) async throws -> [Agent] {
        struct Response: Decodable { let agents: [Agent]? }
        let data = try await get("/api/admin/agents", projectID: projectID)
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.agents {
            return list
        }
        return (try? JSONDecoder().decode([Agent].self, from: data)) ?? []
    }

    // MARK: - Agent Definitions (MP Agent Definitions API)

    func fetchAgentDefs(projectID: String) async throws -> [AgentDef] {
        let data = try await get("/api/agent-definitions", projectID: projectID)
        struct Response: Decodable { let data: [AgentDef]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.data {
            return list
        }
        return (try? JSONDecoder().decode([AgentDef].self, from: data)) ?? []
    }

    func updateAgent(_ agent: Agent) async throws -> Agent {
        let body = try JSONEncoder().encode(agent)
        let data = try await put("/api/admin/agents/\(agent.id)", body: body)
        return try decode(Agent.self, from: data)
    }

    // MARK: - Provider Credentials (org-level)

    func fetchOrgCredentials(orgID: String) async throws -> [OrgCredential] {
        let encoded = orgID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? orgID
        let data = try await get("/api/v1/organizations/\(encoded)/providers/credentials", orgID: orgID)
        return (try? decode([OrgCredential].self, from: data)) ?? []
    }

    func saveGoogleAICredential(orgID: String, apiKey: String) async throws {
        let encoded = orgID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? orgID
        let body = try JSONEncoder().encode(["apiKey": apiKey])
        _ = try await post("/api/v1/organizations/\(encoded)/providers/google-ai/credentials", body: body, orgID: orgID)
    }

    func saveVertexAICredential(orgID: String, serviceAccountJSON: String, gcpProject: String, location: String) async throws {
        let encoded = orgID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? orgID
        struct Req: Encodable {
            let serviceAccountJson: String
            let gcpProject: String
            let location: String
        }
        let body = try JSONEncoder().encode(Req(serviceAccountJson: serviceAccountJSON, gcpProject: gcpProject, location: location))
        _ = try await post("/api/v1/organizations/\(encoded)/providers/vertex-ai/credentials", body: body, orgID: orgID)
    }

    func deleteOrgCredential(orgID: String, provider: String) async throws {
        let encodedOrg = orgID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? orgID
        let encodedProv = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        _ = try await delete("/api/v1/organizations/\(encodedOrg)/providers/\(encodedProv)/credentials", orgID: orgID)
    }

    // MARK: - Provider Project Policies

    func fetchProjectPolicies(projectID: String, orgID: String) async throws -> [ProjectPolicy] {
        let encoded = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
        let data = try await get("/api/v1/projects/\(encoded)/providers/policies", orgID: orgID)
        return (try? decode([ProjectPolicy].self, from: data)) ?? []
    }

    func setProjectPolicy(projectID: String, orgID: String, provider: String, policy: String,
                          embeddingModel: String? = nil, generativeModel: String? = nil) async throws {
        let encodedProj = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
        let encodedProv = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        struct Req: Encodable {
            let policy: String
            let embeddingModel: String?
            let generativeModel: String?
            enum CodingKeys: String, CodingKey {
                case policy
                case embeddingModel  = "embeddingModel"
                case generativeModel = "generativeModel"
            }
        }
        let body = try JSONEncoder().encode(Req(policy: policy, embeddingModel: embeddingModel, generativeModel: generativeModel))
        _ = try await put("/api/v1/projects/\(encodedProj)/providers/\(encodedProv)/policy", body: body, orgID: orgID)
    }

    // MARK: - Embedding Status & Policies

    func fetchEmbeddingStatus() async throws -> EmbeddingStatus {
        let data = try await get("/api/embeddings/status")
        return try decode(EmbeddingStatus.self, from: data)
    }

    func fetchEmbeddingPolicies(projectID: String) async throws -> [EmbeddingPolicy] {
        // This endpoint uses ?project_id= query param, not X-Project-ID header
        let encoded = projectID.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? projectID
        let data = try await get("/api/graph/embedding-policies?project_id=\(encoded)")
        return (try? JSONDecoder().decode([EmbeddingPolicy].self, from: data)) ?? []
    }

    // MARK: - MCP Servers

    func fetchMCPServers(projectID: String) async throws -> [MCPServer] {
        struct Response: Decodable { let servers: [MCPServer]? }
        let data = try await get("/api/admin/mcp-servers", projectID: projectID)
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.servers {
            return list
        }
        return (try? JSONDecoder().decode([MCPServer].self, from: data)) ?? []
    }

    func fetchRelaySessions(projectID: String) async throws -> [RelaySession] {
        let data = try await get("/api/mcp-relay/sessions", projectID: projectID)
        struct Response: Decodable { let sessions: [RelaySession]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.sessions {
            return list
        }
        return (try? JSONDecoder().decode([RelaySession].self, from: data)) ?? []
    }

    // MARK: - User Profile

    func fetchUserProfile() async throws -> UserProfile {
        let data = try await get("/api/user/profile")
        return try decode(UserProfile.self, from: data)
    }

    // MARK: - Account Stats (derived from health + projects)

    func fetchAccountStats() async throws -> AccountStats {
        let data = try await get("/health")
        struct HealthResp: Decodable {
            let status: String
            let version: String?
            let uptime: String?
        }
        let health = try decode(HealthResp.self, from: data)
        let projects = try await fetchProjects()
        // Object/relation counts are no longer embedded in /api/projects;
        // use 0 here — AccountStatusView has its own dedicated fetch path.
        return AccountStats(
            serverURL: baseURL?.absoluteString ?? "",
            serverVersion: health.version,
            latencyMs: nil,
            totalProjects: projects.count,
            totalObjects: 0,
            totalRelations: 0,
            totalApiRequests: 0,
            avgLatencyMs: nil
        )
    }

    // MARK: - Diane Sessions
    
    func fetchSessions(projectID: String, limit: Int = 50) async throws -> [DianeSession] {
        // Uses the Memory Platform's dedicated session API (same endpoints Diane's Go SDK uses internally)
        let path = "/api/graph/sessions?limit=\(limit)"
        let data = try await get(path, projectID: projectID)
        struct Response: Decodable { let items: [DianeSession]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        return (try? JSONDecoder().decode([DianeSession].self, from: data)) ?? []
    }
    
    func fetchSessionMessages(projectID: String, sessionID: String, limit: Int = 200) async throws -> [DianeMessage] {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let data = try await get("/api/graph/sessions/\(encoded)/messages?limit=\(limit)", projectID: projectID)
        struct Response: Decodable { let items: [DianeMessage]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        return (try? JSONDecoder().decode([DianeMessage].self, from: data)) ?? []
    }
    
    // MARK: - HTTP helpers

    private func get(_ path: String, projectID: String? = nil, orgID: String? = nil) async throws -> Data {
        var req = try makeRequest(method: "GET", path: path)
        if let pid = projectID { req.setValue(pid, forHTTPHeaderField: "X-Project-ID") }
        if let oid = orgID { req.setValue(oid, forHTTPHeaderField: "X-Org-ID") }
        return try await perform(req)
    }

    func post(_ path: String, body: Data, projectID: String? = nil, orgID: String? = nil) async throws -> Data {
        var req = try makeRequest(method: "POST", path: path)
        req.httpBody = body
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let pid = projectID { req.setValue(pid, forHTTPHeaderField: "X-Project-ID") }
        if let oid = orgID { req.setValue(oid, forHTTPHeaderField: "X-Org-ID") }
        return try await perform(req)
    }

    func put(_ path: String, body: Data, orgID: String? = nil) async throws -> Data {
        var req = try makeRequest(method: "PUT", path: path)
        req.httpBody = body
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let oid = orgID { req.setValue(oid, forHTTPHeaderField: "X-Org-ID") }
        return try await perform(req)
    }

    private func delete(_ path: String, orgID: String? = nil) async throws -> Data {
        var req = try makeRequest(method: "DELETE", path: path)
        if let oid = orgID { req.setValue(oid, forHTTPHeaderField: "X-Org-ID") }
        return try await perform(req)
    }

    private func makeRequest(method: String, path: String) throws -> URLRequest {
        guard let base = baseURL else {
            logger.error("APIClient: request attempted but server URL not configured (path: \(path, privacy: .public))")
            throw EmergentAPIError.notConfigured
        }
        guard let url = URL(string: path, relativeTo: base) else {
            logger.error("APIClient: invalid URL for path \(path, privacy: .public)")
            throw EmergentAPIError.invalidURL(path)
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        if !apiKey.isEmpty {
            // Match CLI auth logic: emt_* tokens use Bearer auth; standalone keys use X-API-Key.
            if apiKey.hasPrefix("emt_") {
                req.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
            } else {
                req.setValue(apiKey, forHTTPHeaderField: "X-API-Key")
            }
        } else {
            logger.warning("APIClient: no API key configured for request to \(url.absoluteString, privacy: .public)")
        }
        logger.debug("APIClient: \(method, privacy: .public) \(url.absoluteString, privacy: .public)")
        return req
    }

    private func perform(_ request: URLRequest) async throws -> Data {
        let urlStr = request.url?.absoluteString ?? "(nil)"
        do {
            let start = Date()
            let (data, response) = try await session.data(for: request)
            let elapsed = Int(Date().timeIntervalSince(start) * 1000)
            if let http = response as? HTTPURLResponse {
                logger.info("APIClient: \(request.httpMethod ?? "?", privacy: .public) \(urlStr, privacy: .public) → \(http.statusCode) (\(elapsed)ms)")
                switch http.statusCode {
                case 200...299: return data
                case 401, 403:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logger.error("APIClient: unauthorized for \(urlStr, privacy: .public) — \(body, privacy: .public)")
                    throw EmergentAPIError.unauthorized
                case 404:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logger.error("APIClient: not found: \(urlStr, privacy: .public) — \(body, privacy: .public)")
                    throw EmergentAPIError.notFound(request.url?.path ?? "")
                case 500...599:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logger.error("APIClient: server error \(http.statusCode) for \(urlStr, privacy: .public) — \(body, privacy: .public)")
                    throw EmergentAPIError.serverError(http.statusCode)
                default:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logger.error("APIClient: HTTP \(http.statusCode) for \(urlStr, privacy: .public) — \(body, privacy: .public)")
                    throw EmergentAPIError.httpError(http.statusCode)
                }
            }
            return data
        } catch let e as EmergentAPIError {
            throw e
        } catch {
            logger.error("APIClient: network error for \(urlStr, privacy: .public) — \(error.localizedDescription, privacy: .public)")
            throw EmergentAPIError.network(error.localizedDescription)
        }
    }

    private func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        do {
            return try JSONDecoder().decode(type, from: data)
        } catch {
            let raw = String(data: data, encoding: .utf8) ?? "<non-UTF8 body>"
            logger.error("APIClient: decoding \(String(describing: type), privacy: .public) failed — \(error.localizedDescription, privacy: .public) — raw: \(raw, privacy: .public)")
            throw EmergentAPIError.decodingFailed(error.localizedDescription)
        }
    }
}

// MARK: - Document (bridging from EmergentKit)

// Document is defined in EmergentKit — we use it directly.
// Re-export a typealias so Core code doesn't need to import EmergentKit everywhere.

// MARK: - Errors

enum EmergentAPIError: Error, LocalizedError {
    case notConfigured
    case invalidURL(String)
    case unauthorized
    case notFound(String)
    case serverError(Int)
    case httpError(Int)
    case network(String)
    case decodingFailed(String)

    var errorDescription: String? {
        switch self {
        case .notConfigured:          return "Server URL not configured"
        case .invalidURL(let p):      return "Invalid URL: \(p)"
        case .unauthorized:           return "Unauthorized — check your API key"
        case .notFound(let p):        return "Not found: \(p)"
        case .serverError(let code):  return "Server error (\(code))"
        case .httpError(let code):    return "HTTP \(code)"
        case .network(let msg):       return "Network error: \(msg)"
        case .decodingFailed(let msg): return "Decoding failed: \(msg)"
        }
    }
}
