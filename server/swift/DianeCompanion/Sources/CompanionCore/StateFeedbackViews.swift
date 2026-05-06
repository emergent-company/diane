import SwiftUI

/// Full-page empty state with icon, title, description, and optional action button.
struct EmptyStateView: View {
    let title: String
    var icon: String = "tray"
    var description: String = ""
    var action: (() -> Void)? = nil
    var actionLabel: String? = nil

    var body: some View {
        VStack(spacing: Design.Spacing.md) {
            Spacer()
            Image(systemName: icon)
                .font(.system(size: Design.IconSize.large))
                .foregroundStyle(.secondary)
                .padding(.bottom, Design.Spacing.xs)
            Text(title)
                .font(.title3)
                .fontWeight(.medium)
            if !description.isEmpty {
                Text(description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, Design.Padding.card * 3)
            }
            if let action = action, let label = actionLabel {
                Button(label, action: action)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .padding(.top, Design.Spacing.xs)
            }
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// Centered loading spinner with a message.
struct LoadingStateView: View {
    let message: String

    init(message: String = "Loading…") {
        self.message = message
    }

    var body: some View {
        VStack(spacing: Design.Spacing.sm) {
            ProgressView()
                .controlSize(.large)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// Inline error banner with retry button.
struct ErrorBannerView: View {
    let message: String
    var retry: (() -> Void)? = nil

    var body: some View {
        HStack(spacing: Design.Spacing.sm) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
            Text(message)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)
            Spacer()
            if let retry = retry {
                Button("Retry", action: retry)
                    .font(.caption)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.mini)
            }
        }
        .padding(Design.Padding.banner)
        .background(Design.Semantic.warning.opacity(0.08))
        .cornerRadius(Design.CornerRadius.medium)
    }
}
