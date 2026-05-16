import SwiftUI
import DianeShared
import ExyteChat

// MARK: - Tool Call View

struct ToolCallView: View {
    let toolCall: DianeMessage.ToolCall

    var body: some View {
        VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
            HStack(spacing: DesignTokens.spacingXS) {
                Image(systemName: "wrench.and.screwdriver")
                    .font(.caption)
                    .foregroundColor(.purple)
                Text(toolCall.name)
                    .font(.caption.monospaced())
                    .fontWeight(.semibold)
                    .foregroundColor(.purple)
            }

            if let args = toolCall.arguments, !args.isEmpty {
                Text(args)
                    .font(.caption2.monospaced())
                    .foregroundColor(.secondary)
                    .lineLimit(3)
                    .truncationMode(.tail)
            }

            if let result = toolCall.result, !result.isEmpty {
                Text(result)
                    .font(.caption2.monospaced())
                    .foregroundColor(.secondary)
                    .lineLimit(2)
                    .truncationMode(.tail)
            }
        }
        .padding(DesignTokens.spacingSM)
        .background(Color.purple.opacity(0.08))
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

// MARK: - ExyteChat Bubble Content

/// Renders the visible content inside an ExyteChat message cell — tool calls, reasoning, markdown, streaming cursor.
/// ExyteChat handles the cell layout (rotation, alignment containers); this view renders the bubble interior only.
private struct ExyteBubbleContent: View {
    let message: DianeMessage
    let isStreaming: Bool

    private var isUser: Bool { message.role == "user" }

    private var bubbleColor: Color {
        isUser ? Color.accentColor : Color(.secondarySystemBackground)
    }

    private var textColor: Color {
        isUser ? .white : .primary
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: DesignTokens.spacingSM) {
            if isUser { Spacer(minLength: 60) }

            VStack(alignment: isUser ? .trailing : .leading, spacing: DesignTokens.spacingXS) {
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

                // Tool calls
                if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
                    VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
                        ForEach(toolCalls, id: \.name) { tc in
                            ToolCallView(toolCall: tc)
                        }
                    }
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

    // MARK: State

    @State private var messages: [DianeMessage] = []
    @State private var isLoading = true
    @State private var isStreaming = false
    @State private var error: String?
    @State private var streamingTask: Task<Void, Never>?

    // File upload state
    @State private var showFilePicker = false
    @State private var isUploading = false
    @State private var pendingDocumentID: String?
    @State private var pendingDocumentName: String?

    // MARK: ExyteChat users

    private let currentUser = User(id: "user", name: "You", avatarURL: nil, isCurrentUser: true)
    private let assistantUser = User(id: "assistant", name: "Diane", avatarURL: nil, isCurrentUser: false)

    // MARK: Computed

    private var streamingMessageID: String? {
        messages.last(where: { $0.role == "assistant" && isStreaming })?.id
    }

    /// Convert DianeMessage → ExyteChat.Message for the ExyteChat list.
    private var exyteMessages: [ExyteChat.Message] {
        messages.map { msg in
            let user: User = msg.role == "user" ? currentUser : assistantUser
            let date = msg.createdAt.flatMap { ISO8601DateFormatter().date(from: $0) } ?? Date()
            return ExyteChat.Message(
                id: msg.id,
                user: user,
                createdAt: date,
                text: msg.content
            )
        }
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

                ExyteChat.ChatView(
                    messages: exyteMessages,
                    didSendMessage: { _ in
                        // Send is handled via inputViewBuilder's onSend closure
                    },
                    messageBuilder: { params in
                        messageContent(for: params.message.id)
                    },
                    inputViewBuilder: { params in
                        ChatInputBar(
                            text: params.text,
                            isStreaming: isStreaming,
                            onSend: {
                                let text = params.text.wrappedValue.trimmingCharacters(
                                    in: .whitespacesAndNewlines
                                )
                                guard !text.isEmpty else { return }
                                params.text.wrappedValue = ""
                                sendMessage(text: text)
                            },
                            onStop: { stopStreaming() },
                            onPickFile: { showFilePicker = true },
                            hasPendingAttachment: pendingDocumentName != nil,
                            pendingAttachmentName: pendingDocumentName
                        )
                    }
                )
            }
        }
        .navigationTitle(session.title ?? "Chat")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
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
        .sentryView("ChatView")
    }

    // MARK: - Message Content Builder

    @ViewBuilder
    private func messageContent(for id: String) -> some View {
        if let dianeMsg = messages.first(where: { $0.id == id }) {
            if dianeMsg.role == "error" {
                ErrorMessageView(message: dianeMsg.content)
            } else {
                ExyteBubbleContent(
                    message: dianeMsg,
                    isStreaming: dianeMsg.id == streamingMessageID && isStreaming
                )
            }
        }
    }

    // MARK: - Load Messages

    private func loadMessages() async {
        isLoading = true
        error = nil
        do {
            messages = SessionCache.shared.loadCachedMessages(for: session.id)
            SessionCache.shared.cacheMessages(messages, for: session.id)
        } catch {
            // Fall back — loadCachedMessages is synchronous so this shouldn't fail
            let cached = SessionCache.shared.loadCachedMessages(for: session.id)
            if cached.isEmpty {
                self.error = error.localizedDescription
            } else {
                messages = cached
            }
        }
        isLoading = false
        // Mark session as read
        SessionCache.shared.markRead(sessionID: session.id)
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
                    createdAt: ISO8601DateFormatter().string(from: Date())
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
                createdAt: ISO8601DateFormatter().string(from: Date())
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
            createdAt: ISO8601DateFormatter().string(from: Date())
        )
        messages.append(userMsg)

        // Create placeholder assistant message for streaming
        let assistantMsg = DianeMessage(
            id: "stream-\(UUID().uuidString)",
            role: "assistant",
            content: "",
            createdAt: ISO8601DateFormatter().string(from: Date())
        )
        messages.append(assistantMsg)

        isStreaming = true

        streamingTask = Task {
            do {
                let stream = cloudClient.streamACP(
                    agentName: "diane-default",
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
                            createdAt: ISO8601DateFormatter().string(from: Date())
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
                        createdAt: ISO8601DateFormatter().string(from: Date())
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

// MARK: - Preview

#Preview {
    NavigationStack {
        ChatView(session: DianeSession(
            id: "preview-1",
            title: "Preview Chat",
            status: "active",
            messageCount: 3,
            createdAt: ISO8601DateFormatter().string(from: Date()),
            agentName: "diane-default"
        ))
    }
}
