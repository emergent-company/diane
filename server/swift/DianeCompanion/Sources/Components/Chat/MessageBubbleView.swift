import SwiftUI

// MARK: - Message Bubble View

/// A single message bubble in the chat, rendered in Discord-like style.
///
/// Features:
/// - User messages: right-aligned blue bubbles
/// - Assistant messages: left-aligned with avatar, markdown content
/// - System messages: subtle centered style
/// - Thinking/reasoning section: collapsible orange section
/// - Tool calls: Discord-style compact inline blocks
/// - Link previews: OG metadata cards
/// - Role badges, timestamps, token counts
struct MessageBubbleView: View {
    let message: DianeMessage
    var isLatest: Bool = false
    var onResend: (() -> Void)?

    @State private var showFullContent = false

    private let dateFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        return f
    }()

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            // ── Header row ──
            headerRow
                .padding(.horizontal, 4)

            // ── Thinking / Reasoning ──
            if let thinking = message.reasoningContent, !thinking.isEmpty {
                thinkingSection(thinking)
            }

            // ── Tool Calls ──
            if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
                ToolCallGroupView(toolCalls: toolCalls, isLatest: isLatest)
            }

            // ── Main Content ──
            if !message.content.isEmpty {
                messageContent
            }

            // ── Link Previews ──
            if !message.content.isEmpty {
                AutoLinkPreviewView(text: message.content)
                    .padding(.horizontal, 4)
            }

            // ── Footer ──
            footerRow
                .padding(.horizontal, 4)
        }
        .padding(.vertical, 6)
        .id(message.id)
    }

    // MARK: - Computed

    private var isUser: Bool { message.role.lowercased() == "user" }
    private var isSystem: Bool { message.role.lowercased() == "system" }
    private var isAssistant: Bool { message.role.lowercased() == "assistant" }

    // MARK: - Header Row

    @ViewBuilder
    private var headerRow: some View {
        HStack(spacing: 6) {
            // Avatar / Role icon for assistant messages
            if isAssistant {
                assistantAvatar
            }

            // Role badge
            roleBadge(message.role)

            // Sequence number
            if let seq = message.sequenceNumber {
                Text("#\\(seq)")
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.tertiary)
            }

            // Token count
            if let tokens = message.tokenCount, tokens > 0 {
                tokenBadge(tokens)
            }

            Spacer(minLength: 4)

            // Error indicator
            if message.content.hasPrefix("⚠️") {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 10))
                    .foregroundStyle(.orange)
            }
        }
    }

    // MARK: - Avatar

    private var assistantAvatar: some View {
        ZStack {
            Circle()
                .fill(
                    LinearGradient(
                        colors: [Color.purple.opacity(0.7), Color.blue.opacity(0.5)],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
                .frame(width: 26, height: 26)

            Image(systemName: "brain.head.profile")
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(.white)
        }
    }

    // MARK: - Message Content

    @ViewBuilder
    private var messageContent: some View {
        if isUser {
            // User message: blue bubble, right-aligned
            Text(message.content)
                .font(.body)
                .foregroundStyle(.primary)
                .textSelection(.enabled)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(
                    UnevenRoundedRectangle(
                        topLeadingRadius: 14,
                        bottomLeadingRadius: 14,
                        bottomTrailingRadius: 4,
                        topTrailingRadius: 14
                    )
                    .fill(Color.accentColor.opacity(0.12))
                )
                .frame(maxWidth: .infinity, alignment: .trailing)
        } else if isSystem {
            // System message: subtle centered text
            Text(message.content)
                .font(.callout)
                .foregroundStyle(.secondary)
                .italic()
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .center)
                .padding(.horizontal, 4)
        } else {
            // Assistant message: markdown-rendered with code blocks
            VStack(alignment: .leading, spacing: 8) {
                // Split content into text blocks and code blocks
                RichMessageContent(text: message.content)
            }
            .padding(.horizontal, 4)
        }
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
                .padding(.top, 6)
        } label: {
            HStack(spacing: 6) {
                Image(systemName: "brain")
                    .font(.system(size: 10))
                Text("Thinking")
                    .font(.caption)
                    .fontWeight(.medium)
                Text("(\\(content.count) chars)")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .foregroundStyle(.orange)
        }
        .disclosureGroupStyle(PlainDisclosureGroupStyle())
        .padding(8)
        .background(Color.orange.opacity(0.05))
        .cornerRadius(8)
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.orange.opacity(0.1), lineWidth: 0.5)
        )
    }

    // MARK: - Footer

    @ViewBuilder
    private var footerRow: some View {
        HStack(spacing: 8) {
            // Timestamp
            if let dateStr = message.createdAt {
                HStack(spacing: 3) {
                    Image(systemName: "clock")
                        .font(.system(size: 8))
                    Text(formatTimestamp(dateStr))
                        .font(.system(size: 9))
                }
                .foregroundStyle(.tertiary)
            }

            Spacer(minLength: 4)

            // Resend button (for user messages that might have failed)
            if isUser && onResend != nil {
                Button(action: { onResend?() }) {
                    Image(systemName: "arrow.clockwise")
                        .font(.system(size: 9))
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .help("Resend message")
            }
        }
    }

    // MARK: - Role Badge

    private func roleBadge(_ role: String) -> some View {
        HStack(spacing: 3) {
            roleIcon(role)
            Text(role.capitalized)
                .font(.system(size: 9, weight: .semibold))
        }
        .foregroundStyle(roleColor(role))
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(roleColor(role).opacity(0.08))
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
        case "assistant": return .purple
        case "system":    return .orange
        case "tool":      return .purple
        default:          return .secondary
        }
    }

    // MARK: - Token Badge

    private func tokenBadge(_ count: Int) -> some View {
        Text(formatTokenCount(count))
            .font(.system(size: 8, design: .monospaced))
            .foregroundStyle(.tertiary)
            .padding(.horizontal, 4)
            .padding(.vertical, 1)
            .background(Color.primary.opacity(0.05))
            .cornerRadius(3)
    }

    private func formatTokenCount(_ count: Int) -> String {
        switch count {
        case 0..<1000:   return "\\(count) tok"
        case 1000..<1_000_000:
            let k = Double(count) / 1000
            return k >= 100 ? "\\(Int(k))K tok" : String(format: "%.1fK tok", k)
        default:
            let m = Double(count) / 1_000_000
            return m >= 10 ? "\\(Int(m))M tok" : String(format: "%.1fM tok", m)
        }
    }

    // MARK: - Timestamp Formatting

    private func formatTimestamp(_ dateStr: String) -> String {
        // Try ISO8601 first
        if let date = ISO8601DateFormatter().date(from: dateStr) {
            return dateFormatter.string(from: date)
        }
        // Try other common formats
        let otherFormats = [
            "yyyy-MM-dd'T'HH:mm:ss.SSSZ",
            "yyyy-MM-dd'T'HH:mm:ssZ",
            "yyyy-MM-dd HH:mm:ss",
        ]
        for format in otherFormats {
            let f = DateFormatter()
            f.dateFormat = format
            if let date = f.date(from: dateStr) {
                return dateFormatter.string(from: date)
            }
        }
        // Fallback: return first 5 chars (time portion)
        return String(dateStr.suffix(8).prefix(5))
    }
}

