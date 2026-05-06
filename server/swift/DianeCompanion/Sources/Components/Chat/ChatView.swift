import SwiftUI

// MARK: - Chat View

/// A full-featured chat view with Discord-inspired styling.
///
/// Features:
/// - Message list with automatic scrolling
/// - User/Assistant/System message bubbles with Markdown and code blocks
/// - Discord-style tool call display (compact inline blocks)
/// - Auto-detected link previews (Open Graph metadata)
/// - Thinking/reasoning collapsible sections
/// - Streaming animation support for ongoing responses
/// - Rich input bar with multi-line support and send button
/// - Typing indicator while waiting for agent response
///
/// Usage:
/// ```swift
/// ChatView(
///     messages: messages,
///     onSend: { text in /* send to API */ },
///     isLoading: isSending
/// )
/// ```
struct ChatView: View {
    /// The messages to display.
    let messages: [DianeMessage]

    /// Whether a message is currently being sent.
    var isLoading: Bool = false

    /// Called when user sends a message.
    var onSend: ((String) -> Void)?

    /// Called when user requests a resend of a previous message.
    var onResend: ((DianeMessage) -> Void)?

    /// Placeholder text for the input field.
    var inputPlaceholder: String = "Message Diane…"

    /// Optional title shown at the top of the chat.
    var title: String?

    /// The latest message ID that should be auto-scrolled to.
    var scrollTargetID: String?

    /// Called when the user scrolls to the top (for loading more history).
    var onScrollToTop: (() -> Void)?

    @State private var inputText: String = ""
    @State private var scrollProxy: ScrollViewProxy? = nil
    @FocusState private var isInputFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            // ── Title bar ──
            if let title = title {
                titleBar(title)
            }

            // ── Message list ──
            messageList

            Divider()

            // ── Input bar ──
            inputBar
        }
    }

    // MARK: - Computed

    private var canSend: Bool {
        !isLoading && !inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    // MARK: - Title Bar

    private func titleBar(_ title: String) -> some View {
        HStack(spacing: 8) {
            // Status dot
            Circle()
                .fill(isLoading ? Color.orange : Color.green)
                .frame(width: 8, height: 8)

            Text(title)
                .font(.subheadline)
                .fontWeight(.semibold)

            if isLoading {
                HStack(spacing: 4) {
                    ProgressView()
                        .scaleEffect(0.6)
                    Text("Thinking…")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
            }

            Spacer()
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
        .background(Design.Surface.cardBackground)
    }

    // MARK: - Message List

    private var messageList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0) {
                    // Top spacer for initial scroll position
                    Color.clear
                        .frame(height: 1)
                        .id("top")

                    if messages.isEmpty && !isLoading {
                        emptyState
                    } else {
                        ForEach(Array(messages.enumerated()), id: \.element.id) { index, message in
                            MessageBubbleView(
                                message: message,
                                isLatest: index == messages.count - 1,
                                onResend: onResend.map { _ in { onResend?(message) } }
                            )
                            .id(message.id)
                            .transition(.opacity)
                        }
                    }

                    // Bottom spacer
                    Color.clear
                        .frame(height: 8)
                        .id("bottom")
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 4)
            }
            .onAppear {
                scrollProxy = proxy
                scrollToBottom(proxy: proxy, animated: false)
            }
            .onChange(of: messages.count) { _, _ in
                scrollToBottom(proxy: proxy, animated: true)
            }
            .onChange(of: scrollTargetID) { _, id in
                if let id = id {
                    withAnimation {
                        proxy.scrollTo(id, anchor: .bottom)
                    }
                }
            }
        }
    }

    // MARK: - Empty State

    private var emptyState: some View {
        VStack(spacing: 12) {
            Spacer()

            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)

            Text("Start a Conversation")
                .font(.title3)
                .fontWeight(.medium)

            Text("Send a message to Diane and get AI-powered responses with rich formatting.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)

            Spacer()
        }
        .frame(maxWidth: .infinity)
    }

    // MARK: - Input Bar

    private var inputBar: some View {
        VStack(spacing: 0) {
            // Agent thinking indicator
            if isLoading {
                HStack(spacing: 8) {
                    ProgressView()
                        .scaleEffect(0.7)
                    Text("Diane is responding…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 6)
                .background(Color.primary.opacity(0.03))
            }

            // Input field row
            HStack(alignment: .bottom, spacing: 8) {
                // Text input
                TextField(inputPlaceholder, text: $inputText, axis: .vertical)
                    .textFieldStyle(.plain)
                    .font(.body)
                    .lineLimit(1...8)
                    .padding(10)
                    .background(
                        RoundedRectangle(cornerRadius: 12)
                            .fill(Color.primary.opacity(0.04))
                            .overlay(
                                RoundedRectangle(cornerRadius: 12)
                                    .stroke(Color.primary.opacity(0.08), lineWidth: 0.5)
                            )
                    )
                    .focused($isInputFocused)
                    .disabled(isLoading)
                    .onSubmit { sendMessage() }

                // Send button
                Button(action: sendMessage) {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.system(size: 28))
                        .foregroundStyle(canSend ? Color.accentColor : Color.secondary.opacity(0.3))
                }
                .buttonStyle(.plain)
                .disabled(!canSend)
                .keyboardShortcut(.return, modifiers: [])
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
        }
        .background(.regularMaterial)
    }

    // MARK: - Actions

    private func sendMessage() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isLoading else { return }
        inputText = ""
        onSend?(text)
    }

    private func scrollToBottom(proxy: ScrollViewProxy, animated: Bool) {
        guard !messages.isEmpty else { return }
        if animated {
            withAnimation(.easeOut(duration: 0.25)) {
                proxy.scrollTo(messages.last?.id ?? "bottom", anchor: .bottom)
            }
        } else {
            proxy.scrollTo(messages.last?.id ?? "bottom", anchor: .bottom)
        }
    }
}

