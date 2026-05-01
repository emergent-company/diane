import SwiftUI

/// Sessions view — lists Diane conversation sessions with chat-like message transcripts.
/// Shows session status badges, relative timestamps, collapsible tool calls, and thinking sections.
struct SessionsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var dianeAPI: DianeAPIClient

    @State private var sessions: [DianeSession] = []
    @State private var selectedSession: DianeSession? = nil
    @State private var messages: [DianeMessage] = []
    @State private var sessionDetail: SessionDetailResponse? = nil
    @State private var isLoading = false
    @State private var isLoadingMessages = false
    @State private var isLoadingDetail = false
    @State private var error: String? = nil
    @State private var messagesError: String? = nil

    // Chat state
    @State private var inputText: String = ""
    @State private var isSending = false
    @State private var agentDefs: [AgentDef] = []
    @State private var selectedAgent: String = "diane-default"

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
                .badgeStyle(color: statusColor(s))
        }
    }

    private func statusColor(_ status: String) -> Color {
        switch status.lowercased() {
        case "active", "running": return .green
        case "paused", "idle": return .orange
        case "completed", "closed", "done": return .secondary
        case "error", "failed": return .red
        default: return .secondary
        }
    }

    // MARK: - Session Detail (Chat-like Transcript)

    private func sessionDetailPanel(_ session: DianeSession) -> some View {
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

    /// Session metadata row — truncated session ID and agent name badges.
    @ViewBuilder
    private func sessionMetaRow(_ session: DianeSession) -> some View {
        HStack(spacing: 12) {
            // Session ID — short form with copy button, full ID in tooltip
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: "number")
                    .font(.system(size: Design.IconSize.tiny))
                    .foregroundStyle(.tertiary)
                Text(sessionIDShortForm(session.id))
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.tertiary)
                    .help(session.id)
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(session.id, forType: .string)
                } label: {
                    Image(systemName: "doc.on.doc")
                        .font(.system(size: 9))
                        .foregroundStyle(.tertiary.opacity(0.6))
                }
                .buttonStyle(.plain)
                .help("Copy session ID")
            }

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
    private func sessionIDShortForm(_ id: String) -> String {
        if id.count <= 6 { return id }
        return String(id.suffix(6))
    }

    /// Strip common prefixes from agent names for compact display.
    private func agentShortName(_ name: String) -> String {
        for prefix in ["discord-", "diane-", "agent-"] {
            if name.hasPrefix(prefix) {
                return String(name.dropFirst(prefix.count))
            }
        }
        return name
    }

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
            return AnyView(thinkingBubble)
        }

        return AnyView(
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
                    } else {
                        Text(message.content)
                            .font(.body)
                            .textSelection(.enabled)
                    }
                }
            }
            .padding(Design.Padding.banner)
            .background(bubbleBackground(isUser: isUser, isSystem: isSystem))
            .cornerRadius(Design.CornerRadius.medium)
            .overlay(alignment: isUser ? .bottomTrailing : .bottomLeading) {
                BubbleTail(isUser: isUser)
                    .fill(bubbleTailColor(isUser: isUser, isSystem: isSystem))
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
        )
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

    private func bubbleBackground(isUser: Bool, isSystem: Bool) -> Color {
        if isUser { return Color.blue.opacity(0.12) }
        if isSystem { return Color.clear }
        return Color.primary.opacity(0.05)
    }

    private func bubbleTailColor(isUser: Bool, isSystem: Bool) -> Color {
        if isUser { return Color.blue.opacity(0.12) }
        if isSystem { return Color.clear }
        return Color.primary.opacity(0.05)
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
    private func formatToolArgs(_ raw: String) -> String {
        guard let data = raw.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data),
              let pretty = try? JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys, .withoutEscapingSlashes]),
              let str = String(data: pretty, encoding: .utf8)
        else { return raw }
        return str
    }

    // MARK: - Role Badge

    private func roleBadge(_ role: String) -> some View {
        HStack(spacing: Design.Spacing.xxs) {
            roleIcon(role)
            Text(role.capitalized)
                .font(.caption2)
                .fontWeight(.semibold)
        }
        .foregroundStyle(roleColor(role))
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(roleColor(role).opacity(0.1))
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

    private func roleColor(_ role: String) -> Color {
        switch role.lowercased() {
        case "user":      return .blue
        case "assistant": return .green
        case "system":    return .orange
        case "tool":      return .purple
        default:          return .secondary
        }
    }

    // MARK: - Helpers

    /// Convert ISO8601 or RFC3339 timestamp to a human-friendly string.
    /// Recent (< 7d) → relative; older → absolute date.
    private func relativeTimestamp(_ dateStr: String) -> String {
        DateUtils.formatTimestamp(dateStr)
    }

    /// Format large token counts: "1.5K", "12K", "1.2M".
    private func formatTokenCount(_ count: Int) -> String {
        switch count {
        case 0..<1000:   return "\(count)"
        case 1000..<1_000_000:
            let k = Double(count) / 1000
            return k >= 100 ? "\(Int(k))K" : String(format: "%.1fK", k)
        default:
            let m = Double(count) / 1_000_000
            return m >= 10 ? "\(Int(m))M" : String(format: "%.1fM", m)
        }
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
                await load()
            } catch {
                self.error = "Failed to close session: \(error.localizedDescription)"
            }
            isSending = false
        }
    }

    /// Send a message to the current (or new) session and show the agent's response.
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
            createdAt: ISO8601DateFormatter().string(from: Date())
        )
        messages.append(userMessage)

        // 2. Add a "thinking" agent bubble while we wait
        let thinkingID = "thinking-\(UUID().uuidString.prefix(8))"
        let thinkingMessage = DianeMessage(
            id: thinkingID,
            role: "assistant",
            content: "",
            sequenceNumber: nil,
            tokenCount: nil,
            toolCalls: nil,
            reasoningContent: nil,
            createdAt: nil
        )
        messages.append(thinkingMessage)
        isSending = true

        // Determine session ID: use existing or nil for new session
        let currentID = selectedSession?.id

        do {
            let response = try await dianeAPI.sendChatMessage(
                sessionID: currentID,
                content: text,
                agentName: selectedAgent
            )

            // Update selection to the new/existing session
            if selectedSession == nil {
                // Refresh session list to pick up the new session
                await load()
                // Find newly created session in list
                if let newSession = sessions.first(where: { $0.id == response.sessionID }) {
                    selectedSession = newSession
                }
            }

            // 3. Remove thinking placeholder
            messages.removeAll { $0.id == thinkingID }

            // 4. Append response messages (skip user messages — we already have it)
            var inserted = false
            for msg in response.messages {
                if msg.role == "user" { continue }
                if !msg.content.isEmpty || msg.reasoningContent != nil || msg.toolCalls != nil {
                    messages.append(msg)
                    inserted = true
                }
            }

            // 5. Fallback if no substantive response
            if !inserted {
                let fallback = DianeMessage(
                    id: UUID().uuidString,
                    role: "assistant",
                    content: "✓ Done",
                    sequenceNumber: nil,
                    tokenCount: nil,
                    toolCalls: nil,
                    reasoningContent: nil,
                    createdAt: nil
                )
                messages.append(fallback)
            }
        } catch {
            self.error = error.localizedDescription
            // Replace thinking bubble with error
            messages.removeAll { $0.id == thinkingID }
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

    private func formatCost(_ usd: Double) -> String {
        if usd >= 100 {
            return String(format: "$%.2f", usd)
        } else if usd >= 1 {
            return String(format: "$%.3f", usd)
        } else if usd >= 0.001 {
            return String(format: "%.1f¢", usd * 100)
        } else {
            return String(format: "%.2f¢", usd * 100)
        }
    }
}

// MARK: - Bubble Tail Shape

/// A small triangular tail that points to the message sender.
private struct BubbleTail: Shape {
    let isUser: Bool

    func path(in rect: CGRect) -> Path {
        var path = Path()
        if isUser {
            path.move(to: CGPoint(x: 0, y: 0))
            path.addLine(to: CGPoint(x: rect.width, y: 0))
            path.addLine(to: CGPoint(x: rect.width, y: rect.height))
        } else {
            path.move(to: CGPoint(x: 0, y: rect.height))
            path.addLine(to: CGPoint(x: rect.width, y: 0))
            path.addLine(to: CGPoint(x: 0, y: 0))
        }
        path.closeSubpath()
        return path
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
        .frame(width: 800, height: 600)
}
