import SwiftUI

/// A small badge showing a status icon and text label.
///
/// Used for session status, agent health, and general state indicators.
///
/// ```swift
/// StatusBadgeView(status: "completed")
/// StatusBadgeView(text: "Healthy", color: .green)
/// ```
struct StatusBadgeView: View {
    var text: String
    var color: Color
    var icon: String?

    /// Initialize from a session/status string (auto-maps common statuses).
    init(status: String) {
        let lower = status.lowercased()
        switch lower {
        case "active", "running":
            self.text = "Active"
            self.color = .green
            self.icon = "circle.fill"
        case "paused", "idle":
            self.text = "Paused"
            self.color = .orange
            self.icon = "pause.circle.fill"
        case "completed", "closed", "done":
            self.text = "Completed"
            self.color = .gray
            self.icon = "checkmark.circle.fill"
        case "error", "failed":
            self.text = "Error"
            self.color = .red
            self.icon = "exclamationmark.circle.fill"
        default:
            self.text = lower.capitalized
            self.color = .gray
            self.icon = "circle.dashed"
        }
    }

    /// Initialize with explicit values.
    init(text: String, color: Color, icon: String? = nil) {
        self.text = text
        self.color = color
        self.icon = icon
    }

    var body: some View {
        HStack(spacing: 4) {
            if let icon = icon {
                Image(systemName: icon)
                    .font(.system(size: 9))
                    .foregroundStyle(color)
            }
            Text(text)
                .font(.caption2)
                .fontWeight(.medium)
                .foregroundStyle(color)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(color.opacity(0.1))
        .cornerRadius(4)
    }
}

/// A colored dot representing a session/status state.
///
/// ```swift
/// StatusDotView(status: "completed")
/// ```
struct StatusDotView: View {
    let status: String?

    private var color: Color {
        guard let s = status?.lowercased() else { return .gray }
        switch s {
        case "active", "running":  return .green
        case "paused", "idle":     return .orange
        case "completed", "closed", "done": return .secondary
        case "error", "failed":    return .red
        default:                   return .gray
        }
    }

    var body: some View {
        Image(systemName: "circle.fill")
            .font(.system(size: 10))
            .foregroundStyle(color)
    }
}

// MARK: - Previews

#Preview {
    HStack(spacing: 8) {
        StatusBadgeView(status: "active")
        StatusBadgeView(status: "completed")
        StatusBadgeView(status: "error")
        StatusBadgeView(status: "paused")
        StatusBadgeView(text: "Healthy", color: .green, icon: "checkmark.circle.fill")
    }
    .padding()
}

#Preview {
    HStack(spacing: 8) {
        StatusDotView(status: "active")
        StatusDotView(status: "completed")
        StatusDotView(status: "error")
        StatusDotView(status: "paused")
        StatusDotView(status: "unknown")
    }
    .padding()
}
