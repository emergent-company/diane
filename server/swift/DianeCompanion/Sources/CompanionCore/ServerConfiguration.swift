import Foundation
import SwiftUI

/// Persistent app configuration backed by UserDefaults.
@MainActor
final class ServerConfiguration: ObservableObject {
    @Published var serverURL: String {
        didSet { UserDefaults.standard.set(serverURL, forKey: Keys.serverURL) }
    }

    @Published var apiKey: String {
        didSet { UserDefaults.standard.set(apiKey, forKey: Keys.apiKey) }
    }

    @Published var launchAtLogin: Bool {
        didSet { UserDefaults.standard.set(launchAtLogin, forKey: Keys.launchAtLogin) }
    }

    /// Project ID used for scoped API calls, auto-discovered from diane config.
    @Published var projectID: String = ""

    var isConfigured: Bool { !serverURL.isEmpty }

    var baseURL: URL? {
        guard !serverURL.isEmpty else { return nil }
        return URL(string: serverURL)
    }

    enum Keys {
        static let serverURL     = "serverURL"
        static let apiKey        = "apiKey"
        static let launchAtLogin = "launchAtLogin"
    }

    init() {
        self.serverURL     = UserDefaults.standard.string(forKey: Keys.serverURL) ?? ""
        self.apiKey        = UserDefaults.standard.string(forKey: Keys.apiKey) ?? ""
        self.launchAtLogin = UserDefaults.standard.bool(forKey: Keys.launchAtLogin)

        // Auto-discover from ~/.diane/ config if not already persisted
        if self.serverURL.isEmpty || self.apiKey.isEmpty || self.projectID.isEmpty {
            let (discoveredURL, discoveredKey, discoveredProjectID) = Self.discoverFromDianeConfig()
            if self.serverURL.isEmpty { self.serverURL = discoveredURL }
            if self.apiKey.isEmpty    { self.apiKey = discoveredKey }
            if self.projectID.isEmpty { self.projectID = discoveredProjectID }
        }
    }

    /// Reads ~/.diane/config.yaml and ~/.diane/secrets/memory-config.json
    /// to auto-populate server URL, API key, and project ID.
    private static func discoverFromDianeConfig() -> (url: String, key: String, projectID: String) {
        var url = ""
        var key = ""
        var pid = ""

        // 1. Try ~/.diane/config.yaml for server_url and project_id
        let configPath = NSString(string: "~/.diane/config.yaml").expandingTildeInPath
        if let content = try? String(contentsOfFile: configPath, encoding: .utf8) {
            for line in content.components(separatedBy: .newlines) {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                if trimmed.hasPrefix("server_url:") {
                    let value = trimmed.dropFirst("server_url:".count).trimmingCharacters(in: .whitespaces)
                    if !value.isEmpty { url = value }
                } else if trimmed.hasPrefix("project_id:") {
                    let value = trimmed.dropFirst("project_id:".count).trimmingCharacters(in: .whitespaces)
                    if !value.isEmpty { pid = value }
                }
            }
        }

        // 2. Try ~/.diane/secrets/memory-config.json for project_token
        let secretsPath = NSString(string: "~/.diane/secrets/memory-config.json").expandingTildeInPath
        if let data = try? Data(contentsOf: URL(fileURLWithPath: secretsPath)),
           let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            if let token = json["project_token"] as? String, !token.isEmpty {
                key = token
            }
        }

        return (url, key, pid)
    }
}
