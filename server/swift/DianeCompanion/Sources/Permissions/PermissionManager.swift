import SwiftUI
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
        case .accessibility: return "Required for UI automation and controlling other apps"
        case .automation: return "Required for AppleScript automation (Mail, Messages, Notes)"
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

    /// Step-by-step guide text for manual permission setup.
    var setupGuide: String {
        switch self {
        case .accessibility:
            return """
            1. Open System Settings → Privacy & Security → Accessibility
            2. Click the lock icon to make changes
            3. Click the + button below the app list
            4. Navigate to Applications and select Diane.app
            5. Ensure the checkbox next to Diane is checked
            """
        case .automation:
            return """
            1. Open System Settings → Privacy & Security → Automation
            2. Click the lock icon to make changes
            3. Find Diane in the list
            4. Check the boxes for the apps you want to automate (Mail, Messages, Notes, System Events)
            """
        case .notifications:
            return """
            1. Open System Settings → Notifications
            2. Find Diane in the app list
            3. Enable "Allow Notifications"
            """
        default:
            return "Open System Settings → Privacy & Security and grant access for Diane."
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
/// Auto-refreshes status via a timer.
@MainActor
final class PermissionManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Permissions")

    @Published var permissions: [PermissionInfo] = []
    @Published var isRefreshing = false

    private var refreshTimer: Timer?

    init() {
        refresh()
        startAutoRefresh()
    }

    deinit {
        refreshTimer?.invalidate()
    }

    /// Start a timer that auto-refreshes permission status every 15 seconds.
    private func startAutoRefresh() {
        refreshTimer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.refresh()
            }
        }
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
            return .notDetermined
        case .automation:
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
        if let url = type.settingsURL {
            NSWorkspace.shared.open(url)
        }
    }

    // MARK: - Private permission request helpers

    private func requestAccessibility() async -> Bool {
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
        // Try to trigger automation TCC prompt by running a benign AppleScript
        // that targets System Events. This prompts macOS to ask for automation permission.
        let script = """
        tell application "System Events"
            get name of every process
        end tell
        """
        do {
            _ = try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
                DispatchQueue.global(qos: .userInitiated).async {
                    let process = Process()
                    process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
                    process.arguments = ["-e", script]
                    let outputPipe = Pipe()
                    let errorPipe = Pipe()
                    process.standardOutput = outputPipe
                    process.standardError = errorPipe
                    do {
                        try process.run()
                        process.waitUntilExit()
                        let output = String(data: outputPipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
                        if process.terminationStatus == 0 {
                            continuation.resume(returning: output)
                        } else {
                            let error = String(data: errorPipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
                            continuation.resume(throwing: NSError(domain: "Automation", code: Int(process.terminationStatus), userInfo: [NSLocalizedDescriptionKey: error]))
                        }
                    } catch {
                        continuation.resume(throwing: error)
                    }
                }
            }
            refresh()
            return true
        } catch {
            logger.warning("Automation permission not yet granted: \(error.localizedDescription)")
            refresh()
            return false
        }
    }

    // MARK: - Status mapping

    private func mapEKStatus(_ status: EKAuthorizationStatus) -> PermissionStatus {
        switch status {
        case .authorized, .fullAccess: return .granted
        case .denied: return .denied
        case .notDetermined: return .notDetermined
        case .restricted: return .restricted
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
