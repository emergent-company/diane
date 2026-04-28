import Foundation
import Combine

/// Central observable state shared across the entire application.
///
/// Injected into the view hierarchy via `.environmentObject(appState)`.
/// All project-scoped views read `selectedProject` from here to know
/// which project to scope their SDK calls to.
@MainActor
final class AppState: ObservableObject {

    // MARK: - Server connection

    /// The currently configured server URL (mirrors `ServerConfiguration.serverURL`).
    @Published var serverURL: String = ""

    /// Whether the app is currently connected to the server.
    @Published var isConnected: Bool = false

    // MARK: - Project context

    /// All projects loaded from the server. Empty until the first successful fetch.
    @Published var projects: [Project] = []

    /// The currently selected project. Nil means no project is selected.
    @Published var selectedProject: Project? = nil

    /// Whether projects are currently being loaded.
    @Published var isLoadingProjects: Bool = false

    /// Error from the last project fetch, if any.
    @Published var projectLoadError: String? = nil

    // MARK: - Navigation

    /// The currently active sidebar item in the main window.
    @Published var selectedSidebarItem: SidebarItem? = .query

    // MARK: - Auth

    /// The API key used to authenticate with the server (loaded from keychain/UserDefaults).
    @Published var apiKey: String = ""

    // MARK: - Document Content Window

    /// The document to display in the content viewer window.
    /// Set this then call openWindow(id: "document-content").
    @Published var contentViewDocument: Document? = nil

    // MARK: - Computed

    /// The active project ID, for use in SDK calls.
    var activeProjectID: String? { selectedProject?.id }

    /// Whether there is a valid server connection and a project selected.
    var isReady: Bool { isConnected && selectedProject != nil }
}

// MARK: - SidebarItem

/// Represents the navigable sections in the main window sidebar.
enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
    // Project section
    case query     = "Query"
    case sessions  = "Sessions"
    case traces    = "Traces"
    case status    = "Status"
    case workers   = "Workers"
    case objects   = "Objects"
    case documents = "Documents"

    // Account section
    case accountStatus = "Account Status"
    case profile       = "Profile"

    // Configuration section
    case agents    = "Agents"
    case mcpServers = "MCP Servers"
    case providers = "Providers"
    case permissions = "Permissions"

    var id: String { rawValue }

    var systemIcon: String {
        switch self {
        case .query:         return "magnifyingglass"
        case .sessions:      return "message"
        case .traces:        return "chart.bar.doc.horizontal"
        case .status:        return "chart.line.uptrend.xyaxis"
        case .workers:       return "gearshape.2"
        case .objects:       return "cube"
        case .documents:     return "doc.text"
        case .accountStatus: return "star"
        case .profile:       return "person.circle"
        case .agents:        return "cpu"
        case .mcpServers:    return "plug"
        case .providers:     return "cpu.fill"
        case .permissions:   return "lock.shield"
        }
    }

    var sectionGroup: SidebarSection {
        switch self {
        case .query, .sessions, .traces, .status, .workers, .objects, .documents:
            return .project
        case .accountStatus, .profile:
            return .account
        case .agents, .mcpServers, .providers, .permissions:
            return .configuration
        }
    }
}

/// Logical groupings for the sidebar.
enum SidebarSection: String, CaseIterable {
    case project       = "Project"
    case account       = "Account"
    case configuration = "Configuration"
}
