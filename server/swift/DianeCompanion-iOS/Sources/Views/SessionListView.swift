import SwiftUI
import DianeShared

// MARK: - Placeholder Sessions for Loading State

private let placeholderSessions: [DianeSession] = (0..<5).map { i in
    DianeSession(
        id: "placeholder-\(i)",
        title: "Loading Session...",
        status: "active",
        messageCount: 0,
        createdAt: DateUtils.formatISO8601(),
        agentName: "diane-default"
    )
}

// MARK: - SessionRow

struct SessionRow: View {
    let session: DianeSession
    let isArchived: Bool

    var body: some View {
        HStack(spacing: DesignTokens.spacingMD) {
            // Status dot or archive icon
            if isArchived {
                Image(systemName: "archivebox")
                    .font(.caption)
                    .foregroundColor(.secondary)
            } else {
                Circle()
                    .fill(StatusColors.statusColor(session.status))
                    .frame(width: 10, height: 10)
            }

            VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
                // Title (1 line truncated)
                Text(ViewFormatting.sessionTitle(session.title))
                    .font(.body)
                    .lineLimit(DesignTokens.lineLimitSM)
                    .truncationMode(.tail)

                // Agent name + message count subtitle
                HStack(spacing: DesignTokens.spacingXS) {
                    Text(ViewFormatting.agentName(session.agentName))
                        .font(.caption)
                        .foregroundColor(.secondary)

                    if let count = session.messageCount, count > 0 {
                        Text("·")
                            .font(.caption)
                            .foregroundColor(.secondary)
                        Text(ViewFormatting.messageCount(count))
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                }
            }

            Spacer()

            // Relative timestamp (last activity)
            Text(DateUtils.formatRelative(session.updatedAt ?? session.createdAt))
                .font(.caption)
                .foregroundColor(.secondary)
        }
        .padding(.vertical, DesignTokens.spacingXS)
        .accessibilityElement(children: .combine)
    }
}

// MARK: - Empty State View

private struct EmptyStateView: View {
    let onCreate: () -> Void

    var body: some View {
        VStack(spacing: DesignTokens.spacingLG) {
            Spacer()

            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: DesignTokens.fontSizeXXXL * 2))
                .foregroundColor(.secondary.opacity(DesignTokens.opacitySubtle))

            Text("No Conversations Yet")
                .font(.title2)
                .fontWeight(.semibold)
                .foregroundColor(.primary)

            Text("Start a new chat to begin interacting\nwith your Diane agents.")
                .font(.subheadline)
                .foregroundColor(.secondary)
                .multilineTextAlignment(.center)

            Button(action: onCreate) {
                Label("New Chat", systemImage: "plus.bubble")
                    .font(.headline)
                    .padding(.horizontal, DesignTokens.spacingXL)
                    .padding(.vertical, DesignTokens.spacingSM)
                    .background(Color.accentColor)
                    .foregroundColor(.white)
                    .clipShape(Capsule())
            }
            .buttonStyle(.plain)

            Spacer()
        }
        .frame(maxWidth: .infinity)
        .padding()
    }
}

// MARK: - Error State View

private struct ErrorStateView: View {
    let message: String
    let onRetry: () -> Void

    var body: some View {
        VStack(spacing: DesignTokens.spacingMD) {
            Image(systemName: "exclamationmark.triangle")
                .font(.largeTitle)
                .foregroundColor(.orange)

            Text("Something went wrong")
                .font(.headline)

            Text(message)
                .font(.subheadline)
                .foregroundColor(.secondary)
                .multilineTextAlignment(.center)

            Button("Retry", action: onRetry)
                .buttonStyle(.bordered)
        }
        .padding()
    }
}

// MARK: - Agent Picker Sheet

struct AgentPickerView: View {
    @Environment(\.cloudClient) private var cloudClient
    @Environment(\.config) private var config
    @Environment(\.dismiss) private var dismiss

    let onSelect: (AgentDef) -> Void

