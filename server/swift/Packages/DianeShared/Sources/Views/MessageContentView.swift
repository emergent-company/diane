import SwiftUI

#if canImport(Textual)
import Textual
#endif

// MARK: - Message Content View

/// A shared content view that renders message content with intelligent content detection.
///
/// Supports three content types:
/// - **HTML**: Renders in a WKWebView with auto-sizing and dark mode support
/// - **Markdown**: Renders with Textual (when available) or AttributedString fallback
/// - **Plain text**: Rendered as-is with `.font(.body)` and `.textSelection(.enabled)`
///
/// Detection is automatic via `DianeContentDetector`, but can be overridden
/// with an explicit `contentType` parameter.
public struct MessageContentView: View {
    public let content: String
    public let isUser: Bool
    public let contentType: String?

    @State private var htmlHeight: CGFloat = 50

    /// Creates a content view with auto-detection of content type.
    /// - Parameters:
    ///   - content: The message text content
    ///   - isUser: Whether this is a user message (used for styling context)
    ///   - contentType: Optional explicit content type override ("html", "markdown", "text")
    public init(content: String, isUser: Bool = false, contentType: String? = nil) {
        self.content = content
        self.isUser = isUser
        self.contentType = contentType
    }

    /// Creates a content view from a `DianeMessage`, using its `contentType` field if set.
    public init(message: DianeMessage) {
        self.content = message.content
        self.isUser = message.role == "user"
        self.contentType = message.contentType
    }

    public var body: some View {
        Group {
            switch resolvedType {
            case .html:
                htmlView
            case .markdown:
                markdownView
            case .plain:
                plainView
            }
        }
    }

    /// Resolve content type: explicit override > auto-detection
    private var resolvedType: DianeContentType {
        if let ct = contentType?.lowercased() {
            switch ct {
            case "html": return .html
            case "markdown", "md": return .markdown
            default: break
            }
        }
        return DianeContentDetector.detect(content)
    }

    // MARK: - HTML

    @ViewBuilder
    private var htmlView: some View {
        HTMLWebView(htmlContent: content, dynamicHeight: $htmlHeight)
            .frame(height: max(htmlHeight, 50))
            .frame(maxWidth: .infinity)
            .textSelection(.enabled)
    }

    // MARK: - Markdown

    @ViewBuilder
    private var markdownView: some View {
        #if canImport(Textual)
        if #available(iOS 18.0, macOS 15.0, *) {
            textualMarkdownView
        } else {
            attributedMarkdownView
        }
        #else
        attributedMarkdownView
        #endif
    }

    #if canImport(Textual)
    @ViewBuilder
    private var textualMarkdownView: some View {
        StructuredText(markdown: content)
            .textual.textSelection(.enabled)
            .textual.structuredTextStyle(.default)
            .font(.body)
    }
    #endif

    @ViewBuilder
    private var attributedMarkdownView: some View {
        if let attributed = content.toMarkdownAttributed() {
            Text(attributed)
                .font(.body)
                .textSelection(.enabled)
        } else {
            plainView
        }
    }

    // MARK: - Plain

    @ViewBuilder
    private var plainView: some View {
        Text(content)
            .font(.body)
            .textSelection(.enabled)
    }
}

// MARK: - Preview

#if DEBUG
struct MessageContentView_Previews: PreviewProvider {
    static var previews: some View {
        VStack(spacing: 20) {
            MessageContentView(content: "Hello, this is **markdown** with `code`.")
            MessageContentView(content: "<table><tr><th>Name</th><th>Value</th></tr><tr><td>Alpha</td><td>42</td></tr></table>")
            MessageContentView(content: "Just some regular text.")
        }
        .padding()
    }
}
#endif
