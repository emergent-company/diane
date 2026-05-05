import Foundation
import EventKit
import Contacts
import AppKit
@preconcurrency import UserNotifications
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
        case .granted:      return .active
        case .denied:       return .needsPermission
        case .notDetermined: return .notDetermined
        case .restricted:   return .restricted
        }
    }
}

/// Central permission manager that checks all macOS permissions
/// and manages app-level feature toggles (persisted to UserDefaults).
@MainActor
final class PermissionManager: ObservableObject {

    @Published var permissions: [PermissionInfo] = []
    @Published var isRefreshing = false

    private let defaults = UserDefaults.standard
    private let togglePrefix = "feature_toggle_"

    /// All features enabled by default — users opt out, not in.
    private var toggleDefaults: [PermissionType: Bool] {
        Dictionary(uniqueKeysWithValues: PermissionType.allCases.map { ($0, true) })
    }

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
        // If never set, defaults to true (enabled)
        return defaults.object(forKey: key) as? Bool ?? true
    }

    func setFeatureEnabled(_ enabled: Bool, for type: PermissionType) {
        let key = togglePrefix + type.rawValue
        defaults.set(enabled, forKey: key)
        // Update the published permissions array to trigger UI refresh
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
            // Can't check synchronously; assume not determined
            return .notDetermined
        case .automation:
            // Can't check easily; assume not determined
            return .notDetermined
        }
    }

    func request(_ type: PermissionType) async -> Bool {
        switch type {
        case .accessibility:
            return await requestAccessibility()
        case .calendar:
            return await requestCalendar()
        case .reminders:
            return await requestReminders()
        case .contacts:
            return await requestContacts()
        case .notifications:
            return await requestNotifications()
        case .automation:
            return await requestAutomation()
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

    // MARK: - Private permission request helpers

    private func requestAccessibility() async -> Bool {
        let key = kAXTrustedCheckOptionPrompt.takeUnretainedValue() as NSString
        let options: NSDictionary = [key: true]
        let trusted = AXIsProcessTrustedWithOptions(options)
        await asyncRefresh()
        return trusted
    }

    private func requestCalendar() async -> Bool {
        let store = EKEventStore()
        do {
            if #available(macOS 14.0, *) {
                let granted = try await store.requestFullAccessToEvents()
                await asyncRefresh()
                return granted
            } else {
                let granted = try await store.requestAccess(to: .event)
                await asyncRefresh()
                return granted
            }
        } catch {
            logError(error, category: "Permissions")
            return false
        }
    }

    private func requestReminders() async -> Bool {
        let store = EKEventStore()
        do {
            if #available(macOS 14.0, *) {
                let granted = try await store.requestFullAccessToReminders()
                await asyncRefresh()
                return granted
            } else {
                let granted = try await store.requestAccess(to: .reminder)
                await asyncRefresh()
                return granted
            }
        } catch {
            logError(error, category: "Permissions")
            return false
        }
    }

    private func requestContacts() async -> Bool {
        let store = CNContactStore()
        do {
            let granted = try await store.requestAccess(for: .contacts)
            await asyncRefresh()
            return granted
        } catch {
            logError(error, category: "Permissions")
            return false
        }
    }

    private func requestNotifications() async -> Bool {
        do {
            let granted = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
            await asyncRefresh()
            return granted
        } catch {
            logError(error, category: "Permissions")
            return false
        }
    }

    private func requestAutomation() async -> Bool {
        // Automation can't be programmatically requested — open settings instead.
        // Refresh to show settings link.
        await asyncRefresh()
        return false
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
