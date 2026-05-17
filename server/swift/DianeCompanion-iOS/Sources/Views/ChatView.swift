import SwiftUI
import DianeShared
import Foundation

// MARK: - Tool Call View (compact, args hidden by default)

struct ToolCallView: View {
    let toolCall: DianeMessage.ToolCall
    @State private var showDetails = false

    var body: some View {
        VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
            Button(action: {
                withAnimation(.easeInOut(duration: 0.2)) {
                    showDetails.toggle()
                }
            }) {
                HStack(spacing: DesignTokens.spacingXS) {
                    Image(systemName: "wrench.and.screwdriver")
                        .font(.caption2)
                        .foregroundColor(.purple)
                    Text(toolCall.name)
                        .font(.caption2.monospaced())
                        .fontWeight(.semibold)
                        .foregroundColor(.purple)
                        .lineLimit(1)
                    if toolCall.arguments != nil || toolCall.result != nil {
                        Image(systemName: showDetails ? "chevron.down" : "chevron.right")
                            .font(.caption2)
                            .foregroundColor(.purple.opacity(0.6))
                    }
                }
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("toolcall-\(toolCall.name)")

            if showDetails {
                if let args = toolCall.arguments, !args.isEmpty {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Arguments")
                            .font(.system(size: 9).monospaced())
                            .foregroundColor(.secondary)
                        Text(args)
                            .font(.system(size: 9).monospaced())
                            .foregroundColor(.secondary)
                            .textSelection(.enabled)
                    }
                    .padding(.leading, DesignTokens.spacingMD)
                }

                if let result = toolCall.result, !result.isEmpty {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Result")
                            .font(.system(size: 9).monospaced())
                            .foregroundColor(.secondary)
                        Text(result)
                            .font(.system(size: 9).monospaced())
                            .foregroundColor(.secondary)
                            .lineLimit(8)
                            .textSelection(.enabled)
                    }
                    .padding(.leading, DesignTokens.spacingMD)
                }
            }
        }
        .padding(.vertical, 4)
        .padding(.horizontal, DesignTokens.spacingSM)
        .background(Color.purple.opacity(0.06))
        .cornerRadius(DesignTokens.radiusSM)
    }
}

// MARK: - Expanded Tool Call View (for detail sheet)

struct ExpandedToolCallView: View {
    let toolCall: DianeMessage.ToolCall

    var body: some View {
        VStack(alignment: .leading, spacing: DesignTokens.spacingSM) {
            HStack(spacing: DesignTokens.spacingXS) {
                Image(systemName: "wrench.and.screwdriver")
                    .font(.subheadline)
                    .foregroundColor(.purple)
                Text(toolCall.name)
                    .font(.body.monospaced())
                    .fontWeight(.semibold)
                    .foregroundColor(.purple)
            }

            if let args = toolCall.arguments, !args.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Arguments")
                        .font(.caption.monospaced())
                        .fontWeight(.medium)
                        .foregroundColor(.secondary)
                    Text(args)
                        .font(.caption.monospaced())
                        .foregroundColor(.primary)
                        .textSelection(.enabled)
                }
            }

            if let result = toolCall.result, !result.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Result")
                        .font(.caption.monospaced())
                        .fontWeight(.medium)
                        .foregroundColor(.secondary)
                    ScrollView(.vertical) {
                        Text(result)
                            .font(.caption.monospaced())
                            .foregroundColor(.primary)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .frame(maxHeight: 300)
                }
            }
        }
        .padding()
        .background(Color.purple.opacity(0.06))
        .cornerRadius(DesignTokens.radiusMD)
    }
}

// MARK: - Reasoning Section

struct ReasoningSection: View {
    let content: String
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
            Button(action: { withAnimation(.easeInOut(duration: 0.2)) { isExpanded.toggle() } }) {
                HStack(spacing: DesignTokens.spacingXS) {
                    Image(systemName: "brain.head.profile")
                        .font(.caption)
                        .foregroundColor(.orange)
                    Text("Reasoning")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundColor(.orange)
                    Spacer()
                    Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                        .font(.caption2)
                        .foregroundColor(.orange.opacity(0.7))
                }
            }
            .buttonStyle(.plain)

            if isExpanded {
                Text(content)
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .lineLimit(nil)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(DesignTokens.spacingSM)
        .background(Color.orange.opacity(0.06))
        .cornerRadius(DesignTokens.radiusMD)
    }
}

// MARK: - Error Message Styling

struct ErrorMessageView: View {
    let message: String

    var body: some View {
        HStack {
            Spacer(minLength: 60)

            VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
                HStack(spacing: DesignTokens.spacingXS) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundColor(.red)
                    Text("Error")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .foregroundColor(.red)
                }

                Text(message)
                    .font(.body)
                    .foregroundColor(.primary)
            }
            .padding(DesignTokens.spacingMD)
            .background(Color.red.opacity(0.08))
            .cornerRadius(DesignTokens.radiusLG)

