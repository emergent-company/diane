import Foundation
import EventKit
import Contacts
import AppKit
import OSLog
import UserNotifications

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

/// Unified permission status info for UI display.
struct PermissionInfo: Identifiable, Sendable {
    let type: PermissionType
    var status: PermissionStatus
    var id: String { type.rawValue }
}

/// Central permission manager that checks and requests all macOS permissions.
@MainActor
final class PermissionManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Permissions")
    
    @Published var permissions: [PermissionInfo] = []
    @Published var isRefreshing = false
    
    init() {
        refresh()
    }
    
    func refresh() {
        isRefreshing = true
        permissions = PermissionType.allCases.map { type in
            PermissionInfo(type: type, status: checkStatus(type))
        }
        isRefreshing = false
    }
    
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
        // Accessibility cannot be programmatically requested — user must enable manually
        let options: NSDictionary = [kAXTrustedCheckOptionPrompt.takeRetainedValue() as NSString: true]
        let trusted = AXIsProcessTrustedWithOptions(options)
        refresh()
        return trusted
    }
    
    private func requestCalendar() async -> Bool {
        let store = EKEventStore()
        do {
            if #available(macOS 14.0, *) {
                let granted = try await store.requestFullAccessToEvents()
                refresh()
                return granted
            } else {
                let granted = try await store.requestAccess(to: .event)
                refresh()
                return granted
            }
        } catch {
            logger.error("Calendar permission error: \(error.localizedDescription)")
            return false
        }
    }
    
    private func requestReminders() async -> Bool {
        let store = EKEventStore()
        do {
            if #available(macOS 14.0, *) {
                let granted = try await store.requestFullAccessToReminders()
                refresh()
                return granted
            } else {
                let granted = try await store.requestAccess(to: .reminder)
                refresh()
                return granted
            }
        } catch {
            logger.error("Reminders permission error: \(error.localizedDescription)")
            return false
        }
    }
    
    private func requestContacts() async -> Bool {
        let store = CNContactStore()
        do {
            let granted = try await store.requestAccess(for: .contacts)
            refresh()
            return granted
        } catch {
            logger.error("Contacts permission error: \(error.localizedDescription)")
            return false
        }
    }
    
    private func requestNotifications() async -> Bool {
        do {
            let granted = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
            refresh()
            return granted
        } catch {
            logger.error("Notification permission error: \(error.localizedDescription)")
            return false
        }
    }
    
    private func requestAutomation() async -> Bool {
        // Automation can't be programmatically requested — user must enable in System Settings
        // We just show the settings link
        refresh()
        return false
    }
    
    // MARK: - Status mapping
    
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
