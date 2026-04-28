import Foundation
import Combine

/// Central observable state shared across the entire application.
@MainActor
final class AppState: ObservableObject {

    // MARK: - Server connection

    @Published var isConnected: Bool = false

    // MARK: - Navigation

    @Published var selectedSidebarItem: SidebarItem? = .dashboard

    // MARK: - Computed

    var isReady: Bool { isConnected }
}

// MARK: - SidebarItem

/// Represents the navigable sections in the main window sidebar.
enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
    case dashboard  = "Dashboard"
    case sessions   = "Sessions"
    case mcpServers = "MCP Servers"
    case permissions = "Permissions"

    var id: String { rawValue }

    var systemIcon: String {
        switch self {
        case .dashboard:  return "chart.bar.fill"
        case .sessions:    return "message"
        case .mcpServers:  return "cable.connector.horizontal"
        case .permissions: return "lock.shield"
        }
    }
}