            Spacer(minLength: 60)
        }
        .padding(.horizontal, DesignTokens.spacingMD)
        .padding(.vertical, DesignTokens.spacingXXS)
    }
}

// MARK: - Message Detail Sheet

struct MessageDetailSheet: View {
    let message: DianeMessage
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: DesignTokens.spacingMD) {
                    // Metadata header
                    VStack(alignment: .leading, spacing: 4) {
                        LabeledContent("Role", value: message.role)
                        if let createdAt = message.createdAt {
                            LabeledContent("Time", value: DateUtils.formatShort(createdAt))
                        }
                        LabeledContent("ID", value: message.id)
                            .font(.caption.monospaced())
                    }
                    .accessibilityIdentifier("message-detail-metadata")
                    .padding()
                    .background(Color(.secondarySystemGroupedBackground))
                    .cornerRadius(DesignTokens.radiusMD)

                    // Content
                    if !message.content.isEmpty {
                        VStack(alignment: .leading, spacing: DesignTokens.spacingXS) {
                            Text("Content")
                                .font(.caption.monospaced())
                                .fontWeight(.medium)
                                .foregroundColor(.secondary)

                            Text(message.content)
                                .font(.body)
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .padding()
                        .background(Color(.secondarySystemGroupedBackground))
                        .cornerRadius(DesignTokens.radiusMD)
                    }

                    // Reasoning
                    if let reasoning = message.reasoningContent, !reasoning.isEmpty {
                        VStack(alignment: .leading, spacing: DesignTokens.spacingXS) {
                            Text("Reasoning")
                                .font(.caption.monospaced())
                                .fontWeight(.medium)
                                .foregroundColor(.secondary)
                            Text(reasoning)
                                .font(.caption)
                                .foregroundColor(.primary)
                                .textSelection(.enabled)
                        }
                        .padding()
                        .background(Color(.secondarySystemGroupedBackground))
                        .cornerRadius(DesignTokens.radiusMD)
                    }

                    // Tool Calls
                    if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
                        Text("Tool Calls (\(toolCalls.count))")
                            .font(.caption.monospaced())
                            .fontWeight(.medium)
                            .foregroundColor(.secondary)

                        ForEach(toolCalls, id: \.name) { tc in
                            ExpandedToolCallView(toolCall: tc)
                        }
                    }

                    // Copy button
                    Button(action: {
                        UIPasteboard.general.string = fullText
                    }) {
                        Label("Copy Message Text", systemImage: "doc.on.doc")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.bordered)
                    .accessibilityIdentifier("copy-message-button")
                    .padding(.top)
                }
                .padding()
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle("Message Details")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }

    /// Full plain text of the message for copying.
    private var fullText: String {
        var parts: [String] = []
        if !message.content.isEmpty {
            parts.append(message.content)
        }
        if let reasoning = message.reasoningContent, !reasoning.isEmpty {
            parts.append("[Reasoning]\n\(reasoning)")
        }
        if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
            for tc in toolCalls {
                var tcParts = ["[Tool: \(tc.name)]"]
                if let args = tc.arguments, !args.isEmpty {
                    tcParts.append("Args: \(args)")
                }
                if let result = tc.result, !result.isEmpty {
                    tcParts.append("Result: \(result)")
                }
                parts.append(tcParts.joined(separator: "\n"))
            }
        }
        return parts.joined(separator: "\n\n")
    }
}

// MARK: - Session Detail Sheet

