import Foundation
import DianeShared

/// HTTP client for the user's remote Diane server.
/// Handles chat streaming sessions, agents, MCP servers, and all data that comes from the Diane serve API.
public final class RemoteDianeAPIClient: @unchecked Sendable {
    private let http: HTTPClient

    public var baseURL: String { http.baseURL }

    public init() {
        self.http = HTTPClient()
    }

    public func configure(baseURL: String, apiKey: String) {
        http.baseURL = baseURL
        if !apiKey.isEmpty {
            http.defaultHeaders["X-API-Key"] = apiKey
        }
    }

    // MARK: - Sessions

    public func fetchSessions() async throws -> [DianeSession] {
        let data = try await http.get("/api/sessions")
        struct Response: Decodable { let items: [DianeSession]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        return (try? JSONDecoder().decode([DianeSession].self, from: data)) ?? []
    }

    public func deleteSession(_ id: String) async throws {
        let encoded = id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id
        _ = try await http.delete("/api/sessions/\(encoded)")
    }

    public func fetchMessages(sessionID: String) async throws -> [DianeMessage] {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let data = try await http.get("/api/sessions/\(encoded)/messages")
        struct Response: Decodable { let items: [DianeMessage]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        return (try? JSONDecoder().decode([DianeMessage].self, from: data)) ?? []
    }

    public func createSession() async throws -> DianeSession {
        let data = try await http.post("/api/sessions", body: nil)
        return try JSONDecoder().decode(DianeSession.self, from: data)
    }

    // MARK: - Chat Streaming

    /// Send a message and stream the response via SSE.
    public func streamChat(sessionID: String?, content: String, agentName: String = "diane-default") -> AsyncThrowingStream<StreamChatEvent, Error> {
        let body: [String: Any] = [
            "session_id": sessionID as Any,
            "content": content,
            "agent_name": agentName
        ]
        let jsonData = (try? JSONSerialization.data(withJSONObject: body)) ?? Data()

        return AsyncThrowingStream { continuation in
            Task {
                do {
                    // Use raw URLSession SSE streaming since HTTPClient.streamSSE
                    // uses a simpler event parser; for the chat endpoint we need
                    // the ACP-style event parsing from EmergentAPIClient
                    guard let url = URL(string: "/api/chat/stream", relativeTo: URL(string: self.http.baseURL)) ?? URL(string: self.http.baseURL + "/api/chat/stream") else {
                        continuation.finish(throwing: HTTPError.invalidURL("/api/chat/stream"))
                        return
                    }

                    var request = URLRequest(url: url)
                    request.httpMethod = "POST"
                    request.httpBody = jsonData
                    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    request.timeoutInterval = 300

                    // Add default headers
                    for (key, value) in self.http.defaultHeaders {
                        request.setValue(value, forHTTPHeaderField: key)
                    }

                    let (bytes, response) = try await URLSession.shared.bytes(for: request)
                    guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
                        throw HTTPError.httpError((response as? HTTPURLResponse)?.statusCode ?? 0, nil)
                    }

                    for try await line in bytes.lines {
                        if line.hasPrefix("data: ") {
                            let jsonStr = String(line.dropFirst(6))
                            if jsonStr == "[DONE]" { break }

                            guard let data = jsonStr.data(using: .utf8),
                                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                                continue
                            }

                            let type = json["type"] as? String ?? "token"
                            let event = StreamChatEvent(
                                type: type,
                                content: json["content"] as? String,
                                name: json["name"] as? String,
                                role: json["role"] as? String,
                                sessionID: sessionID,
                                runID: json["run_id"] as? String,
                                message: type == "error" ? (json["message"] as? String ?? "Unknown error") : nil
                            )
                            continuation.yield(event)

                            if type == "done" || type == "error" {
                                break
                            }
                        }
                    }
                    continuation.finish()
                } catch {
                    if Task.isCancelled { continuation.finish(); return }
                    continuation.finish(throwing: error)
                }
            }
        }
    }

    // MARK: - Agents

    public func fetchAgents() async throws -> [AgentDef] {
        let data = try await http.get("/api/agents")
        struct Response: Decodable { let agents: [AgentDef]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.agents {
            return list
        }
        return []
    }

    // MARK: - MCP Servers

    public func fetchMCPServers() async throws -> [MCPServer] {
        let data = try await http.get("/api/mcp-servers")
        struct Response: Decodable { let servers: [MCPServer]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.servers {
            return list
        }
        return (try? JSONDecoder().decode([MCPServer].self, from: data)) ?? []
    }

    // MARK: - Relay Nodes

    public func fetchRelayNodes() async throws -> [RelayNode] {
        let data = try await http.get("/api/nodes")
        struct Response: Decodable { let nodes: [RelayNode]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.nodes {
            return list
        }
        return (try? JSONDecoder().decode([RelayNode].self, from: data)) ?? []
    }
}
