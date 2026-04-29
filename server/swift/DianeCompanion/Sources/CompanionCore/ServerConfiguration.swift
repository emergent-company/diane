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

    private let home: String

    init() {
        let defaults = UserDefaults.standard

        // Load persisted values first (fast, stays on main actor)
        self.serverURL     = defaults.string(forKey: Keys.serverURL) ?? ""
        self.apiKey        = defaults.string(forKey: Keys.apiKey) ?? ""
        self.projectID     = defaults.string(forKey: Keys.projectID) ?? ""
        self.launchAtLogin = defaults.bool(forKey: Keys.launchAtLogin)
        self.home = FileManager.default.homeDirectoryForCurrentUser.path

        // Offload file I/O and YAML parsing to background queue
        discoverFromConfig()
    }

    /// Reads ~/.config/diane.yml on a background queue and updates published properties on the main actor.
    private func discoverFromConfig() {
        let configPath = home + "/.config/diane.yml"
        guard serverURL.isEmpty || apiKey.isEmpty || projectID.isEmpty else { return }

        DispatchQueue.global(qos: .utility).async { [weak self] in
            guard let self = self else { return }
            guard let yamlData = try? Data(contentsOf: URL(fileURLWithPath: configPath)),
                  let yamlStr = String(data: yamlData, encoding: .utf8) else {
                return
            }

            // Parse YAML key:value pairs on background thread
            var discoveredURL: String?
            var discoveredKey: String?
            var discoveredProject: String?

            for line in yamlStr.components(separatedBy: .newlines) {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                guard !trimmed.isEmpty, !trimmed.hasPrefix("#") else { continue }

                guard let colonIndex = trimmed.firstIndex(of: ":") else { continue }
                let key = trimmed[..<colonIndex].trimmingCharacters(in: .whitespaces)
                let value = trimmed[trimmed.index(after: colonIndex)...].trimmingCharacters(in: .whitespaces)
                guard !value.isEmpty else { continue }

                switch key {
                case "server_url":  discoveredURL     = value
                case "project_id":  discoveredProject = value
                case "api_key", "token": discoveredKey = value
                default: break
                }
            }

            // Dispatch back to main actor to update published properties
            Task { @MainActor in
                if self.serverURL.isEmpty, let url = discoveredURL {
                    self.serverURL = url
                }
                if self.apiKey.isEmpty, let key = discoveredKey {
                    self.apiKey = key
                }
                if self.projectID.isEmpty, let pid = discoveredProject {
                    self.projectID = pid
                }
            }
        }
    }
}
