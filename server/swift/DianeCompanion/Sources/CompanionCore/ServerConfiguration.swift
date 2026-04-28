import Foundation
import SwiftUI

/// Persistent app configuration backed by UserDefaults, with auto-discovery
/// from Diane's config file (~/.config/diane.yml).
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

        // Auto-discover from Diane config file if not already set
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let configPath = home + "/.config/diane.yml"

        guard (serverURL.isEmpty || apiKey.isEmpty || projectID.isEmpty),
              let yamlData = try? Data(contentsOf: URL(fileURLWithPath: configPath)),
              let yamlStr = String(data: yamlData, encoding: .utf8) else {
            return
        }

        for line in yamlStr.components(separatedBy: .newlines) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty, !trimmed.hasPrefix("#") else { continue }

            // Parse key: value pairs
            guard let colonIndex = trimmed.firstIndex(of: ":") else { continue }
            let key = trimmed[..<colonIndex].trimmingCharacters(in: .whitespaces)
            let value = trimmed[trimmed.index(after: colonIndex)...].trimmingCharacters(in: .whitespaces)
            guard !value.isEmpty else { continue }

            switch key {
            case "server_url" where serverURL.isEmpty:
                serverURL = value
            case "project_id" where projectID.isEmpty:
                projectID = value
            case "api_key" where apiKey.isEmpty:
                apiKey = value
            case "token" where apiKey.isEmpty:
                apiKey = value
            default:
                break
            }
        }
    }
}
