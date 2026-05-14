import SwiftUI
import DianeShared

enum AppTab: String, CaseIterable {
    case chats, agents, status, settings

    var title: String {
        switch self {
        case .chats: return "Chats"
        case .agents: return "Agents"
        case .status: return "Status"
        case .settings: return "Settings"
        }
    }

    var icon: String {
        switch self {
        case .chats: return "message.fill"
        case .agents: return "brain.head.profile"
        case .status: return "antenna.radiowaves.left.and.right"
        case .settings: return "gearshape.fill"
        }
    }
}

struct ContentView: View {
    @Environment(\.horizontalSizeClass) private var sizeClass

    var body: some View {
        if sizeClass == .regular {
            iPadLayout()
        } else {
            iPhoneLayout()
        }
    }
}

// MARK: - iPhone Layout

struct iPhoneLayout: View {
    @State private var selectedTab: AppTab = .chats

    var body: some View {
        TabView(selection: $selectedTab) {
            NavigationStack {
                SessionListView()
            }
            .tabItem { Label(AppTab.chats.title, systemImage: AppTab.chats.icon) }
            .tag(AppTab.chats)

            NavigationStack {
                AgentsListView()
            }
            .tabItem { Label(AppTab.agents.title, systemImage: AppTab.agents.icon) }
            .tag(AppTab.agents)

            NavigationStack {
                SystemView()
            }
            .tabItem { Label(AppTab.status.title, systemImage: AppTab.status.icon) }
            .tag(AppTab.status)

            NavigationStack {
                SettingsView()
            }
            .tabItem { Label(AppTab.settings.title, systemImage: AppTab.settings.icon) }
            .tag(AppTab.settings)
        }
    }
}

// MARK: - iPad Layout

struct iPadLayout: View {
    @State private var selectedSession: DianeSession?
    @State private var showSettings = false

    var body: some View {
        NavigationSplitView {
            SessionListiPadView(selectedSession: $selectedSession)
                .navigationTitle("Chats")
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        Button(action: { showSettings = true }) {
                            Image(systemName: "gearshape")
                        }
                    }
                }
                .sheet(isPresented: $showSettings) {
                    NavigationStack { SettingsView() }
                }
        } detail: {
            if let session = selectedSession {
                ChatView(session: session)
            } else {
                ContentUnavailableView(
                    "Select a Conversation",
                    systemImage: "bubble.left.and.bubble.right",
                    description: Text("Choose a session from the sidebar to view messages.")
                )
            }
        }
        .navigationSplitViewStyle(.balanced)
    }
}

// MARK: - Session List for iPad Sidebar

struct SessionListiPadView: View {
    @Environment(\.apiClient) private var apiClient
    @Binding var selectedSession: DianeSession?

    @State private var sessions: [DianeSession] = []
    @State private var isLoading = true
    @State private var error: String?
    @State private var isOffline = false
    @State private var searchText = ""

    var filteredSessions: [DianeSession] {
        if searchText.isEmpty { return sessions }
        return sessions.filter {
            ($0.title ?? "").localizedCaseInsensitiveContains(searchText)
            || ($0.agentName ?? "").localizedCaseInsensitiveContains(searchText)
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            if isOffline { OfflineBanner() }

            if isLoading && sessions.isEmpty {
                List {
                    ForEach(0..<5, id: \.self) { _ in
                        HStack {
                            Circle().fill(Color.gray.opacity(0.3)).frame(width: 10, height: 10)
                            VStack(alignment: .leading) {
                                Text("Loading...").font(.body)
                                Text("agent...").font(.caption).foregroundColor(.secondary)
                            }
                        }
                        .redacted(reason: .placeholder)
                    }
                }
                .listStyle(.insetGrouped)
            } else if let err = error, sessions.isEmpty {
                ContentUnavailableView(
                    "Could Not Load",
                    systemImage: "exclamationmark.triangle",
                    description: Text(err)
                )
            } else if sessions.isEmpty {
                ContentUnavailableView(
                    "No Conversations",
                    systemImage: "bubble.left.and.bubble.right",
                    description: Text("Start a new chat to begin interacting with your Diane agents.")
                )
            } else {
                List(filteredSessions) { session in
                    Button(action: { selectedSession = session }) {
                        SessionRow(session: session)
                    }
                    .buttonStyle(.plain)
                    .listRowBackground(
                        selectedSession?.id == session.id
                            ? Color.accentColor.opacity(0.15)
                            : Color.clear
                    )
                }
                .listStyle(.insetGrouped)
            }
        }
        .searchable(text: $searchText, prompt: "Search sessions...")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(action: { Task { await createNewSession() } }) {
                    Image(systemName: "plus.bubble")
                }
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .onReceive(NotificationCenter.default.publisher(for: NetworkMonitor.connectivityChanged)) { notification in
            if let connected = notification.userInfo?["isConnected"] as? Bool, connected {
                Task { await load() }
            }
        }
    }

    private func load() async {
        isLoading = true
        error = nil
        isOffline = false
        do {
            sessions = try await apiClient.fetchSessions()
            SessionCache.shared.cacheSessions(sessions)
        } catch {
            let cached = SessionCache.shared.loadCachedSessions()
            if cached.isEmpty {
                self.error = error.localizedDescription
            } else {
                sessions = cached
                isOffline = true
            }
        }
        isLoading = false
    }

    private func createNewSession() async {
        do {
            let session = try await apiClient.createSession()
            sessions.insert(session, at: 0)
            selectedSession = session
        } catch {
            self.error = error.localizedDescription
        }
    }
}

// MARK: - Placeholder View (reserved for MCPServersView access)