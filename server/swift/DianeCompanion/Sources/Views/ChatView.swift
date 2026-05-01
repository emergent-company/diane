import SwiftUI

/// Interactive chat view — start new conversations, send messages, see agent responses.
struct ChatView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var dianeAPI: DianeAPIClient

    @State private var sessions: [DianeSession] = []
    @State private var currentSessionID: String? = nil
    @State private var messages: [DianeMessage] = []
    @State private var inputText: String = ""
    @State private var isSending = false
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var agentDefs: [AgentDef] = []
    @State private var selectedAgent: String = "diane-default"

    var body: some View {
        VStack(spacing: 0) {
            // Header
            headerBar

            Divider()

            if let err = error {
                ErrorBannerView(message: err) { error = nil }
                    .padding(8)
            }

            // Message list
            if isLoading {
                LoadingStateView(message: "Loading conversations…")
            } else if messages.isEmpty {
                emptyChatView
            } else {
                messageList
            }

            Divider()

            // Input bar
            inputBar
        }
        .navigationTitle("Chat")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(action: startNewChat) {
                    Label("New Chat", systemImage: "plus.bubble")
                }
                .disabled(isSending)
            }
            ToolbarItem(placement: .automatic) {
                if currentSessionID != nil {
                    Button(action: closeCurrentSession) {
                        Label("Close Session", systemImage: "xmark.circle")
                    }
                    .disabled(isSending)
                }
            }
        }
        .task { await loadSessions() }
        .task { await loadAgentDefs() }
    }

    // MARK: - Header

    private var headerBar: some View {
        HStack(spacing: 10) {
            Image(systemName: "bubble.left.and.bubble.right.fill")
                .font(.system(size: 12))
                .foregroundStyle(.blue)

            if let id = currentSessionID {
                Text("Session: \(id.prefix(8))…")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Agent selector
            if !agentDefs.isEmpty {
                Picker("Agent", selection: $selectedAgent) {
                    ForEach(agentDefs) { def in
                        Text(def.name).tag(def.name)
                    }
                }
                .pickerStyle(.menu)
                .font(.caption)
                .frame(width: 160)
                .disabled(isSending)
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
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color.primary.opacity(0.03))
    }

    // MARK: - Empty State

    private var emptyChatView: some View {
        VStack(spacing: 12) {
            Spacer()
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("Start a Conversation")
                .font(.title3)
                .fontWeight(.medium)
            Text("Type a message below or click New Chat to start fresh.\nYour conversations are saved as sessions.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)
            if !sessions.isEmpty {
                Button("Resume Recent Session") {
                    if let latest = sessions.first {
                        attachToSession(latest)
                    }
                }
                .buttonStyle(.bordered)
                .padding(.top, 4)
            }
            Spacer()
        }
    }

    // MARK: - Message List

    private var messageList: some View {
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
            .onChange(of: messages.count) { _, _ in
                if let last = messages.last {
                    withAnimation {
                        proxy.scrollTo(last.id, anchor: .bottom)
                    }
                }
            }
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

        VStack(alignment: isUser ? .trailing : .leading, spacing: 4) {
            // Role label
            HStack(spacing: 6) {
                if !isUser {
                    roleBadge(message.role)
                }
                if let seq = message.sequenceNumber {
                    Text("#\(seq)")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 4)

            // Content bubble
            VStack(alignment: .leading, spacing: 6) {
                if let thinking = message.reasoningContent, !thinking.isEmpty {
                    thinkingSection(thinking)
                }

                if !message.content.isEmpty {
                    if isSystem {
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

                // Show "Thinking…" for the last message (no assistant response yet)
                if isSending && message.id == messages.last?.id && message.role != "assistant" {
                    HStack(spacing: 4) {
                        ProgressView()
                            .scaleEffect(0.5)
                        Text("Thinking…")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
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

            // Timestamp
            if let dateStr = message.createdAt {
                Text(DateUtils.formatTimestamp(dateStr))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 4)
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

    // MARK: - Thinking Section

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

    // MARK: - Actions

    @MainActor
    private func startNewChat() {
        currentSessionID = nil
        messages = []
        inputText = ""
        error = nil
    }

    @MainActor
    private func attachToSession(_ session: DianeSession) {
        currentSessionID = session.id
        messages = []
        inputText = ""
        error = nil
        Task { await loadMessages() }
    }

    @MainActor
    private func sendMessage() async {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }

        inputText = ""
        error = nil

        // 1. Optimistic update: show user message immediately
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
        isSending = true

        // 2. Show working indicator
        let workingID = UUID().uuidString

        do {
            let response = try await dianeAPI.sendChatMessage(
                sessionID: currentSessionID,
                content: text,
                agentName: selectedAgent
            )
            currentSessionID = response.sessionID
            // Replace the working indicator + user message with actual response messages
            messages = response.messages
        } catch {
            self.error = error.localizedDescription
            // Keep the user message but add an error indicator
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
    private func closeCurrentSession() {
        guard let id = currentSessionID else { return }
        isSending = true
        Task {
            do {
                try await dianeAPI.closeSession(sessionID: id)
                currentSessionID = nil
                messages = []
                inputText = ""
                await loadSessions()
            } catch {
                self.error = "Failed to close session: \(error.localizedDescription)"
            }
            isSending = false
        }
    }

    @MainActor
    private func loadSessions() async {
        isLoading = true
        do {
            sessions = try await dianeAPI.fetchSessions()
        } catch {
            // Non-fatal — silence list errors
        }
        isLoading = false
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
            logDebug("ChatView: failed to load agent defs: \(error.localizedDescription)", category: "Chat")
        }
    }

    @MainActor
    private func loadMessages() async {
        guard let id = currentSessionID else { return }
        do {
            messages = try await dianeAPI.fetchSessionMessages(sessionID: id)
        } catch {
            self.error = error.localizedDescription
        }
    }
}

// MARK: - Bubble Tail Shape

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

// MARK: - Previews

#Preview {
    ChatView()
        .environmentObject(AppState())
        .environmentObject(ServerConfiguration())
        .environmentObject(DianeAPIClient())
        .frame(width: 500, height: 600)
}
