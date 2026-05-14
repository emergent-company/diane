import Foundation
import Combine

/// Central observable state shared across the entire application.
@MainActor
final class AppState: ObservableObject {

    // MARK: - Server connection

    @Published var isConnected: Bool = false

    // MARK: - Navigation

    @Published var selectedSidebarItem: SidebarItem? = .dashboard

    // MARK: - Project context

    @Published var selectedProject: ProjectInfo? = nil

    var activeProjectID: String? { selectedProject?.id }

    // MARK: - Computed

    var isReady: Bool { isConnected }
    
    // MARK: - Initialization
    
    nonisolated init() {}
    
    @MainActor
    func loadDefaultProject() {
        if selectedProject == nil {
            selectedProject = ProjectInfo(id: "default", name: "Default Project", orgId: nil)
        }
    }
}

// MARK: - SidebarItem

/// Represents the navigable sections in the main window sidebar.
enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
    case dashboard  = "Dashboard"
    case sessions   = "Sessions"
    case documents  = "Documents"
    case agents     = "Agents"
    case schema     = "Schema"
    case mcpServers = "MCP Servers"
    case nodes      = "Nodes"
    case objects    = "Objects"
    case providers  = "Providers"
    case permissions = "Permissions"
    case system     = "System"

    var id: String { rawValue }

    var systemIcon: String {
        switch self {
        case .dashboard:  return "chart.bar.fill"
        case .sessions:    return "message"
        case .documents:   return "doc.text.fill"
        case .agents:      return "brain.head.profile"
        case .schema:      return "square.grid.3x3.fill"
        case .mcpServers:  return "cable.connector.horizontal"
        case .nodes:       return "server.rack"
        case .objects:     return "cube"
        case .providers:   return "gearshape.2"
        case .permissions: return "lock.shield"
        case .system:      return "gearshape.2"
        }
    }
}

// MARK: - ProjectInfo

/// Represents a project context for scoping API calls.
struct ProjectInfo: Identifiable, Hashable, Sendable {
    let id: String
    let name: String
    let orgId: String?
}
