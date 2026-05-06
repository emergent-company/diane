import SwiftUI

/// A consistent, styled card for displaying a single high-level metric.
///
/// Used in Project Status, Account Status, and Profile views.
///
/// ```swift
/// StatCardView(title: "Total Objects", value: "120", icon: "cube")
/// ```
struct StatCardView: View {
    let title: String
    let value: String
    let icon: String
    var tint: Color = .accentColor

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .foregroundStyle(tint)
                    .font(.system(size: 13, weight: .medium))
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.system(.title2, design: .rounded))
                .fontWeight(.semibold)
                .monospacedDigit()
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.background.opacity(0.8))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(Color.primary.opacity(0.08), lineWidth: 1)
        )
    }
}
