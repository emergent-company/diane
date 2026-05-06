import Foundation

/// Shared date formatting utilities for the Diane companion app.
///
/// Provides consistent timestamp rendering across all views:
/// - Recent items (< 7 days): relative format ("just now", "3h ago", "yesterday", "5d ago")
/// - Older items: friendly absolute date ("Apr 28, 2026 2:06 PM", omitting year if current year)
/// - Unparseable strings: falls back to displaying the raw string
enum DateUtils {

    private static nonisolated(unsafe) let iso8601Formatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    /// Parses an ISO8601 string with or without fractional seconds.
    private static func parseISO8601(_ str: String) -> Date? {
        iso8601Formatter.date(from: str)
            ?? ISO8601DateFormatter().date(from: str)
    }

    /// Returns a human-friendly timestamp string.
    ///
    /// - Recent (< 7 days): relative like "2m ago", "3h ago", "yesterday", "5d ago"
    /// - Older: absolute like "Apr 28, 2026 2:06 PM" (year omitted if current year)
    /// - Unparseable: the raw `dateStr` as fallback
    static func formatTimestamp(_ dateStr: String) -> String {
        guard let date = parseISO8601(dateStr) else {
            return dateStr
        }
        let interval = -date.timeIntervalSinceNow
        switch interval {
        case ..<60:      return "just now"
        case ..<3600:    return "\(Int(interval / 60))m ago"
        case ..<86400:   return "\(Int(interval / 3600))h ago"
        case ..<172800:  return "yesterday"
        case ..<604800:  return "\(Int(interval / 86400))d ago"
        default:
            return absoluteDate(date)
        }
    }

    /// Friendly absolute date, omitting year if it's the current year.
    private static func absoluteDate(_ date: Date) -> String {
        let calendar = Calendar.current
        let isCurrentYear = calendar.isDate(date, equalTo: Date(), toGranularity: .year)
        let df = DateFormatter()
        df.dateFormat = isCurrentYear ? "MMM d, h:mm a" : "MMM d, yyyy, h:mm a"
        return df.string(from: date)
    }
}