struct SessionDetailSheet: View {
    let session: DianeSession
    let onDelete: () -> Void
    let onArchive: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var showDeleteConfirm = false

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: DesignTokens.spacingMD) {
                    // Session info
                    VStack(alignment: .leading, spacing: 8) {
                        if let title = session.title {
                            Text(title)
                                .font(.title2)
                                .fontWeight(.bold)
                        }

                        if let agent = session.agentName {
                            Label(agent, systemImage: "person.circle")
                                .font(.subheadline)
                                .foregroundColor(.secondary)
                        }
                    }
                    .padding()
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.secondarySystemGroupedBackground))
                    .cornerRadius(DesignTokens.radiusMD)

                    // Stats grid
                    LazyVGrid(columns: [
                        GridItem(.flexible()),
                        GridItem(.flexible())
                    ], spacing: DesignTokens.spacingMD) {
                        StatCard(
                            title: "Run Count",
                            value: "\(session.runCount ?? 0)",
                            icon: "arrow.triangle.2.circlepath"
                        )
                        StatCard(
                            title: "Messages",
                            value: "\(session.messageCount ?? 0)",
                            icon: "bubble.left.and.bubble.right"
                        )
                        StatCard(
                            title: "Tokens",
                            value: formattedTokens,
                            icon: "number"
                        )
                        StatCard(
                            title: "Cost",
                            value: formattedCost,
                            icon: "dollarsign.circle"
                        )
                    }

                    // Status
                    if let status = session.lastRunStatus ?? session.status {
                        HStack {
                            Image(systemName: "info.circle")
                                .font(.subheadline)
                                .foregroundColor(.secondary)
                            Text("Status: ")
                                .font(.subheadline)
                                .foregroundColor(.secondary)
                            Text(status.capitalized)
                                .font(.subheadline.monospaced())
                                .fontWeight(.medium)
                        }
                        .padding()
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color(.secondarySystemGroupedBackground))
                        .cornerRadius(DesignTokens.radiusMD)
                    }

                    // Timestamps
                    VStack(alignment: .leading, spacing: 4) {
                        if let created = session.createdAt {
                            LabeledContent("Created", value: DateUtils.formatShort(created))
                        }
                        if let updated = session.updatedAt {
                            LabeledContent("Updated", value: DateUtils.formatShort(updated))
                        }
                    }
                    .padding()
                    .background(Color(.secondarySystemGroupedBackground))
                    .cornerRadius(DesignTokens.radiusMD)

                    // Action buttons
                    VStack(spacing: DesignTokens.spacingSM) {
                        Button(role: .destructive) {
                            showDeleteConfirm = true
                        } label: {
                            Label("Delete Session", systemImage: "trash")
                                .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                        .tint(.red)
                        .accessibilityIdentifier("delete-session-button")
                    }
                    .padding(.top)
                }
                .padding()
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle("Session Details")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .alert("Delete Session", isPresented: $showDeleteConfirm) {
                Button("Cancel", role: .cancel) {}
                Button("Delete", role: .destructive) {
                    onDelete()
                    dismiss()
                }
            } message: {
                Text("Are you sure you want to delete this session? This cannot be undone.")
            }
        }
    }

    private var formattedTokens: String {
        guard let tokens = session.totalTokens else { return "—" }
        if tokens < 1000 { return "\(tokens)" }
        let k = Double(tokens) / 1000.0
        return String(format: "%.1fK", k)
    }

    private var formattedCost: String {
        guard let cost = session.totalCostUsd, cost > 0 else { return "—" }
        if cost < 0.01 {
            return String(format: "%.4f¢", cost * 100)
        }
        return String(format: "$%.4f", cost)
    }
}

// MARK: - Stat Card

private struct StatCard: View {
    let title: String
    let value: String
    let icon: String

    var body: some View {
        VStack(spacing: DesignTokens.spacingXS) {
            Image(systemName: icon)
                .font(.title3)
                .foregroundColor(.accentColor)
            Text(value)
                .font(.title3.monospaced())
                .fontWeight(.semibold)
            Text(title)
                .font(.caption)
                .foregroundColor(.secondary)
        }
        .padding()
        .frame(maxWidth: .infinity)
        .background(Color(.secondarySystemGroupedBackground))
        .cornerRadius(DesignTokens.radiusMD)
        .accessibilityIdentifier("stat-\(title.lowercased().replacingOccurrences(of: " ", with: "-"))")
    }
}

// MARK: - Message Bubble Content

/// Renders a message bubble with tool calls, reasoning, markdown content, and streaming cursor.
private struct MessageBubbleContent: View {
    let message: DianeMessage
    let isStreaming: Bool
    let onTap: () -> Void

    private var isUser: Bool { message.role == "user" }

    private var bubbleColor: Color {
        isUser ? Color.accentColor : Color(.secondarySystemBackground)
    }

    private var textColor: Color {
        isUser ? .white : .primary
    }

    var body: some View {
        Button(action: onTap) {
            HStack(alignment: .bottom, spacing: DesignTokens.spacingSM) {
                if isUser { Spacer(minLength: 60) }

                VStack(alignment: isUser ? .trailing : .leading, spacing: DesignTokens.spacingXS) {
                    // Tool calls appear ABOVE the response text
                    if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
                        VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
                            ForEach(toolCalls, id: \.name) { tc in
                                ToolCallView(toolCall: tc)
                            }
                        }
                    }

                    // Content — auto-detects markdown, renders with Textual (iOS 18+) or AttributedString fallback
                    MessageContentView(content: message.content, isUser: isUser)
                        .foregroundColor(textColor)

                    // Streaming cursor
                    if isStreaming {
                        HStack(spacing: 0) {
                            Text("\u{258D}")
                                .font(.body)
                                .foregroundColor(textColor)
                                .opacity(0.6)
                        }
                    }

                    // Reasoning section
                    if let reasoning = message.reasoningContent, !reasoning.isEmpty {
                        ReasoningSection(content: reasoning)
                    }

                    // Timestamp
                    if let createdAt = message.createdAt {
                        Text(DateUtils.formatTime(createdAt))
                            .font(.caption2)
                            .foregroundColor(isUser ? .white.opacity(0.7) : .secondary.opacity(0.7))
                    }
                }
                .padding(DesignTokens.spacingMD)
                .background(bubbleColor)
                .cornerRadius(DesignTokens.radiusLG)

                if !isUser { Spacer(minLength: 60) }
            }
            .padding(.horizontal, DesignTokens.spacingMD)
            .padding(.vertical, DesignTokens.spacingXXS)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("bubble-\(message.id)")
    }
}

