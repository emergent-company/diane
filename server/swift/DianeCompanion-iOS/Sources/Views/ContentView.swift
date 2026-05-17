import SwiftUI
import DianeShared

struct ContentView: View {
    @Environment(\.horizontalSizeClass) private var sizeClass

    var body: some View {
        Group {
            if sizeClass == .regular {
                iPadLayout()
            } else {
                iPhoneLayout()
            }
        }
        .sentryView("ContentView")
    }
}

// MARK: - iPhone Layout

struct iPhoneLayout: View {
    var body: some View {
        NavigationStack {
            SessionListView()
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
    @Environment(\.cloudClient) private var cloudClient
    @Binding var selectedSession: DianeSession?

    @StateObject private var archiveStore = ArchivedSessionsStore.shared
    @State private var sessions: [DianeSession] = []
    @State private var isLoading = true
    @State private var error: String?
    @State private var searchText = ""
    @State private var showAgentPicker = false

    private var filteredSessions: [DianeSession] {
        let visible = archiveStore.showArchived
            ? sessions
            : sessions.filter { !archiveStore.isArchived($0.id) }
        if searchText.isEmpty { return visible }
        return visible.filter {
            ($0.title ?? "").localizedCaseInsensitiveContains(searchText)
            || ($0.agentName ?? "").localizedCaseInsensitiveContains(searchText)
        }
    }

    var body: some View {
        VStack(spacing: 0) {
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
                        SessionRow(session: session, isArchived: archiveStore.isArchived(session.id))
                    }
                    .buttonStyle(.plain)
                    .listRowBackground(
                        selectedSession?.id == session.id
                            ? Color.accentColor.opacity(0.15)
                            : Color.clear
                    )
                    .swipeActions(edge: .trailing) {
                        Button {
                            withAnimation(.easeInOut(duration: 0.25)) {
                                archiveStore.toggleArchive(sessionID: session.id)
                            }
                        } label: {
                            Label(archiveStore.isArchived(session.id) ? "Unarchive" : "Archive",
                                  systemImage: "archivebox")
                        }
                        .tint(.gray)
                    }
                }
                .listStyle(.insetGrouped)
            }
        }
        .searchable(text: $searchText, prompt: "Search sessions...")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                HStack(spacing: 4) {
                    Button(action: { archiveStore.toggleShowArchived() }) {
                        Image(systemName: archiveStore.showArchived
                            ? "archivebox.fill"
                            : "archivebox")
                    }
                    Button(action: { showAgentPicker = true }) {
                        Image(systemName: "plus.bubble")
                    }
                }
            }
        }
        .sheet(isPresented: $showAgentPicker) {
            AgentPickerView { agent in
                Task { await createNewSession(agentName: agent.name) }
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

        // 1. Load local cache immediately
        let cached = SessionCache.shared.loadCachedSessions()
        sessions = cached
        isLoading = false

        // 2. Try to fetch remote sessions from MP
        do {
            let remoteItems = try await cloudClient.fetchACPSessions()
            let remoteSessions = remoteItems.map { $0.toDianeSession() }

            var merged = remoteSessions
            for cachedSession in cached {
                if !remoteSessions.contains(where: { $0.id == cachedSession.id }) {
                    merged.append(cachedSession)
                }
            }

            merged.sort { a, b in
                let dateA = DateUtils.parseISO8601(a.createdAt) ?? .distantPast
                let dateB = DateUtils.parseISO8601(b.createdAt) ?? .distantPast
                return dateA > dateB
            }

            await MainActor.run {
                sessions = merged
                SessionCache.shared.cacheSessions(merged)
            }
        } catch {
            if cached.isEmpty {
                await MainActor.run { self.error = error.localizedDescription }
            }
        }
    }

    private func createNewSession(agentName: String) async {
        do {
            let acpID = try await cloudClient.createACPSession(agentName: agentName)
            let session = DianeSession(id: acpID, title: "New Chat", createdAt: DateUtils.formatISO8601(), agentName: agentName)
            sessions.insert(session, at: 0)
            selectedSession = session
            SessionCache.shared.cacheSessions(sessions)
        } catch {
            self.error = error.localizedDescription
        }
    }
}
// MARK: - Preview

#Preview("ContentView") {
    ContentView()
}

#Preview("iPhone Layout") {
    iPhoneLayout()
}

#Preview("iPad Layout") {
    iPadLayout()
}

#Preview("iPad Sidebar") {
    NavigationStack {
        SessionListiPadView(selectedSession: .constant(nil))
    }
}

