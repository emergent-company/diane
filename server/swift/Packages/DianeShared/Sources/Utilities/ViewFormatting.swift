import SwiftUI

// MARK: - View Formatting Utilities

public enum ViewFormatting {
    /// Truncate a string to a maximum length with ellipsis
    public static func truncate(_ string: String, maxLength: Int) -> String {
        guard string.count > maxLength else { return string }
        return String(string.prefix(maxLength)) + "..."
    }

    /// Format a message preview, stripping newlines and truncating
    public static func messagePreview(_ text: String?, maxLength: Int = 100) -> String {
        guard let text, !text.isEmpty else { return "Empty message" }
        let cleaned = text
            .replacingOccurrences(of: "\n", with: " ")
            .replacingOccurrences(of: "\r", with: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return truncate(cleaned, maxLength: maxLength)
    }

    /// Format an agent name with fallback
    public static func agentName(_ name: String?) -> String {
        guard let name, !name.isEmpty else { return "Unknown Agent" }
        return name
    }

    /// Format a session title with fallback
    public static func sessionTitle(_ title: String?) -> String {
        guard let title, !title.isEmpty else { return "Untitled Session" }
        return title
    }

    /// Pluralize a word
    public static func pluralize(_ count: Int, singular: String, plural: String? = nil) -> String {
        let pluralForm = plural ?? "\(singular)s"
        return count == 1 ? singular : pluralForm
    }

    /// Format "X message(s)" 
    public static func messageCount(_ count: Int) -> String {
        "\(count) \(pluralize(count, singular: "message"))"
    }

    /// Format "X session(s)"
    public static func sessionCount(_ count: Int) -> String {
        "\(count) \(pluralize(count, singular: "session"))"
    }

    /// Format a tool call for display
    public static func toolCallSummary(_ name: String, args: String?) -> String {
        guard let args, !args.isEmpty else { return "🔧 \(name)" }
        let truncated = truncate(args, maxLength: 60)
        return "🔧 \(name)(\(truncated))"
    }

    /// Format status with an emoji indicator
    public static func statusBadge(_ status: String?) -> String {
        guard let status else { return "⏹️ unknown" }
        switch status.lowercased() {
        case "active", "running", "online": return "🟢 \(status)"
        case "idle", "paused": return "🟡 \(status)"
        case "error", "failed", "critical": return "🔴 \(status)"
        case "offline", "stopped", "completed": return "⚪ \(status)"
        case "pending", "starting", "connecting": return "🟠 \(status)"
        case "archived": return "📦 \(status)"
        default: return "🔵 \(status)"
        }
    }

    /// Format a role with an emoji indicator
    public static func roleIndicator(_ role: String?) -> String {
        guard let role else { return "❓" }
        switch role.lowercased() {
        case "user": return "👤"
        case "assistant", "agent": return "🤖"
        case "system": return "⚙️"
        case "tool": return "🔧"
        case "error": return "⚠️"
        case "reasoning": return "🧠"
        default: return "💬"
        }
    }
}
