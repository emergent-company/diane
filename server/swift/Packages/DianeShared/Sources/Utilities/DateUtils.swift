import Foundation

// MARK: - Date Formatting

public enum DateUtils {
    private static nonisolated(unsafe) let relativeFormatter: RelativeDateTimeFormatter = {
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .abbreviated
        return f
    }()

    private static nonisolated(unsafe) let isoFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static nonisolated(unsafe) let isoFormatterNoFractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    private static let dateFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateStyle = .medium
        f.timeStyle = .short
        return f
    }()

    private static let timeFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateStyle = .none
        f.timeStyle = .short
        return f
    }()

    // MARK: - Parsing

    public static func parseISO8601(_ string: String?) -> Date? {
        guard let string, !string.isEmpty else { return nil }
        if let date = isoFormatter.date(from: string) { return date }
        if let date = isoFormatterNoFractional.date(from: string) { return date }
        return nil
    }

    // MARK: - Formatting

    public static func formatRelative(_ dateString: String?) -> String {
        guard let date = parseISO8601(dateString) else { return "—" }
        let now = Date()
        let interval = now.timeIntervalSince(date)
        if interval < 60 { return "just now" }
        if interval < 3600 { return "\(Int(interval / 60))m ago" }
        if interval < 86400 { return "\(Int(interval / 3600))h ago" }
        if interval < 604800 { return "\(Int(interval / 86400))d ago" }
        return relativeFormatter.localizedString(for: date, relativeTo: now)
    }

    public static func formatShort(_ dateString: String?) -> String {
        guard let date = parseISO8601(dateString) else { return "—" }
        return dateFormatter.string(from: date)
    }

    public static func formatTime(_ dateString: String?) -> String {
        guard let date = parseISO8601(dateString) else { return "—" }
        return timeFormatter.string(from: date)
    }

    public static func formatDateOnly(_ dateString: String?) -> String {
        guard let date = parseISO8601(dateString) else { return "—" }
        let f = DateFormatter()
        f.dateStyle = .medium
        f.timeStyle = .none
        return f.string(from: date)
    }

    // MARK: - Age calculations

    public static func minutesSince(_ dateString: String?) -> Int? {
        guard let date = parseISO8601(dateString) else { return nil }
        return Int(Date().timeIntervalSince(date) / 60)
    }

    public static func isRecent(_ dateString: String?, within minutes: Int = 5) -> Bool {
        guard let mins = minutesSince(dateString) else { return false }
        return mins < minutes
    }

    /// Format a Date to an ISO8601 string with fractional seconds.
    public static func formatISO8601(_ date: Date = Date()) -> String {
        isoFormatter.string(from: date)
    }

    public static func isStale(_ dateString: String?, after minutes: Int = 30) -> Bool {
        guard let mins = minutesSince(dateString) else { return true }
        return mins >= minutes
    }
}

// MARK: - Date Extensions

extension Date {
    public var isToday: Bool { Calendar.current.isDateInToday(self) }
    public var isYesterday: Bool { Calendar.current.isDateInYesterday(self) }
    public var isThisWeek: Bool {
        Calendar.current.isDate(self, equalTo: Date(), toGranularity: .weekOfYear)
    }
}
