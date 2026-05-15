import Foundation

/// Lightweight HTTP client for Emergent REST API endpoints not yet
/// exposed via the EmergentKit Swift Package CGO bridge.
///
/// This covers: projects, stats, traces (extraction jobs), workers,
/// graph objects, agents, MCP servers, user profile, and ACP streaming chat.
@MainActor
class EmergentAPIClient: ObservableObject {

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
            logInfo("APIClient: server URL cleared", category: "APIClient")
        } else {
            baseURL = URL(string: serverURL)
            logInfo("APIClient: configured for \(serverURL)", category: "APIClient")
        }
    }

    // MARK: - ACP v1 Streaming Chat (Direct to Memory Platform)

    /// ACP session object returned by /acp/v1/sessions
    private struct ACPSessionResponse: Decodable {
        let id: String
    }

    /// Creates an ACP session for the given agent.
    /// POST /acp/v1/sessions { agent_name: "..." }
    func createACPSession(agentName: String) async throws -> String {
        guard let base = baseURL else {
            throw EmergentAPIError.notConfigured
        }
        guard let url = URL(string: "/acp/v1/sessions", relativeTo: base) else {
            throw EmergentAPIError.invalidURL("/acp/v1/sessions")
        }

        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        setACPHeaders(&req)
        req.httpBody = try JSONEncoder().encode(["agent_name": agentName])

        let (data, response) = try await session.data(for: req)
        guard let http = response as? HTTPURLResponse else {
            throw EmergentAPIError.network("No HTTP response")
        }
        guard (200...299).contains(http.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? ""
            logError("ACP: create session failed (\(http.statusCode)): \(body)", category: "ACP")
            throw EmergentAPIError.httpError(http.statusCode)
        }

        let sessionResp = try JSONDecoder().decode(ACPSessionResponse.self, from: data)
        logInfo("ACP: created session \(sessionResp.id.prefix(12))", category: "ACP")
        return sessionResp.id
    }

    /// Stream a chat message directly to the ACP SSE endpoint.
    /// POST /acp/v1/agents/:name/runs with mode=stream
    ///
    /// ACP SSE format:
    ///   event: run.created
    ///   data: {"run":{...}}
    ///
    ///   event: message.part
    ///   data: {"part":{"content":"...","content_type":"text/plain"}}
    ///
    ///   event: run.completed
    ///   data: {"run":{"run_id":"...","status":"completed"}}
    func streamACP(agentName: String, sessionID: String, content: String) -> AsyncThrowingStream<StreamChatEvent, Error> {
        return AsyncThrowingStream { continuation in
            let task = Task {
                guard let base = self.baseURL else {
                    continuation.finish(throwing: EmergentAPIError.notConfigured)
                    return
                }
                guard let url = URL(string: "/acp/v1/agents/\(agentName)/runs", relativeTo: base) else {
                    continuation.finish(throwing: EmergentAPIError.invalidURL("/acp/v1/agents/\(agentName)/runs"))
                    return
                }

                let body: [String: Any] = [
                    "mode": "stream",
                    "session_id": sessionID,
                    "message": [
                        ["content_type": "text/plain", "content": content]
                    ]
                ]
                guard let jsonData = try? JSONSerialization.data(withJSONObject: body) else {
                    continuation.finish(throwing: EmergentAPIError.network("Failed to serialize request"))
                    return
                }

                var request = URLRequest(url: url)
                request.httpMethod = "POST"
                request.httpBody = jsonData
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                request.timeoutInterval = 300
                self.setACPHeaders(&request)

                do {
                    let (bytes, response) = try await URLSession.shared.bytes(for: request)
                    guard let http = response as? HTTPURLResponse else {
                        continuation.finish(throwing: EmergentAPIError.network("No HTTP response"))
                        return
                    }
                    guard (200...299).contains(http.statusCode) else {
                        let bodyStr = try? await String(data: Data(Array(try await URLSession.shared.data(for: request).0)), encoding: .utf8)
                        continuation.finish(throwing: EmergentAPIError.httpError(http.statusCode))
                        return
                    }

                    var currentEvent: String? = nil
                    var tokenCount = 0
                    var hadDone = false
                    var hadError = false
                    var runID: String? = nil

                    for try await line in bytes.lines {
                        if line.hasPrefix("event: ") {
                            // Save the event type from the SSE event header
                            currentEvent = String(line.dropFirst(7))
                        } else if line.hasPrefix("data: ") {
                            let jsonStr = String(line.dropFirst(6))

                            if jsonStr == "[DONE]" {
                                break
                            }

                            guard let data = jsonStr.data(using: .utf8),
                                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                                continue
                            }

                            let eventType = currentEvent ?? ""
                            currentEvent = nil // reset after consuming

                            switch eventType {
                            case "run.created":
                                if let run = json["run"] as? [String: Any],
                                   let rid = run["run_id"] as? String {
                                    runID = rid
                                }

                            case "run.in-progress":
                                // Stream is active — no UI event needed
                                break

                            case "message.part":
                                guard let part = json["part"] as? [String: Any],
                                      let contentType = part["content_type"] as? String else {
                                    continue
                                }
                                switch contentType {
                                case "text/plain":
                                    if let content = part["content"] as? String, !content.isEmpty {
                                        tokenCount += 1
                                        continuation.yield(StreamChatEvent(
                                            type: "token",
                                            content: content,
                                            name: nil,
                                            role: nil,
                                            sessionID: sessionID,
                                            runID: runID,
                                            message: nil
                                        ))
                                    }
                                case "application/json":
                                    if let meta = part["metadata"] as? [String: Any],
                                       let kind = meta["kind"] as? String,
                                       kind == "trajectory" {
                                        let toolName = meta["tool_name"] as? String ?? "unknown"
                                        let hasOutput = meta["tool_output"] != nil
                                        continuation.yield(StreamChatEvent(
                                            type: hasOutput ? "tool_result" : "tool_call",
                                            content: nil,
                                            name: toolName,
                                            role: nil,
                                            sessionID: sessionID,
                                            runID: runID,
                                            message: nil
                                        ))
                                    }
                                default:
                                    break
                                }

                            case "message.created":
                                if let msg = json["message"] as? [String: Any] {
                                    let role = msg["role"] as? String ?? ""
                                    let parts = msg["parts"] as? [[String: Any]] ?? []
                                    let textContent = parts.compactMap { p -> String? in
                                        guard let ct = p["content_type"] as? String, ct == "text/plain" else { return nil }
                                        return p["content"] as? String
                                    }.joined()
                                    continuation.yield(StreamChatEvent(
                                        type: "message",
                                        content: textContent.isEmpty ? nil : textContent,
                                        name: nil,
                                        role: role.isEmpty ? nil : role,
                                        sessionID: sessionID,
                                        runID: runID,
                                        message: nil
                                    ))
                                }

                            case "run.completed":
                                hadDone = true
                                continuation.yield(StreamChatEvent(
                                    type: "done",
                                    content: nil,
                                    name: nil,
                                    role: nil,
                                    sessionID: sessionID,
                                    runID: runID,
                                    message: nil
                                ))

                            case "run.failed", "run.cancelled":
                                hadError = true
                                var errMsg = eventType
                                if let run = json["run"] as? [String: Any],
                                   let err = run["error"] as? [String: Any],
                                   let m = err["message"] as? String {
                                    errMsg = m
                                }
                                continuation.yield(StreamChatEvent(
                                    type: "error",
                                    content: nil,
                                    name: nil,
                                    role: nil,
                                    sessionID: sessionID,
                                    runID: runID,
                                    message: errMsg
                                ))

                            case "error":
                                hadError = true
                                var errMsg = "stream error"
                                if let e = json["error"] as? [String: Any],
                                   let m = e["message"] as? String {
                                    errMsg = m
                                }
                                continuation.yield(StreamChatEvent(
                                    type: "error",
                                    content: nil,
                                    name: nil,
                                    role: nil,
                                    sessionID: sessionID,
                                    runID: runID,
                                    message: errMsg
                                ))

                            default:
                                // Unknown event type — skip
                                break
                            }

                            if hadDone || hadError {
                                break
                            }
                        }
                        // Empty lines separate SSE events — event type carries over
                    }

                    // Post-stream diagnostics
                    if !hadDone && !hadError {
                        logWarning("ACP stream ended without done/error (tokens=\(tokenCount))", category: "ACP")
                    }
                    logInfo("ACP stream complete: tokens=\(tokenCount) done=\(hadDone) error=\(hadError)", category: "ACP")

                    continuation.finish()

                } catch {
                    if Task.isCancelled {
                        continuation.finish()
                        return
                    }
                    logError("ACP stream error: \(error.localizedDescription)", category: "ACP")
                    continuation.finish(throwing: EmergentAPIError.network(error.localizedDescription))
                }
            }

            continuation.onTermination = { @Sendable _ in
                task.cancel()
            }
        }
    }

    // MARK: - ACP auth helper

    private func setACPHeaders(_ req: inout URLRequest) {
        if !apiKey.isEmpty {
            if apiKey.hasPrefix("emt_") {
                req.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
            } else {
                req.setValue(apiKey, forHTTPHeaderField: "X-API-Key")
            }
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

    /// Recreate chunks for a document (deletes existing chunks, regenerates from source content).
    /// POST /api/documents/{id}/recreate-chunks
    func recreateChunks(projectID: String, documentID: String) async throws {
        _ = try await post("/api/documents/\(documentID)/recreate-chunks",
                           body: Data("{}".utf8),
                           projectID: projectID)
    }

    // MARK: - Query

    func executeQuery(projectID: String, query: String) async throws -> QueryResult {
        let body = try JSONEncoder().encode(["query": query])
        let data = try await post("/api/graph/search", body: body, projectID: projectID)
        return try decode(QueryResult.self, from: data)
    }

    /// Fetch the extraction summary for a document.
    func fetchExtractionSummary(projectID: String, documentID: String) async throws -> ExtractionSummary {
        let data = try await get("/api/documents/\(documentID)/extraction-summary", projectID: projectID)
        // Check for 404 "no extraction" response
        if let err = try? JSONDecoder().decode(ExtractionSummaryError.self, from: data),
           err.error?.code == "not_found" {
            throw EmergentAPIError.notFound(err.error?.message ?? "No extraction completed")
        }
        return try decode(ExtractionSummary.self, from: data)
    }

    /// Fetch graph objects from a specific branch (e.g. "extraction/{docId}/{jobId}").
    /// This is how extraction-created objects are isolated from the main graph.
    func fetchBranchObjects(projectID: String, branch: String, limit: Int = 100) async throws -> [GraphObject] {
        let encoded = branch.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? branch
        let data = try await get("/api/graph/objects/search?branch=\(encoded)&limit=\(limit)", projectID: projectID)
        struct Response: Decodable { let objects: [GraphObject]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.objects {
            return list
        }
        return (try? JSONDecoder().decode([GraphObject].self, from: data)) ?? []
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

    // MARK: - Project Provider Configs (project-level provider CRUD)

    /// Fetch all project-level provider configs.
    /// GET /api/v1/projects/{projectId}/providers
    func fetchProjectProviderConfigs(projectID: String) async throws -> [OrgProviderConfig] {
        let encoded = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
        let data = try await get("/api/v1/projects/\(encoded)/providers", projectID: projectID)
        return (try? decode([OrgProviderConfig].self, from: data)) ?? []
    }

    /// Upsert a project-level provider config.
    /// PUT /api/v1/projects/{projectId}/providers/{provider}
    /// Body fields: apiKey, baseUrl, generativeModel, serviceAccountJson, gcpProject, location, embeddingModel
    func saveProjectProviderConfig(projectID: String, provider: String,
                                   apiKey: String? = nil,
                                   baseURL: String? = nil,
                                   generativeModel: String? = nil,
                                   serviceAccountJSON: String? = nil,
                                   gcpProject: String? = nil,
                                   location: String? = nil,
                                   embeddingModel: String? = nil) async throws {
        let encodedProj = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
        let encodedProv = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        struct Req: Encodable {
            let apiKey: String?
            let baseUrl: String?
            let generativeModel: String?
            let serviceAccountJson: String?
            let gcpProject: String?
            let location: String?
            let embeddingModel: String?
        }
        let body = try JSONEncoder().encode(Req(
            apiKey: apiKey,
            baseUrl: baseURL,
            generativeModel: generativeModel,
            serviceAccountJson: serviceAccountJSON,
            gcpProject: gcpProject,
            location: location,
            embeddingModel: embeddingModel
        ))
        _ = try await put("/api/v1/projects/\(encodedProj)/providers/\(encodedProv)", body: body)
    }

    /// Delete a project-level provider config.
    /// DELETE /api/v1/projects/{projectId}/providers/{provider}
    func deleteProjectProviderConfig(projectID: String, provider: String) async throws {
        let encodedProj = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
        let encodedProv = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        _ = try await delete("/api/v1/projects/\(encodedProj)/providers/\(encodedProv)")
    }

    /// Test a project-level provider via a live generate call.
    /// POST /api/v1/providers/{provider}/test?projectId=...
    func testProjectProvider(projectID: String, provider: String) async throws -> TestProviderResponse {
        let encodedProv = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        let encodedPID = projectID.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? projectID
        let path = "/api/v1/providers/\(encodedProv)/test?projectId=\(encodedPID)"
        let data = try await post(path, body: Data(), projectID: projectID)
        return try decode(TestProviderResponse.self, from: data)
    }

    /// Fetch cached models for a provider from the catalog.
    /// GET /api/v1/providers/{provider}/models?type={modelType}
    func fetchProviderModels(provider: String, modelType: String = "generative") async throws -> [ProviderModel] {
        let encodedProv = provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider
        let path = "/api/v1/providers/\(encodedProv)/models?type=\(modelType)"
        let data = try await get(path)
        return (try? decode([ProviderModel].self, from: data)) ?? []
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

    // MARK: - Agent Questions

    /// Fetch agent questions for a project, optionally filtered by status.
    /// GET /api/projects/{projectId}/agent-questions?status={status}
    func fetchAgentQuestions(projectID: String, status: AgentQuestionStatus? = nil) async throws -> [AgentQuestion] {
        var path = "/api/projects/\(projectID)/agent-questions"
        if let s = status {
            path += "?status=\(s.rawValue)"
        }
        let data = try await get(path, projectID: projectID)
        return (try? decode(AgentQuestionListResponse.self, from: data).data) ?? []
    }

    /// Respond to a pending agent question and resume the agent run.
    /// POST /api/projects/{projectId}/agent-questions/{questionId}/respond
    func respondToAgentQuestion(projectID: String, questionID: String, response: String) async throws {
        let path = "/api/projects/\(projectID)/agent-questions/\(questionID)/respond"
        let body = try JSONEncoder().encode(RespondToQuestionRequest(response: response))
        _ = try await post(path, body: body, projectID: projectID)
    }

    // MARK: - Upload Document

    /// Upload a file to the Emergent platform. The upload endpoint returns `name` instead of
    /// `filename`, so we decode via a separate response type and map it to the standard Document.
    func uploadDocument(fileURL: URL, projectID: String, autoExtract: Bool = true) async throws -> Document {
        guard fileURL.startAccessingSecurityScopedResource() else {
            throw EmergentAPIError.network("Cannot access file at \(fileURL.lastPathComponent)")
        }
        defer { fileURL.stopAccessingSecurityScopedResource() }

        struct UploadResponse: Decodable {
            let document: UploadDocument
            let isDuplicate: Bool?
        }
        struct UploadDocument: Decodable {
            let id: String
            let name: String         // upload API returns "name", not "filename"
            let mimeType: String?
            let fileSizeBytes: Int?
            let conversionStatus: String?
            let storageKey: String?
            let createdAt: String?
        }

        var req = try makeRequest(method: "POST", path: "/api/documents/upload")
        req.setValue(projectID, forHTTPHeaderField: "X-Project-ID")

        let boundary = "Boundary-\(UUID().uuidString)"
        req.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")

        var body = Data()
        // autoExtract field
        body.append("--\(boundary)\r\n".data(using: .utf8)!)
        body.append("Content-Disposition: form-data; name=\"autoExtract\"\r\n\r\n".data(using: .utf8)!)
        body.append("\(autoExtract)\r\n".data(using: .utf8)!)
        // file field
        let fileData = try Data(contentsOf: fileURL)
        let filename = fileURL.lastPathComponent
        let mime = mimeTypeForFile(filename)
        body.append("--\(boundary)\r\n".data(using: .utf8)!)
        body.append("Content-Disposition: form-data; name=\"file\"; filename=\"\(filename)\"\r\n".data(using: .utf8)!)
        body.append("Content-Type: \(mime)\r\n\r\n".data(using: .utf8)!)
        body.append(fileData)
        body.append("\r\n".data(using: .utf8)!)
        body.append("--\(boundary)--\r\n".data(using: .utf8)!)

        req.httpBody = body
        req.setValue("\(body.count)", forHTTPHeaderField: "Content-Length")

        let data = try await perform(req)
        let uploadResp = try decode(UploadResponse.self, from: data)

        return Document(
            id: uploadResp.document.id,
            projectId: projectID,
            filename: uploadResp.document.name,
            mimeType: uploadResp.document.mimeType,
            fileHash: nil,
            contentHash: nil,
            sourceType: "upload",
            conversionStatus: uploadResp.document.conversionStatus,
            extractionStatus: nil,
            processingStatus: nil,
            storageKey: uploadResp.document.storageKey,
            storageUrl: nil,
            fileSizeBytes: uploadResp.document.fileSizeBytes,
            syncVersion: nil,
            chunks: nil,
            embeddedChunks: nil,
            totalChars: nil,
            objectsCreated: nil,
            relationshipsCreated: nil,
            content: nil,
            createdAt: uploadResp.document.createdAt,
            updatedAt: nil
        )
    }

    /// Guess a MIME type from file extension for upload.
    private func mimeTypeForFile(_ filename: String) -> String {
        let ext = (filename as NSString).pathExtension.lowercased()
        switch ext {
        case "pdf":           return "application/pdf"
        case "docx", "doc":   return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
        case "xlsx", "xls":   return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
        case "pptx", "ppt":   return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
        case "txt":           return "text/plain"
        case "md":            return "text/markdown"
        case "csv":           return "text/csv"
        case "json":          return "application/json"
        case "xml":           return "application/xml"
        case "html", "htm":   return "text/html"
        case "png":           return "image/png"
        case "jpg", "jpeg":   return "image/jpeg"
        case "gif":           return "image/gif"
        case "webp":          return "image/webp"
        case "rtf":           return "application/rtf"
        default:              return "application/octet-stream"
        }
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
            logError("APIClient: request attempted but server URL not configured (path: \(path))", category: "APIClient")
            throw EmergentAPIError.notConfigured
        }
        guard let url = URL(string: path, relativeTo: base) else {
            logError("APIClient: invalid URL for path \(path)", category: "APIClient")
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
            logWarning("APIClient: no API key configured for request to \(url.absoluteString)", category: "APIClient")
        }
        logDebug("APIClient: \(method) \(url.absoluteString)", category: "APIClient")
        return req
    }

    private func perform(_ request: URLRequest) async throws -> Data {
        let urlStr = request.url?.absoluteString ?? "(nil)"
        do {
            let start = Date()
            let (data, response) = try await session.data(for: request)
            let elapsed = Int(Date().timeIntervalSince(start) * 1000)
            if let http = response as? HTTPURLResponse {
                logInfo("APIClient: \(request.httpMethod ?? "?") \(urlStr) → \(http.statusCode) (\(elapsed)ms)", category: "APIClient")
                switch http.statusCode {
                case 200...299: return data
                case 401, 403:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logError("APIClient: unauthorized for \(urlStr) — \(body)", category: "APIClient")
                    throw EmergentAPIError.unauthorized
                case 404:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logError("APIClient: not found: \(urlStr) — \(body)", category: "APIClient")
                    throw EmergentAPIError.notFound(request.url?.path ?? "")
                case 500...599:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logError("APIClient: server error \(http.statusCode) for \(urlStr) — \(body)", category: "APIClient")
                    throw EmergentAPIError.serverError(http.statusCode)
                default:
                    let body = String(data: data, encoding: .utf8) ?? ""
                    logError("APIClient: HTTP \(http.statusCode) for \(urlStr) — \(body)", category: "APIClient")
                    throw EmergentAPIError.httpError(http.statusCode)
                }
            }
            return data
        } catch let e as EmergentAPIError {
            throw e
        } catch {
            logError("APIClient: network error for \(urlStr) — \(error.localizedDescription)", category: "APIClient")
            throw EmergentAPIError.network(error.localizedDescription)
        }
    }

    private func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        do {
            return try JSONDecoder().decode(type, from: data)
        } catch {
            let raw = String(data: data, encoding: .utf8) ?? "<non-UTF8 body>"
            logError("APIClient: decoding \(String(describing: type)) failed — \(error.localizedDescription) — raw: \(raw)", category: "APIClient")
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
