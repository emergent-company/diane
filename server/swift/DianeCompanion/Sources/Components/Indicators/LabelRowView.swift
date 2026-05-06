import SwiftUI

/// A small icon + label + value row used inside info cards.
///
/// ```swift
/// LabelRowView(icon: "sparkle", label: "Model", value: "gpt-4")
/// ```
struct LabelRowView: View {
    let icon: String
    let label: String
    let value: String

    var body: some View {
        HStack(spacing: Design.Spacing.xs) {
            Image(systemName: icon)
                .font(.system(size: Design.IconSize.tiny))
                .foregroundStyle(.secondary)
            Text(label + ":")
                .font(.caption2)
                .foregroundStyle(.tertiary)
            Text(value)
                .font(.caption2)
                .foregroundStyle(.primary)
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }
}

// MARK: - Previews

#Preview {
    VStack(alignment: .leading, spacing: Design.Spacing.xs) {
        LabelRowView(icon: "sparkle", label: "Model", value: "gpt-4o")
        LabelRowView(icon: "square.text.square", label: "Embed", value: "text-embedding-3-small")
        LabelRowView(icon: "link", label: "URL", value: "https://api.openai.com/v1")
    }
    .padding()
    .frame(width: 300)
}