// MARK: - Chat Input Bar

struct ChatInputBar: View {
    @Binding var text: String
    let isStreaming: Bool
    let onSend: () -> Void
    let onStop: () -> Void
    let onPickFile: () -> Void
    let hasPendingAttachment: Bool
    let pendingAttachmentName: String?

    var body: some View {
        VStack(spacing: 4) {
            // Pending attachment indicator
            if let name = pendingAttachmentName, hasPendingAttachment {
                HStack(spacing: DesignTokens.spacingXS) {
                    Image(systemName: "doc.fill")
                        .font(.caption)
                        .foregroundColor(.accentColor)
                    Text(name)
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .lineLimit(1)
                    Spacer()
                }
                .padding(.horizontal, DesignTokens.spacingMD)
            }

            HStack(spacing: DesignTokens.spacingSM) {
                // File picker button
                Button(action: onPickFile) {
                    Image(systemName: "plus.circle.fill")
                        .font(.title2)
                        .foregroundStyle(Color.secondary)
                        .frame(width: DesignTokens.minTouchTarget, height: DesignTokens.minTouchTarget)
                }
                .disabled(isStreaming)

                // Text input
                TextField("Message Diane...", text: $text, axis: .vertical)
                    .textFieldStyle(.plain)
                    .padding(DesignTokens.spacingMD)
                    .background(Color(.systemGray6))
                    .cornerRadius(DesignTokens.radiusXL)
                    .lineLimit(1...6)
                    .disabled(isStreaming)

                // Send / Stop button
                Button(action: isStreaming ? onStop : onSend) {
                    Image(systemName: isStreaming ? "stop.circle.fill" : "arrow.up.circle.fill")
                        .font(.title2)
                        .foregroundStyle(
                            (text.trimmingCharacters(in: .whitespaces).isEmpty && !isStreaming)
                                ? Color.secondary
                                : Color.accentColor
                        )
                        .frame(width: DesignTokens.minTouchTarget, height: DesignTokens.minTouchTarget)
                }
                .disabled(text.trimmingCharacters(in: .whitespaces).isEmpty && !isStreaming)
            }
            .padding(.horizontal, DesignTokens.spacingMD)
            .padding(.vertical, DesignTokens.spacingSM)
            .background(Color(.systemBackground))
        }
    }
}

// MARK: - ChatView

struct ChatView: View {
    @Environment(\.cloudClient) private var cloudClient
    @Environment(\.config) private var config
    let session: DianeSession
    @StateObject private var archiveStore = ArchivedSessionsStore.shared

    // MARK: State

    @State private var messages: [DianeMessage] = []
    @State private var isLoading = true
    @State private var isStreaming = false
    @State private var error: String?
    @State private var streamingTask: Task<Void, Never>?

    // Session detail state (enriched from API)
    @State private var enrichedSession: DianeSession?

    // Sheet state
    @State private var selectedMessage: DianeMessage?
    @State private var showMessageDetail = false
    @State private var showSessionDetail = false

    // File upload state
    @State private var showFilePicker = false
    @State private var isUploading = false
    @State private var pendingDocumentID: String?
    @State private var pendingDocumentName: String?

    // Input text
    @State private var messageText = ""

    // MARK: Computed

    private var streamingMessageID: String? {
        messages.last(where: { $0.role == "assistant" && isStreaming })?.id
    }

    /// The session object to display (enriched or original).
    private var displaySession: DianeSession {
        enrichedSession ?? session
    }

    // MARK: Body

