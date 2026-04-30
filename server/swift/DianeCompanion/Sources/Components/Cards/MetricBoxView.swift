import SwiftUI

/// A small icon + value + label cell used inside agent and provider cards.
///
/// ```swift
/// MetricBoxView(icon: "checkmark", value: "42", label: "OK", color: .green)
/// ```
struct MetricBoxView: View {
    let icon: String
    let value: String
    let label: String
    var color: Color = .secondary

    var body: some View {
        VStack(spacing: Design.Spacing.xxs) {
            HStack(spacing: Design.Spacing.xxs) {
                Image(systemName: icon)
                    .font(.system(size: Design.IconSize.tiny))
                    .foregroundStyle(color)
                Text(value)
                    .font(.caption)
                    .fontWeight(.medium)
                    .monospacedDigit()
                    .foregroundStyle(.primary)
            }
            Text(label)
                .font(.system(size: Design.IconSize.tiny))
                .foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, Design.Padding.sectionHeader)
        .background(Design.Surface.cardBackground)
        .cornerRadius(Design.CornerRadius.medium)
    }
}

// MARK: - Previews

#Preview {
    HStack(spacing: Design.Spacing.sm) {
        MetricBoxView(icon: "arrow.triangle.branch", value: "42", label: "Runs")
        MetricBoxView(icon: "checkmark", value: "40", label: "OK", color: .green)
        MetricBoxView(icon: "xmark", value: "2", label: "Err", color: .red)
        MetricBoxView(icon: "clock", value: "24s", label: "Avg")
        MetricBoxView(icon: "dollarsign", value: "$0.01", label: "/run")
    }
    .padding()
    .frame(width: 500)
}
