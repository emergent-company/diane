import Foundation
import EventKit
import Contacts
import AppKit
import UserNotifications
@preconcurrency import ApplicationServices

/// Types of macOS permissions the app needs to manage.
enum PermissionType: String, CaseIterable, Identifiable, Sendable {
    case accessibility
    case automation
    case notifications
    case calendar
    case reminders
    case contacts

    var id: String { rawValue }

    var settingsURL: URL? {
        switch self {
        case .accessibility:
            return URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")
        case .automation:
            return URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation")
        case .notifications:
            return URL(string: "x-apple.systempreferences:com.apple.preference.notifications")
        default:
            return URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy")
        }
    }

    var setupGuide: String {
        switch self {
        case .accessibility:
            return "1. Open System Settings → Privacy & Security → Accessibility\n2. Find \"Diane\" in the app list\n3. Toggle the switch to enable"
        case .automation:
            return "1. Open System Settings → Privacy & Security → Automation\n2. Find \"Diane\" in the app list\n3. Toggle the switch to allow control of other apps"
        case .notifications:
            return "1. Open System Settings → Notifications\n2. Find \"Diane\" in the app list\n3. Enable \"Allow Notifications\""
        case .calendar:
            return "1. Open System Settings → Privacy & Security → Calendar\n2. Find \"Diane\" in the app list\n3. Toggle the switch to enable"
        case .reminders:
            return "1. Open System Settings → Privacy & Security → Reminders\n2. Find \"Diane\" in the app list\n3. Toggle the switch to enable"
        case .contacts:
            return "1. Open System Settings → Privacy & Security → Contacts\n2. Find \"Diane\" in the app list\n3. Toggle the switch to enable"
        }
    }

    var displayName: String {
        switch self {
        case .accessibility: return "Accessibility"
        case .automation: return "Automation"
        case .notifications: return "Notifications"
        case .calendar: return "Calendar"
        case .reminders: return "Reminders"
        case .contacts: return "Contacts"
        }
    }

    var description: String {
        switch self {
        case .accessibility: return "Required for controlling other apps and UI automation"
        case .automation: return "Required for AppleScript automation of other apps"
        case .notifications: return "Required for local notifications and alerts"
        case .calendar: return "Required for reading and creating calendar events"
        case .reminders: return "Required for reading and creating reminders"
        case .contacts: return "Required for searching and reading contacts"
        }
    }

    var systemIcon: String {
        switch self {
        case .accessibility: return "figure.arm.seatbelt"
        case .automation: return "gearshape.2"
        case .notifications: return "bell.badge"
        case .calendar: return "calendar"
        case .reminders: return "checklist"
        case .contacts: return "person.crop.circle"
        }
    }
}

enum PermissionStatus: Sendable {
    case granted
    case denied
    case notDetermined
    case restricted

    var isGranted: Bool {
        if case .granted = self { return true }
        return false
    }
}

/// Combined feature-level status — merges OS permission grant with app-level toggle.
enum FeatureStatus: Sendable {
    /// Toggle ON + macOS granted → ready to use
    case active
    /// Toggle OFF → feature is intentionally disabled by user
    case disabled
    /// Toggle ON but macOS permission denied → needs user action
    case needsPermission
    /// Toggle ON but macOS hasn't prompted yet
    case notDetermined
    /// Toggle ON but macOS permission is restricted (parental controls, MDM, etc.)
    case restricted

    var isUsable: Bool {
        if case .active = self { return true }
        return false
    }
}

/// Unified permission status info for UI display.
struct PermissionInfo: Identifiable, Sendable {
    let type: PermissionType
    var status: PermissionStatus
    var featureEnabled: Bool
    var id: String { type.rawValue }

    var featureStatus: FeatureStatus {
        guard featureEnabled else { return .disabled }
        switch status {
        case .granted:       return .active
        case .denied:        return .needsPermission
        case .notDetermined: return .notDetermined
        case .restricted:    return .restricted
        }
    }
}

