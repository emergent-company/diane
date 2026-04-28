import SwiftUI

// MARK: - EmptyStateView

/// Centralized component for "No Data", "No Results", or "Select a Project" states.
///
/// Usage:
/// ```swift
/// EmptyStateView(
///     title: "No Traces",
///     icon: "chart.bar.doc.horizontal",
///     description: "No traces recorded yet for this project."
/// )
/// ```
struct EmptyStateView: View {
    let title: String
    let icon: String
    var description: String? = nil
    var action: (() -> Void)? = nil
    var actionLabel: String? = nil

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 36, weight: .light))
                .foregroundStyle(.tertiary)

            Text(title)
                .font(.headline)
                .foregroundStyle(.secondary)

            if let desc = description {
                Text(desc)
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 260)
            }

            if let act = action, let label = actionLabel {
                Button(label, action: act)
                    .buttonStyle(.bordered)
                    .padding(.top, 4)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }
}

// MARK: - ErrorBannerView

/// Inline banner for displaying API or validation errors without blocking
/// the user with modal alerts. Shows a retry button if `retryAction` is provided.
///
/// Usage:
/// ```swift
/// ErrorBannerView(message: "Failed to load traces.", retryAction: { viewModel.refresh() })
/// ```
struct ErrorBannerView: View {
    let message: String
    var retryAction: (() -> Void)? = nil

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
            Text(message)
                .font(.caption)
                .foregroundStyle(.primary)
                .lineLimit(2)
            Spacer()
            if let retry = retryAction {
                Button("Retry", action: retry)
                    .font(.caption)
                    .buttonStyle(.bordered)
                    .controlSize(.mini)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color.red.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 6))
        .overlay(
            RoundedRectangle(cornerRadius: 6)
                .strokeBorder(Color.red.opacity(0.2), lineWidth: 1)
        )
    }
}

// MARK: - LoadingStateView

/// A centered ProgressView with contextual text for initial data loads.
///
/// Usage:
/// ```swift
/// LoadingStateView(message: "Loading traces…")
/// ```
struct LoadingStateView: View {
    var message: String = "Loading…"

    var body: some View {
        VStack(spacing: 10) {
            ProgressView()
                .controlSize(.regular)
            Text(message)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
