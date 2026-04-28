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

        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let alreadyConfigured = !serverURL.isEmpty && !apiKey.isEmpty && !projectID.isEmpty

        // Read ~/.diane/secrets/memory-config.json — server URL + API key
        let secretsPath = home + "/.diane/secrets/memory-config.json"
        if !alreadyConfigured,
           let jsonData = try? Data(contentsOf: URL(fileURLWithPath: secretsPath)),
           let json = try? JSONSerialization.jsonObject(with: jsonData) as? [String: String] {
            if serverURL.isEmpty, let url = json["server_url"] {
                serverURL = url
            }
            if apiKey.isEmpty, let token = json["project_token"] {
                apiKey = token
            }
        }

        // Read ~/.config/diane.yml — project ID (nested under projects.<default>)
        let configYamlPath = home + "/.config/diane.yml"
        if projectID.isEmpty,
           let yamlData = try? Data(contentsOf: URL(fileURLWithPath: configYamlPath)),
           let yamlStr = String(data: yamlData, encoding: .utf8) {
            projectID = Self.parseProjectIDFromYAML(yamlStr)
        }

        // Read ~/.diane/config.yaml as fallback
        let altYamlPath = home + "/.diane/config.yaml"
        if projectID.isEmpty,
           let yamlData = try? Data(contentsOf: URL(fileURLWithPath: altYamlPath)),
           let yamlStr = String(data: yamlData, encoding: .utf8) {
            projectID = Self.parseProjectIDFromYAML(yamlStr)
        }
    }

    /// Parse the project ID from Diane's YAML config structure.
    /// Supports both flat (project_id:) and nested (projects.<default>.project_id:) formats.
    private static func parseProjectIDFromYAML(_ yaml: String) -> String {
        // Scan each line for project_id in any context (flat or nested YAML)
        for line in yaml.components(separatedBy: .newlines) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if let value = Self.extractYAMLValue(line: trimmed, key: "project_id") {
                return value
            }
        }
        return ""
    }

    /// Extract the value for a YAML key, handling colons in URLs.
    /// e.g., "server_url: https://example.com/path" -> "https://example.com/path"
    private static func extractYAMLValue(line: String, key: String) -> String? {
        let pattern = "^\(key):\\s*(.*)"
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)),
              let range = Range(match.range(at: 1), in: line)
        else { return nil }
        var value = String(line[range])
        // Strip surrounding quotes
        value = value.trimmingCharacters(in: .whitespaces)
        if value.hasPrefix("\"") && value.hasSuffix("\"") {
            value = String(value.dropFirst().dropLast())
        } else if value.hasPrefix("'") && value.hasSuffix("'") {
            value = String(value.dropFirst().dropLast())
        }
        return value
    }
}
