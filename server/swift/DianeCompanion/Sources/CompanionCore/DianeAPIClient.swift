import Foundation
import OSLog

/// Client for Diane's local companion API (served by `diane serve` on 127.0.0.1:8890).
///
/// This is the preferred data source for the companion app — it uses the
/// same data paths as the diane CLI (Memory Bridge for sessions, local
/// config for MCP servers, Memory Platform relay for nodes).
@MainActor
final class DianeAPIClient: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "DianeAPI")
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

    // MARK: - Health / Reachability

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
        return (try? JSONDecoder().decode([DianeSession].self, from: data)) ?? []
    }

    func fetchSessionMessages(sessionID: String) async throws -> [DianeMessage] {
        let encoded = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        let data = try await get("/api/sessions/\(encoded)/messages")
        struct Response: Decodable { let items: [DianeMessage]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        return (try? JSONDecoder().decode([DianeMessage].self, from: data)) ?? []
    }

    // MARK: - MCP Servers

    func fetchMCPServers() async throws -> [MCPServer] {
        let data = try await get("/api/mcp-servers")
        struct Response: Decodable { let servers: [MCPServer]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.servers {
            return list
        }
        return (try? JSONDecoder().decode([MCPServer].self, from: data)) ?? []
    }

    // MARK: - Relay Nodes

    func fetchRelayNodes() async throws -> [RelayNode] {
        let data = try await get("/api/nodes")
        struct Response: Decodable { let nodes: [RelayNode]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.nodes {
            return list
        }
        return (try? JSONDecoder().decode([RelayNode].self, from: data)) ?? []
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
}

enum DianeAPIError: Error, LocalizedError {
    case invalidURL(String)
    case network(String)
    case httpError(Int, String)

    var errorDescription: String? {
        switch self {
        case .invalidURL(let p): return "Invalid URL: \(p)"
        case .network(let msg):  return "Network error: \(msg)"
        case .httpError(let c, let b): return "HTTP \(c): \(b)"
        }
    }
}

// MARK: - Relay Node Model

struct RelayNode: Identifiable, Codable, Hashable, Sendable {
    let instanceID: String
    let hostname: String?
    let version: String?
    let toolCount: Int?
    let connectedAt: String?

    var id: String { instanceID }

    enum CodingKeys: String, CodingKey {
        case instanceID = "instance_id"
        case hostname, version
        case toolCount = "tool_count"
        case connectedAt = "connected_at"
    }

    func hash(into hasher: inout Hasher) { hasher.combine(instanceID) }
    static func == (lhs: RelayNode, rhs: RelayNode) -> Bool { lhs.instanceID == rhs.instanceID }
}
