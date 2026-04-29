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
                    EmptyStateView(
                        title: "Select a Session",
                        icon: "message",
                        description: "Select a conversation session to view its transcript."
                    )
                }
            }
        )
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
                Task { await loadSessionDetail(session: s) }
            } else {
                sessionDetail = nil
            }
        }
    }

    private func sessionRow(_ session: DianeSession) -> some View {
        HStack(spacing: 10) {
            // Status indicator
            statusIcon(session.status)
                .font(.system(size: 10))
                .frame(width: 20, height: 20)

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(session.title ?? "Untitled")
                        .font(.subheadline)
                        .fontWeight(.medium)
                        .lineLimit(1)
                    statusBadge(session.status)
                }
                HStack(spacing: 8) {
                    if let count = session.messageCount {
                        HStack(spacing: 3) {
                            Image(systemName: "text.bubble")
                                .font(.system(size: 9))
                            Text("\(count)")
                                .font(.caption2)
                        }
                        .foregroundStyle(.secondary)
                    }
                    if let tokens = session.totalTokens {
                        HStack(spacing: 3) {
                            Image(systemName: "number")
                                .font(.system(size: 9))
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
        .padding(.vertical, 3)
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
            Image(systemName: "circle.dashed")
                .foregroundStyle(.tertiary)
        }
    }

    @ViewBuilder
    private func statusBadge(_ status: String?) -> some View {
        if let s = status, !s.isEmpty {
            Text(s.capitalized)
                .font(.system(size: 9, weight: .medium))
                .foregroundStyle(statusColor(s))
                .padding(.horizontal, 5)
                .padding(.vertical, 1)
                .background(statusColor(s).opacity(0.1))
                .cornerRadius(3)
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
                        .padding(.horizontal, 16)
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
    }

    private func sessionHeader(_ session: DianeSession) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                statusIcon(session.status)
                    .font(.system(size: 14))

                VStack(alignment: .leading, spacing: 3) {
                    Text(session.title ?? "Untitled")
                        .font(.subheadline)
                        .fontWeight(.semibold)
                    HStack(spacing: 8) {
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

                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.top, 12)
            .padding(.bottom, 8)

            // Stats bar
            if let detail = sessionDetail {
                statsBar(detail)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 12)
            } else if isLoadingDetail {
                HStack {
                    ProgressView()
                        .scaleEffect(0.6)
                    Text("Loading stats…")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                .padding(.horizontal, 12)
                .padding(.bottom, 12)
            }
        }
        .background(Color.primary.opacity(0.04))
    }

    @ViewBuilder
    private func statsBar(_ detail: SessionDetailResponse) -> some View {
        let agg = detail.aggregates
        HStack(spacing: 16) {
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
        HStack(spacing: 4) {
            Image(systemName: icon)
                .font(.system(size: 9))
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
        .padding(.vertical, 4)
        .background(Color.primary.opacity(0.05))
        .cornerRadius(5)
    }

    // MARK: - Message Bubble

    @ViewBuilder
    private func messageBubble(_ message: DianeMessage) -> some View {
        let isUser = message.role.lowercased() == "user"
        let isSystem = message.role.lowercased() == "system"

        VStack(alignment: isUser ? .trailing : .leading, spacing: 4) {
            // Role label + sequence
            HStack(spacing: 6) {
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
            .padding(.horizontal, 4)

            // Content bubble
            VStack(alignment: .leading, spacing: 6) {
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
            .padding(10)
            .background(bubbleBackground(isUser: isUser, isSystem: isSystem))
            .cornerRadius(10)
            .overlay(alignment: isUser ? .bottomTrailing : .bottomLeading) {
                BubbleTail(isUser: isUser)
                    .fill(bubbleTailColor(isUser: isUser, isSystem: isSystem))
                    .frame(width: 8, height: 8)
                    .offset(x: isUser ? 6 : -6, y: 4)
            }
        }
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: isUser ? .trailing : .leading)
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
                .padding(.top, 4)
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "brain")
                    .font(.system(size: 10))
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
        .padding(6)
        .background(Color.orange.opacity(0.06))
        .cornerRadius(6)
    }

    // MARK: - Tool Calls Section

    @ViewBuilder
    private func toolCallsSection(_ toolCalls: [ToolCall]) -> some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(toolCalls) { tc in
                    toolCallRow(tc)
                }
            }
            .padding(.top, 4)
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "wrench.and.screwdriver")
                    .font(.system(size: 10))
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
        .padding(6)
        .background(Color.purple.opacity(0.06))
        .cornerRadius(6)
    }

    @ViewBuilder
    private func toolCallRow(_ tc: ToolCall) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: "function")
                    .font(.system(size: 9))
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
        .padding(6)
        .background(Color.purple.opacity(0.04))
        .cornerRadius(4)
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
        HStack(spacing: 3) {
            roleIcon(role)
            Text(role.capitalized)
                .font(.caption2)
                .fontWeight(.semibold)
        }
        .foregroundStyle(roleColor(role))
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(roleColor(role).opacity(0.1))
        .cornerRadius(4)
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

    /// Convert ISO8601 or RFC3339 timestamp to a relative string like "2m ago", "3h ago", "yesterday".
    private func relativeTimestamp(_ dateStr: String) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = formatter.date(from: dateStr)
            ?? ISO8601DateFormatter().date(from: dateStr) else {
            return dateStr
        }
        let interval = -date.timeIntervalSinceNow
        switch interval {
        case ..<60:      return "just now"
        case ..<3600:    return "\(Int(interval / 60))m ago"
        case ..<86400:   return "\(Int(interval / 3600))h ago"
        case ..<172800:  return "yesterday"
        case ..<604800:  return "\(Int(interval / 86400))d ago"
        case ..<2592000: return "\(Int(interval / 604800))w ago"
        default:         return "\(Int(interval / 2592000))mo ago"
        }
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
                HStack(spacing: 4) {
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
