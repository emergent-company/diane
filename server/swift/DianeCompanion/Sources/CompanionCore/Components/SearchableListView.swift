import SwiftUI
import Combine

/// A generic search + list wrapper that standardises search debouncing
/// and empty-state handling for any collection of identifiable items.
///
/// Usage:
/// ```swift
/// SearchableListView(
///     items: objects,
///     searchText: $searchText,
///     isLoading: viewModel.isLoading,
///     errorMessage: viewModel.errorMessage,
///     emptyTitle: "No objects found",
///     emptyIcon: "cube",
///     onSearch: { query in await viewModel.search(query) }
/// ) { item in
///     ObjectRowView(object: item)
/// }
/// ```
struct SearchableListView<Item: Identifiable & Hashable, RowContent: View>: View {
    let items: [Item]
    @Binding var searchText: String
    var isLoading: Bool = false
    var errorMessage: String? = nil
    var emptyTitle: String = "No results"
    var emptyIcon: String = "magnifyingglass"
    var emptyDescription: String? = nil
    var footerLabel: String? = nil
    var onSearch: ((String) async -> Void)? = nil
    var onRetry: (() -> Void)? = nil

    @ViewBuilder let rowContent: (Item) -> RowContent

    var body: some View {
        VStack(spacing: 0) {
            // Error banner
            if let error = errorMessage {
                ErrorBannerView(message: error, retryAction: onRetry)
                    .padding(.horizontal, 8)
                    .padding(.top, 6)
            }

            // Content
            if isLoading && items.isEmpty {
                LoadingStateView()
            } else if items.isEmpty {
                EmptyStateView(
                    title: emptyTitle,
                    icon: emptyIcon,
                    description: emptyDescription ?? (searchText.isEmpty ? nil : "No results for \"\(searchText)\"")
                )
            } else {
                List(items) { item in
                    rowContent(item)
                }
                .listStyle(.inset)
            }

            // Footer
            if let footer = footerLabel {
                Divider()
                Text(footer)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
            }
        }
        .searchable(text: $searchText, placement: .toolbar)
        .onChange(of: searchText) { newValue in
            Task { await onSearch?(newValue) }
        }
    }
}
