import SwiftUI

// MARK: - Status Color Mappings

public enum StatusColors {
    // MARK: - Session/Agent Status
    public static func statusColor(_ status: String?) -> Color {
        guard let status = status?.lowercased() else { return .gray }
        switch status {
        case "active", "running", "online": return .green
        case "idle", "paused": return .orange
        case "error", "failed", "critical": return .red
        case "offline", "stopped", "completed": return .secondary
        case "pending", "starting", "connecting": return .yellow
        case "archived", "deleted": return .gray
        default: return .blue
        }
    }

    // MARK: - Message Roles
    public static func roleColor(_ role: String?) -> Color {
        guard let role = role?.lowercased() else { return .primary }
        switch role {
        case "user": return .accentColor
        case "assistant", "agent": return .primary
        case "system": return .secondary
        case "tool": return .purple
        case "error": return .red
        case "reasoning": return .orange
        default: return .primary
        }
    }

    // MARK: - Severity Levels
    public static func severityColor(_ level: String?) -> Color {
        guard let level = level?.lowercased() else { return .gray }
        switch level {
        case "error", "critical", "fatal": return .red
        case "warning": return .orange
        case "info", "debug": return .blue
        case "success": return .green
        default: return .gray
        }
    }

    // MARK: - Connection Status
    public static func connectionColor(_ connected: Bool?) -> Color {
        guard let connected else { return .gray }
        return connected ? .green : .red
    }

    // MARK: - Health Status
    public static func healthColor(_ healthy: Bool?) -> Color {
        guard let healthy else { return .gray }
        return healthy ? .green : .red
    }

    // MARK: - Generic Semantic Colors
    public static let success = Color.green
    public static let warning = Color.orange
    public static let error = Color.red
    public static let info = Color.blue
    public static let muted = Color.secondary
}