/// Central permission manager that checks all macOS permissions
/// and manages app-level feature toggles (persisted to UserDefaults).
///
/// macOS permissions work implicitly — the system prompts when an API is first
/// accessed, not via programmatic "request" calls. This view only shows status
/// and guides you to System Settings. The actual permission prompt happens
/// when you use an Apple tool (apple_list_events, apple_send_imessage, etc.).
@MainActor
final class PermissionManager: ObservableObject {

    @Published var permissions: [PermissionInfo] = []
    @Published var isRefreshing = false

    private let defaults = UserDefaults.standard
    private let togglePrefix = "feature_toggle_"

    // MARK: - Init / Refresh

    init() {
        refresh()
        // Kick off an async refresh to get notification status post-init
        Task { await asyncRefresh() }
    }

    /// Synchronous refresh — fast, uses only sync-checkable permissions.
    func refresh() {
        isRefreshing = true
        permissions = PermissionType.allCases.map { type in
            PermissionInfo(
                type: type,
                status: checkStatus(type),
                featureEnabled: isFeatureEnabled(type)
            )
        }
        isRefreshing = false
    }

    /// Async refresh — calls sync refresh first, then checks
    /// permissions that require async queries (notifications).
    func asyncRefresh() async {
        refresh()
        // Update notification status asynchronously
        let notifStatus = await checkNotificationStatus()
        if let idx = permissions.firstIndex(where: { $0.type == .notifications }) {
            permissions[idx] = PermissionInfo(
                type: .notifications,
                status: notifStatus,
                featureEnabled: permissions[idx].featureEnabled
            )
        }
    }

    // MARK: - Feature Toggles

    func isFeatureEnabled(_ type: PermissionType) -> Bool {
        let key = togglePrefix + type.rawValue
        return defaults.object(forKey: key) as? Bool ?? true
    }

    func setFeatureEnabled(_ enabled: Bool, for type: PermissionType) {
        let key = togglePrefix + type.rawValue
        defaults.set(enabled, forKey: key)
        if let idx = permissions.firstIndex(where: { $0.type == type }) {
            permissions[idx] = PermissionInfo(
                type: type,
                status: permissions[idx].status,
                featureEnabled: enabled
            )
        }
    }

    // MARK: - macOS Permission Status

    func checkStatus(_ type: PermissionType) -> PermissionStatus {
        switch type {
        case .accessibility:
            return AXIsProcessTrusted() ? .granted : .denied
        case .calendar:
            return mapEKStatus(EKEventStore.authorizationStatus(for: .event))
        case .reminders:
            return mapEKStatus(EKEventStore.authorizationStatus(for: .reminder))
        case .contacts:
            return mapCNStatus(CNContactStore.authorizationStatus(for: .contacts))
        case .notifications:
            return .notDetermined  // checked async via checkNotificationStatus()
        case .automation:
            return .notDetermined  // no programmatic API exists
        }
    }

    func openSystemSettings(_ type: PermissionType) {
        switch type {
        case .accessibility:
            guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") else { return }
            NSWorkspace.shared.open(url)
        case .automation:
            guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation") else { return }
            NSWorkspace.shared.open(url)
        case .notifications:
            guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.notifications") else { return }
            NSWorkspace.shared.open(url)
        default:
            guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy") else { return }
            NSWorkspace.shared.open(url)
        }
    }

    // MARK: - Status mapping

    /// Asynchronously check notification authorization status via UNNotificationSettings.
    private func checkNotificationStatus() async -> PermissionStatus {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        switch settings.authorizationStatus {
        case .authorized, .provisional, .ephemeral:
            return .granted
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        @unknown default:
            return .notDetermined
        }
    }

    private func mapEKStatus(_ status: EKAuthorizationStatus) -> PermissionStatus {
        switch status {
        case .authorized: return .granted
        case .denied: return .denied
        case .notDetermined: return .notDetermined
        case .restricted: return .restricted
        case .fullAccess: return .granted
        case .writeOnly: return .granted
        @unknown default: return .notDetermined
        }
    }

    private func mapCNStatus(_ status: CNAuthorizationStatus) -> PermissionStatus {
        switch status {
        case .authorized: return .granted
        case .denied: return .denied
        case .notDetermined: return .notDetermined
        case .restricted: return .restricted
        @unknown default: return .notDetermined
        }
    }
}
