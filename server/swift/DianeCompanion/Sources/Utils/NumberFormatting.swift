import Foundation

/// Format milliseconds as a human-readable duration string.
///
/// - 60000+ ms → "1.5m"
/// - 1000+ ms  → "2.3s"
/// - < 1000 ms → "500ms"
func formatDuration(_ ms: Double) -> String {
    if ms >= 60_000 {
        return String(format: "%.1fm", ms / 60_000)
    } else if ms >= 1_000 {
        return String(format: "%.1fs", ms / 1_000)
    } else {
        return String(format: "%.0fms", ms)
    }
}

/// Format a count as a short human-readable string.
///
/// - 1,000,000+ → "1.5M"
/// - 1,000+     → "12.5K"
/// - < 1,000    → "999"
func formatCount(_ count: Int) -> String {
    if count >= 1_000_000 {
        let m = Double(count) / 1_000_000
        return m >= 10 ? "\(Int(m))M" : String(format: "%.1fM", m)
    } else if count >= 1_000 {
        let k = Double(count) / 1_000
        return k >= 100 ? "\(Int(k))K" : String(format: "%.1fK", k)
    }
    return "\(count)"
}

/// Format a USD cost value as a short string.
///
/// - $100+  → "$12.34"
/// - $1+    → "$1.234"
/// - $0.001+ → "0.5¢"
/// - < $0.001 → "0.08¢"
func formatCost(_ usd: Double) -> String {
    if usd >= 100 {
        return String(format: "$%.2f", usd)
    } else if usd >= 1 {
        return String(format: "$%.3f", usd)
    } else if usd >= 0.001 {
        return String(format: "%.1f¢", usd * 100)
    } else {
        return String(format: "%.2f¢", usd * 100)
    }
}

/// Format a token count as a short human-readable string (same as formatCount).
func formatTokenCount(_ count: Int) -> String {
    formatCount(count)
}
