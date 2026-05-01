import SwiftUI

/// The main application window with a sidebar + content NavigationSplitView.
struct MainWindowView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var serverConfig: ServerConfiguration

    @Environment(\.openWindow) private var openWindow

    var body: some View {
        if statusMonitor.isLocalAPIReachable {
            mainContent
                .onAppear {
                    if serverConfig.serverURL.isEmpty {
                        openWindow(id: "settings")
                    }
                }
        } else {
            notConnectedView
                .onAppear {
                    if serverConfig.serverURL.isEmpty {
                        openWindow(id: "settings")
                    }
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

    // MARK: - Sidebar

    private var sidebarView: some View {
        List(selection: $appState.selectedSidebarItem) {
            Section("Diane") {
                ForEach(SidebarItem.allCases) { item in
                    Label(item.rawValue, systemImage: item.systemIcon)
                        .tag(item)
                }
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Diane")
    }

    // MARK: - Content column

    @ViewBuilder
    private var contentView: some View {
        switch appState.selectedSidebarItem {
        case .dashboard:
            StatsView()
        case .chat:
            SessionsView()
        case .sessions:
            SessionsView()
        case .agents:
            AgentsView()
        case .schema:
            SchemaView()
        case .mcpServers:
            MCPServersView()
        case .nodes:
            RelayNodesView()
        case .permissions:
            PermissionsView()
        case .system:
            SystemView()
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

// MARK: - Previews

#Preview {
    MainWindowView()
        .environmentObject(AppState())
        .environmentObject(EmergentAPIClient())
        .environmentObject(StatusMonitor())
        .environmentObject(ServerConfiguration())
        .frame(width: 800, height: 600)
}
