import Foundation
import EventKit

/// Manages macOS Calendar access via EventKit.
@MainActor
final class CalendarManager: ObservableObject {
    private nonisolated(unsafe) let store = EKEventStore()
    
    @Published private(set) var isAuthorized = false
    @Published private(set) var calendars: [EKCalendar] = []
    
    var authorizationStatus: EKAuthorizationStatus {
        EKEventStore.authorizationStatus(for: .event)
    }
    
    func requestPermission() async -> Bool {
        do {
            if #available(macOS 14.0, *) {
                let granted = try await store.requestFullAccessToEvents()
                isAuthorized = granted
                return granted
            } else {
                let granted = try await store.requestAccess(to: .event)
                isAuthorized = granted
                return granted
            }
        } catch {
            logError("Calendar permission request failed: \(error.localizedDescription)", category: "Calendar")
            return false
        }
    }
    
    func refreshCalendars() {
        calendars = store.calendars(for: .event)
    }
    
    func fetchEvents(in dateRange: DateInterval) -> [EKEvent] {
        let predicate = store.predicateForEvents(withStart: dateRange.start, end: dateRange.end, calendars: calendars.isEmpty ? nil : calendars)
        return store.events(matching: predicate)
    }
    
    func createEvent(title: String, startDate: Date, endDate: Date, calendar: EKCalendar? = nil) throws -> EKEvent {
        let event = EKEvent(eventStore: store)
        event.title = title
        event.startDate = startDate
        event.endDate = endDate
        event.calendar = calendar ?? store.defaultCalendarForNewEvents ?? calendars.first
        try store.save(event, span: .thisEvent)
        logInfo("Created event: \(title)", category: "Calendar")
        return event
    }
}
