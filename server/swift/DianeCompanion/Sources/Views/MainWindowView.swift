import SwiftUI

/// The main application window with a sidebar + content NavigationSplitView.
struct MainWindowView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var serverConfig: ServerConfiguration

    @Environment(\.openWindow) private var openWindow

    var body: some View {
        mainContent
            .onAppear {
                if serverConfig.serverURL.isEmpty {
                    openWindow(id: "settings")
                }
            }
    }

    // MARK: - Main two-column content

    private var mainContent: some View {
        NavigationSplitView {
            sidebarView
                .navigationSplitViewColumnWidth(min: 160, ideal: 180, max: 220)
        } detail: {
            contentView
        }
    }

    // MARK: - Sidebar with sections

    private var sidebarView: some View {
        List(selection: $appState.selectedSidebarItem) {
            Section("Diane") {
                Label("Sessions", systemImage: "message")
                    .tag(SidebarItem.sessions)
                Label("MCP Servers", systemImage: "cable.connector.horizontal")
                    .tag(SidebarItem.mcpServers)
                Label("Relay Nodes", systemImage: "antenna.radiowaves.left.and.right")
                    .tag(SidebarItem.relayNodes)
                Label("Permissions", systemImage: "lock.shield")
                    .tag(SidebarItem.permissions)
            }

            Section("Apple Services") {
                Label("Calendar", systemImage: "calendar")
                    .tag(SidebarItem.calendar)
                Label("Reminders", systemImage: "checklist")
                    .tag(SidebarItem.reminders)
                Label("Contacts", systemImage: "person.crop.circle")
                    .tag(SidebarItem.contacts)
                Label("Mail", systemImage: "envelope")
                    .tag(SidebarItem.mail)
                Label("Messages", systemImage: "message")
                    .tag(SidebarItem.messages)
                Label("Notes", systemImage: "note.text")
                    .tag(SidebarItem.notes)
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Diane")
    }

    // MARK: - Content column

    @ViewBuilder
    private var contentView: some View {
        switch appState.selectedSidebarItem {
        case .sessions:
            SessionsView()
        case .mcpServers:
            MCPServersView()
        case .relayNodes:
            RelayNodesView()
        case .permissions:
            PermissionsView()
        case .calendar:
            CalendarView()
        case .reminders:
            RemindersView()
        case .contacts:
            ContactsView()
        case .mail:
            MailView()
        case .messages:
            MessagesView()
        case .notes:
            NotesView()
        case .none:
            EmptyStateView(
                title: "Select a Section",
                icon: "sidebar.left",
                description: "Choose a section from the sidebar to get started."
            )
        }
    }

    // MARK: - Not Connected State

    private var notConnectedView: some View {
        VStack(spacing: 16) {
            EmptyStateView(
                title: "Not Connected to Server",
                icon: "wifi.slash",
                description: serverConfig.serverURL.isEmpty
                    ? "No server URL configured. Open Settings to enter your server address and API key."
                    : "Cannot reach \(serverConfig.serverURL). Check your connection and server settings.",
                action: statusMonitor.isChecking ? nil : {
                    statusMonitor.checkNow()
                },
                actionLabel: statusMonitor.isChecking ? nil : "Retry Connection"
            )

            if statusMonitor.isChecking {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Checking connection…")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }

            Button("Open Settings…") {
                openWindow(id: "settings")
            }
            .buttonStyle(.bordered)
            .disabled(statusMonitor.isChecking)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