    var body: some View {
        VStack(spacing: 0) {
            // Content area
            if isLoading && messages.isEmpty {
                ProgressView("Loading messages...")
                    .frame(maxHeight: .infinity)
            } else if let err = error, messages.isEmpty {
                VStack(spacing: DesignTokens.spacingMD) {
                    Spacer()
                    Image(systemName: "exclamationmark.bubble")
                        .font(.largeTitle)
                        .foregroundColor(.secondary)
                    Text("Could not load messages")
                        .font(.headline)
                    Text(err)
                        .font(.subheadline)
                        .foregroundColor(.secondary)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal)
                    Button("Retry") {
                        Task { await loadMessages() }
                    }
                    .buttonStyle(.bordered)
                    Spacer()
                }
                .frame(maxHeight: .infinity)
            } else {
                // Uploading progress bar
                if isUploading {
                    HStack(spacing: DesignTokens.spacingSM) {
                        ProgressView()
                            .scaleEffect(0.8)
                        Text("Uploading file\u{2026}")
                            .font(.caption)
                            .foregroundColor(.secondary)
                        Spacer()
                    }
                    .padding(.horizontal, DesignTokens.spacingMD)
                    .padding(.vertical, 4)
                }

                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            ForEach(messages) { msg in
                                messageContent(for: msg.id)
                                    .id(msg.id)
                            }
                        }
                    }
                    .scrollDismissesKeyboard(.immediately)
                    .onChange(of: messages.count) { _, _ in
                        if let lastId = messages.last?.id {
                            withAnimation {
                                proxy.scrollTo(lastId, anchor: .bottom)
                            }
                        }
                    }
                }
                .frame(maxHeight: .infinity)

                // Input bar pinned to bottom
                ChatInputBar(
                    text: $messageText,
                    isStreaming: isStreaming,
                    onSend: {
                        let pending = messageText.trimmingCharacters(
                            in: .whitespacesAndNewlines
                        )
                        guard !pending.isEmpty else { return }
                        sendMessage(text: pending)
                        messageText = ""
                    },
                    onStop: { stopStreaming() },
                    onPickFile: { showFilePicker = true },
                    hasPendingAttachment: pendingDocumentName != nil,
                    pendingAttachmentName: pendingDocumentName
                )
            }
        }
        .navigationTitle(session.title ?? "Chat")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Button(action: { showSessionDetail = true }) {
                    VStack(spacing: 2) {
                        Text(session.title ?? "Chat")
                            .font(.headline)
                            .lineLimit(1)
                        if let agentName = session.agentName {
                            Text(agentName)
                                .font(.caption2)
                                .foregroundColor(.secondary)
                                .lineLimit(1)
                        }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("chat-title-button")
            }
        }
        .task { await loadMessages() }
        .onDisappear { stopStreaming() }
        .fileImporter(
            isPresented: $showFilePicker,
            allowedContentTypes: [.data, .pdf, .text, .plainText, .image, .movie, .audio],
            allowsMultipleSelection: false
        ) { result in
            switch result {
            case .success(let urls):
                guard let url = urls.first else { return }
                Task { await uploadAndAttach(url: url) }
            case .failure:
                break
            }
        }
        // Message detail sheet
        .sheet(isPresented: $showMessageDetail) {
            if let msg = selectedMessage {
                MessageDetailSheet(message: msg)
            }
        }
        // Session detail sheet
        .sheet(isPresented: $showSessionDetail) {
            SessionDetailSheet(
                session: displaySession,
                onDelete: {
                    Task { await deleteSession() }
                },
                onArchive: {
                    archiveStore.toggleArchive(sessionID: session.id)
                }
            )
        }
        .sentryView("ChatView")
    }

    // MARK: - Actions

    private func deleteSession() async {
        // Remove locally
        SessionCache.shared.cacheMessages([], for: session.id)

        // Attempt remote deletion (best-effort)
        do {
            try await cloudClient.http.delete("/acp/v1/sessions/\(session.id)")
        } catch {
            #if DEBUG
            print("[Diane] Remote session deletion failed: \(error.localizedDescription)")
            #endif
        }
    }

    // MARK: - Message Content Builder

    @ViewBuilder
    private func messageContent(for id: String) -> some View {
        if let dianeMsg = messages.first(where: { $0.id == id }) {
            if dianeMsg.role == "error" {
                ErrorMessageView(message: dianeMsg.content)
            } else {
                MessageBubbleContent(
                    message: dianeMsg,
                    isStreaming: dianeMsg.id == streamingMessageID && isStreaming,
                    onTap: {
                        selectedMessage = dianeMsg
                        showMessageDetail = true
                    }
                )
            }
        }
    }

    // MARK: - Load Messages

    private func loadMessages() async {
        isLoading = true
        error = nil

        // 1. Load from local cache first (instant)
        let cached = SessionCache.shared.loadCachedMessages(for: session.id)
        messages = cached
        isLoading = false

        // 2. Try to fetch session detail from ACP for remote run history
        do {
            let data = try await cloudClient.http.get("/acp/v1/sessions/\(session.id)")
            guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                SessionCache.shared.markRead(sessionID: session.id)
                return
            }

            // Enrich session metadata from the detail response
            enrichSession(from: json)

            guard let history = json["history"] as? [[String: Any]] else {
                SessionCache.shared.markRead(sessionID: session.id)
                return
            }

            // Extract messages from run history
            var remoteMessages: [DianeMessage] = []
            for run in history {
                // User message from trigger_message
                if let trigger = run["trigger_message"] as? String, !trigger.isEmpty {
                    let runID = run["run_id"] as? String ?? UUID().uuidString
                    let createdAt = run["created_at"] as? String ?? DateUtils.formatISO8601()
                    remoteMessages.append(DianeMessage(
                        id: "\(runID)-user",
                        role: "user",
                        content: trigger,
                        createdAt: createdAt
                    ))
                }

                // Assistant messages from events
                if let events = run["events"] as? [[String: Any]] {
                    var assistantText = ""
                    var toolCalls: [DianeMessage.ToolCall] = []
                    var eventCreatedAt: String?

                    for event in events {
                        eventCreatedAt = event["created_at"] as? String ?? eventCreatedAt
                        guard let eventType = event["type"] as? String else { continue }
                        guard let eventData = event["data"] as? [String: Any] else { continue }

                        if eventType == "message.part" {
                            guard let part = eventData["part"] as? [String: Any] else { continue }
                            let contentType = part["content_type"] as? String ?? ""

                            if contentType == "text/plain",
                               let content = part["content"] as? String {
                                assistantText += content
                            }

                            if let meta = part["metadata"] as? [String: Any],
                               let kind = meta["kind"] as? String, kind == "trajectory",
                               let toolName = meta["tool_name"] as? String {
                                let toolInput: String? = {
                                    if let input = meta["tool_input"] {
                                        if let d = try? JSONSerialization.data(withJSONObject: input, options: .fragmentsAllowed) {
                                            return String(data: d, encoding: .utf8)
                                        }
                                    }
                                    return nil
                                }()
                                let toolOutput: String? = {
                                    if let output = meta["tool_output"] {
                                        if let d = try? JSONSerialization.data(withJSONObject: output, options: .fragmentsAllowed) {
                                            return String(data: d, encoding: .utf8)
                                        }
                                    }
                                    return nil
                                }()
                                toolCalls.append(DianeMessage.ToolCall(
                                    name: toolName,
                                    arguments: toolInput,
                                    result: toolOutput
                                ))
                            }
                        }
                    }

                    if !assistantText.isEmpty || !toolCalls.isEmpty {
                        let runID = run["run_id"] as? String ?? UUID().uuidString
                        let runCreatedAt = run["completed_at"] as? String ?? eventCreatedAt ?? DateUtils.formatISO8601()
                        remoteMessages.append(DianeMessage(
                            id: "\(runID)-assistant",
                            role: "assistant",
                            content: assistantText,
                            createdAt: runCreatedAt,
                            toolCalls: toolCalls.isEmpty ? nil : toolCalls
                        ))
                    }
                }
            }

            // Merge: prefer remote messages, keep any cached messages not from remote
            if !remoteMessages.isEmpty {
                await MainActor.run {
                    // Only use remote messages (they're authoritative)
                    // But preserve any messages that were sent locally and not yet on server
                    let remoteIDs = Set(remoteMessages.map(\.id))
                    let localOnly = cached.filter { !remoteIDs.contains($0.id) }
                    messages = (remoteMessages + localOnly).sorted { a, b in
                        let dateA = DateUtils.parseISO8601(a.createdAt) ?? .distantPast
                        let dateB = DateUtils.parseISO8601(b.createdAt) ?? .distantPast
                        return dateA < dateB
                    }
                    SessionCache.shared.cacheMessages(messages, for: session.id)
                }
            }

            SessionCache.shared.markRead(sessionID: session.id)
        } catch {
            #if DEBUG
            print("[Diane] Failed to fetch remote session: \(error.localizedDescription)")
            #endif
        }
    }

    /// Enrich the session display data from the ACP session detail JSON.
    private func enrichSession(from json: [String: Any]?) {
        guard let json else { return }
        let tokens = json["total_tokens"] as? Int
        let cost = json["total_cost_usd"] as? Double
        let runCount = json["run_count"] as? Int
        let messageCount = json["message_count"] as? Int
        let lastRunStatus = json["last_run_status"] as? String

        if tokens != nil || cost != nil || runCount != nil {
            enrichedSession = DianeSession(
                id: session.id,
                title: session.title,
                status: session.status,
                messageCount: messageCount ?? session.messageCount,
                runCount: runCount ?? session.runCount,
                totalTokens: tokens ?? session.totalTokens,
                totalCostUsd: cost ?? session.totalCostUsd,
                lastRunStatus: lastRunStatus ?? session.lastRunStatus,
                entityID: session.entityID,
                createdAt: session.createdAt,
                updatedAt: session.updatedAt,
                agentName: session.agentName
            )
        }
    }

    // MARK: - File Upload

    /// Upload a file to the Memory Platform and attach it as a pending document.
    private func uploadAndAttach(url: URL) async {
        guard url.startAccessingSecurityScopedResource() else { return }
        defer { url.stopAccessingSecurityScopedResource() }

        isUploading = true
        defer { isUploading = false }

        do {
            let fileData = try Data(contentsOf: url)
            let filename = url.lastPathComponent
            let mimeType = mimeTypeForFile(filename)

            let projectID: String
            if !config.projectID.isEmpty {
                projectID = config.projectID
            } else {
                projectID = ""
            }

            guard !projectID.isEmpty else {
                let errMsg = DianeMessage(
                    id: "error-\(UUID().uuidString)",
                    role: "error",
                    content: "Cannot upload file: no project ID configured. Add your project ID in Settings.",
                    createdAt: DateUtils.formatISO8601()
                )
                await MainActor.run { messages.append(errMsg) }
                return
            }

            let document = try await cloudClient.uploadDocument(
                fileData: fileData,
                filename: filename,
                mimeType: mimeType,
                projectID: projectID,
                autoExtract: true
            )

            await MainActor.run {
                pendingDocumentID = document.id
                pendingDocumentName = document.title ?? filename
            }
        } catch {
            let errMsg = DianeMessage(
                id: "error-\(UUID().uuidString)",
                role: "error",
                content: "Upload failed: \(error.localizedDescription)",
                createdAt: DateUtils.formatISO8601()
            )
            await MainActor.run { messages.append(errMsg) }
        }
    }

    /// Guess a MIME type from file extension for upload.
    private func mimeTypeForFile(_ filename: String) -> String {
        let ext = (filename as NSString).pathExtension.lowercased()
        switch ext {
        case "pdf":             return "application/pdf"
        case "md", "markdown":  return "text/markdown"
        case "txt":             return "text/plain"
        case "rtf":             return "application/rtf"
        case "json":            return "application/json"
        case "csv":             return "text/csv"
        case "xml":             return "application/xml"
        case "html", "htm":     return "text/html"
        case "png":             return "image/png"
        case "jpg", "jpeg":     return "image/jpeg"
        case "gif":             return "image/gif"
        case "svg":             return "image/svg+xml"
        case "docx":            return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
        case "doc":             return "application/msword"
        case "xlsx":            return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
        case "ppt", "pptx":     return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
        default:                return "application/octet-stream"
        }
    }

    // MARK: - Send Message

    private func sendMessage(text: String) {
        guard !isStreaming else { return }

        // Build message content — prepend document reference if uploading
        var messageContent = text
        if let docName = pendingDocumentName {
            let prefix = text.isEmpty
                ? "\u{1F4C4} Uploaded: \(docName)"
                : "[\u{1F4C4} \(docName)] "
            messageContent = prefix + (text.isEmpty ? "" : "\n\n" + text)
        }

        guard !messageContent.trimmingCharacters(in: .whitespaces).isEmpty else {
            if pendingDocumentName != nil {
                // Restore? Not needed with ExyteChat binding — text was already cleared
            }
            return
        }

        // Clear pending attachment
        pendingDocumentName = nil
        pendingDocumentID = nil

        // Create user message
        let userMsg = DianeMessage(
            id: "user-\(UUID().uuidString)",
            role: "user",
            content: messageContent,
            createdAt: DateUtils.formatISO8601()
        )
        messages.append(userMsg)

        // Create placeholder assistant message for streaming
        let assistantMsg = DianeMessage(
            id: "stream-\(UUID().uuidString)",
            role: "assistant",
            content: "",
            createdAt: DateUtils.formatISO8601()
        )
        messages.append(assistantMsg)

        isStreaming = true

        streamingTask = Task {
            do {
                let stream = cloudClient.streamACP(
                    agentName: session.agentName ?? "diane-default",
                    sessionID: session.id,
                    content: messageContent
                )

                var accumulatedContent = ""
                var toolCalls: [DianeMessage.ToolCall] = []
                var reasoningContent: String?

                for try await event in stream {
                    if Task.isCancelled { break }

                    switch event.type {
                    case "token", "text":
                        if let content = event.content {
                            accumulatedContent += content
                        }

                    case "reasoning":
                        if let content = event.content {
                            reasoningContent = (reasoningContent ?? "") + content
                        }

                    case "tool_call":
                        if let name = event.name {
                            toolCalls.append(
                                DianeMessage.ToolCall(
                                    name: name,
                                    arguments: event.content,
                                    result: nil
                                )
                            )
                        }

                    case "tool_result":
                        if let name = event.name, let idx = toolCalls.lastIndex(where: { $0.name == name }) {
                            let existing = toolCalls[idx]
                            toolCalls[idx] = DianeMessage.ToolCall(
                                name: existing.name,
                                arguments: existing.arguments,
                                result: event.content
                            )
                        }

                    case "error":
                        let errorMsg = event.message ?? event.content ?? "Unknown error"
                        let err = DianeMessage(
                            id: "error-\(UUID().uuidString)",
                            role: "error",
                            content: errorMsg,
                            createdAt: DateUtils.formatISO8601()
                        )
                        await MainActor.run {
                            if let idx = self.messages.firstIndex(where: { $0.id == assistantMsg.id }) {
                                self.messages[idx] = err
                            } else {
                                self.messages.append(err)
                            }
                            self.isStreaming = false
                        }
                        return

                    case "done":
                        break

                    default:
                        break
                    }

                    // Update the streaming message on each token
                    await MainActor.run {
                        if let idx = self.messages.firstIndex(where: { $0.id == assistantMsg.id }) {
                            self.messages[idx] = DianeMessage(
                                id: assistantMsg.id,
                                role: "assistant",
                                content: accumulatedContent,
                                createdAt: assistantMsg.createdAt,
                                toolCalls: toolCalls.isEmpty ? nil : toolCalls,
                                reasoningContent: reasoningContent
                            )
                        }
                    }
                }

                // Streaming complete — finalize
                await MainActor.run {
                    if let idx = self.messages.firstIndex(where: { $0.id == assistantMsg.id }) {
                        self.messages[idx] = DianeMessage(
                            id: assistantMsg.id,
                            role: "assistant",
                            content: accumulatedContent,
                            createdAt: assistantMsg.createdAt,
                            toolCalls: toolCalls.isEmpty ? nil : toolCalls,
                            reasoningContent: reasoningContent
                        )
                    }
                    self.isStreaming = false
                }

                // Cache messages after successful stream
                SessionCache.shared.cacheMessages(messages, for: session.id)

            } catch {
                if Task.isCancelled { return }
                await MainActor.run {
                    let errMsg = DianeMessage(
                        id: "error-\(UUID().uuidString)",
                        role: "error",
                        content: error.localizedDescription,
                        createdAt: DateUtils.formatISO8601()
                    )
                    if let idx = self.messages.firstIndex(where: { $0.id == assistantMsg.id }) {
                        self.messages[idx] = errMsg
                    } else {
                        self.messages.append(errMsg)
                    }
                    self.isStreaming = false
                }
            }
        }
    }

    // MARK: - Stop Streaming

    private func stopStreaming() {
        streamingTask?.cancel()
        streamingTask = nil

        // Remove the empty streaming placeholder if it has no content
        if let idx = messages.firstIndex(where: { $0.id.hasPrefix("stream-") && $0.content.isEmpty }) {
            messages.remove(at: idx)
        }

        isStreaming = false
    }
}

