import SwiftUI

/// The main application window with a three-column NavigationSplitView.
///
/// Layout:
///   [Sidebar] | [Content / List] | [Detail]
///
/// The sidebar groups navigation items by section. The project dropdown
/// lives in the toolbar and scopes all project-level views.
struct MainWindowView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var serverConfig: ServerConfiguration

    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Group {
            if statusMonitor.connectionState == .connected {
                mainContent
            } else if statusMonitor.connectionState == .unknown && statusMonitor.isChecking {
                // First-launch: check is in flight — show connecting spinner
                connectingView
            } else {
                // Check completed and failed, or no URL configured
                notConnectedView
            }
        }
        .toolbar {
            // Task 5.3: Global project dropdown in the window toolbar
            ToolbarItem(placement: .automatic) {
                projectPicker
            }
        }
        .task {
            await loadProjectsIfNeeded()
        }
        .onChange(of: statusMonitor.connectionState) { newState in
            if newState == .connected {
                Task { await loadProjectsIfNeeded() }
            }
        }
        .onAppear {
            // Auto-open settings when no server URL is configured
            if serverConfig.serverURL.isEmpty {
                openWindow(id: "settings")
            }
        }
    }

    // MARK: - Main three-column content (task 5.1)

    /// Whether the currently selected view is a simple full-page view
    /// that should not show a detail column.
    /// Views that manage their own internal NavigationSplitView are also included here.
    private var isSimplePage: Bool {
        switch appState.selectedSidebarItem {
        case .status, .query, .workers, .accountStatus, .profile,
             .agents, .mcpServers, .providers, .traces,
             .sessions, .permissions,
             .documents, .objects, .none:
            return true
        }
    }

    private var mainContent: some View {
        Group {
            if isSimplePage {
                // Two-column layout: no empty detail pane on the right
                NavigationSplitView {
                    sidebarView
                        .navigationSplitViewColumnWidth(min: 160, ideal: 180, max: 220)
                } detail: {
                    contentView
                }
            } else {
                // Three-column layout for views with their own internal detail panel
                NavigationSplitView {
                    sidebarView
                        .navigationSplitViewColumnWidth(min: 160, ideal: 180, max: 220)
                } content: {
                    contentView
                        .navigationSplitViewColumnWidth(min: 300, ideal: 420)
                } detail: {
                    detailView
                }
            }
        }
    }

    // MARK: - Sidebar (task 5.2)

    private var sidebarView: some View {
        List(selection: $appState.selectedSidebarItem) {
            // Project section
            Section("Project") {
                ForEach(SidebarSection.project.items) { item in
                    sidebarRow(item)
                }
            }

            // Account section
            Section("Account") {
                ForEach(SidebarSection.account.items) { item in
                    sidebarRow(item)
                }
            }

            // Configuration section
            Section("Configuration") {
                ForEach(SidebarSection.configuration.items) { item in
                    sidebarRow(item)
                }
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Diane Companion")
    }

    private func sidebarRow(_ item: SidebarItem) -> some View {
        Label(item.rawValue, systemImage: item.systemIcon)
            .tag(item)
    }

    // MARK: - Content column (center)

    @ViewBuilder
    private var contentView: some View {
        switch appState.selectedSidebarItem {
        case .traces:        TracesView()
        case .sessions:      SessionsView()
        case .query:         QueryView()
        case .status:        ProjectStatusView()
        case .workers:       WorkersView()
        case .objects:       ObjectsBrowserView()
        case .documents:     DocumentBrowserView()
        case .accountStatus: AccountStatusView()
        case .profile:       ProfileView()
        case .agents:        AgentsView()
        case .mcpServers:    MCPServersView()
        case .providers:     ProvidersView()
        case .permissions:   PermissionsView()
        case .none:
            EmptyStateView(
                title: "Select a Section",
                icon: "sidebar.left",
                description: "Choose a section from the sidebar to get started."
            )
        }
    }

    // MARK: - Detail column (right, three-column layout only)

    @ViewBuilder
    private var detailView: some View {
        // Views that manage their own internal detail panel (e.g. DocumentBrowserView)
        // use this as a fallback; they render their own detail content inside HSplitView.
        EmptyStateView(
            title: "No Selection",
            icon: "rectangle.3.group",
            description: "Select an item to view details."
        )
    }

    // MARK: - Project Picker (task 5.3)

    @ViewBuilder
    private var projectPicker: some View {
        if appState.isLoadingProjects {
            ProgressView()
                .controlSize(.small)
                .padding(.trailing, 4)
        } else if appState.projects.isEmpty {
            Text("No projects")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        } else {
            Picker("Project", selection: $appState.selectedProject) {
                Text("Select project…")
                    .tag(Optional<Project>.none)
                ForEach(appState.projects) { project in
                    Text(project.name).tag(Optional(project))
                }
            }
            .pickerStyle(.menu)
            .labelsHidden()
            .frame(minWidth: 140)
        }
    }

    // MARK: - Connecting State

    private var connectingView: some View {
        VStack(spacing: 12) {
            ProgressView()
                .controlSize(.large)
            Text("Connecting to server…")
                .font(.headline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Not Connected State (task 5.4)

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

    // MARK: - Helpers

    @MainActor
    private func loadProjectsIfNeeded() async {
        guard appState.projects.isEmpty,
              statusMonitor.connectionState == .connected else { return }
        appState.isLoadingProjects = true
        appState.projectLoadError = nil
        do {
            appState.projects = try await apiClient.fetchProjects()
            // Auto-select first project if none selected
            if appState.selectedProject == nil {
                appState.selectedProject = appState.projects.first
            }
        } catch {
            appState.projectLoadError = error.localizedDescription
        }
        appState.isLoadingProjects = false
    }
}

// MARK: - SidebarSection helper

private extension SidebarSection {
    var items: [SidebarItem] {
        SidebarItem.allCases.filter { $0.sectionGroup == self }
    }
}
