import Foundation

// MARK: - Emergent API Client

public final class EmergentAPIClient: @unchecked Sendable {
    public let http: HTTPClient
    public var projectID: String = ""

    /// Raw API key, stored directly (not derived from headers).
    private var _apiKey: String = ""

    public var baseURL: String {
        get { http.baseURL }
        set { http.baseURL = newValue }
    }

    public var apiKey: String {
        get { _apiKey }
        set {
            _apiKey = newValue.trimmingCharacters(in: .whitespaces)
            if _apiKey.hasPrefix("emt_") {
                http.defaultHeaders["Authorization"] = "Bearer \(_apiKey)"
                http.defaultHeaders.removeValue(forKey: "X-API-Key")
            } else {
                http.defaultHeaders["X-API-Key"] = _apiKey
                http.defaultHeaders.removeValue(forKey: "Authorization")
            }
        }
    }

    // MARK: - Init

    public init(baseURL: String = "", apiKey: String = "", projectID: String = "") {
        self.http = HTTPClient(baseURL: baseURL, timeoutForRequest: 30, timeoutForResource: 60)
        self.projectID = projectID
        if !apiKey.isEmpty {
            self.apiKey = apiKey
        }
        http.defaultHeaders["Content-Type"] = "application/json"
    }

    // MARK: - Configuration

    public func configure(baseURL: String, apiKey: String, projectID: String = "") {
        self.baseURL = baseURL
        self.apiKey = apiKey
        self.projectID = projectID
    }

    public var isConfigured: Bool {
        !baseURL.isEmpty && !apiKey.isEmpty
    }

    // MARK: - Private JSON helpers

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

    /// GET with X-Project-ID header (used for MP API endpoints that require project scoping).
    private func getWithProject(_ path: String) async throws -> Data {
        let headers = projectID.isEmpty ? nil : ["X-Project-ID": projectID]
        return try await http.get(path, headers: headers)
    }

    /// POST with X-Project-ID header.
    private func postWithProject(_ path: String, body: Data? = nil) async throws -> Data {
        let headers = projectID.isEmpty ? nil : ["X-Project-ID": projectID]
        return try await http.post(path, body: body, headers: headers)
    }

    // MARK: - ACP v1 Streaming Chat (Direct to Memory Platform)

    /// Creates an ACP session for the given agent.
    /// POST /acp/v1/sessions { agent_name: "..." }
    public func createACPSession(agentName: String = "diane-default") async throws -> String {
        let body: [String: String] = ["agent_name": agentName]
        let data = try await http.post("/acp/v1/sessions", body: JSONEncoder().encode(body))
        struct ACPSessionResponse: Decodable, Sendable { let id: String }
        let resp = try JSONDecoder().decode(ACPSessionResponse.self, from: data)
        return resp.id
    }

