import Foundation

// MARK: - Open Graph Metadata

/// Represents a parsed Open Graph link preview.
public struct OpenGraphData: Identifiable, Sendable {
    public let id: String  // the URL that was fetched
    public let url: URL
    public let siteName: String?
    public let title: String?
    public let description: String?
    public let imageURL: URL?
    public let faviconURL: URL?

    public var displayTitle: String { title ?? url.host ?? url.absoluteString }
    public var displaySite: String { siteName ?? url.host ?? "" }
}

/// Fetches and parses Open Graph metadata from a URL.
/// Uses a simple HTML parser — no external dependencies.
public enum OpenGraphService {

    /// Maximum number of bytes to download for OG scanning.
    private static let maxScanBytes = 65_536

    /// Attempt to fetch OG metadata for a given URL.
    /// Returns nil if the URL can't be reached or contains no usable OG tags.
    public static func fetch(url: URL) async -> OpenGraphData? {
        var request = URLRequest(url: url)
        request.timeoutInterval = 8
        request.setValue("DianeCompanion/1.0", forHTTPHeaderField: "User-Agent")

        guard let (data, response) = try? await URLSession.shared.data(for: request),
              let http = response as? HTTPURLResponse,
              (200...299).contains(http.statusCode)
        else { return nil }

        // Only scan the first portion for og: tags
        let head = data.prefix(maxScanBytes)
        guard let html = String(data: head, encoding: .utf8) ?? String(data: head, encoding: .ascii)
        else { return nil }

        return parseOG(from: html, url: url)
    }

    /// Extract URLs from a plain-text message body.
    /// Returns unique, valid URLs (http/https only).
    public static func extractURLs(from text: String) -> [URL] {
        // Simple regex to find URLs in text
        guard let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue)
        else { return [] }

        let range = NSRange(text.startIndex..., in: text)
        let matches = detector.matches(in: text, range: range)

        var seen = Set<String>()
        return matches.compactMap { match -> URL? in
            guard let url = match.url,
                  let scheme = url.scheme,
                  (scheme == "http" || scheme == "https"),
                  seen.insert(url.absoluteString).inserted
            else { return nil }
            return url
        }
    }

    // MARK: - Simple OG Parser

    /// Parse Open Graph meta tags from raw HTML using basic string scanning.
    /// Does NOT use XML/HTML parsers — avoids any external dependency.
    private static func parseOG(from html: String, url: URL) -> OpenGraphData? {
        var siteName: String?
        var title: String?
        var description: String?
        var imageURL: URL?

        // Parse each OG property individually
        siteName = extractMetaContent(html: html, property: "og:site_name")
        title = extractMetaContent(html: html, property: "og:title")
        description = extractMetaContent(html: html, property: "og:description")

        // og:image
        if let imgStr = extractMetaContent(html: html, property: "og:image"),
           let imgURL = URL(string: imgStr) ?? (imgStr.hasPrefix("/") ? URL(string: "\(url.scheme ?? "https")://\(url.host ?? "")\(imgStr)") : nil) {
            imageURL = imgURL
        }

        // Fallback: use <title> tag if no og:title
        if title == nil {
            title = extractTitleTag(html: html)
        }

        // Require at least a title or description to consider this a valid preview
        guard title != nil || description != nil else { return nil }

        // Favicon
        let faviconURL = extractFaviconURL(html: html, baseURL: url)

        return OpenGraphData(
            id: url.absoluteString,
            url: url,
            siteName: siteName,
            title: title,
            description: description,
            imageURL: imageURL,
            faviconURL: faviconURL
        )
    }

    /// Extract content from `<meta property="og:XXX" content="YYY">`.
    private static func extractMetaContent(html: String, property: String) -> String? {
        let patterns = [
            "property=\"\(property)\" content=\"",
            "property='\(property)' content='",
            "property=\(property) content=",
        ]
        for pattern in patterns {
            if let range = html.range(of: pattern) {
                let start = html[range.upperBound...]
                // Find closing quote or whitespace
                let endChars: [Character] = ["\"", "'", ">", "/"]
                var value = ""
                for char in start {
                    if endChars.contains(char) || char.isWhitespace { break }
                    value.append(char)
                }
                if !value.isEmpty {
                    return value
                        .replacingOccurrences(of: "&amp;", with: "&")
                        .replacingOccurrences(of: "&quot;", with: "\"")
                        .replacingOccurrences(of: "&#39;", with: "'")
                        .replacingOccurrences(of: "&lt;", with: "<")
                        .replacingOccurrences(of: "&gt;", with: ">")
                }
            }
        }
        return nil
    }

    /// Extract text from `<title>...</title>`.
    private static func extractTitleTag(html: String) -> String? {
        guard let openRange = html.range(of: "<title>"),
              let closeRange = html.range(of: "</title>"),
              openRange.upperBound < closeRange.lowerBound
        else { return nil }
        return String(html[openRange.upperBound..<closeRange.lowerBound]).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Extract favicon URL from `<link rel="icon" ...>` or similar.
    private static func extractFaviconURL(html: String, baseURL: URL) -> URL? {
        let patterns = [
            "rel=\"icon\" href=\"",
            "rel=\"shortcut icon\" href=\"",
            "rel=\"apple-touch-icon\" href=\"",
        ]
        for pattern in patterns {
            if let range = html.range(of: pattern) {
                let start = html[range.upperBound...]
                var href = ""
                for char in start {
                    if char == "\"" || char == "'" || char == ">" { break }
                    href.append(char)
                }
                if !href.isEmpty {
                    if href.hasPrefix("http") {
                        return URL(string: href)
                    } else if href.hasPrefix("//") {
                        return URL(string: "https:\(href)")
                    } else if href.hasPrefix("/") {
                        return URL(string: "\(baseURL.scheme ?? "https")://\(baseURL.host ?? "")\(href)")
                    } else {
                        return baseURL.appendingPathComponent(href)
                    }
                }
            }
        }
        // Default: /favicon.ico
        return URL(string: "\(baseURL.scheme ?? "https")://\(baseURL.host ?? "")/favicon.ico")
    }
}
