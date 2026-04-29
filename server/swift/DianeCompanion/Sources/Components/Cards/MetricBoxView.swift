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
        VStack(spacing: 2) {
            HStack(spacing: 3) {
                Image(systemName: icon)
                    .font(.system(size: 9))
                    .foregroundStyle(color)
                Text(value)
                    .font(.caption)
                    .fontWeight(.medium)
                    .monospacedDigit()
                    .foregroundStyle(.primary)
            }
            Text(label)
                .font(.system(size: 9))
                .foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 4)
        .background(Color.primary.opacity(0.04))
        .cornerRadius(6)
    }
}

// MARK: - Previews

#Preview {
    HStack(spacing: 8) {
        MetricBoxView(icon: "arrow.triangle.branch", value: "42", label: "Runs")
        MetricBoxView(icon: "checkmark", value: "40", label: "OK", color: .green)
        MetricBoxView(icon: "xmark", value: "2", label: "Err", color: .red)
        MetricBoxView(icon: "clock", value: "24s", label: "Avg")
        MetricBoxView(icon: "dollarsign", value: "$0.01", label: "/run")
    }
    .padding()
    .frame(width: 500)
}
