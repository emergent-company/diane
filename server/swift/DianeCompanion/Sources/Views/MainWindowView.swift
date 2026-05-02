import SwiftUI

/// The main application window with a sidebar + content NavigationSplitView.
/// When the server is not configured, shows the inline onboarding flow instead.
struct MainWindowView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var serverConfig: ServerConfiguration

    var body: some View {
        if serverConfig.isConfigured {
            if statusMonitor.isLocalAPIReachable {
                mainContent
            } else {
                notConnectedView
            }
        } else {
            OnboardingView()
                .environmentObject(statusMonitor)
                .environmentObject(serverConfig)
                .environmentObject(apiClient)
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
        case .documents:
            DocumentsView()
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
            Image(systemName: "wifi.slash")
                .font(.system(size: 48))
                .foregroundStyle(.secondary)

            Text("Not Connected to Server")
                .font(.title2)
                .fontWeight(.semibold)

            Text("Cannot reach \(serverConfig.serverURL). Check your connection and server settings.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)

            if statusMonitor.isChecking {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Checking connection…")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }

            HStack(spacing: 12) {
                Button(action: { statusMonitor.checkNow() }) {
                    Label("Retry", systemImage: "arrow.clockwise")
                }
                .buttonStyle(.bordered)
                .disabled(statusMonitor.isChecking)

                Button(action: { resetToOnboarding() }) {
                    Label("Change Server", systemImage: "gearshape")
                }
                .buttonStyle(.bordered)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Actions

    private func resetToOnboarding() {
        serverConfig.serverURL = ""
        serverConfig.apiKey = ""
        // Don't clear projectID — may be useful to keep
        statusMonitor.configure(from: serverConfig)
        apiClient.configure(serverURL: "", apiKey: "")
    }
}

// MARK: - Previews

#Preview("Configured + Connected") {
    let config = ServerConfiguration()
    config.serverURL = "https://memory.example.com"
    config.apiKey = "emt_xxx"
    let monitor = StatusMonitor.forPreviews(connectionState: .connected, isLocalReachable: true)

    MainWindowView()
        .environmentObject(AppState())
        .environmentObject(EmergentAPIClient())
        .environmentObject(monitor)
        .environmentObject(config)
        .frame(width: 800, height: 600)
}

#Preview("Configured + Disconnected") {
    let config = ServerConfiguration()
    config.serverURL = "https://memory.example.com"
    config.apiKey = "emt_xxx"
    let monitor = StatusMonitor.forPreviews(connectionState: .disconnected, isLocalReachable: false)

    MainWindowView()
        .environmentObject(AppState())
        .environmentObject(EmergentAPIClient())
        .environmentObject(monitor)
        .environmentObject(config)
        .frame(width: 800, height: 600)
}

#Preview("Not Configured") {
    MainWindowView()
        .environmentObject(AppState())
        .environmentObject(EmergentAPIClient())
        .environmentObject(StatusMonitor())
        .environmentObject(ServerConfiguration())
        .frame(width: 800, height: 600)
}
