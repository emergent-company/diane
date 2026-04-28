import Foundation
import OSLog

/// Lightweight HTTP client for Diane REST API endpoints.
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

    // MARK: - Diane Sessions

    func fetchSessions(projectID: String, status: String? = nil, limit: Int = 50) async throws -> [DianeSession] {
        var path = "/api/graph/objects?type=Session&limit=\(limit)"
        if let s = status { path += "&status=\(s)" }
        let data = try await get(path, projectID: projectID)
        struct Response: Decodable { let items: [DianeSession]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        return (try? JSONDecoder().decode([DianeSession].self, from: data)) ?? []
    }

    func fetchSessionMessages(projectID: String, sessionID: String, limit: Int = 200) async throws -> [DianeMessage] {
        let data = try await get("/api/graph/objects/\(sessionID)/messages?limit=\(limit)", projectID: projectID)
        struct Response: Decodable { let items: [DianeMessage]? }
        if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
            return list
        }
        return (try? JSONDecoder().decode([DianeMessage].self, from: data)) ?? []
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

    // MARK: - HTTP helpers

    private func get(_ path: String, projectID: String? = nil) async throws -> Data {
        var req = try makeRequest(method: "GET", path: path)
        if let pid = projectID { req.setValue(pid, forHTTPHeaderField: "X-Project-ID") }
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
                    throw EmergentAPIError.unauthorized
                case 404:
                    throw EmergentAPIError.notFound(request.url?.path ?? "")
                case 500...599:
                    throw EmergentAPIError.serverError(http.statusCode)
                default:
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
}

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
