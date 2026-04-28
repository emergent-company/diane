import Foundation
import SwiftUI

/// Persistent app configuration backed by UserDefaults, with auto-discovery
/// from Diane's config files (~/.diane/config.yaml + ~/.diane/secrets/memory-config.json).
@MainActor
final class ServerConfiguration: ObservableObject {
    @Published var serverURL: String {
        didSet { UserDefaults.standard.set(serverURL, forKey: Keys.serverURL) }
    }

    @Published var apiKey: String {
        didSet { UserDefaults.standard.set(apiKey, forKey: Keys.apiKey) }
    }

    @Published var projectID: String {
        didSet { UserDefaults.standard.set(projectID, forKey: Keys.projectID) }
    }

    @Published var launchAtLogin: Bool {
        didSet { UserDefaults.standard.set(launchAtLogin, forKey: Keys.launchAtLogin) }
    }

    var isConfigured: Bool { !serverURL.isEmpty && !apiKey.isEmpty }

    var baseURL: URL? {
        guard !serverURL.isEmpty else { return nil }
        return URL(string: serverURL)
    }

    enum Keys {
        static let serverURL     = "serverURL"
        static let apiKey        = "apiKey"
        static let projectID     = "projectID"
        static let launchAtLogin = "launchAtLogin"
    }

    init() {
        let defaults = UserDefaults.standard

        // Load persisted values first
        self.serverURL     = defaults.string(forKey: Keys.serverURL) ?? ""
        self.apiKey        = defaults.string(forKey: Keys.apiKey) ?? ""
        self.projectID     = defaults.string(forKey: Keys.projectID) ?? ""
        self.launchAtLogin = defaults.bool(forKey: Keys.launchAtLogin)

        // Auto-discover from Diane config files if not already set
        let home = FileManager.default.homeDirectoryForCurrentUser.path

        // Read ~/.diane/config.yaml
        let configYamlPath = home + "/.diane/config.yaml"
        if serverURL.isEmpty || projectID.isEmpty,
           let yamlData = try? Data(contentsOf: URL(fileURLWithPath: configYamlPath)),
           let yamlStr = String(data: yamlData, encoding: .utf8) {
            for line in yamlStr.components(separatedBy: .newlines) {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                if serverURL.isEmpty, trimmed.hasPrefix("server_url:"),
                   let value = trimmed.components(separatedBy: ":").dropFirst().first?.trimmingCharacters(in: .whitespaces) {
                    serverURL = value
                }
                if projectID.isEmpty, trimmed.hasPrefix("project_id:"),
                   let value = trimmed.components(separatedBy: ":").dropFirst().first?.trimmingCharacters(in: .whitespaces) {
                    projectID = value
                }
            }
        }

        // Read ~/.diane/secrets/memory-config.json
        let secretsPath = home + "/.diane/secrets/memory-config.json"
        if apiKey.isEmpty || serverURL.isEmpty,
           let jsonData = try? Data(contentsOf: URL(fileURLWithPath: secretsPath)),
           let json = try? JSONSerialization.jsonObject(with: jsonData) as? [String: String] {
            if apiKey.isEmpty, let token = json["project_token"] {
                apiKey = token
            }
            if serverURL.isEmpty, let url = json["server_url"] {
                serverURL = url
            }
        }
    }
}
