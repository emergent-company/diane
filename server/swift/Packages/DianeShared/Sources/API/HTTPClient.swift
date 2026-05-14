import Foundation

// MARK: - Errors

public enum HTTPError: LocalizedError, Sendable {
    case notConfigured
    case invalidURL(String)
    case httpError(Int, String?)
    case network(String)
    case notFound(String)
    case cancelled

    public var errorDescription: String? {
        switch self {
        case .notConfigured: return "Client not configured"
        case .invalidURL(let path): return "Invalid URL: \(path)"
        case .httpError(let code, let body): return "HTTP \(code)\(body.map { ": \($0)" } ?? "")"
        case .network(let msg): return "Network error: \(msg)"
        case .notFound(let msg): return msg
        case .cancelled: return "Request cancelled"
        }
    }
}

// MARK: - HTTP Client

public final class HTTPClient: @unchecked Sendable {
    private let session: URLSession
    public var baseURL: String = ""
    public var defaultHeaders: [String: String] = [:]

    public init(baseURL: String = "", timeoutForRequest: Double = 15, timeoutForResource: Double = 30) {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = timeoutForRequest
        config.timeoutIntervalForResource = timeoutForResource
        self.session = URLSession(configuration: config)
        self.baseURL = baseURL
    }

    // MARK: - HTTP Methods

    public func get(_ path: String, headers: [String: String]? = nil) async throws -> Data {
        let request = try buildRequest(path: path, method: "GET", headers: headers)
        return try await perform(request)
    }

    public func post(_ path: String, body: Data? = nil, headers: [String: String]? = nil, timeout: TimeInterval? = nil) async throws -> Data {
        let request = try buildRequest(path: path, method: "POST", body: body, headers: headers, timeout: timeout)
        return try await perform(request)
    }

    public func put(_ path: String, body: Data? = nil, headers: [String: String]? = nil) async throws -> Data {
        let request = try buildRequest(path: path, method: "PUT", body: body, headers: headers)
        return try await perform(request)
    }

    public func delete(_ path: String, headers: [String: String]? = nil) async throws -> Data {
        let request = try buildRequest(path: path, method: "DELETE", headers: headers)
        return try await perform(request)
    }

    public func patch(_ path: String, body: Data? = nil, headers: [String: String]? = nil) async throws -> Data {
        let request = try buildRequest(path: path, method: "PATCH", body: body, headers: headers)
        return try await perform(request)
    }

    // MARK: - SSE Streaming

    /// Stream SSE events from a POST endpoint. Returns parsed StreamChatEvents.
    /// The endpoint should respond with text/event-stream.
    public func streamSSE(
        path: String,
        body: Data,
        headers: [String: String]? = nil
    ) -> AsyncThrowingStream<StreamChatEvent, Error> {
        return AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var request = try buildRequest(path: path, method: "POST", body: body, headers: headers, timeout: 300)
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")

                    let (bytes, response) = try await URLSession.shared.bytes(for: request)
                    guard let http = response as? HTTPURLResponse else {
                        continuation.finish(throwing: HTTPError.network("No HTTP response"))
                        return
                    }
                    guard (200...299).contains(http.statusCode) else {
                        continuation.finish(throwing: HTTPError.httpError(http.statusCode, nil))
                        return
                    }

                    var currentEvent: String?

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

                            let event = StreamChatEvent(
                                type: eventType,
                                content: json["content"] as? String,
                                name: json["name"] as? String,
                                role: json["role"] as? String,
                                sessionID: json["session_id"] as? String,
                                runID: json["run_id"] as? String,
                                message: json["message"] as? String
                            )
                            continuation.yield(event)

                            if eventType == "done" || eventType == "error" {
                                break
                            }
                        }
                    }
                    continuation.finish()
                } catch {
                    if Task.isCancelled {
                        continuation.finish()
                    } else {
                        continuation.finish(throwing: HTTPError.network(error.localizedDescription))
                    }
                }
            }
            continuation.onTermination = { @Sendable _ in task.cancel() }
        }
    }

    // MARK: - Private

    private func buildRequest(
        path: String,
        method: String,
        body: Data? = nil,
        headers: [String: String]? = nil,
        timeout: TimeInterval? = nil
    ) throws -> URLRequest {
        guard !baseURL.isEmpty else { throw HTTPError.notConfigured }
        guard let url = URL(string: path, relativeTo: URL(string: baseURL)) ?? URL(string: baseURL + path) else {
            throw HTTPError.invalidURL(path)
        }

        var request = URLRequest(url: url)
        request.httpMethod = method
        request.httpBody = body

        // Default headers
        for (key, value) in defaultHeaders {
            request.setValue(value, forHTTPHeaderField: key)
        }
        // Per-request overrides
        if let extra = headers {
            for (key, value) in extra {
                request.setValue(value, forHTTPHeaderField: key)
            }
        }

        if let t = timeout {
            request.timeoutInterval = t
        }

        return request
    }

    private func perform(_ request: URLRequest) async throws -> Data {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw HTTPError.network("No HTTP response")
        }
        guard (200...299).contains(http.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? ""
            throw HTTPError.httpError(http.statusCode, body.isEmpty ? nil : body)
        }
        return data
    }
}
