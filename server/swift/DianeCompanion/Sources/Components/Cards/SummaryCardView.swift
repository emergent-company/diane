import SwiftUI

/// A stat card showing an icon, title, and large value.
///
/// Used in the dashboard summary grid for quick-glance metrics
/// like Total Runs, Success Rate, Avg Duration, Total Tokens, Total Cost.
///
/// ```swift
/// SummaryCardView(
///     title: "Total Runs",
///     value: "42",
///     icon: "arrow.triangle.branch",
///     color: .blue
/// )
/// ```
struct SummaryCardView: View {
    let title: String
    let value: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: icon)
                    .foregroundStyle(color)
                    .font(.system(size: Design.IconSize.medium, weight: .semibold))
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.title2)
                .fontWeight(.bold)
                .monospacedDigit()
                .foregroundStyle(.primary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardStyle()
    }
}

// MARK: - Previews

#Preview {
    HStack(spacing: Design.Spacing.md) {
        SummaryCardView(title: "Total Runs", value: "42", icon: "arrow.triangle.branch", color: .blue)
        SummaryCardView(title: "Success Rate", value: "95.2%", icon: "checkmark.circle.fill", color: .green)
        SummaryCardView(title: "Avg Duration", value: "24.1s", icon: "clock.fill", color: .orange)
        SummaryCardView(title: "Total Cost", value: "$0.001", icon: "dollarsign.circle.fill", color: .yellow)
    }
    .padding()
    .frame(width: 800)
}
