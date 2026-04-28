import SwiftUI

/// Sessions view — lists Diane conversation sessions from the local API (or remote fallback).
struct SessionsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var sessions: [DianeSession] = []
    @State private var selectedSession: DianeSession? = nil
    @State private var messages: [DianeMessage] = []
    @State private var isLoading = false
    @State private var isLoadingMessages = false
    @State private var error: String? = nil

    var body: some View {
        GeometryReader { geometry in
            HSplitView {
                sessionsList
                    .frame(width: geometry.size.width * 0.5, minWidth: 250)

                if let session = selectedSession {
                    sessionDetailPanel(session)
                        .frame(minWidth: 250)
                } else {
                    EmptyStateView(
                        title: "Select a Session",
                        icon: "message",
                        description: "Select a conversation session to view its transcript."
                    )
                    .frame(minWidth: 250)
                }
            }
        }
        .navigationTitle("Sessions")
        .task { await load() }
    }

    // MARK: - Sessions List

    @ViewBuilder
    private var sessionsList: some View {
        VStack(spacing: 0) {
            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding(8)
            }

            if isLoading && sessions.isEmpty {
                LoadingStateView(message: "Loading sessions…")
            } else if sessions.isEmpty {
                EmptyStateView(
                    title: "No Sessions",
                    icon: "message",
                    description: "No conversation sessions found."
                )
            } else {
                List(sessions, selection: $selectedSession) { session in
                    sessionRow(session)
                        .tag(session)
                }
                .listStyle(.plain)
            }

            Divider()
            HStack {
                Text("\(sessions.count) session\(sessions.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Button("Refresh") { Task { await load() } }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
        .onChange(of: selectedSession) { session in
            if let s = session {
                Task { await loadMessages(session: s) }
            }
        }
    }

    private func sessionRow(_ session: DianeSession) -> some View {
        HStack(spacing: 8) {
            Circle()
                .fill(session.status == "active" ? Color.green : Color.secondary)
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 2) {
                Text(session.title ?? "Untitled")
                    .font(.subheadline)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let count = session.messageCount {
                        Text("\(count) messages")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    if let tokens = session.totalTokens {
                        Text("\(tokens) tokens")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    if let date = session.createdAt {
                        Text(date)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
            Spacer()
        }
        .padding(.vertical, 2)
    }

    // MARK: - Session Detail (Message Transcript)

    private func sessionDetailPanel(_ session: DianeSession) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text(session.title ?? "Untitled")
                        .font(.subheadline)
                        .fontWeight(.semibold)
                    HStack(spacing: 6) {
                        Text(session.status?.capitalized ?? "Unknown")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            if isLoadingMessages {
                LoadingStateView(message: "Loading messages…")
            } else if messages.isEmpty {
                EmptyStateView(
                    title: "No Messages",
                    icon: "text.bubble",
                    description: "This session has no messages."
                )
            } else {
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            ForEach(messages) { message in
                                messageBubble(message)
                                    .id(message.id)
                            }
                        }
                        .padding(12)
                    }
                    .onAppear {
                        if let last = messages.last {
                            proxy.scrollTo(last.id, anchor: .bottom)
                        }
                    }
                }
            }
        }
    }

    private func messageBubble(_ message: DianeMessage) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                roleBadge(message.role)
                if let seq = message.sequenceNumber {
                    Text("#\(seq)")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Spacer()
            }

            Text(message.content)
                .font(.system(.body, design: .monospaced))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(10)
        .background(message.role == "assistant" ? Color.primary.opacity(0.03) : Color.clear)
    }

    private func roleBadge(_ role: String) -> some View {
        Text(role.capitalized)
            .font(.caption2)
            .fontWeight(.semibold)
            .foregroundStyle(roleColor(role))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(roleColor(role).opacity(0.1))
            .cornerRadius(4)
    }

    private func roleColor(_ role: String) -> Color {
        switch role.lowercased() {
        case "user": return .blue
        case "assistant": return .green
        case "system": return .orange
        default: return .secondary
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        do {
            if dianeAPI.isReachable {
                sessions = try await dianeAPI.fetchSessions()
            } else {
                sessions = try await apiClient.fetchSessions(projectID: serverConfig.projectID)
            }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    @MainActor
    private func loadMessages(session: DianeSession) async {
        isLoadingMessages = true
        do {
            if dianeAPI.isReachable {
                messages = try await dianeAPI.fetchSessionMessages(sessionID: session.id)
            } else {
                messages = try await apiClient.fetchSessionMessages(projectID: serverConfig.projectID, sessionID: session.id)
            }
        } catch {
            messages = []
        }
        isLoadingMessages = false
    }
}
