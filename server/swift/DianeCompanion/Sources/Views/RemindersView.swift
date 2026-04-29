import SwiftUI
import EventKit

/// Reminders view — shows reminder lists and items via EventKit.
struct RemindersView: View {
    @StateObject private var manager = RemindersManager()

    @State private var reminders: [EKReminder] = []
    @State private var selectedList: EKCalendar? = nil
    @State private var isLoading = false
    @State private var showAddReminder = false
    @State private var newReminderTitle = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            permissionBanner
            Divider()

            if !manager.isAuthorized {
                EmptyStateView(
                    title: "Reminders Access Required",
                    icon: "checklist",
                    description: "Grant reminders permission to view and create reminders.",
                    action: { Task { await manager.requestPermission() } },
                    actionLabel: "Grant Access"
                )
            } else {
                remindersContent
            }
        }
        .navigationTitle("Reminders")
        .task {
            await loadIfAuthorized()
        }
    }

    @ViewBuilder
    private var permissionBanner: some View {
        HStack {
            Image(systemName: "checklist")
                .foregroundStyle(manager.isAuthorized ? .green : .secondary)
            Text(manager.isAuthorized ? "Reminders Access Granted" : "Reminders Access Required")
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

    @MainActor
    private func loadIfAuthorized() async {
        if #available(macOS 14.0, *) {
            guard manager.authorizationStatus == .authorized || manager.authorizationStatus == .fullAccess else { return }
        } else {
            guard manager.authorizationStatus == .authorized else { return }
        }
        manager.refreshLists()
        await loadReminders()
    }

    @ViewBuilder
    private var remindersContent: some View {
        HSplitView {
            listSidebar
                .frame(minWidth: 200)
            remindersPanel
        }
    }

    @ViewBuilder
    private var listSidebar: some View {
        listSidebarContent
            .onChange(of: selectedList) { _ in
                Task { await loadReminders() }
            }
    }

    @ViewBuilder
    private var listSidebarContent: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Lists (\(manager.lists.count))")
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .textCase(.uppercase)
                .padding(12)

            if manager.lists.isEmpty {
                Text("No reminder lists found")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 12)
            } else {
                remindersListPicker
            }
            Spacer()
        }
        .frame(minWidth: 200)
    }

    @ViewBuilder
    private var remindersListPicker: some View {
        List(manager.lists, id: \.self, selection: $selectedList) { list in
            HStack(spacing: 8) {
                Image(systemName: "list.bullet")
                    .font(.caption)
                    .foregroundStyle(Color(cgColor: list.color.cgColor ?? NSColor.labelColor.cgColor))
                Text(list.title)
                    .font(.subheadline)
                    .lineLimit(1)
            }
            .tag(list as EKCalendar?)
        }
        .listStyle(.plain)
    }
    @ViewBuilder
    private var remindersPanel: some View {
        VStack(spacing: 0) {
            quickAddBar
            remindersList
        }
    }

    @ViewBuilder
    private var quickAddBar: some View {
        HStack {
            TextField("New reminder…", text: $newReminderTitle)
                .textFieldStyle(.roundedBorder)
                .font(.caption)
            Button("Add") {
                addReminder()
            }
            .font(.caption)
            .buttonStyle(.borderedProminent)
            .controlSize(.small)
            .disabled(newReminderTitle.trimmingCharacters(in: .whitespaces).isEmpty)
        }
        .padding(8)
    }

    @ViewBuilder
    private var remindersList: some View {
        if reminders.isEmpty {
            EmptyStateView(
                title: "No Reminders",
                icon: "checklist",
                description: selectedList != nil
                    ? "This list is empty. Add a reminder above."
                    : "Select a list or add a reminder above."
            )
        } else {
            List(reminders, id: \.self) { reminder in
                HStack(spacing: 8) {
                    Image(systemName: reminder.isCompleted ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(reminder.isCompleted ? .green : .secondary)
                        .onTapGesture {
                            toggleReminder(reminder)
                        }
                    Text(reminder.title)
                        .font(.subheadline)
                        .strikethrough(reminder.isCompleted)
                        .foregroundStyle(reminder.isCompleted ? .secondary : .primary)
                    Spacer()
                    if let due = reminder.dueDateComponents?.date {
                        Text(due, style: .date)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 2)
            }
            .listStyle(.plain)
        }
    }

    @MainActor
    private func loadReminders() async {
        isLoading = true
        do {
            reminders = try manager.fetchReminders(in: selectedList)
        } catch {
            reminders = []
        }
        isLoading = false
    }

    private func addReminder() {
        let title = newReminderTitle.trimmingCharacters(in: .whitespaces)
        guard !title.isEmpty else { return }
        do {
            _ = try manager.createReminder(title: title, list: selectedList)
            newReminderTitle = ""
            Task { await loadReminders() }
        } catch {
            print("Failed to create reminder: \(error)")
        }
    }

    private func toggleReminder(_ reminder: EKReminder) {
        reminder.isCompleted = !reminder.isCompleted
        do {
            try manager.saveReminder(reminder)
            Task { await loadReminders() }
        } catch {
            print("Failed to toggle reminder: \(error)")
        }
    }
}
