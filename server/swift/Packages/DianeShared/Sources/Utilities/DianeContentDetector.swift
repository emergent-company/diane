import Foundation
import Down

// MARK: - Content Type Detection

public enum DianeContentType: Sendable, Equatable {
    case plain
    case markdown
    case html
}

public struct DianeContentDetector: Sendable {
    // Heuristics for HTML detection
    private static let htmlStartPatterns: Set<String> = [
        "<html", "<!DOCTYPE", "<!doctype", "<div", "<table", "<style",
        "<script", "<form", "<h1", "<h2", "<h3", "<p>", "<p ", "<ul>",
        "<ol>", "<li>", "<a ", "<img ", "<svg", "<canvas", "<header",
        "<footer", "<section", "<article", "<nav", "<main", "<aside",
    ]

    /// Detect the content type of a string.
    /// - If it looks like HTML, returns `.html`
    /// - Everything else defaults to `.markdown` — plain text is a valid
    ///   subset of markdown and renders identically through the markdown pipeline.
    ///   The server rarely sets explicit `contentType`, so client-side detection
    ///   must be permissive rather than conservative.
    public static func detect(_ content: String) -> DianeContentType {
        if isEmptyHTML(content) { return .plain }
        if isHTML(content) { return .html }
        // Default to markdown — plain text is a valid subset of markdown
        return .markdown
    }

    private static func isEmptyHTML(_ content: String) -> Bool {
        content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private static func isHTML(_ content: String) -> Bool {
        let trimmed = content.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count > 10 else { return false }

        let lowercased = trimmed.lowercased()

        // Check for DOCTYPE
        if lowercased.hasPrefix("<!doctype html") || lowercased.hasPrefix("<!doctype") {
            return true
        }

        // Check for known HTML start patterns
        for pattern in htmlStartPatterns {
            if lowercased.hasPrefix(pattern) {
                return true
            }
        }

        // Check for multiple HTML tags (at least 2 tag-like structures)
        // This catches inline HTML that doesn't start with a common tag
        // Uses a simple character-level scan instead of regex for performance
        var tagCount = 0
        var i = trimmed.startIndex
        while i < trimmed.endIndex {
            if trimmed[i] == "<" {
                // Check if this looks like a tag (followed by letter, /, or !)
                let nextIdx = trimmed.index(after: i)
                if nextIdx < trimmed.endIndex {
                    let nextChar = trimmed[nextIdx]
                    if nextChar.isLetter || nextChar == "/" || nextChar == "!" || nextChar == "?" {
                        tagCount += 1
                        if tagCount >= 3 {
                            return true
                        }
                    }
                }
            }
            i = trimmed.index(after: i)
        }

        return false
    }
}

// MARK: - Markdown to HTML

extension String {
    /// Convert markdown to HTML using Down (cmark).
    /// Returns a complete HTML document wrapped for WebView rendering.
    public func markdownToHTML(isUser: Bool = false) -> String {
        guard !isEmpty else { return "" }
        do {
            let down = Down(markdownString: self)
            let html = try down.toHTML()
            return html.htmlWrappedForWebView(darkMode: true, isUser: isUser)
        } catch {
            // Fallback: render plain text in a paragraph if markdown parsing fails
            return "<p>\(self)</p>".htmlWrappedForWebView(darkMode: true, isUser: isUser)
        }
    }
}

// MARK: - HTML Wrapping

extension String {
    /// Wraps the content in a minimal HTML document for WKWebView rendering.
    /// Includes dark mode support, base styling, and bubble-aware colors for user messages.
    public func htmlWrappedForWebView(darkMode: Bool = false, isUser: Bool = false) -> String {
        let htmlContent = self
        let bgColor: String
        let textColor: String
        let linkColor: String
        let codeBg: String
        let codeColor: String
        if isUser {
            bgColor = "#007AFF" // accentColor
            textColor = "#FFFFFF"
            linkColor = "#FFFFFF"
            codeBg = "#0055CC"
            codeColor = "#E5E5E5"
        } else if darkMode {
            bgColor = "#1C1C1E"
            textColor = "#E5E5E5"
            linkColor = "#64A8FF"
            codeBg = "#2C2C2E"
            codeColor = "#E5E5E5"
        } else {
            bgColor = "#FFFFFF"
            textColor = "#1C1C1E"
            linkColor = "#007AFF"
            codeBg = "#F2F2F7"
            codeColor = "#1C1C1E"
        }

        return """
        <!DOCTYPE html>
        <html>
        <head>
        <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0">
        <style>
            * { box-sizing: border-box; margin: 0; padding: 0; }
            body {
                font-family: -apple-system, -apple-system-body, HelveticaNeue, sans-serif;
                font-size: 17px;
                line-height: 1.5;
                color: \(textColor);
                background-color: \(bgColor);
                padding: 0;
                overflow-x: hidden;
                word-wrap: break-word;
            }
            a { color: \(linkColor); text-decoration: none; }
            a:hover { text-decoration: underline; }
            pre, code {
                font-family: SFMono-Regular, Menlo, Monaco, monospace;
                font-size: 14px;
            }
            code {
                background-color: \(codeBg);
                color: \(codeColor);
                padding: 2px 6px;
                border-radius: 4px;
            }
            pre {
                background-color: \(codeBg);
                padding: 12px;
                border-radius: 8px;
                overflow-x: auto;
                margin: 8px 0;
            }
            pre code {
                background: none;
                padding: 0;
                border-radius: 0;
            }
            p { margin: 8px 0; }
            h1, h2, h3, h4, h5, h6 { margin: 16px 0 8px; font-weight: 600; }
            h1 { font-size: 22px; }
            h2 { font-size: 20px; }
            h3 { font-size: 18px; }
            ul, ol { margin: 8px 0; padding-left: 24px; }
            li { margin: 4px 0; }
            blockquote {
                margin: 8px 0;
                padding: 8px 16px;
                border-left: 4px solid \(linkColor);
                background-color: \(darkMode ? "#2C2C2E" : "#F2F2F7");
                border-radius: 0 4px 4px 0;
            }
            table {
                border-collapse: collapse;
                margin: 8px 0;
                width: 100%;
            }
            th, td {
                border: 1px solid \(darkMode ? "#3A3A3C" : "#D1D1D6");
                padding: 8px 12px;
                text-align: left;
            }
            th {
                background-color: \(darkMode ? "#2C2C2E" : "#F2F2F7");
                font-weight: 600;
            }
            tr:nth-child(even) { background-color: \(darkMode ? "#2C2C2E" : "#F2F2F7"); }
            img { max-width: 100%; height: auto; border-radius: 8px; margin: 8px 0; }
            hr { border: none; border-top: 1px solid \(darkMode ? "#3A3A3C" : "#D1D1D6"); margin: 16px 0; }
            .custom-content {
                margin: 0;
                padding: 0;
            }
        </style>
        </head>
        <body>
        <div class="custom-content">
        \(htmlContent)
        </div>
        </body>
        </html>
        """
    }
}