    @State private var agents: [AgentDef] = []
    @State private var isLoading = true
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Group {
                if isLoading {
                    ProgressView("Loading agents...")
                        .frame(maxHeight: .infinity)
                } else if let err = error {
                    VStack(spacing: DesignTokens.spacingMD) {
                        ContentUnavailableView(
                            "Could Not Load Agents",
                            systemImage: "exclamationmark.triangle",
                            description: Text(err)
                        )
                        HStack(spacing: DesignTokens.spacingMD) {
                            Button("Retry") {
                                Task { await loadAgents() }
                            }
                            .buttonStyle(.bordered)
                            Button("Use Default Agent (diane-default)") {
                                let defaultAgent = AgentDef(
                                    id: "diane-default",
                                    name: "diane-default",
                                    description: "Default Diane agent",
                                    model: nil, provider: nil
                                )
                                onSelect(defaultAgent)
                                dismiss()
                            }
                            .buttonStyle(.borderedProminent)
                        }
                    }
                } else if agents.isEmpty {
                    VStack(spacing: DesignTokens.spacingMD) {
                        ContentUnavailableView(
                            "No Agents Available",
                            systemImage: "person.2.slash",
                            description: Text("Create an agent in the Memory Platform dashboard first.")
                        )
                        Button("Start with Default Agent") {
                            let defaultAgent = AgentDef(
                                id: "diane-default",
                                name: "diane-default",
                                description: "Default Diane agent",
                                model: nil, provider: nil
                            )
                            onSelect(defaultAgent)
                            dismiss()
                        }
                        .buttonStyle(.bordered)
                    }
                } else {
                    List(agents) { agent in
                        Button {
                            onSelect(agent)
                            dismiss()
                        } label: {
                            HStack(spacing: DesignTokens.spacingMD) {
                                Image(systemName: "person.circle.fill")
                                    .font(.title2)
                                    .foregroundColor(.accentColor)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(agent.name)
                                        .font(.body)
                                        .foregroundColor(.primary)
                                    if let desc = agent.description, !desc.isEmpty {
                                        Text(desc)
                                            .font(.caption)
                                            .foregroundColor(.secondary)
                                            .lineLimit(2)
                                    }
                                    HStack(spacing: 4) {
                                        if let model = agent.model {
                                            Text(model)
                                                .font(.caption2)
                                                .foregroundColor(.secondary)
                                        }
                                        if let provider = agent.provider {
                                            Text("· \(provider)")
                                                .font(.caption2)
                                                .foregroundColor(.secondary)
                                        }
                                    }
                                }
                                Spacer()
                                Image(systemName: "chevron.right")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            .padding(.vertical, 4)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .navigationTitle("Choose Agent")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .task { await loadAgents() }
        }
    }

    private func loadAgents() async {
        isLoading = true
        error = nil
        do {
            agents = try await cloudClient.fetchAgentDefs(projectID: config.projectID)
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - SessionListView

struct SessionListView: View {
    @Environment(\.cloudClient) private var cloudClient
    @StateObject private var archiveStore = ArchivedSessionsStore.shared
    @State private var sessions: [DianeSession] = []
    @State private var isLoading = true
    @State private var error: String?
    @State private var searchText = ""
    @State private var isCreating = false
    @State private var showSettings = false
    @State private var showAgentPicker = false
    @State private var navigateToNewSession: DianeSession?
    @State private var isSearching = false

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
            Group {
                if isLoading && sessions.isEmpty {
                    List {
                        ForEach(placeholderSessions) { session in
                            SessionRow(session: session, isArchived: false)
                                .redacted(reason: .placeholder)
                        }
                    }
                    .listStyle(.insetGrouped)
                } else if let err = error, sessions.isEmpty {
                    ErrorStateView(message: err) {
                        Task { await load() }
                    }
                } else if sessions.isEmpty {
                    EmptyStateView {
                        showAgentPicker = true
                    }
                } else {
                    List {
                        ForEach(filteredSessions) { session in
                            ZStack(alignment: .leading) {
                                NavigationLink(value: session) {
                                    EmptyView()
                                }
                                .opacity(0)
                                .buttonStyle(.plain)

                                SessionRow(session: session, isArchived: archiveStore.isArchived(session.id))
                            }
                            .swipeActions(edge: .trailing) {
                                Button {
                                    withAnimation(.easeInOut(duration: 0.25)) {
                                        archiveStore.toggleArchive(sessionID: session.id)
                                    }
                                } label: {
                                    Label(archiveStore.isArchived(session.id) ? "Unarchive" : "Archive", systemImage: "archivebox")
                                }
                                .tint(.gray)
                            }
                        }
                    
                    }
                    .listStyle(.insetGrouped)
                }
            }
        }
        .searchable(text: $searchText, isPresented: $isSearching, prompt: "Search sessions...")
        .navigationTitle("Chats")
        .toolbar {
            ToolbarItem(placement: .navigationBarTrailing) {
                HStack(spacing: 4) {
                    Button(action: { archiveStore.toggleShowArchived() }) {
                        Image(systemName: archiveStore.showArchived
                            ? "archivebox.fill"
                            : "archivebox")
                    }
                    .help(archiveStore.showArchived ? "Hide Archived" : "Show Archived")

                    Button(action: { isSearching = true }) {
                        Image(systemName: "magnifyingglass")
                    }
                    .keyboardShortcut("f", modifiers: .command)

                    Button(action: { showAgentPicker = true }) {
                        if isCreating {
                            ProgressView()
                                .scaleEffect(0.8)
                        } else {
                            Image(systemName: "plus.bubble")
                        }
                    }
                    .disabled(isCreating)
                    .keyboardShortcut("n", modifiers: .command)

                    Button(action: { showSettings = true }) {
                        Image(systemName: "gearshape")
                    }
                    .keyboardShortcut(",", modifiers: .command)
                }
            }
        }
        .sheet(isPresented: $showSettings) {
            NavigationStack {
                SettingsView()
            }
        }
        .sheet(isPresented: $showAgentPicker) {
            AgentPickerView { agent in
                Task { await createNewSession(agentName: agent.name) }
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .navigationDestination(for: DianeSession.self) { session in
            ChatView(session: session)
        }
        .navigationDestination(item: $navigateToNewSession) { session in
            ChatView(session: session)
        }
        .onReceive(NotificationCenter.default.publisher(for: NetworkMonitor.connectivityChanged)) { notification in
            if let connected = notification.userInfo?["isConnected"] as? Bool, connected {
                Task { await load() }
            }
        }
        .sentryView("SessionListView")
    }

    // MARK: - Data Loading

    private func load() async {
        isLoading = true
        error = nil

        // 1. Load local cache immediately
        let cached = SessionCache.shared.loadCachedSessions()
        sessions = cached
        isLoading = false

        // 2. Try to fetch remote sessions from MP (ACP API)
        do {
            let remoteItems = try await cloudClient.fetchACPSessions()
            let remoteSessions = remoteItems.map { $0.toDianeSession() }

            // Merge: prefer remote, keep local-only sessions
            var merged = remoteSessions
            for cachedSession in cached {
                if !remoteSessions.contains(where: { $0.id == cachedSession.id }) {
                    merged.append(cachedSession)
                }
            }

            // Sort by last activity descending (updatedAt, fallback to createdAt)
            merged.sort { a, b in
                let dateA = DateUtils.parseISO8601(a.updatedAt ?? a.createdAt) ?? .distantPast
                let dateB = DateUtils.parseISO8601(b.updatedAt ?? b.createdAt) ?? .distantPast
                return dateA > dateB
            }

            await MainActor.run {
                sessions = merged
                SessionCache.shared.cacheSessions(merged)
            }
        } catch {
            // Remote fetch failed — show error only if we have nothing cached
            if cached.isEmpty {
                await MainActor.run { self.error = error.localizedDescription }
            }
        }
    }

    private func createNewSession(agentName: String) async {
        isCreating = true
        do {
            let acpID = try await cloudClient.createACPSession(agentName: agentName)
            let session = DianeSession(
                id: acpID,
                title: "New Chat",
                createdAt: DateUtils.formatISO8601(),
                agentName: agentName
            )
            sessions.insert(session, at: 0)
            SessionCache.shared.cacheSessions(sessions)
            navigateToNewSession = session
        } catch {
            self.error = error.localizedDescription
        }
        isCreating = false
    }
}

// MARK: - Preview

#Preview("SessionListView") {
    NavigationStack {
        SessionListView()
    }
}

#Preview("Session Row") {
    List {
        SessionRow(
            session: DianeSession(
                id: "preview-1",
                title: "Project Kickoff Notes",
                status: "active",
                messageCount: 12,
                createdAt: DateUtils.formatISO8601(Date().addingTimeInterval(-3600)),
                agentName: "diane-default"
            ),
            isArchived: false
        )
        SessionRow(
            session: DianeSession(
                id: "preview-2",
                title: "Archived Conversation",
                status: "completed",
                messageCount: 3,
                createdAt: DateUtils.formatISO8601(Date().addingTimeInterval(-86_400)),
                agentName: "research-agent"
            ),
            isArchived: true
        )
    }
    .listStyle(.insetGrouped)
}

#Preview("Agent Picker") {
    AgentPickerView { _ in }
}
