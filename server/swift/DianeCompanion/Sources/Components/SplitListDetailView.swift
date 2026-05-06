import SwiftUI

/// A reusable HSplitView layout component for list + detail (master-detail) views.
///
/// Provides a consistent 50/50 split layout with a minimum width constraint on both
/// panes. Used by `AgentsView`, `MCPServersView`, and `SessionsView`.
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
        HSplitView {
            listContent
                .frame(minWidth: minWidth)
                .layoutPriority(1)

            detailContent
                .frame(minWidth: minWidth)
                .layoutPriority(1)
        }
    }
}
