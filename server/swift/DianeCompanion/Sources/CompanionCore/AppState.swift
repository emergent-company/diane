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
    case relayNodes = "Relay Nodes"
    case permissions = "Permissions"
    case calendar   = "Calendar"
    case reminders  = "Reminders"
    case contacts   = "Contacts"
    case mail       = "Mail"
    case messages   = "Messages"
    case notes      = "Notes"

    var id: String { rawValue }

    var systemIcon: String {
        switch self {
        case .sessions:    return "message"
        case .mcpServers:  return "cable.connector.horizontal"
        case .relayNodes:  return "antenna.radiowaves.left.and.right"
        case .permissions: return "lock.shield"
        case .calendar:    return "calendar"
        case .reminders:   return "checklist"
        case .contacts:    return "person.crop.circle"
        case .mail:        return "envelope"
        case .messages:    return "message"
        case .notes:       return "note.text"
        }
    }
}