    /// Stream a chat message directly to the ACP SSE endpoint.
    /// POST /acp/v1/agents/{name}/runs with mode=stream
    public func streamACP(
        agentName: String,
        sessionID: String,
        content: String
    ) -> AsyncThrowingStream<StreamChatEvent, Error> {
        return AsyncThrowingStream<StreamChatEvent, Error>(StreamChatEvent.self, bufferingPolicy: .unbounded) { continuation in
            let task = Task {
                do {
                    guard let url = URL(string: "/acp/v1/agents/\(agentName)/runs", relativeTo: URL(string: self.http.baseURL))
                            ?? URL(string: self.http.baseURL + "/acp/v1/agents/\(agentName)/runs") else {
                        continuation.finish(throwing: HTTPError.invalidURL("/acp/v1/agents/\(agentName)/runs"))
                        return
                    }

                    let body: [String: Any] = [
                        "mode": "stream",
                        "session_id": sessionID,
                        "message": [
                            ["content_type": "text/plain", "content": content]
                        ]
                    ]
                    let jsonData = try JSONSerialization.data(withJSONObject: body)

                    var request = URLRequest(url: url)
                    request.httpMethod = "POST"
                    request.httpBody = jsonData
                    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    request.setValue("no-cache", forHTTPHeaderField: "Cache-Control")
                    request.cachePolicy = .reloadIgnoringLocalCacheData
                    request.timeoutInterval = 300
                    // ACP auth: use Bearer if key starts with emt_
                    if !self.apiKey.isEmpty {
                        request.setValue(self.apiKey.hasPrefix("emt_")
                            ? "Bearer \(self.apiKey)"
                            : self.apiKey,
                            forHTTPHeaderField: "Authorization")
                    }

                    let (bytes, response) = try await URLSession.shared.bytes(for: request)
                    guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
                        let code = (response as? HTTPURLResponse)?.statusCode ?? 0
                        continuation.finish(throwing: HTTPError.httpError(code, nil))
                        return
                    }

                    var currentEvent: String?
                    var hadDone = false
                    var hadError = false

                    for try await line in bytes.lines {
                        if line.hasPrefix("event: ") {
                            currentEvent = String(line.dropFirst(7))
                        } else if line.hasPrefix("data: ") {
                            let jsonStr = String(line.dropFirst(6))
                            if jsonStr == "[DONE]" { break }

                            guard let data = jsonStr.data(using: .utf8),
                                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                                continue
                            }

                            let eventType = currentEvent ?? ""
                            currentEvent = nil

                            switch eventType {
                            case "run.created", "run.in-progress":
                                // Extract the run status from event data (submitted, working, etc.)
                                let status = json["status"] as? String
                                if let status, let runID = json["run_id"] as? String ?? (json["run"] as? [String: Any])?["run_id"] as? String {
                                    continuation.yield(StreamChatEvent(
                                        type: eventType, content: status,
                                        name: nil, role: nil, sessionID: sessionID, runID: runID, message: nil))
                                } else {
                                    continuation.yield(StreamChatEvent(
                                        type: eventType, content: nil,
                                        name: nil, role: nil, sessionID: sessionID, runID: nil, message: nil))
                                }

                            case "message.part":
                                guard let part = json["part"] as? [String: Any],
                                      let contentType = part["content_type"] as? String else {
                                    continue
                                }
                                switch contentType {
                                case "text/plain":
                                    if let content = part["content"] as? String, !content.isEmpty {
                                        continuation.yield(StreamChatEvent(
                                            type: "token", content: content,
                                            name: nil, role: nil, sessionID: sessionID, runID: nil, message: nil))
                                    }
                                case "application/json":
                                    if let meta = part["metadata"] as? [String: Any],
                                       let kind = meta["kind"] as? String,
                                       kind == "trajectory" {
                                        let toolName = meta["tool_name"] as? String ?? "unknown"
                                        let hasOutput = meta["tool_output"] != nil
                                        // Serialize tool input/output from metadata
                                        let toolContent: String? = {
                                            let raw = hasOutput ? meta["tool_output"] : meta["tool_input"]
                                            guard let raw else { return nil }
                                            if let str = raw as? String { return str }
                                            if let d = try? JSONSerialization.data(withJSONObject: raw, options: .fragmentsAllowed) {
                                                return String(data: d, encoding: .utf8)
                                            }
                                            return nil
                                        }()
                                        continuation.yield(StreamChatEvent(
                                            type: hasOutput ? "tool_result" : "tool_call",
                                            content: toolContent, name: toolName, role: nil,
                                            sessionID: sessionID, runID: nil, message: nil))
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
                                        type: "message", content: textContent.isEmpty ? nil : textContent,
                                        name: nil, role: role.isEmpty ? nil : role,
                                        sessionID: sessionID, runID: nil, message: nil))
                                }

                            case "run.completed":
                                hadDone = true
                                continuation.yield(StreamChatEvent(
                                    type: "done", content: nil, name: nil, role: nil,
                                    sessionID: sessionID, runID: nil, message: nil))

                            case "run.failed", "run.cancelled":
                                hadError = true
                                var errMsg = eventType
                                if let run = json["run"] as? [String: Any],
                                   let err = run["error"] as? [String: Any],
                                   let m = err["message"] as? String { errMsg = m }
                                continuation.yield(StreamChatEvent(
                                    type: "error", content: nil, name: nil, role: nil,
                                    sessionID: sessionID, runID: nil, message: errMsg))

                            case "error":
                                hadError = true
                                var errMsg = "stream error"
                                if let e = json["error"] as? [String: Any],
                                   let m = e["message"] as? String { errMsg = m }
                                continuation.yield(StreamChatEvent(
                                    type: "error", content: nil, name: nil, role: nil,
                                    sessionID: sessionID, runID: nil, message: errMsg))

                            default:
                                break
                            }

                            if hadDone || hadError { break }
                        }
                    }

                    continuation.finish()
                } catch {
                    if Task.isCancelled { continuation.finish(); return }
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { @Sendable _ in task.cancel() }
        }
    }

    /// Convenience: send a message via ACP and wait for completion.
    public func sendMessageACP(
        agentName: String = "diane-default",
        sessionID: String,
        content: String
    ) async throws -> StreamChatEvent {
        let stream = streamACP(agentName: agentName, sessionID: sessionID, content: content)
        var finalEvent: StreamChatEvent? = nil
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

    // MARK: - Projects (MP API)

    public func fetchProjects() async throws -> [Project] {
        struct ProjectsResponse: Decodable, Sendable { let projects: [Project] }
        let data = try await http.get("/api/projects")
        if let resp = try? JSONDecoder().decode(ProjectsResponse.self, from: data), !resp.projects.isEmpty {
            return resp.projects
        }
        return (try? JSONDecoder().decode([Project].self, from: data)) ?? []
    }

    public func fetchProjectStats() async throws -> ProjectStats {
        let data = try await http.get("/api/stats")
        // Try multiple response formats
        if let s = try? JSONDecoder().decode(ProjectStats.self, from: data) { return s }
        return ProjectStats(totalSessions: 0, totalMessages: 0, activeAgents: 0, totalProjects: 0, sessionsToday: 0, messagesToday: 0)
    }

    // MARK: - ACP Sessions Listing

    /// Fetch existing ACP sessions from Memory Platform.
    /// GET /acp/v1/sessions
    public func fetchACPSessions() async throws -> [ACPSessionItem] {
        let data = try await http.get("/acp/v1/sessions")
        // Try wrapped format first
        struct WrappedResponse: Decodable, Sendable { let sessions: [ACPSessionItem] }
        if let resp = try? JSONDecoder().decode(WrappedResponse.self, from: data) {
            return resp.sessions
        }
        // Fallback to direct array
        return (try? JSONDecoder().decode([ACPSessionItem].self, from: data)) ?? []
    }

    /// Fetch a single ACP session with full details including run history.
    /// GET /acp/v1/sessions/:sessionId
    public func fetchACPSession(id: String) async throws -> ACPSessionItem {
        let data = try await http.get("/acp/v1/sessions/\(id)")
        return try JSONDecoder().decode(ACPSessionItem.self, from: data)
    }

    // MARK: - Documents (MP API)

    public func fetchDocuments(projectID: String) async throws -> [Document] {
        struct Response: Decodable, Sendable { let documents: [Document] }
        let data = try await http.get("/api/documents", headers: ["X-Project-ID": projectID])
        if let resp = try? JSONDecoder().decode(Response.self, from: data) { return resp.documents }
        return (try? JSONDecoder().decode([Document].self, from: data)) ?? []
    }

    public func uploadDocument(
        fileData: Data,
        filename: String,
        mimeType: String,
        projectID: String,
        autoExtract: Bool = true
    ) async throws -> Document {
        let boundary = "Boundary-\(UUID().uuidString)"
        var body = Data()
        body.append("--\(boundary)\r\n".data(using: .utf8)!)
        body.append("Content-Disposition: form-data; name=\"autoExtract\"\r\n\r\n".data(using: .utf8)!)
        body.append("\(autoExtract)\r\n".data(using: .utf8)!)
        body.append("--\(boundary)\r\n".data(using: .utf8)!)
        body.append("Content-Disposition: form-data; name=\"file\"; filename=\"\(filename)\"\r\n".data(using: .utf8)!)
        body.append("Content-Type: \(mimeType)\r\n\r\n".data(using: .utf8)!)
        body.append(fileData)
        body.append("\r\n".data(using: .utf8)!)
        body.append("--\(boundary)--\r\n".data(using: .utf8)!)
        let headers: [String: String] = [
            "Content-Type": "multipart/form-data; boundary=\(boundary)",
            "X-Project-ID": projectID,
        ]
        let data = try await http.post("/api/documents/upload", body: body, headers: headers, timeout: 120)
        struct UploadResponse: Decodable, Sendable {
            let document: UploadDocument; let isDuplicate: Bool?
        }
        struct UploadDocument: Decodable, Sendable {
            let id: String; let name: String; let mimeType: String?
            let fileSizeBytes: Int?; let conversionStatus: String?; let storageKey: String?
            let createdAt: String?
        }
        let uploadResp = try JSONDecoder().decode(UploadResponse.self, from: data)
        return Document(
            id: uploadResp.document.id, title: uploadResp.document.name,
            content: nil, fileType: uploadResp.document.mimeType,
            size: uploadResp.document.fileSizeBytes, projectID: projectID,
            createdAt: uploadResp.document.createdAt, updatedAt: nil)
    }

    // MARK: - Schema (MP API)

    public func fetchSchema() async throws -> SchemaResponse {
        try await getJSON("/api/schema")
    }

    // MARK: - Agents (MP API)

    public func fetchAgentDefs(projectID: String) async throws -> [AgentDef] {
        let data = try await http.get("/api/agent-definitions")
        struct Response: Decodable, Sendable { let data: [AgentDef] }
        if let resp = try? JSONDecoder().decode(Response.self, from: data) { return resp.data }
        return (try? JSONDecoder().decode([AgentDef].self, from: data)) ?? []
    }
}