// MARK: - Preview Samples

private enum ChatPreviewSamples {
    static let toolCall = DianeMessage.ToolCall(
        name: "web_search",
        arguments: "{\"query\": \"swift previews\"}",
        result: "Found 12 results matching the query."
    )

    static let session = DianeSession(
        id: "preview-1",
        title: "Preview Chat",
        status: "active",
        messageCount: 3,
        runCount: 2,
        totalTokens: 161128,
        totalCostUsd: 0.0161368,
        lastRunStatus: "completed",
        createdAt: DateUtils.formatISO8601(),
        updatedAt: DateUtils.formatISO8601(),
        agentName: "diane-default"
    )

    static let assistantMessage = DianeMessage(
        id: "msg-2",
        role: "assistant",
        content: "Here are a few results I found. Let me know if you want me to dig deeper.",
        createdAt: DateUtils.formatISO8601(),
        toolCalls: [toolCall],
        reasoningContent: "The user is asking about Swift previews; I should search and summarize."
    )
}

// MARK: - Preview

#Preview("ChatView") {
    NavigationStack {
        ChatView(session: ChatPreviewSamples.session)
    }
}

#Preview("Tool Call") {
    ToolCallView(toolCall: ChatPreviewSamples.toolCall)
        .padding()
}

#Preview("Expanded Tool Call") {
    ExpandedToolCallView(toolCall: ChatPreviewSamples.toolCall)
        .padding()
}

