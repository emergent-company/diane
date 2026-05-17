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

    // MARK: - ACP Status Animation (pulsing dots)

    /// Defines the animation type for a session status dot.
    /// `.static` for terminal/completed states, `.pulse` for active/in-progress states.
    public enum StatusAnimation: Equatable, Sendable {
        /// No animation — dot is static
        case `static`
        /// Gentle pulse — indicates activity (submitted, working, cancelling, or nil/no runs)
        case pulse
    }

    /// Returns the animation type for a given ACP `last_run_status` value.
    /// - Parameter status: The raw status string from the ACP API (`completed`, `failed`, `submitted`, `working`, etc.)
    /// - Returns: `.pulse` for active states, `.static` for terminal states
    public static func statusAnimation(_ status: String?) -> StatusAnimation {
        guard let status = status?.lowercased(), !status.isEmpty else {
            return .pulse  // nil = no runs yet = waiting
        }
        switch status {
        case "submitted", "working", "cancelling":
            return .pulse
        default:
            return .static  // completed, failed, input-required, cancelled, skipped
        }
    }
}
