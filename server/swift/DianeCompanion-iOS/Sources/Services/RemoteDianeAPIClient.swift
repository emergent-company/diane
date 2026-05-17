import Foundation
import DianeShared

/// HTTP client for the user's remote Diane server.
/// Primarily used for server-specific endpoints not covered by the MP ACP API.
public final class RemoteDianeAPIClient: @unchecked Sendable {
    public static let shared = RemoteDianeAPIClient()

    private let http: HTTPClient

    public var baseURL: String { http.baseURL }

    private init() {
        self.http = HTTPClient()
    }

    public func configure(baseURL: String, apiKey: String) {
        http.baseURL = baseURL
        if !apiKey.isEmpty {
            http.defaultHeaders["X-API-Key"] = apiKey
        }
    }
}
