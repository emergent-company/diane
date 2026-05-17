import Foundation
import SwiftUI

/// Persistent app configuration backed by ~/.config/diane.yml — same single
/// source of truth as the `diane` CLI. No UserDefaults fallback for config values.
@MainActor
final class ServerConfiguration: ObservableObject {
    @Published var serverURL: String = ""
    @Published var apiKey: String = ""
    @Published var projectID: String = ""

    // Launch-at-login is an app preference, not a project config — keep in UserDefaults.
    @Published var launchAtLogin: Bool {
        didSet { UserDefaults.standard.set(launchAtLogin, forKey: Keys.launchAtLogin) }
    }

    var isConfigured: Bool { !serverURL.isEmpty && !apiKey.isEmpty }

    var baseURL: URL? {
        guard !serverURL.isEmpty else { return nil }
        return URL(string: serverURL)
    }

    enum Keys {
        static let launchAtLogin = "launchAtLogin"
    }

    private let home: String

    init() {
        self.launchAtLogin = UserDefaults.standard.bool(forKey: Keys.launchAtLogin)
        self.home = FileManager.default.homeDirectoryForCurrentUser.path

        // In --uitesting mode, start with a clean slate — don't load real config.
        if CommandLine.arguments.contains("--uitesting") {
            AppLogger.shared.info("ServerConfig: skipping config load in uitesting mode", category: "Config")
            return
        }

        // Load config from diane.yml — single source of truth.
        loadFromConfig()
    }

    /// Reloads config from ~/.config/diane.yml. Returns true if config changed.
    @discardableResult
    func reload() -> Bool {
        let oldURL = serverURL
        let oldKey = apiKey
        let oldPID = projectID
        loadFromConfig()
        return serverURL != oldURL || apiKey != oldKey || projectID != oldPID
    }

    // MARK: - Config file parsing

    /// Reads ~/.config/diane.yml and extracts the active project's config.
    /// Properly handles YAML nesting so nested keys (e.g. `api_key` under
    /// `generative_provider`) are NOT mistaken for top-level config values.
    private func loadFromConfig() {
        let configPath = home + "/.config/diane.yml"
        guard let yamlData = try? Data(contentsOf: URL(fileURLWithPath: configPath)),
              let yamlStr = String(data: yamlData, encoding: .utf8) else {
            logInfo("ServerConfig: no diane.yml found at \(configPath)", category: "Config")
            return
        }

        let parsed = parseDianeYAML(yamlStr)
        guard let config = parsed else {
            logWarning("ServerConfig: failed to parse diane.yml", category: "Config")
            return
        }

        if !config.serverURL.isEmpty {
            serverURL = config.serverURL
        }
        if !config.token.isEmpty {
            apiKey = config.token
        }
        if !config.projectID.isEmpty {
            projectID = config.projectID
        }

        logInfo("ServerConfig: loaded project=\(projectID.prefix(12))… url=\(serverURL)", category: "Config")
    }

    /// Parse diane.yml and return the active (default) project's config.
    /// Handles indentation-based nesting: only captures key-value pairs that are
    /// direct children of the active project section, ignoring sub-sections like
    /// `generative_provider`, `embedding_provider`, and `agents`.
    private func parseDianeYAML(_ content: String) -> (projectID: String, serverURL: String, token: String)? {
        let lines = content.components(separatedBy: .newlines)

        // Phase 1: find the default project name
        var defaultProject: String?
        for line in lines {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard trimmed.hasPrefix("default:") else { continue }
            defaultProject = String(trimmed.dropFirst("default:".count)).trimmingCharacters(in: .whitespaces)
            break
        }
        guard let projectName = defaultProject, !projectName.isEmpty else {
            logWarning("ServerConfig: no 'default:' key in diane.yml", category: "Config")
            return nil
        }

        // Phase 2: find indentation of the project section and extract its direct keys
        var projectIndent: Int?          // spaces before the project name
        var inProject = false
        var inSubSection = false
        var subSectionIndent: Int?       // spaces before sub-section keys

        var result = (projectID: "", serverURL: "", token: "")

        for line in lines {
            let leadingSpaces = line.prefix(while: { $0 == " " }).count
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty, !trimmed.hasPrefix("#") else { continue }

            // Find our project section: "    projectName:"
            if let pi = projectIndent {
                // We know the project indent — check if we're inside it
                if leadingSpaces <= pi {
                    // We've left the project section
                    if inProject { break }
                    continue
                }
            } else {
                // Still looking for the project section header
                // It should be at indent > 0, under "projects:"
                // Format: "    local:" where "local" matches our default project name
                guard let colonIdx = trimmed.firstIndex(of: ":") else { continue }
                let key = String(trimmed[..<colonIdx]).trimmingCharacters(in: .whitespaces)
                if key == projectName, leadingSpaces > 0 {
                    projectIndent = leadingSpaces
                    inProject = true
                    // Check if the value is empty (section header) or has inline value
                    let valueAfterColon = String(trimmed[trimmed.index(after: colonIdx)...]).trimmingCharacters(in: .whitespaces)
                    // Project section should have no value after colon
                    continue
                }
                continue
            }

            // We're inside the project section
            guard let colonIdx = trimmed.firstIndex(of: ":") else { continue }
            let key = String(trimmed[..<colonIdx]).trimmingCharacters(in: .whitespaces)
            let valueAfterColon = String(trimmed[trimmed.index(after: colonIdx)...]).trimmingCharacters(in: .whitespaces)

            if valueAfterColon.isEmpty {
                // This is a sub-section header (e.g. "generative_provider:", "agents:")
                // Everything indented further is a sub-section value — skip it
                inSubSection = true
                subSectionIndent = leadingSpaces
                continue
            }

            // We have a key:value pair at this level
            // Check if we're still in a sub-section
            if inSubSection, let si = subSectionIndent {
                if leadingSpaces <= si {
                    inSubSection = false
                    subSectionIndent = nil
                } else {
                    // Still inside sub-section — skip
                    continue
                }
            }

            // Capture the value (only if NOT inside a sub-section)
            switch key {
            case "server_url":
                result.serverURL = valueAfterColon
            case "project_id":
                result.projectID = valueAfterColon
            case "token":
                result.token = valueAfterColon
            default:
                break
            }
        }

        guard !result.projectID.isEmpty else {
            logWarning("ServerConfig: no project_id found for project '\(projectName)' in diane.yml", category: "Config")
            return nil
        }

        return result
    }
}
