import SwiftUI

// MARK: - Markdown Text View

/// Renders plain text with basic Markdown formatting (bold, italic, code, links).
///
/// Uses `AttributedString` for native rendering on macOS 15+.
/// Falls back to a simple inline parser for code blocks and monospaced sections.
///
/// This is intentionally lightweight — no external dependencies.
/// For production, consider adding `gonzalezreal/Textual` as SPM dependency
/// which provides full Markdown + syntax highlighting.
struct MarkdownTextView: View {
    let text: String
    var font: Font = .body
    var textColor: Color = .primary

    /// Enable link preview detection for URLs in the text.
    var detectLinks: Bool = true

    /// Called when the user taps a detected URL (for link preview).
    var onOpenURL: ((URL) -> Void)?

    @State private var linkURLs: [URL] = []

    var body: some View {
        content
            .task(id: text) { await extractLinks() }
    }

    @ViewBuilder
    private var content: some View {
        if text.isEmpty {
            EmptyView()
        } else if let attributed = try? AttributedString(markdown: text) {
            // Native SwiftUI AttributedString markdown rendering (macOS 12+)
            Text(attributed)
                .font(font)
                .foregroundStyle(textColor)
                .textSelection(.enabled)
                .environment(\.openURL, OpenURLAction { url in
                    onOpenURL?(url)
                    return .systemAction
                })
        } else {
            // Fallback: basic markdown rendering with inline formatting
            basicMarkdownText
        }
    }

    /// Lightweight fallback for when AttributedString markdown parsing fails.
    /// Handles: **bold**, *italic*, `code`, and [links](url).
    private var basicMarkdownText: some View {
        Text(parsedAttributedString(from: text))
            .font(font)
            .foregroundStyle(textColor)
            .textSelection(.enabled)
            .environment(\.openURL, OpenURLAction { url in
                onOpenURL?(url)
                return .systemAction
            })
    }

    /// Simple attributed string parser for common markdown patterns.
    private func parsedAttributedString(from text: String) -> AttributedString {
        var attributed = AttributedString(text)

        // Bold: **text**
        if let regex = try? NSRegularExpression(pattern: "\\*\\*(.+?)\\*\\*") {
            let range = NSRange(text.startIndex..., in: text)
            for match in regex.matches(in: text, range: range).reversed() {
                guard let swiftRange = Range(match.range(at: 1), in: text) else { continue }
                let nsRange = NSRange(swiftRange, in: text)
                if let attrRange = Range(nsRange, in: attributed) {
                    attributed[attrRange].inlinePresentationIntent = .stronglyEmphasized
                }
                // Remove the markdown markers
                if let fullRange = Range(match.range, in: text),
                   let attrFullRange = Range(NSRange(fullRange, in: text), in: attributed) {
                    let markerLength = 2
                    let start = attributed.index(attrFullRange.lowerBound, offsetByCharacters: markerLength)
                    let end = attributed.index(attrFullRange.upperBound, offsetByCharacters: -markerLength)
                    attributed.replaceSubrange(attrFullRange, with: attributed[start..<end])
                }
            }
        }

        // Italic: *text*
        if let regex = try? NSRegularExpression(pattern: "(?<!\\*)\\*(?!\\*)(.+?)(?<!\\*)\\*(?!\\*)") {
            let range = NSRange(text.startIndex..., in: text)
            for match in regex.matches(in: text, range: range).reversed() {
                guard let swiftRange = Range(match.range(at: 1), in: text) else { continue }
                let nsRange = NSRange(swiftRange, in: text)
                if let attrRange = Range(nsRange, in: attributed) {
                    attributed[attrRange].inlinePresentationIntent = .emphasized
                }
            }
        }

        // Inline code: `text`
        if let regex = try? NSRegularExpression(pattern: "`([^`]+)`") {
            let range = NSRange(text.startIndex..., in: text)
            for match in regex.matches(in: text, range: range).reversed() {
                guard let swiftRange = Range(match.range(at: 1), in: text) else { continue }
                let nsRange = NSRange(swiftRange, in: text)
                if let attrRange = Range(nsRange, in: attributed) {
                    attributed[attrRange].font = .system(.body, design: .monospaced)
                    attributed[attrRange].backgroundColor = .primary.opacity(0.08)
                }
            }
        }

        return attributed
    }

    /// Extract URLs from text for link preview detection.
    private func extractLinks() async {
        guard detectLinks else { return }
        linkURLs = OpenGraphService.extractURLs(from: text)
    }
}

// MARK: - Code Block View

/// Renders a fenced code block with a monospaced font and dark background.
/// Detects language hint from the opening fence.
struct CodeBlockView: View {
    let code: String
    let language: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            // Language label
            if let lang = language, !lang.isEmpty {
                Text(lang)
                    .font(.system(size: 9, weight: .semibold, design: .monospaced))
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 2)
            }

            // Code content with copy button
            HStack(alignment: .top, spacing: 0) {
                Text(code)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.primary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)

                // Copy button
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(code, forType: .string)
                } label: {
                    Image(systemName: "doc.on.doc")
                        .font(.system(size: 9))
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .help("Copy code")
                .padding(.leading, 4)
            }
        }
        .padding(10)
        .background(Color.primary.opacity(0.07))
        .cornerRadius(8)
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.primary.opacity(0.06), lineWidth: 1)
        )
    }
}

// MARK: - Previews

#Preview("Markdown Text") {
    VStack(alignment: .leading, spacing: 16) {
        MarkdownTextView(text: "Hello **world**! This is *italic* and `code`.")
        MarkdownTextView(text: "Check out https://github.com for more info.")
        CodeBlockView(code: "let x = 42\nprint(x)", language: "swift")
    }
    .padding()
    .frame(width: 400)
}
