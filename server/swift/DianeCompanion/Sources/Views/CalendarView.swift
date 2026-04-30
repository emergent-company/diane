import SwiftUI
import EventKit

/// Calendar view — shows calendars and upcoming events via EventKit.
struct CalendarView: View {
    @StateObject private var manager = CalendarManager()

    @State private var events: [EKEvent] = []
    @State private var isLoading = false
    @State private var error: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Permission status
            permissionBanner

            Divider()

            if !manager.isAuthorized {
                EmptyStateView(
                    title: "Calendar Access Required",
                    icon: "calendar",
                    description: "Grant calendar permission to view and create events.",
                    action: { Task { await manager.requestPermission() } },
                    actionLabel: "Grant Access"
                )
            } else {
                calendarContent
            }
        }
        .navigationTitle("Calendar")
        .task {
            if #available(macOS 14.0, *) {
                if manager.authorizationStatus == .fullAccess {
                    manager.refreshCalendars()
                    await loadEvents()
                }
            } else {
                if manager.authorizationStatus == .authorized {
                    manager.refreshCalendars()
                    await loadEvents()
                }
            }
        }
    }

    @ViewBuilder
    private var permissionBanner: some View {
        HStack {
            Image(systemName: "calendar")
                .foregroundStyle(manager.isAuthorized ? .green : .secondary)
            Text(manager.isAuthorized ? "Calendar Access Granted" : "Calendar Access Required")
                .font(.caption)
            Spacer()
            if !manager.isAuthorized {
                Button("Authorize") {
                    Task { await manager.requestPermission() }
                }
                .font(.caption)
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
            }
        }
        .padding(8)
        .background(manager.isAuthorized ? Color.green.opacity(0.05) : Color.orange.opacity(0.05))
    }

    @ViewBuilder
    private var calendarContent: some View {
        HSplitView {
            // Calendars list
            VStack(alignment: .leading, spacing: 0) {
                Text("Calendars (\(manager.calendars.count))")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                    .textCase(.uppercase)
                    .padding(12)

                if manager.calendars.isEmpty {
                    Text("No calendars found")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 12)
                } else {
                    List(manager.calendars, id: \.self) { calendar in
                        HStack(spacing: 8) {
                            Circle()
                                .fill(Color(cgColor: calendar.color.cgColor))
                                .frame(width: 8, height: 8)
                            Text(calendar.title)
                                .font(.subheadline)
                                .lineLimit(1)
                        }
                    }
                    .listStyle(.plain)
                }

                Spacer()

                Divider()
                HStack {
                    Text("\(events.count) upcoming events")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Refresh") { Task { await loadEvents() } }
                        .font(.caption)
                        .buttonStyle(.borderless)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
            }
            .frame(minWidth: 220)

            // Events list
            if events.isEmpty {
                EmptyStateView(
                    title: "No Upcoming Events",
                    icon: "calendar.badge.clock",
                    description: "No events in the next 7 days."
                )
                .frame(minWidth: 220)
            } else {
                List(events, id: \.eventIdentifier) { event in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(event.title ?? "Untitled")
                            .font(.subheadline)
                            .fontWeight(.medium)
                        HStack(spacing: 6) {
                            if let calendar = event.calendar {
                                Circle()
                                    .fill(Color(cgColor: calendar.color.cgColor))
                                    .frame(width: 6, height: 6)
                            }
                            Text(formatEventDate(event))
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                            if let loc = event.location, !loc.isEmpty {
                                Text(loc)
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            }
                        }
                    }
                    .padding(.vertical, 2)
                }
                .listStyle(.plain)
            }
        }
    }

    @MainActor
    private func loadEvents() async {
        isLoading = true
        let start = Date()
        let end = Calendar.current.date(byAdding: .day, value: 7, to: start) ?? start
        events = manager.fetchEvents(in: DateInterval(start: start, end: end))
        isLoading = false
    }

    private func formatEventDate(_ event: EKEvent) -> String {
        let df = DateFormatter()
        df.dateStyle = .short
        df.timeStyle = .short
        return df.string(from: event.startDate)
    }
}