// MARK: - Previews

#Preview("Chat with Messages") {
    let messages = [
        DianeMessage(
            id: "1",
            role: "user",
            content: "Hi Diane! Can you search GitHub for SwiftUI chat libraries?",
            sequenceNumber: 1,
            tokenCount: 8,
            toolCalls: nil,
            reasoningContent: nil,
            createdAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(-120))
        ),
        DianeMessage(
            id: "2",
            role: "assistant",
            content: "I found several SwiftUI chat libraries. The best one is **exyte/Chat** with ~1,800 stars. It has:\n- Customizable message bubbles\n- Built-in media picker\n- MIT license\n\n```swift\n// Example usage\nimport Chat\n\nlet chatView = ChatView<MyMessageEntity>()\n```\n\nCheck out https://github.com/exyte/Chat for more info.",
            sequenceNumber: 1,
            tokenCount: 45,
            toolCalls: [
                ToolCall(id: "call_abc", name: "web_search", arguments: "{\"query\": \"SwiftUI chat library\"}"),
                ToolCall(id: "call_def", name: "read_url", arguments: "{\"url\": \"https://github.com/exyte/Chat\"}"),
            ],
            reasoningContent: "The user wants to build a chat feature. I should search for existing SwiftUI chat libraries to save them time and effort.",
            createdAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(-60))
        ),
        DianeMessage(
            id: "3",
            role: "user",
            content: "That looks great! Link previews would be amazing too.",
            sequenceNumber: 2,
            tokenCount: 10,
            toolCalls: nil,
            reasoningContent: nil,
            createdAt: ISO8601DateFormatter().string(from: Date())
        ),
    ]

    ChatView(
        messages: messages,
        isLoading: false,
        onSend: { text in print("Send: \\(text)") },
        title: "Chat with Diane"
    )
    .frame(width: 500, height: 600)
}

#Preview("Chat Empty State") {
    ChatView(
        messages: [],
        isLoading: false,
        onSend: { text in print("Send: \\(text)") }
    )
    .frame(width: 500, height: 600)
}

#Preview("Chat Loading") {
    ChatView(
        messages: [
            DianeMessage(
                id: "1",
                role: "user",
                content: "What's the weather in London?",
                sequenceNumber: 1,
                tokenCount: 6,
                toolCalls: nil,
                reasoningContent: nil,
                createdAt: nil
            ),
        ],
        isLoading: true,
        onSend: { text in print("Send: \\(text)") }
    )
    .frame(width: 500, height: 600)
}