// MARK: - Rich Message Content (Text + Code Blocks)

/// Renders message text by splitting code blocks (```) from regular text.
/// Regular text uses MarkdownTextView, code blocks use CodeBlockView.
struct RichMessageContent: View {
    let text: String

    var body: some View {
        // Split by code fences
        let blocks = parseContent(text)

        VStack(alignment: .leading, spacing: 6) {
            ForEach(blocks.indices, id: \.self) { idx in
                switch blocks[idx] {
                case .text(let content):
                    MarkdownTextView(text: content)
                case .code(let code, let language):
                    CodeBlockView(code: code, language: language)
                }
            }
        }
    }

    enum ContentBlock {
        case text(String)
        case code(String, language: String?)
    }

    /// Parse text content into alternating text and code blocks.
    private func parseContent(_ text: String) -> [ContentBlock] {
        var blocks: [ContentBlock] = []
        var remaining = text[...]

        while !remaining.isEmpty {
            // Find next code fence
            if let fenceStart = remaining.range(of: "```") {
                // Text before the fence
                let before = String(remaining[..<fenceStart.lowerBound])
                if !before.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    blocks.append(.text(before))
                }

                // After the opening fence
                let afterFence = remaining[fenceStart.upperBound...]

                // Parse language hint (rest of line after ```)
                let lineEnd = afterFence.firstIndex(of: "\n") ?? afterFence.endIndex
                let language = lineEnd > afterFence.startIndex
                    ? String(afterFence[afterFence.startIndex..<lineEnd]).trimmingCharacters(in: .whitespaces)
                    : nil

                // Find closing fence
                let codeStart = lineEnd < afterFence.endIndex ? afterFence[lineEnd...].dropFirst() : Substring()
                if let fenceEnd = codeStart.range(of: "```") {
                    let code = String(codeStart[..<fenceEnd.lowerBound])
                    if !code.isEmpty {
                        blocks.append(.code(code, language: language))
                    }
                    remaining = codeStart[fenceEnd.upperBound...]
                } else {
                    // No closing fence — treat rest as text
                    blocks.append(.text(String(remaining)))
                    remaining = ""
                }
            } else {
                // No code fence found
                blocks.append(.text(String(remaining)))
                remaining = ""
            }
        }

        return blocks
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
                HStack(spacing: 6) {
                    Image(systemName: configuration.isExpanded ? "chevron.down" : "chevron.right")
                        .font(.system(size: 8, weight: .semibold))
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

#Preview("User Message") {
    MessageBubbleView(message: DianeMessage(
        id: "1",
        role: "user",
        content: "Hello! Can you search for SwiftUI chat libraries on GitHub?",
        sequenceNumber: 1,
        tokenCount: 12,
        toolCalls: nil,
        reasoningContent: nil,
        createdAt: ISO8601DateFormatter().string(from: Date())
    ))
    .padding()
    .frame(width: 400)
}

#Preview("Assistant Message with Code") {
    MessageBubbleView(message: DianeMessage(
        id: "2",
        role: "assistant",
        content: "Here's a SwiftUI chat view:\n\n```swift\nstruct ChatView: View {\n    var body: some View {\n        Text(\"Hello, World!\")\n    }\n}\n```\n\nYou can use this as a starting point.",
        sequenceNumber: 1,
        tokenCount: 45,
        toolCalls: [ToolCall(id: "call_1", name: "search_files", arguments: "{\"pattern\": \"*.swift\"}")],
        reasoningContent: "The user wants SwiftUI chat libraries. I'll search for them.",
        createdAt: ISO8601DateFormatter().string(from: Date())
    ))
    .padding()
    .frame(width: 400)
}

#Preview("System Message") {
    MessageBubbleView(message: DianeMessage(
        id: "3",
        role: "system",
        content: "Session created successfully.",
        sequenceNumber: nil,
        tokenCount: nil,
        toolCalls: nil,
        reasoningContent: nil,
        createdAt: nil
    ))
    .padding()
    .frame(width: 400)
}
