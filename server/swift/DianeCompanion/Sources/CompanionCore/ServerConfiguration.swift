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

        // Auto-discover server_url from ~/.diane/config.yaml if not already set
        if self.serverURL.isEmpty {
            self.serverURL = Self.discoverServerURL()
        }
    }

    /// Reads ~/.diane/config.yaml and extracts the server_url value.
    private static func discoverServerURL() -> String {
        let configPath = NSString(string: "~/.diane/config.yaml").expandingTildeInPath
        guard let content = try? String(contentsOfFile: configPath, encoding: .utf8) else {
            return ""
        }
        for line in content.components(separatedBy: .newlines) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            let prefix = "server_url:"
            if trimmed.hasPrefix(prefix) {
                let value = trimmed.dropFirst(prefix.count).trimmingCharacters(in: .whitespaces)
                if !value.isEmpty { return value }
            }
        }
        return ""
    }
}