#Preview("Reasoning Section") {
    ReasoningSection(content: "Thinking about how to phrase a clear answer for the user, including caveats.")
        .padding()
}

#Preview("Error Message") {
    ErrorMessageView(message: "Could not reach the server. Please try again.")
}

#Preview("Message Detail") {
    MessageDetailSheet(message: ChatPreviewSamples.assistantMessage)
}

#Preview("Session Detail") {
    SessionDetailSheet(
        session: ChatPreviewSamples.session,
        onDelete: {},
        onArchive: {}
    )
}

#Preview("Chat Input Bar — idle") {
    StatefulPreviewWrapper("") { text in
        ChatInputBar(
            text: text,
            isStreaming: false,
            onSend: {},
            onStop: {},
            onPickFile: {},
            hasPendingAttachment: false,
            pendingAttachmentName: nil
        )
    }
}

#Preview("Chat Input Bar — streaming") {
    StatefulPreviewWrapper("Streaming response…") { text in
        ChatInputBar(
            text: text,
            isStreaming: true,
            onSend: {},
            onStop: {},
            onPickFile: {},
            hasPendingAttachment: true,
            pendingAttachmentName: "notes.pdf"
        )
    }
}

/// Helper that gives `#Preview` blocks a writable `Binding`.
private struct StatefulPreviewWrapper<Value, Content: View>: View {
    @State private var value: Value
    let content: (Binding<Value>) -> Content

    init(_ initial: Value, @ViewBuilder content: @escaping (Binding<Value>) -> Content) {
        self._value = State(initialValue: initial)
        self.content = content
    }

    var body: some View { content($value) }
}
