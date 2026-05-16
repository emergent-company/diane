import SwiftUI
import DianeShared

// MARK: - Corner Radius Modifier

struct RoundedCorner: Shape {
    var radius: CGFloat = .infinity
    var corners: UIRectCorner = .allCorners

    func path(in rect: CGRect) -> Path {
        let path = UIBezierPath(
            roundedRect: rect,
            byRoundingCorners: corners,
            cornerRadii: CGSize(width: radius, height: radius)
        )
        return Path(path.cgPath)
    }
}

extension View {
    func cornerRadius(_ radius: CGFloat, corners: UIRectCorner) -> some View {
        clipShape(RoundedCorner(radius: radius, corners: corners))
    }
}

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

// MARK: - Message Bubble

struct MessageBubble: View {
    let message: DianeMessage
    var isStreaming: Bool = false

    private var isUser: Bool { message.role == "user" }

    private var bubbleColor: Color {
        isUser ? Color.accentColor : Color(.secondarySystemBackground)
    }

    private var textColor: Color {
        isUser ? .white : .primary
    }

    private var bubbleCorners: UIRectCorner {
        isUser
            ? [.topLeft, .topRight, .bottomLeft]
            : [.topLeft, .topRight, .bottomRight]
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: DesignTokens.spacingSM) {
            if isUser { Spacer(minLength: 60) }

            VStack(alignment: isUser ? .trailing : .leading, spacing: DesignTokens.spacingXS) {
                // Content
                Text(message.content)
                    .font(.body)
                    .foregroundColor(textColor)
                    .textSelection(.enabled)

                // Streaming cursor
                if isStreaming {
                    HStack(spacing: 0) {
                        Text("▍")
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
            .cornerRadius(DesignTokens.radiusLG, corners: bubbleCorners)

            if !isUser { Spacer(minLength: 60) }
        }
        .padding(.horizontal, DesignTokens.spacingMD)
        .padding(.vertical, DesignTokens.spacingXXS)
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
            .cornerRadius(DesignTokens.radiusLG, corners: [.topLeft, .topRight, .bottomRight])

            Spacer(minLength: 60)
        }
        .padding(.horizontal, DesignTokens.spacingMD)
        .padding(.vertical, DesignTokens.spacingXXS)
    }
}

// MARK: - Message List

struct MessageListView: View {
    let messages: [DianeMessage]
    let streamingMessageID: String?

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0) {
                    // Earliest messages first, display oldest to newest
                    ForEach(messages) { message in
                        if message.role == "error" {
                            ErrorMessageView(message: message.content)
                                .id(message.id)
                        } else {
                            MessageBubble(
                                message: message,
                                isStreaming: message.id == streamingMessageID
                            )
                            .id(message.id)
                        }
                    }
                }
                .padding(.vertical, DesignTokens.spacingSM)
            }
            .defaultScrollAnchor(.bottom)
        }
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

    @State private var messages: [DianeMessage] = []
    @State private var inputText = ""
    @State private var isLoading = true
    @State private var isStreaming = false
    @State private var error: String?
    @State private var streamingTask: Task<Void, Never>?

    // File upload state
    @State private var showFilePicker = false
    @State private var isUploading = false
    @State private var pendingDocumentID: String?
    @State private var pendingDocumentName: String?

    private var streamingMessageID: String? {
        messages.last(where: { $0.role == "assistant" && isStreaming })?.id
    }

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
                MessageListView(
                    messages: messages,
                    streamingMessageID: streamingMessageID
                )
            }

            // Uploading progress bar
            if isUploading {
                HStack(spacing: DesignTokens.spacingSM) {
                    ProgressView()
                        .scaleEffect(0.8)
                    Text("Uploading file…")
                        .font(.caption)
                        .foregroundColor(.secondary)
                    Spacer()
                }
                .padding(.horizontal, DesignTokens.spacingMD)
                .padding(.vertical, 4)
            }

            // Divider
            Divider()

            // Input bar
            ChatInputBar(
                text: $inputText,
                isStreaming: isStreaming,
                onSend: { sendMessage() },
                onStop: { stopStreaming() },
                onPickFile: { showFilePicker = true },
                hasPendingAttachment: pendingDocumentName != nil,
                pendingAttachmentName: pendingDocumentName
            )
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
                break // user cancelled or error — silently ignore
            }
        }
        .sentryView("ChatView")
    }

    // MARK: - Load Messages

    private func loadMessages() async {
        isLoading = true
        error = nil
        do {
            messages = SessionCache.shared.loadCachedMessages(for: session.id)
            SessionCache.shared.cacheMessages(messages, for: session.id)
        } catch {
            // Fall back to cached messages
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
                // Fallback: extract from session if projectID available
                projectID = ""
            }

            guard !projectID.isEmpty else {
                // No project configured — user needs to set one in settings
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

    private func sendMessage() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !isStreaming else { return }
        inputText = ""

        // Build message content — prepend document reference if uploading
        var messageContent = text
        if let docName = pendingDocumentName {
            let prefix = text.isEmpty
                ? "📄 Uploaded: \(docName)"
                : "[📄 \(docName)] "
            messageContent = prefix + (text.isEmpty ? "" : "\n\n" + text)
        }

        guard !messageContent.trimmingCharacters(in: .whitespaces).isEmpty else {
            // Nothing to send — restore input text if we had a document but cleared it
            if pendingDocumentName != nil {
                inputText = text
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
                            // Replace the streaming placeholder with error
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
