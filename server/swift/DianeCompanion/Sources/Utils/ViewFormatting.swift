import Foundation
import SwiftUI

/// Centralized view formatting utilities extracted from individual views.
///
/// Each function can be called directly rather than instantiating a view,
/// making tests simpler and avoiding duplication.
enum ViewFormatting {

    // MARK: - ID & Name Formatting

    /// Short form: last 6 characters of the session ID.
    static func sessionIDShortForm(_ id: String) -> String {
        if id.count <= 6 { return id }
        return String(id.suffix(6))
    }

    /// Strip common prefixes from agent names for compact display.
    static func agentShortName(_ name: String) -> String {
        for prefix in ["discord-", "diane-", "agent-"] {
            if name.hasPrefix(prefix) {
                return String(name.dropFirst(prefix.count))
            }
        }
        return name
    }

    /// Strip Calendar/Financial/Shopping prefixes from schema type names.
    static func shortTypeName(_ name: String) -> String {
        let prefixes = ["Calendar", "Financial", "Shopping"]
        for prefix in prefixes {
            if name.hasPrefix(prefix) {
                return String(name.dropFirst(prefix.count))
            }
        }
        return name
    }

    /// Determine namespace category from type name.
    static func typeNamespace(_ typeName: String) -> String {
        if typeName.hasPrefix("Diane") || typeName == "SkillMonitorCheckpoint" {
            return "system"
        }
        return "personal"
    }

    /// Color for namespace badge.
    static func typeNamespaceColor(_ typeName: String) -> Color {
        typeNamespace(typeName) == "system" ? Color.purple : Color.blue
    }

    /// Color for chat bubble backgrounds.
    static func bubbleBackground(isUser: Bool, isSystem: Bool) -> Color {
        if isUser { return Color.blue.opacity(0.12) }
        if isSystem { return Color.clear }
        return Color.primary.opacity(0.05)
    }

    /// Color for chat bubble tail (same as background).
    static func bubbleTailColor(isUser: Bool, isSystem: Bool) -> Color {
        bubbleBackground(isUser: isUser, isSystem: isSystem)
    }

    // MARK: - Date Formatting

    /// Format ISO8601 date string as "MMM d, yyyy HH:mm" for schema detail.
    static func formatSchemaDate(_ iso: String) -> String {
        guard let date = Self.isoFormatter.date(from: iso) ?? Self.isoFormatterNoFractional.date(from: iso) else {
            return iso
        }
        let df = DateFormatter()
        df.dateFormat = "MMM d, yyyy HH:mm"
        return df.string(from: date)
    }

    /// Format ISO8601 date as a friendly string.
    static func friendlyDate(_ iso: String) -> String {
        guard let date = Self.isoFormatter.date(from: iso) ?? Self.isoFormatterNoFractional.date(from: iso) else {
            return iso
        }
        return Self.friendlyFormatter.string(from: date)
    }

    /// Format a Date as a friendly string.
    static func friendlyDate(_ date: Date) -> String {
        Self.friendlyFormatter.string(from: date)
    }

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

    private static let friendlyFormatter: DateFormatter = {
        let df = DateFormatter()
        df.dateFormat = "MMM d, yyyy HH:mm"
        return df
    }()

    // MARK: - Tool Call Formatting

    /// Pretty-print tool arguments JSON.
    static func formatToolArgs(_ raw: String) -> String {
        guard let data = raw.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data),
              let pretty = try? JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys, .withoutEscapingSlashes]),
              let str = String(data: pretty, encoding: .utf8)
        else { return raw }
        return str
    }

    // MARK: - Profile Formatting

    /// Extract initials from a name string.
    static func initials(for name: String?) -> String {
        guard let name, !name.isEmpty else { return "?" }
        let parts = name.split(separator: " ")
        if parts.count >= 2 {
            let initials = parts.compactMap { $0.first }.prefix(2).map { String($0).uppercased() }
            return initials.joined()
        }
        return String(name.prefix(2)).uppercased()
    }

    /// Mask an API key, showing only the last 4 characters.
    static func maskedKey(_ key: String) -> String {
        let suffix = key.suffix(4)
        return "****\(suffix)"
    }

    // MARK: - Query Formatting

    /// Convert an AnyCodable value to a display string.
    static func propString(_ v: AnyCodable?) -> String {
        guard let v else { return "—" }
        switch v.value {
        case let s as String: return s
        case let i as Int: return "\(i)"
        case let d as Double:
            if d == floor(d) { return "\(Int(d))" }
            return String(format: "%.2f", d)
        case let b as Bool: return b ? "true" : "false"
        default: return "\(v.value)"
        }
    }

    // MARK: - Node Sorting

    /// Sort order for relay node modes.
    static func modeOrder(_ mode: String?) -> Int {
        switch mode {
        case "master":   return 0
        case "slave":    return 1
        default:         return 2
        }
    }

    /// Sort nodes so master is first, then slave, then others. Within same mode, sort by hostname.
    static func sortedNodes(_ nodes: [RelayNode]) -> [RelayNode] {
        nodes.sorted { a, b in
            let orderA = modeOrder(a.mode)
            let orderB = modeOrder(b.mode)
            if orderA != orderB { return orderA < orderB }
            return (a.hostname ?? "") < (b.hostname ?? "")
        }
    }
}
