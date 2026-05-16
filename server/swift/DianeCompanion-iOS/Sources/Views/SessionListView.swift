import SwiftUI
import DianeShared

// MARK: - RemoteDianeAPIClient Delete Extension

// MARK: - Placeholder Sessions for Loading State

private let placeholderSessions: [DianeSession] = (0..<5).map { i in
    DianeSession(
        id: "placeholder-\(i)",
        title: "Loading Session...",
        status: "active",
        messageCount: 0,
        createdAt: ISO8601DateFormatter().string(from: Date()),
        agentName: "diane-default"
    )
}

// MARK: - SessionRow

struct SessionRow: View {
    let session: DianeSession

    var body: some View {
        HStack(spacing: DesignTokens.spacingMD) {
            // Status dot
            Circle()
                .fill(StatusColors.statusColor(session.status))
                .frame(width: 10, height: 10)

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

            // Relative timestamp
            Text(DateUtils.formatRelative(session.createdAt))
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

// MARK: - SessionListView

struct SessionListView: View {
    @Environment(\.cloudClient) private var cloudClient
    @State private var sessions: [DianeSession] = []
    @State private var isLoading = true
    @State private var error: String?
    @State private var searchText = ""
    @State private var sessionToDelete: DianeSession?
    @State private var showDeleteAlert = false
    @State private var isCreating = false
    @State private var isOffline = false
    @State private var showSettings = false

    var filteredSessions: [DianeSession] {
        if searchText.isEmpty { return sessions }
        return sessions.filter {
            ($0.title ?? "").localizedCaseInsensitiveContains(searchText)
            || ($0.agentName ?? "").localizedCaseInsensitiveContains(searchText)
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            if isOffline {
                OfflineBanner()
            }

            Group {
            if isLoading && sessions.isEmpty {
                // Redacted placeholder rows while loading
                List {
                    ForEach(placeholderSessions) { session in
                        SessionRow(session: session)
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
                    Task { await createNewSession() }
                }
            } else {
                List {
                    ForEach(filteredSessions) { session in
                        NavigationLink(value: session) {
                            SessionRow(session: session)
                        }
                        .swipeActions(edge: .trailing) {
                            Button(role: .destructive) {
                                sessionToDelete = session
                                showDeleteAlert = true
                            } label: {
                                Label("Delete", systemImage: "trash")
                            }
                        }
                    }
                    .onDelete { indexSet in
                        guard let index = indexSet.first, index < filteredSessions.count else { return }
                        sessionToDelete = filteredSessions[index]
                        showDeleteAlert = true
                    }
                }
                .listStyle(.insetGrouped)
            }
        }  // closes Group
        }  // closes VStack
        .searchable(text: $searchText, prompt: "Search sessions...")
        .navigationTitle("Chats")
        .toolbar {
            ToolbarItem(placement: .navigationBarTrailing) {
                HStack(spacing: 4) {
                    Button(action: { Task { await createNewSession() } }) {
                        if isCreating {
                            ProgressView()
                                .scaleEffect(0.8)
                        } else {
                            Image(systemName: "plus.bubble")
                        }
                    }
                    .disabled(isCreating)

                    Button(action: { showSettings = true }) {
                        Image(systemName: "gearshape")
                    }
                }
            }
        }
        .sheet(isPresented: $showSettings) {
            NavigationStack {
                SettingsView()
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .navigationDestination(for: DianeSession.self) { session in
            ChatView(session: session)
        }
        .alert("Delete Session", isPresented: $showDeleteAlert, presenting: sessionToDelete) { session in
            Button("Cancel", role: .cancel) {
                sessionToDelete = nil
            }
            Button("Delete", role: .destructive) {
                if let idx = sessions.firstIndex(where: { $0.id == session.id }) {
                    sessions.remove(at: idx)
                }
                Task { await performDelete(session) }
            }
        } message: { session in
            Text("Are you sure you want to delete \"\(ViewFormatting.sessionTitle(session.title))\"? This action cannot be undone.")
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
        isOffline = false
        do {
            sessions = SessionCache.shared.loadCachedSessions()
        } catch {
            // Fall back to cached sessions
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
        isCreating = true
        do {
            let acpID = try await cloudClient.createACPSession(agentName: "diane-default")
            let session = DianeSession(id: acpID, title: "New Chat", createdAt: ISO8601DateFormatter().string(from: Date()), agentName: "diane-default")
            sessions.insert(session, at: 0)
            SessionCache.shared.cacheSessions(sessions)
        } catch {
            self.error = error.localizedDescription
        }
        isCreating = false
    }

    private func performDelete(_ session: DianeSession) async {
        // Remove from local cache only
        SessionCache.shared.cacheSessions(sessions)
    }
}

// MARK: - Preview

#Preview {
    NavigationStack {
        SessionListView()
    }
}
