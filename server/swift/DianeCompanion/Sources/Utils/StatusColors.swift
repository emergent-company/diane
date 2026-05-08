import SwiftUI

/// Centralized color mappings for all status/role-based UI indicators.
///
/// Each domain has its own namespace to keep semantic intent clear
/// and avoid accidental cross-domain reuse.
enum StatusColors {

    // MARK: - Session Status

    /// Active session → green,  paused/idle → orange,  error → red,  rest → secondary
    static func sessionStatus(_ status: String) -> Color {
        switch status.lowercased() {
        case "active", "running":    return .green
        case "paused", "idle":       return .orange
        case "completed", "closed",
             "done":                 return .secondary
        case "error", "failed":      return .red
        default:                     return .secondary
        }
    }

    // MARK: - Schema Type Status

    /// Active/open schema → green,  inactive/closed → gray,  error → red,  rest → secondary
    static func schemaStatus(_ status: String) -> Color {
        switch status.lowercased() {
        case "active", "open":       return .green
        case "inactive", "closed":   return .gray
        case "error", "failed":      return .red
        default:                     return .secondary
        }
    }

    // MARK: - Trace Status

    /// Completed/success → green,  running → blue,  failed → red,  pending → orange
    static func traceStatus(for status: String) -> Color {
        switch status.lowercased() {
        case "completed", "success": return .green
        case "running", "processing": return .blue
        case "failed", "error":      return .red
        case "pending", "queued":    return .orange
        default:                     return .secondary
        }
    }

    /// Trace status with 15 % opacity (for background fills)
    static func traceStatusBackground(for status: String) -> Color {
        traceStatus(for: status).opacity(0.15)
    }

    // MARK: - Doctor (System Health) Status

    /// ok → green,  warning → orange,  error → red,  rest → secondary
    static func doctorStatus(_ status: String) -> Color {
        switch status {
        case "ok":                   return .green
        case "warning":              return .orange
        case "error":                return .red
        default:                     return .secondary
        }
    }

    // MARK: - Message Role

    /// Color associated with each chat message role.
    static func messageRole(_ role: String) -> Color {
        switch role.lowercased() {
        case "user":                 return .blue
        case "assistant":            return .green
        case "system":               return .orange
        case "tool":                 return .purple
        default:                     return .secondary
        }
    }

    // MARK: - Agent Flow

    /// Color for each agent flow type.
    static func agentFlow(_ flow: String) -> Color {
        switch flow.lowercased() {
        case "chat", "":             return .green
        case "agent":                return .purple
        case "chain":                return .orange
        case "workflow":             return .blue
        default:                     return .secondary
        }
    }
}
