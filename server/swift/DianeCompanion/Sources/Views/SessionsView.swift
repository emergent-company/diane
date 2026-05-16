import SwiftUI
import Textual

/// Sessions view — lists Diane conversation sessions with chat-like message transcripts.
/// Shows session status badges, relative timestamps, collapsible tool calls, and thinking sections.
struct SessionsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var sessions: [DianeSession] = []
    @State private var selectedSession: DianeSession? = nil
    @State private var messages: [DianeMessage] = []
    @State private var sessionDetail: SessionDetailResponse? = nil
    @State private var isLoading = false
    @State private var acpSessionID: String? = nil
    @State private var isLoadingMessages = false
    @State private var isLoadingDetail = false
    @State private var error: String? = nil
    @State private var messagesError: String? = nil

    // Chat state
    @State private var inputText: String = ""
    @State private var isSending = false
    @State private var agentDefs: [AgentDef] = []
    @State private var selectedAgent: String = "diane-default"

    // Session copy feedback
    @State private var sessionIDCopied = false

    private static nonisolated(unsafe) let isoFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static nonisolated(unsafe) let isoFormatterNoFractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    // Session metadata panel state
    @State private var sessionRuns: [SessionRunSummary] = []
    @State private var sessionTodos: [SessionTodoItem] = []
    @State private var isLoadingRuns = false
    @State private var isLoadingTodos = false

    var body: some View {
        SplitListDetailView(
            emptyTitle: "Select a Session",
            emptyIcon: "message",
            emptyDescription: "Select a conversation session to view its transcript.",
            listContent: { sessionsList },
            detailContent: {
                if let session = selectedSession {
                    sessionDetailPanel(session)
                } else {
                    newChatView
                }
            }
        )
        .navigationTitle("Sessions")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(action: startNewChat) {
                    Label("New Chat", systemImage: "plus.bubble")
                }
                .disabled(isSending)
            }
            ToolbarItem(placement: .automatic) {
                if selectedSession != nil {
                    Button(action: closeCurrentSession) {
                        Label("Close Session", systemImage: "xmark.circle")
                    }
                    .disabled(isSending)
                }
            }
        }
        .task { await load() }
        .task { await loadAgentDefs() }
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
            .padding(.horizontal, Design.Padding.sectionHeader)
            .padding(.vertical, 6)
        }
        .onChange(of: selectedSession) { _, session in
            if let s = session {
                Task { await loadMessages(session: s) }
                Task { await loadSessionDetail(session: s) }
            } else {
                sessionDetail = nil
                sessionRuns = []
                sessionTodos = []
            }
        }
    }

    private func sessionRow(_ session: DianeSession) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            // Status indicator
            statusIcon(session.status)
                .font(.system(size: Design.IconSize.tiny + 1))
                .frame(width: 20, height: 20)

            VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                HStack(spacing: Design.Spacing.xs) {
                    Text(session.title ?? "Untitled")
                        .font(.subheadline)
                        .fontWeight(.medium)
                        .lineLimit(1)
                    statusBadge(session.status)
                }
                HStack(spacing: Design.Spacing.sm) {
                    if let count = session.messageCount {
                        HStack(spacing: Design.Spacing.xxs) {
                            Image(systemName: "text.bubble")
                                .font(.system(size: Design.IconSize.tiny))
                            Text("\(count)")
                                .font(.caption2)
                        }
                        .foregroundStyle(.secondary)
                    }
                    if let tokens = session.totalTokens {
                        HStack(spacing: Design.Spacing.xxs) {
                            Image(systemName: "number")
                                .font(.system(size: Design.IconSize.tiny))
                            Text(formatTokenCount(tokens))
                                .font(.caption2)
                        }
                        .foregroundStyle(.tertiary)
                    }
                    Spacer()
                    if let dateStr = session.updatedAt ?? session.createdAt {
                        Text(relativeTimestamp(dateStr))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .help(dateStr)
                    }
                }
            }
        }
        .padding(.vertical, Design.Spacing.xxs)
    }

    @ViewBuilder
    private func statusIcon(_ status: String?) -> some View {
        switch status?.lowercased() {
        case "active", "running":
            Image(systemName: "circle.fill")
                .foregroundStyle(.green)
        case "paused", "idle":
            Image(systemName: "pause.circle.fill")
                .foregroundStyle(.orange)
        case "completed", "closed", "done":
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(.secondary)
        case "error", "failed":
            Image(systemName: "exclamationmark.circle.fill")
                .foregroundStyle(.red)
        default:
            // nil/empty status = active session (not yet closed)
            Image(systemName: "circle.fill")
                .foregroundStyle(.green)
        }
    }

    @ViewBuilder
    private func statusBadge(_ status: String?) -> some View {
        if let s = status, !s.isEmpty {
            Text(s.capitalized)
                .font(.system(size: Design.IconSize.tiny, weight: .medium))
                .badgeStyle(color: StatusColors.sessionStatus(s))
        }
    }

    func statusColor(_ status: String) -> Color { StatusColors.sessionStatus(status) }

    // MARK: - Session Detail (Chat-like Transcript)

    private func sessionDetailPanel(_ session: DianeSession) -> some View {
        HSplitView {
            // Left: chat area
            VStack(alignment: .leading, spacing: 0) {
                // Header
                sessionHeader(session)

                Divider()

                // Messages area (takes all remaining space)
                Group {
                    if isLoadingMessages {
                        LoadingStateView(message: "Loading messages…")
                    } else if let err = messagesError {
                        ErrorBannerView(message: err) {
                            Task {
                                if let session = selectedSession {
                                    await loadMessages(session: session)
                                }
                            }
                        }
                        .padding(8)
                    } else if messages.isEmpty {
                        EmptyStateView(
                            title: "No Messages",
                            icon: "text.bubble",
                            description: "Type a message below to start the conversation."
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
                                .padding(.horizontal, Design.Spacing.lg)
                                .padding(.vertical, 8)
                                // GPU-composite complex bubble shapes to avoid main-thread
                                // ShapeStyledDisplayList updates during token streaming.
                                .drawingGroup()
                            }
                            .onAppear {
                                if let last = messages.last {
                                    proxy.scrollTo(last.id, anchor: .bottom)
                                }
                            }
                        }
                    }
                }
                .layoutPriority(1)

                Divider()

                // Input bar
                inputBar
            }
            .frame(minWidth: 400)

            // Right: session metadata panel
            sessionMetadataPanel(session: session)
                .frame(minWidth: 280, idealWidth: 300, maxWidth: 360)
        }
        .onChange(of: messages.count) { _, _ in
            // Auto-scroll handled by ScrollViewReader id binding
        }
    }

    private func sessionHeader(_ session: DianeSession) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: Design.Spacing.sm) {
                statusIcon(session.status)
                    .font(.system(size: Design.IconSize.small))

                VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                    Text(session.title ?? "Untitled")
                        .font(.subheadline)
                        .fontWeight(.semibold)
                    HStack(spacing: Design.Spacing.sm) {
                        statusBadge(session.status)
                        if let dateStr = session.updatedAt ?? session.createdAt {
                            Text(relativeTimestamp(dateStr))
                                .font(.caption)
                                .foregroundStyle(.tertiary)
                        }
                        if let count = session.messageCount {
                            Text("\(count) messages")
                                .font(.caption)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }

                Spacer(minLength: 8)

                // Agent picker
                if !agentDefs.isEmpty {
                    Picker("Agent", selection: $selectedAgent) {
                        ForEach(agentDefs) { def in
                            Text(def.name).tag(def.name)
                        }
                    }
                    .pickerStyle(.menu)
                    .font(.caption)
                    .frame(width: 160)
                    .disabled(isSending || selectedSession == nil)
                    .help("Agent used when sending messages to this session")
                }

                if isSending {
                    HStack(spacing: 6) {
                        ProgressView()
                            .scaleEffect(0.7)
                        Text("Agent is thinking…")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
            .padding(.horizontal, Design.Padding.sectionHeader)
            .padding(.top, Design.Padding.sectionHeader)
            .padding(.bottom, Design.Spacing.sm)

            // Session ID + Agent info row
            sessionMetaRow(session)
                .padding(.horizontal, Design.Padding.sectionHeader)
                .padding(.bottom, Design.Spacing.sm)

            // Stats bar
            if let detail = sessionDetail {
                statsBar(detail)
                    .padding(.horizontal, Design.Padding.sectionHeader)
                    .padding(.bottom, Design.Padding.sectionHeader)
            } else if isLoadingDetail {
                HStack {
                    ProgressView()
                        .scaleEffect(0.6)
                    Text("Loading stats…")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                .padding(.horizontal, Design.Padding.sectionHeader)
                .padding(.bottom, Design.Padding.sectionHeader)
            }
        }
        .background(Design.Surface.cardBackground)
    }

    /// Session metadata row — agent name badges only (session ID shown in right panel).
    @ViewBuilder
    private func sessionMetaRow(_ session: DianeSession) -> some View {
        HStack(spacing: 12) {
            // Agent name from run aggregates
            if let detail = sessionDetail, let names = detail.aggregates?.agentNames, !names.isEmpty {
                HStack(spacing: Design.Spacing.xs) {
                    Image(systemName: "brain.head.profile")
                        .font(.system(size: Design.IconSize.tiny))
                        .foregroundStyle(.secondary)
                    ForEach(names, id: \.self) { name in
                        Text(agentShortName(name))
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, Design.Spacing.xs)
                            .padding(.vertical, 1)
                            .background(Color.primary.opacity(0.06))
                            .cornerRadius(3)
                    }
                }
            }

            Spacer()
        }
    }

    /// Short form: last 6 characters of the session ID.
    func sessionIDShortForm(_ id: String) -> String { ViewFormatting.sessionIDShortForm(id) }

    /// Strip common prefixes from agent names for compact display.
    func agentShortName(_ name: String) -> String { ViewFormatting.agentShortName(name) }

    @ViewBuilder
    private func statsBar(_ detail: SessionDetailResponse) -> some View {
        let agg = detail.aggregates
        HStack(spacing: Design.Spacing.lg) {
            if let agg = agg {
                if detail.totalTokens > 0 {
                    statsBadge(icon: "number", value: formatTokenCount(detail.totalTokens), label: "tokens")
                }
                if agg.totalRuns > 0 {
                    statsBadge(icon: "arrow.triangle.branch", value: "\(agg.totalRuns)", label: "runs")
                }
                if agg.estimatedCostUsd > 0 {
                    statsBadge(icon: "dollarsign.circle.fill", value: formatCost(agg.estimatedCostUsd), label: "cost")
                }
                if agg.totalInputTokens > 0 || agg.totalOutputTokens > 0 {
                    statsBadge(icon: "textformat.size", value: "\(formatTokenCount(Int(agg.totalInputTokens)))→\(formatTokenCount(Int(agg.totalOutputTokens)))", label: "in→out")
                }
            }
            Spacer()
        }
    }

    private func statsBadge(icon: String, value: String, label: String) -> some View {
        HStack(spacing: Design.Spacing.xs) {
            Image(systemName: icon)
                .font(.system(size: Design.IconSize.tiny))
                .foregroundStyle(.secondary)
            Text(value)
                .font(.caption2)
                .fontWeight(.medium)
                .monospacedDigit()
                .foregroundStyle(.primary)
            Text(label)
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, Design.Spacing.xs)
        .background(Color.primary.opacity(0.05))
        .cornerRadius(5)
    }

    // MARK: - Session Metadata Panel (Right Column)

    @ViewBuilder
    private func sessionMetadataPanel(session: DianeSession) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Design.Spacing.md) {
                // Session Info
                sessionInfoSection(session)

                Divider()

                // Summary
                if let detail = sessionDetail {
                    sessionSummarySection(detail)
                    Divider()
                }

                // Run History
                runHistorySection

                Divider()

                // Todo List
                todoListSection
            }
            .padding(Design.Spacing.md)
        }
        .background(Design.Surface.cardBackground)
    }

    // MARK: - Session Info Section

    @ViewBuilder
    private func sessionInfoSection(_ session: DianeSession) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.xs) {
            Label("Session Info", systemImage: "info.circle.fill")
                .font(.caption)
                .foregroundStyle(.secondary)

            // Session ID — full ID, click to copy with visual feedback
            HStack(alignment: .center, spacing: Design.Spacing.xs) {
                Text("ID")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .frame(width: 56, alignment: .leading)
                if sessionIDCopied {
                    Text("✓ Copied")
                        .font(.system(size: 10, design: .monospaced))
                        .fontWeight(.medium)
                        .foregroundStyle(.green)
                        .transition(.opacity.combined(with: .scale(scale: 0.95)))
                } else {
                    Text(session.id)
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(.primary)
                        .lineLimit(2)
                        .textSelection(.enabled)
                        .onTapGesture {
                            copySessionID(session.id)
                        }
                }
                Spacer()
                Button {
                    copySessionID(session.id)
                } label: {
                    if sessionIDCopied {
                        Image(systemName: "checkmark")
                            .font(.system(size: 9))
                            .foregroundStyle(.green)
                    } else {
                        Image(systemName: "doc.on.doc")
                            .font(.system(size: 9))
                            .foregroundStyle(.tertiary)
                            .opacity(0.6)
                    }
                }
                .buttonStyle(.plain)
                .help("Copy session ID")
            }
            .padding(.vertical, 1)
            if let key = session.key, !key.isEmpty {
                metadataRow(label: "Key", value: key, monospaced: true)
            }
            metadataRow(label: "Status", value: session.status ?? "active")
            if let created = session.createdAt {
                metadataRow(label: "Created", value: formatTimestamp(created))
            }
            if let detail = sessionDetail, let updated = detail.updatedAt, !updated.isEmpty {
                metadataRow(label: "Updated", value: formatTimestamp(updated))
            }
        }
    }

    // MARK: - Summary Section

    @ViewBuilder
    private func sessionSummarySection(_ detail: SessionDetailResponse) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.xs) {
            Label("Summary", systemImage: "chart.bar.fill")
                .font(.caption)
                .foregroundStyle(.secondary)

            if let agg = detail.aggregates {
                metadataRow(label: "Runs", value: "\(agg.totalRuns)")
                metadataRow(label: "Messages", value: "\(detail.messageCount)")
                metadataRow(label: "Tokens", value: formatTokenCount(detail.totalTokens))
                if agg.estimatedCostUsd > 0 {
                    metadataRow(label: "Cost", value: formatCost(agg.estimatedCostUsd))
                }
                if let names = agg.agentNames, !names.isEmpty {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Agents")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                        HStack(spacing: 4) {
                            ForEach(names, id: \.self) { name in
                                Text(agentShortName(name))
                                    .font(.system(size: 9, design: .monospaced))
                                    .foregroundStyle(.secondary)
                                    .padding(.horizontal, 4)
                                    .padding(.vertical, 1)
                                    .background(Color.primary.opacity(0.06))
                                    .cornerRadius(3)
                            }
                        }
                    }
                }
            } else {
                metadataRow(label: "Messages", value: "\(detail.messageCount)")
                metadataRow(label: "Tokens", value: formatTokenCount(detail.totalTokens))
            }
        }
    }

    // MARK: - Run History Section

    @ViewBuilder
    private var runHistorySection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.xs) {
            HStack {
                Label("Run History", systemImage: "arrow.triangle.branch")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                if isLoadingRuns {
                    ProgressView()
                        .scaleEffect(0.5)
                }
            }

            if sessionRuns.isEmpty && !isLoadingRuns {
                Text("No runs recorded for this session.")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .padding(.top, 2)
            } else {
                ForEach(sessionRuns) { run in
                    runRow(run)
                }
            }
        }
    }

    @ViewBuilder
    private func runRow(_ run: SessionRunSummary) -> some View {
        HStack(spacing: Design.Spacing.xs) {
            // Status dot
            Circle()
                .fill(runStatusColor(run.status))
                .frame(width: 6, height: 6)

            VStack(alignment: .leading, spacing: 1) {
                Text(agentShortName(run.agentName))
                    .font(.caption2)
                    .fontWeight(.medium)
                    .lineLimit(1)
                if let model = run.model {
                    Text(model)
                        .font(.system(size: 8))
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
            }

            Spacer()

            if let ms = run.durationMs {
                Text(formatDuration(Double(ms)))
                    .font(.system(size: 9, design: .monospaced))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, 2)
        .padding(.horizontal, 4)
        .background(run.status == "failed" || run.status == "error" ? Color.red.opacity(0.05) : Color.clear)
        .cornerRadius(3)
    }

    private func runStatusColor(_ status: String) -> Color {
        switch status.lowercased() {
        case "completed", "success": return .green
        case "running", "pending":   return .orange
        case "failed", "error":     return .red
        default:                     return .gray
        }
    }

    // MARK: - Todo List Section

    @ViewBuilder
    private var todoListSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.xs) {
            HStack {
                Label("Todo List", systemImage: "checklist")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                if isLoadingTodos {
                    ProgressView()
                        .scaleEffect(0.5)
                }
            }

            let pending = sessionTodos.filter { $0.status == "pending" }
            let completed = sessionTodos.filter { $0.status == "completed" }

            if sessionTodos.isEmpty && !isLoadingTodos {
                Text("No todos for this session.")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .padding(.top, 2)
            } else {
                if !pending.isEmpty {
                    Text("To Do (\(pending.count))")
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(.secondary)
                        .padding(.top, 2)
                    ForEach(pending) { todo in
                        todoRow(todo)
                    }
                }
                if !completed.isEmpty {
                    Text("Done (\(completed.count))")
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(.tertiary)
                        .padding(.top, 4)
                    ForEach(completed) { todo in
                        todoRow(todo)
                            .opacity(0.6)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func todoRow(_ todo: SessionTodoItem) -> some View {
        HStack(spacing: Design.Spacing.xs) {
            Image(systemName: todo.status == "completed" ? "checkmark.circle.fill" : "circle")
                .font(.system(size: 10))
                .foregroundStyle(todo.status == "completed" ? .green : .secondary)

            Text(todo.content)
                .font(.caption2)
                .lineLimit(2)
                .strikethrough(todo.status == "completed")
                .foregroundStyle(todo.status == "completed" ? .tertiary : .primary)

            Spacer()
        }
        .padding(.vertical, 1)
    }

    // MARK: - Helpers

    private func copySessionID(_ id: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(id, forType: .string)
        withAnimation(.easeInOut(duration: 0.15)) {
            sessionIDCopied = true
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            withAnimation(.easeInOut(duration: 0.15)) {
                sessionIDCopied = false
            }
        }
    }

    private func metadataRow(label: String, value: String, monospaced: Bool = false) -> some View {
        HStack(alignment: .top, spacing: Design.Spacing.xs) {
            Text(label)
                .font(.caption2)
                .foregroundStyle(.tertiary)
                .frame(width: 56, alignment: .leading)
            Text(value)
                .font(monospaced ? .system(size: 10, design: .monospaced) : .caption2)
                .foregroundStyle(.primary)
                .lineLimit(2)
            Spacer()
        }
    }

    private func formatTimestamp(_ iso: String) -> String {
        DateUtils.formatTimestamp(iso)
    }

    // MARK: - New Chat (Empty State)

    /// Shown when no session is selected — start a new conversation or resume a recent one.
    @ViewBuilder
    private var newChatView: some View {
        VStack(spacing: 0) {
            VStack(spacing: 12) {
                Spacer()
                Image(systemName: "bubble.left.and.bubble.right")
                    .font(.system(size: 40))
                    .foregroundStyle(.tertiary)
                Text("Start a Conversation")
                    .font(.title3)
                    .fontWeight(.medium)
                Text("Type a message below or select a session from the list.\\nYour conversations are saved as sessions.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 40)
                if !sessions.isEmpty {
                    Button("Resume Recent Session") {
                        if let latest = sessions.first {
                            selectSession(latest)
                        }
                    }
                    .buttonStyle(.bordered)
                    .padding(.top, 4)
                }
                Spacer()
            }
            .layoutPriority(1)

            Divider()
            inputBar
        }
    }

    // MARK: - Input Bar

    private var inputBar: some View {
        HStack(spacing: 8) {
            TextField("Type a message…", text: $inputText, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.body)
                .lineLimit(1...6)
                .padding(10)
                .background(Color.primary.opacity(0.05))
                .cornerRadius(10)
                .disabled(isSending)
                .onSubmit { Task { await sendMessage() } }

            Button(action: { Task { await sendMessage() } }) {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.system(size: 28))
                    .foregroundStyle(canSend ? Color.accentColor : Color.secondary.opacity(0.3))
            }
            .buttonStyle(.plain)
            .disabled(!canSend)
        }
        .padding(12)
    }

    private var canSend: Bool {
        !isSending && !inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    // MARK: - Message Bubble

    @ViewBuilder
    private func messageBubble(_ message: DianeMessage) -> some View {
        let isUser = message.role.lowercased() == "user"
        let isSystem = message.role.lowercased() == "system"

        // Thinking placeholder — animated indicator while agent is generating
        if message.id.hasPrefix("thinking-") {
            thinkingBubble
        } else {
            VStack(alignment: isUser ? .trailing : .leading, spacing: Design.Spacing.xs) {
            // Role label + sequence
            HStack(spacing: Design.Spacing.xs) {
                if !isUser {
                    roleBadge(message.role)
                }
                if let seq = message.sequenceNumber {
                    Text("#\(seq)")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                if let tokens = message.tokenCount, tokens > 0 {
                    Text("\(tokens) tok")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, Design.Spacing.xs)

            // Content bubble
            VStack(alignment: .leading, spacing: Design.Spacing.xs) {
                // Reasoning / Thinking section (collapsible)
                if let thinking = message.reasoningContent, !thinking.isEmpty {
                    thinkingSection(thinking)
                }

                // Tool calls section (collapsible)
                if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
                    toolCallsSection(toolCalls)
                }

                // Main content
                if !message.content.isEmpty {
                    if isSystem {
                        // System messages: subtle italic style
                        Text(message.content)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .italic()
                    } else if isUser {
                        // User messages: plain text
                        Text(message.content)
                            .font(.body)
                            .textSelection(.enabled)
                    } else {
                        // Assistant messages: rich markdown with syntax highlighting
                        StructuredText(markdown: message.content)
                            .font(.body)
                            .textual.textSelection(.enabled)
                    }
                }
            }
            .padding(Design.Padding.banner)
            .background(bubbleBackground(isUser: isUser, isSystem: isSystem))
            .cornerRadius(Design.CornerRadius.medium)
            .overlay(alignment: isUser ? .bottomTrailing : .bottomLeading) {
                // Use Canvas to draw the bubble tail triangle — this bypasses the
                // ShapeStyledDisplayList / _ShapeStyle_RenderedShape rendering pipeline
                // that causes main-thread hangs during view updates (APPLE-MACOS-C).
                Canvas { context, size in
                    var path = Path()
                    if isUser {
                        path.move(to: .zero)
                        path.addLine(to: CGPoint(x: size.width, y: 0))
                        path.addLine(to: CGPoint(x: size.width, y: size.height))
                    } else {
                        path.move(to: CGPoint(x: 0, y: size.height))
                        path.addLine(to: CGPoint(x: size.width, y: 0))
                        path.addLine(to: CGPoint(x: 0, y: 0))
                    }
                    path.closeSubpath()
                    context.fill(path, with: .color(bubbleTailColor(isUser: isUser, isSystem: isSystem)))
                }
                .frame(width: 8, height: 8)
                .offset(x: isUser ? 6 : -6, y: 4)
            }

            // Message timestamp below bubble
            if let dateStr = message.createdAt {
                Text(DateUtils.formatTimestamp(dateStr))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, Design.Spacing.xs)
            }
        }
        .padding(.vertical, Design.Spacing.xs)
        .frame(maxWidth: .infinity, alignment: isUser ? .trailing : .leading)
    }
    }

    private var thinkingBubble: some View {
        HStack(spacing: 10) {
            ProgressView()
                .scaleEffect(0.9)
            Text("Agent is thinking…")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(Design.Padding.banner)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, Design.Spacing.xs)
    }

    func bubbleBackground(isUser: Bool, isSystem: Bool) -> Color {
        ViewFormatting.bubbleBackground(isUser: isUser, isSystem: isSystem)
    }

    func bubbleTailColor(isUser: Bool, isSystem: Bool) -> Color {
        ViewFormatting.bubbleTailColor(isUser: isUser, isSystem: isSystem)
    }

    // MARK: - Thinking / Reasoning Section

    @ViewBuilder
    private func thinkingSection(_ content: String) -> some View {
        DisclosureGroup {
            Text(content)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, Design.Spacing.xs)
        } label: {
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: "brain")
                    .font(.system(size: Design.IconSize.tiny + 1))
                Text("Thinking")
                    .font(.caption)
                    .fontWeight(.medium)
                Text("(\(content.count) chars)")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .foregroundStyle(.orange)
        }
        .disclosureGroupStyle(PlainDisclosureGroupStyle())
        .padding(Design.Spacing.xs)
        .background(Design.Semantic.warning.opacity(0.06))
        .cornerRadius(Design.CornerRadius.medium)
    }

    // MARK: - Tool Calls Section

    @ViewBuilder
    private func toolCallsSection(_ toolCalls: [ToolCall]) -> some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: Design.Spacing.xs) {
                ForEach(toolCalls) { tc in
                    toolCallRow(tc)
                }
            }
            .padding(.top, Design.Spacing.xs)
        } label: {
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: "wrench.and.screwdriver")
                    .font(.system(size: Design.IconSize.tiny + 1))
                Text("Tool Calls")
                    .font(.caption)
                    .fontWeight(.medium)
                Text("(\(toolCalls.count))")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .foregroundStyle(.purple)
        }
        .disclosureGroupStyle(PlainDisclosureGroupStyle())
        .padding(Design.Spacing.xs)
        .background(Design.Semantic.info.opacity(0.06))
        .cornerRadius(Design.CornerRadius.medium)
    }

    @ViewBuilder
    private func toolCallRow(_ tc: ToolCall) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: "function")
                    .font(.system(size: Design.IconSize.tiny))
                    .foregroundStyle(.purple)
                Text(tc.name)
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.purple)
                if !tc.id.isEmpty {
                    Text(tc.id)
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }

            if let args = tc.arguments, !args.isEmpty {
                Text(formatToolArgs(args))
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .lineLimit(3)
            }
        }
        .padding(Design.Spacing.xs)
        .background(Design.Semantic.info.opacity(0.04))
        .cornerRadius(Design.CornerRadius.small)
    }

    /// Format tool arguments: try to pretty-print JSON, fall back to raw string.
    func formatToolArgs(_ raw: String) -> String { ViewFormatting.formatToolArgs(raw) }

    // MARK: - Role Badge

    private func roleBadge(_ role: String) -> some View {
        HStack(spacing: Design.Spacing.xxs) {
            roleIcon(role)
            Text(role.capitalized)
                .font(.caption2)
                .fontWeight(.semibold)
        }
        .foregroundStyle(StatusColors.messageRole(role))
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(StatusColors.messageRole(role).opacity(0.1))
        .cornerRadius(Design.CornerRadius.small)
    }

    private func roleIcon(_ role: String) -> Image {
        switch role.lowercased() {
        case "user":      return Image(systemName: "person.fill")
        case "assistant": return Image(systemName: "brain.head.profile")
        case "system":    return Image(systemName: "gearshape.fill")
        case "tool":      return Image(systemName: "wrench.fill")
        default:          return Image(systemName: "questionmark")
        }
    }

    func roleColor(_ role: String) -> Color { StatusColors.messageRole(role) }

    // MARK: - Helpers

    /// Convert ISO8601 or RFC3339 timestamp to a human-friendly string.
    /// Recent (< 7d) → relative; older → absolute date.
    private func relativeTimestamp(_ dateStr: String) -> String {
        DateUtils.formatTimestamp(dateStr)
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        do {
            sessions = try await dianeAPI.fetchSessions()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    @MainActor
    private func loadMessages(session: DianeSession) async {
        isLoadingMessages = true
        messagesError = nil
        do {
            messages = try await dianeAPI.fetchSessionMessages(sessionID: session.id)
        } catch {
            messages = []
            messagesError = error.localizedDescription
        }
        isLoadingMessages = false
    }

    @MainActor
    private func loadSessionDetail(session: DianeSession) async {
        isLoadingDetail = true
        do {
            sessionDetail = try await dianeAPI.fetchSessionDetail(sessionID: session.id)
        } catch {
            sessionDetail = nil
        }
        isLoadingDetail = false

        // Load runs and todos in parallel
        async let runsTask: () = loadSessionRuns(session: session)
        async let todosTask: () = loadSessionTodos(session: session)
        _ = await (runsTask, todosTask)
    }

    @MainActor
    private func loadSessionRuns(session: DianeSession) async {
        isLoadingRuns = true
        do {
            let resp = try await dianeAPI.fetchSessionRuns(sessionID: session.id)
            sessionRuns = resp.items
        } catch {
            sessionRuns = []
        }
        isLoadingRuns = false
    }

    @MainActor
    private func loadSessionTodos(session: DianeSession) async {
        isLoadingTodos = true
        do {
            sessionTodos = try await dianeAPI.fetchSessionTodos(sessionID: session.id)
        } catch {
            sessionTodos = []
        }
        isLoadingTodos = false
    }

    // MARK: - Chat Actions

    /// Select a session and load its messages (used by "Resume Recent" button).
    @MainActor
    private func selectSession(_ session: DianeSession) {
        selectedSession = session
        messages = []
        inputText = ""
        error = nil
        Task { await loadMessages(session: session) }
        Task { await loadSessionDetail(session: session) }
    }

    /// Start a new chat — clear session, allow typing in the empty state input bar.
    @MainActor
    private func startNewChat() {
        selectedSession = nil
        sessionDetail = nil
        messages = []
        inputText = ""
        error = nil
        acpSessionID = nil
    }

    /// Close the current session via the API.
    @MainActor
    private func closeCurrentSession() {
        guard let session = selectedSession else { return }
        isSending = true
        Task {
            do {
                try await dianeAPI.closeSession(sessionID: session.id)
                selectedSession = nil
                sessionDetail = nil
                messages = []
                inputText = ""
                acpSessionID = nil
                await load()
            } catch {
                self.error = "Failed to close session: \(error.localizedDescription)"
            }
            isSending = false
        }
    }

    /// Send a message to the current (or new) session via ACP SSE streaming.
    /// Shows tokens progressively as they arrive from the agent.
    @MainActor
    private func sendMessage() async {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }

        inputText = ""
        error = nil

        // 1. Optimistic: show user message immediately
        let userMessage = DianeMessage(
            id: UUID().uuidString,
            role: "user",
            content: text,
            sequenceNumber: nil,
            tokenCount: nil,
            toolCalls: nil,
            reasoningContent: nil,
            createdAt: Self.isoFormatter.string(from: Date())
        )
        messages.append(userMessage)

        // 2. Create a placeholder assistant message (replaced progressively)
        let assistantID = UUID().uuidString
        let assistantMessage = DianeMessage(
            id: assistantID,
            role: "assistant",
            content: "",
            sequenceNumber: nil,
            tokenCount: nil,
            toolCalls: nil,
            reasoningContent: nil,
            createdAt: nil
        )
        messages.append(assistantMessage)
        isSending = true

        var toolCallsBuffer: [ToolCall] = []

        // Ensure we have a local Diane session for persistence
        if selectedSession?.id == nil {
            do {
                let newSession = try await dianeAPI.createSession(title: "Chat")
                selectedSession = newSession
            } catch {
                logDebug("SessionsView: failed to create local session: \(error.localizedDescription)", category: "Sessions")
            }
        }

        do {
            // Direct ACP: create session if needed
            if acpSessionID == nil {
                acpSessionID = try await apiClient.createACPSession(agentName: selectedAgent)
            }
            let stream = apiClient.streamACP(
                agentName: selectedAgent,
                sessionID: acpSessionID!,
                content: text
            )

            for try await event in stream {
                switch event.type {
                case "token":
                    // Append token content to the assistant message
                    if let idx = messages.lastIndex(where: { $0.id == assistantID }),
                       let token = event.content {
                        let current = messages[idx]
                        messages[idx] = DianeMessage(
                            id: current.id,
                            role: current.role,
                            content: current.content + token,
                            sequenceNumber: current.sequenceNumber,
                            tokenCount: current.tokenCount,
                            toolCalls: current.toolCalls,
                            reasoningContent: current.reasoningContent,
                            createdAt: current.createdAt
                        )
                    }

                case "tool_call":
                    if let name = event.name {
                        let tc = ToolCall(id: UUID().uuidString, name: name, arguments: nil)
                        toolCallsBuffer.append(tc)
                        // Update message with tool calls
                        if let idx = messages.lastIndex(where: { $0.id == assistantID }) {
                            let current = messages[idx]
                            messages[idx] = DianeMessage(
                                id: current.id,
                                role: current.role,
                                content: current.content,
                                sequenceNumber: current.sequenceNumber,
                                tokenCount: current.tokenCount,
                                toolCalls: toolCallsBuffer.isEmpty ? nil : toolCallsBuffer,
                                reasoningContent: current.reasoningContent,
                                createdAt: current.createdAt
                            )
                        }
                    }

                case "tool_result":
                    // Tool result — just track, already shown via tool_call
                    break

                case "message":
                    // Final assembled message — update content to full text
                    if let idx = messages.lastIndex(where: { $0.id == assistantID }),
                       let content = event.content, !content.isEmpty {
                        let current = messages[idx]
                        messages[idx] = DianeMessage(
                            id: current.id,
                            role: current.role,
                            content: content,
                            sequenceNumber: current.sequenceNumber,
                            tokenCount: current.tokenCount,
                            toolCalls: current.toolCalls,
                            reasoningContent: current.reasoningContent,
                            createdAt: current.createdAt
                        )
                    }

                case "done":
                    // Fallback: ensure there's visible content, but only if
                    // there are no tool calls (tool-call-only responses are
                    // already displayed via the toolCalls section above).
                    if let idx = messages.lastIndex(where: { $0.id == assistantID }),
                       messages[idx].content.isEmpty {
                        let current = messages[idx]
                        let hasToolCalls = current.toolCalls != nil && !current.toolCalls!.isEmpty
                        if !hasToolCalls {
                            messages[idx] = DianeMessage(
                                id: current.id,
                                role: current.role,
                                content: "✓ Done",
                                sequenceNumber: current.sequenceNumber,
                                tokenCount: current.tokenCount,
                                toolCalls: current.toolCalls,
                                reasoningContent: current.reasoningContent,
                                createdAt: current.createdAt
                            )
                        }
                    }

                case "error":
                    self.error = event.message ?? "Stream error"
                    // Replace assistant message with error
                    messages.removeAll { $0.id == assistantID }
                    let errorMsg = DianeMessage(
                        id: UUID().uuidString,
                        role: "system",
                        content: "⚠️ Error: \(event.message ?? "Unknown error")",
                        sequenceNumber: nil,
                        tokenCount: nil,
                        toolCalls: nil,
                        reasoningContent: nil,
                        createdAt: nil
                    )
                    messages.append(errorMsg)

                default:
                    break
                }
            }

            // Persist messages to local Diane session and update sidebar
            if let sessionID = selectedSession?.id {
                do {
                    // Encode tool calls as JSON for persistence
                    let toolCallsData = try? JSONEncoder().encode(toolCallsBuffer)
                    let toolCallsStr = toolCallsData.flatMap { String(data: $0, encoding: .utf8) }

                    try await dianeAPI.appendSessionMessage(sessionID: sessionID, role: "user", content: text)
                    if let assistantMsg = messages.last(where: { $0.id == assistantID }),
                       !assistantMsg.content.isEmpty {
                        try await dianeAPI.appendSessionMessage(
                            sessionID: sessionID, role: "assistant",
                            content: assistantMsg.content,
                            toolCallsJSON: toolCallsStr
                        )
                    }
                } catch {
                    logDebug("SessionsView: failed to persist messages: \(error.localizedDescription)", category: "Sessions")
                }
                // Reload sessions list to show updated session in sidebar
                await load()
            }

        } catch {
            self.error = error.localizedDescription
            messages.removeAll { $0.id == assistantID }
            let errorMsg = DianeMessage(
                id: UUID().uuidString,
                role: "system",
                content: "⚠️ Error: \(error.localizedDescription)",
                sequenceNumber: nil,
                tokenCount: nil,
                toolCalls: nil,
                reasoningContent: nil,
                createdAt: nil
            )
            messages.append(errorMsg)
        }
        isSending = false
    }

    @MainActor
    private func loadAgentDefs() async {
        do {
            let defs = try await dianeAPI.fetchAgentDefs()
            agentDefs = defs
            if !defs.isEmpty, !defs.contains(where: { $0.name == selectedAgent }) {
                selectedAgent = defs.first?.name ?? "diane-default"
            }
        } catch {
            logDebug("SessionsView: failed to load agent defs: \(error.localizedDescription)", category: "Sessions")
        }
    }
}

// MARK: - Plain Disclosure Group Style

/// A disclosure group style that doesn't add its own indentation or extra styling.
private struct PlainDisclosureGroupStyle: DisclosureGroupStyle {
    func makeBody(configuration: Configuration) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    configuration.isExpanded.toggle()
                }
            } label: {
                HStack(spacing: Design.Spacing.xs) {
                    Image(systemName: configuration.isExpanded ? "chevron.down" : "chevron.right")
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(.secondary)
                    configuration.label
                }
            }
            .buttonStyle(.plain)

            if configuration.isExpanded {
                configuration.content
            }
        }
    }
}

// MARK: - Previews

#Preview {
    SessionsView()
        .environmentObject(AppState())
        .environmentObject(ServerConfiguration())
        .environmentObject(DianeAPIClient())
        .environmentObject(EmergentAPIClient())
        .frame(width: 800, height: 600)
}
