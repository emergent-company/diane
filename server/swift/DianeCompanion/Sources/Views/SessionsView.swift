import SwiftUI
import UniformTypeIdentifiers

/// Sessions view — lists Diane conversation sessions with search, filter, and
/// rich message display (thinking blocks, tool calls, chat bubbles).
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

    // Search & filter
    @State private var searchText = ""
    @State private var statusFilter: SessionFilter = .all

    enum SessionFilter: String, CaseIterable {
        case all = "All"
        case active = "Active"
        case completed = "Completed"
    }

    var body: some View {
        HSplitView {
            sessionsList
                .frame(minWidth: 250)

            if let session = selectedSession {
                sessionDetailPanel(session)
                    .frame(minWidth: 320)
            } else {
                EmptyStateView(
                    title: "Select a Session",
                    icon: "message",
                    description: "Select a conversation session to view its transcript."
                )
                .frame(minWidth: 320)
            }
        }
        .navigationTitle("Sessions")
        .task { await load() }
    }

    // MARK: - Filtered Sessions

    var filteredSessions: [DianeSession] {
        var result = sessions
        // Filter by status
        switch statusFilter {
        case .all: break
        case .active: result = result.filter { $0.status == "active" || $0.status == nil }
        case .completed: result = result.filter { $0.status == "completed" }
        }
        // Filter by search text
        if !searchText.isEmpty {
            result = result.filter { ($0.title ?? "").localizedCaseInsensitiveContains(searchText) }
        }
        return result
    }

    // MARK: - Sessions List

    @ViewBuilder
    private var sessionsList: some View {
        VStack(spacing: 0) {
            // Search bar
            HStack {
                Image(systemName: "magnifyingglass")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                TextField("Search sessions…", text: $searchText)
                    .textFieldStyle(.plain)
                    .font(.caption)
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
            }
            .padding(8)
            .background(Color.primary.opacity(0.03))

            // Status filter
            Picker("", selection: $statusFilter) {
                ForEach(SessionFilter.allCases, id: \.self) { filter in
                    Text(filter.rawValue).tag(filter)
                }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal, 8)
            .padding(.vertical, 6)

            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding(8)
            }

            if isLoading && sessions.isEmpty {
                LoadingStateView(message: "Loading sessions…")
            } else if filteredSessions.isEmpty {
                EmptyStateView(
                    title: searchText.isEmpty ? "No Sessions" : "No Results",
                    icon: searchText.isEmpty ? "message" : "magnifyingglass",
                    description: searchText.isEmpty
                        ? "No conversation sessions found."
                        : "No sessions match \"\(searchText)\"."
                )
            } else {
                List(filteredSessions, selection: $selectedSession) { session in
                    sessionRow(session)
                        .tag(session)
                }
                .listStyle(.plain)
            }

            Divider()
            HStack {
                Text("\(filteredSessions.count) session\(filteredSessions.count == 1 ? "" : "s")")
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
                        Text(formatDate(date))
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

    @State private var expandedReasoning: Set<String> = []
    @State private var expandedToolCalls: Set<String> = []
    @State private var showingExporter = false
    @State private var exportData: Data? = nil

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
                        if let count = session.messageCount {
                            Text("\(count) messages")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
                Spacer()

                Button("Export") {
                    exportSession(session)
                }
                .font(.caption)
                .buttonStyle(.borderless)
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
                                messageBlock(message)
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
        .fileExporter(
            isPresented: $showingExporter,
            document: JSONFile(data: exportData ?? Data()),
            contentType: .json,
            defaultFilename: "session-\(session.id.prefix(8)).json"
        ) { _ in }
    }

    // MARK: - Message Block

    private func messageBlock(_ message: DianeMessage) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            // Header row
            HStack(spacing: 6) {
                roleBadge(message.role)
                if let seq = message.sequenceNumber {
                    Text("#\(seq)")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Spacer()
            }

            // Reasoning content (thinking block) — collapsible, orange
            if let reasoning = message.reasoningContent, !reasoning.isEmpty {
                let isExpanded = expandedReasoning.contains(message.id)
                VStack(alignment: .leading, spacing: 4) {
                    Button {
                        if isExpanded {
                            expandedReasoning.remove(message.id)
                        } else {
                            expandedReasoning.insert(message.id)
                        }
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "brain")
                                .font(.caption2)
                            Text("Thinking")
                                .font(.caption)
                                .fontWeight(.medium)
                            Spacer()
                            Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                                .font(.caption2)
                        }
                        .foregroundStyle(.orange)
                    }
                    .buttonStyle(.plain)

                    if isExpanded {
                        Text(reasoning)
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(8)
                            .background(Color.orange.opacity(0.05))
                            .cornerRadius(6)
                    }
                }
                .padding(.vertical, 4)
            }

            // Tool calls — collapsible, purple
            if let calls = message.toolCalls, !calls.isEmpty {
                let isExpanded = expandedToolCalls.contains(message.id)
                VStack(alignment: .leading, spacing: 4) {
                    Button {
                        if isExpanded {
                            expandedToolCalls.remove(message.id)
                        } else {
                            expandedToolCalls.insert(message.id)
                        }
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "wrench")
                                .font(.caption2)
                            Text("\(calls.count) tool call\(calls.count == 1 ? "" : "s")")
                                .font(.caption)
                                .fontWeight(.medium)
                            Spacer()
                            Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                                .font(.caption2)
                        }
                        .foregroundStyle(.purple)
                    }
                    .buttonStyle(.plain)

                    if isExpanded {
                        ForEach(Array(calls.enumerated()), id: \.offset) { idx, call in
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 4) {
                                    Text(call.name ?? "tool")
                                        .font(.caption)
                                        .fontWeight(.semibold)
                                        .foregroundStyle(.purple)
                                }
                                if let args = call.arguments, !args.isEmpty {
                                    Text(args)
                                        .font(.system(.caption2, design: .monospaced))
                                        .foregroundStyle(.secondary)
                                        .lineLimit(5)
                                }
                            }
                            .padding(8)
                            .background(Color.purple.opacity(0.05))
                            .cornerRadius(6)
                        }
                    }
                }
                .padding(.vertical, 4)
            }

            // Main content
            Text(message.content)
                .font(.system(.body, design: .monospaced))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(10)
        .background(message.role == "assistant" ? Color.primary.opacity(0.03) : Color.clear)
    }

    // MARK: - Role Badge

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
        expandedReasoning.removeAll()
        expandedToolCalls.removeAll()
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

    // MARK: - Export

    private func exportSession(_ session: DianeSession) {
        let exportDict: [String: Any] = [
            "session": [
                "id": session.id,
                "title": session.title ?? "",
                "status": session.status ?? "",
                "message_count": session.messageCount ?? 0,
                "total_tokens": session.totalTokens ?? 0,
                "created_at": session.createdAt ?? ""
            ] as [String: Any],
            "messages": messages.map { msg -> [String: Any] in
                var dict: [String: Any] = [
                    "id": msg.id,
                    "role": msg.role,
                    "content": msg.content,
                    "sequence_number": msg.sequenceNumber ?? 0
                ]
                if let reasoning = msg.reasoningContent {
                    dict["reasoning_content"] = reasoning
                }
                if let calls = msg.toolCalls {
                    dict["tool_calls"] = calls.map { c in
                        [
                            "id": c.id ?? "",
                            "name": c.name ?? "",
                            "arguments": c.arguments ?? ""
                        ]
                    }
                }
                return dict
            }
        ]
        exportData = try? JSONSerialization.data(withJSONObject: exportDict, options: [.prettyPrinted, .sortedKeys])
        showingExporter = true
    }

    // MARK: - Date Formatting

    private func formatDate(_ iso: String) -> String {
        // Show just the date part for brevity
        if iso.count >= 10 {
            return String(iso.prefix(10))
        }
        return iso
    }
}

// MARK: - JSON File Exporter

struct JSONFile: FileDocument {
    var data: Data

    static var readableContentTypes: [UTType] { [.json] }

    init(data: Data) {
        self.data = data
    }

    init(configuration: ReadConfiguration) throws {
        data = configuration.file.regularFileContents ?? Data()
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: data)
    }
}
