import Foundation
import Combine

/// Central observable state shared across the entire application.
@MainActor
final class AppState: ObservableObject {

    // MARK: - Server connection

    @Published var isConnected: Bool = false

    // MARK: - Navigation

    @Published var selectedSidebarItem: SidebarItem? = .sessions

    // MARK: - Computed

    var isReady: Bool { isConnected }
}

// MARK: - SidebarItem

/// Represents the navigable sections in the main window sidebar.
enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
    case sessions   = "Sessions"
    case mcpServers = "MCP Servers"
    case permissions = "Permissions"

    var id: String { rawValue }

    var systemIcon: String {
        switch self {
        case .sessions:    return "message"
        case .mcpServers:  return "plug"
        case .permissions: return "lock.shield"
        }
    }
}
