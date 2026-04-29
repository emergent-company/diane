import SwiftUI

/// Full-page empty state with icon, title, description, and optional action button.
struct EmptyStateView: View {
    let title: String
    var icon: String = "tray"
    var description: String = ""
    var action: (() -> Void)? = nil
    var actionLabel: String? = nil

    var body: some View {
        VStack(spacing: 12) {
            Spacer()
            Image(systemName: icon)
                .font(.system(size: 32))
                .foregroundStyle(.secondary)
                .padding(.bottom, 4)
            Text(title)
                .font(.title3)
                .fontWeight(.medium)
            if !description.isEmpty {
                Text(description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 40)
            }
            if let action = action, let label = actionLabel {
                Button(label, action: action)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .padding(.top, 4)
            }
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// Centered loading spinner with a message.
struct LoadingStateView: View {
    let message: String

    var body: some View {
        VStack(spacing: 10) {
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
        HStack(spacing: 8) {
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
        .padding(10)
        .background(Color.orange.opacity(0.08))
        .cornerRadius(6)
    }
}

/// A reusable HSplitView with a list on the left and detail on the right.
/// Both sides default to 50/50 via equal layoutPriority.
///
/// Usage:
/// ```swift
/// SplitListDetailView(
///     emptyTitle: "Select an Item",
///     emptyIcon: "tray",
///     emptyDescription: "Select an item from the list."
/// ) {
///     myListView
/// } detail: {
///     if let item = selectedItem { myDetailPanel(item) }
/// }
/// ```
struct SplitListDetailView<ListContent: View, DetailContent: View>: View {
    let emptyTitle: String
    var emptyIcon: String = "tray"
    var emptyDescription: String = ""
    let minWidth: CGFloat
    let listContent: ListContent
    let detailContent: DetailContent

    @State private var dividerRatio: CGFloat = 0.5

    init(
        emptyTitle: String,
        emptyIcon: String = "tray",
        emptyDescription: String = "",
        minWidth: CGFloat = 220,
        @ViewBuilder listContent: () -> ListContent,
        @ViewBuilder detailContent: () -> DetailContent
    ) {
        self.emptyTitle = emptyTitle
        self.emptyIcon = emptyIcon
        self.emptyDescription = emptyDescription
        self.minWidth = minWidth
        self.listContent = listContent()
        self.detailContent = detailContent()
    }

    var body: some View {
        GeometryReader { geo in
            let listWidth = max(minWidth, (geo.size.width - 4) * dividerRatio)
            let detailWidth = max(minWidth, (geo.size.width - 4) * (1 - dividerRatio))

            HStack(spacing: 0) {
                listContent
                    .frame(width: listWidth)
                    .clipped()

                Rectangle()
                    .fill(.separator)
                    .frame(width: 4)
                    .onHover { inside in
                        if inside { NSCursor.resizeLeftRight.push() }
                        else { NSCursor.pop() }
                    }
                    .gesture(
                        DragGesture()
                            .onChanged { value in
                                let newRatio = value.location.x / geo.size.width
                                dividerRatio = min(0.8, max(0.2, newRatio))
                            }
                    )

                detailContent
                    .frame(width: detailWidth)
                    .clipped()
            }
        }
        .frame(minHeight: 200)
    }
}
