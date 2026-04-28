import Foundation
import EventKit
import OSLog

/// Manages macOS Reminders access via EventKit.
@MainActor
final class RemindersManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Reminders")
    private let store = EKEventStore()
    
    @Published private(set) var isAuthorized = false
    @Published private(set) var lists: [EKCalendar] = []
    
    var authorizationStatus: EKAuthorizationStatus {
        EKEventStore.authorizationStatus(for: .reminder)
    }
    
    func requestPermission() async -> Bool {
        do {
            if #available(macOS 14.0, *) {
                let granted = try await store.requestFullAccessToReminders()
                isAuthorized = granted
                return granted
            } else {
                let granted = try await store.requestAccess(to: .reminder)
                isAuthorized = granted
                return granted
            }
        } catch {
            logger.error("Reminders permission request failed: \(error.localizedDescription)")
            return false
        }
    }
    
    func refreshLists() {
        lists = store.calendars(for: .reminder)
    }
    
    func fetchReminders(in list: EKCalendar? = nil) throws -> [EKReminder] {
        let predicate = store.predicateForReminders(in: list.map { [$0] } ?? (lists.isEmpty ? nil : lists))
        var reminders: [EKReminder] = []
        let semaphore = DispatchSemaphore(value: 0)
        store.fetchReminders(matching: predicate) { result in
            reminders = result ?? []
            semaphore.signal()
        }
        semaphore.wait()
        return reminders
    }
    
    func createReminder(title: String, list: EKCalendar? = nil, dueDate: Date? = nil, notes: String? = nil) throws -> EKReminder {
        let reminder = EKReminder(eventStore: store)
        reminder.title = title
        reminder.calendar = list ?? store.defaultCalendarForNewReminders() ?? lists.first
        reminder.notes = notes
        if let due = dueDate {
            let alarm = EKAlarm(absoluteDate: due)
            reminder.addAlarm(alarm)
            reminder.dueDateComponents = Calendar.current.dateComponents([.year, .month, .day, .hour, .minute], from: due)
        }
        try store.save(reminder, commit: true)
        logger.info("Created reminder: \(title)")
        return reminder
    }
}