public struct ACPSessionItem: Decodable, Sendable {
    public let id: String
    public let agentName: String?
    public let title: String?
    public let createdAt: String?
    public let updatedAt: String?
    public let messageCount: Int?
    public let status: String?
    public let runCount: Int?
    public let totalTokens: Int?
    public let totalCostUsd: Double?
    public let lastRunStatus: String?
    /// Server-side archive flag — iOS currently uses local archive only
    public let isArchived: Bool?

    enum CodingKeys: String, CodingKey {
        case id
        case agentName = "agent_name"
        case title
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case messageCount = "message_count"
        case status
        case runCount = "run_count"
        case totalTokens = "total_tokens"
        case totalCostUsd = "total_cost_usd"
        case lastRunStatus = "last_run_status"
        case isArchived = "is_archived"
    }
}

public extension ACPSessionItem {
    func toDianeSession() -> DianeSession {
        // Derive a display title from agent_name + date when server doesn't provide one
        let displayTitle: String? = {
            if let t = title, !t.isEmpty { return t }
            let agent = agentName ?? "Agent"
            if let created = createdAt {
                let dateStr = DateUtils.formatRelative(created)
                return "\(agent) — \(dateStr)"
            }
            return "Chat with \(agent)"
        }()

        // Use run_count as message_count when available (each run = 1 exchange)
        let displayMessageCount: Int? = messageCount ?? runCount

        return DianeSession(
            id: id,
            title: displayTitle,
            status: lastRunStatus,  // use last_run_status directly — nil means "no runs yet"
            messageCount: displayMessageCount,
            runCount: runCount,
            totalTokens: totalTokens,
            totalCostUsd: totalCostUsd,
            lastRunStatus: lastRunStatus,
            createdAt: createdAt,
            updatedAt: updatedAt,
            agentName: agentName
        )
    }
}
