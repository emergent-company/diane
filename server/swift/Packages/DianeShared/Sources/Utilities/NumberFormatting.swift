import Foundation

// MARK: - Number Formatting

public enum NumberFormatting {
    private static let numberFormatter: NumberFormatter = {
        let f = NumberFormatter()
        f.numberStyle = .decimal
        f.groupingSeparator = ","
        return f
    }()

    private static let compactFormatter: NumberFormatter = {
        let f = NumberFormatter()
        f.numberStyle = .decimal
        f.minimumFractionDigits = 0
        f.maximumFractionDigits = 1
        return f
    }()

    /// Format a count with compact notation (1.2K, 3.4M, etc.)
    public static func compact(_ count: Int) -> String {
        if count < 1000 { return "\(count)" }
        if count < 1_000_000 {
            let val = Double(count) / 1000.0
            let formatted = compactFormatter.string(from: NSNumber(value: val)) ?? "\(val)"
            return "\(formatted)K"
        }
        if count < 1_000_000_000 {
            let val = Double(count) / 1_000_000.0
            let formatted = compactFormatter.string(from: NSNumber(value: val)) ?? "\(val)"
            return "\(formatted)M"
        }
        let val = Double(count) / 1_000_000_000.0
        let formatted = compactFormatter.string(from: NSNumber(value: val)) ?? "\(val)"
        return "\(formatted)B"
    }

    /// Format with commas: 1,234,567
    public static func formatted(_ count: Int) -> String {
        numberFormatter.string(from: NSNumber(value: count)) ?? "\(count)"
    }

    /// Format percentage
    public static func percent(_ value: Double, decimals: Int = 1) -> String {
        String(format: "%.\(decimals)f%%", value * 100)
    }

    /// Format duration in seconds to human-readable
    public static func duration(_ seconds: TimeInterval) -> String {
        if seconds < 60 { return "\(Int(seconds))s" }
        if seconds < 3600 {
            let mins = Int(seconds) / 60
            let secs = Int(seconds) % 60
            return "\(mins)m \(secs)s"
        }
        if seconds < 86400 {
            let hrs = Int(seconds) / 3600
            let mins = (Int(seconds) % 3600) / 60
            return "\(hrs)h \(mins)m"
        }
        let days = Int(seconds) / 86400
        let hrs = (Int(seconds) % 86400) / 3600
        return "\(days)d \(hrs)h"
    }

    /// Format file size
    public static func fileSize(_ bytes: Int) -> String {
        let b = Double(bytes)
        if b < 1024 { return "\(bytes) B" }
        if b < 1024 * 1024 { return String(format: "%.1f KB", b / 1024) }
        if b < 1024 * 1024 * 1024 { return String(format: "%.1f MB", b / (1024 * 1024)) }
        return String(format: "%.2f GB", b / (1024 * 1024 * 1024))
    }

    /// Token count (assuming ~4 chars per token for estimation)
    public static func estimateTokens(for text: String) -> Int {
        max(1, text.count / 4)
    }
}
