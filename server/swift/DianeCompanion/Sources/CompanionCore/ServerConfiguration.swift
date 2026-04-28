import Foundation
import SwiftUI

/// Persistent app configuration backed by UserDefaults.
/// Injected into the view hierarchy via `.environmentObject()`.
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
        if self.serverURL.isEmpty || self.apiKey.isEmpty {
            let (discoveredURL, discoveredKey) = Self.discoverFromDianeConfig()
            if self.serverURL.isEmpty { self.serverURL = discoveredURL }
            if self.apiKey.isEmpty    { self.apiKey = discoveredKey }
        }
    }

    /// Reads ~/.diane/config.yaml and ~/.diane/secrets/memory-config.json
    /// to auto-populate server URL and API key.
    private static func discoverFromDianeConfig() -> (url: String, key: String) {
        var url = ""
        var key = ""

        // 1. Try ~/.diane/config.yaml for server_url
        let configPath = NSString(string: "~/.diane/config.yaml").expandingTildeInPath
        if let content = try? String(contentsOfFile: configPath, encoding: .utf8) {
            for line in content.components(separatedBy: .newlines) {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                let prefix = "server_url:"
                if trimmed.hasPrefix(prefix) {
                    let value = trimmed.dropFirst(prefix.count).trimmingCharacters(in: .whitespaces)
                    if !value.isEmpty { url = value }
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

        return (url, key)
    }
}
