import SwiftUI

/// A reusable HSplitView layout component for list + detail (master-detail) views.
///
/// Provides a 1/3 list + 2/3 detail split layout by default. The list pane has a
/// lower `layoutPriority` so the detail pane expands first, and the divider can still
/// be dragged by the user to adjust proportions.
///
/// Used by `AgentsView`, `MCPServersView`, and `SessionsView`.
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
    let listMinWidth: CGFloat
    let listContent: ListContent
    let detailContent: DetailContent

    init(
        emptyTitle: String,
        emptyIcon: String = "tray",
        emptyDescription: String = "",
        listMinWidth: CGFloat = 260,
        @ViewBuilder listContent: () -> ListContent,
        @ViewBuilder detailContent: () -> DetailContent
    ) {
        self.emptyTitle = emptyTitle
        self.emptyIcon = emptyIcon
        self.emptyDescription = emptyDescription
        self.listMinWidth = listMinWidth
        self.listContent = listContent()
        self.detailContent = detailContent()
    }

    var body: some View {
        HSplitView {
            listContent
                .frame(minWidth: listMinWidth)
                .layoutPriority(0)

            detailContent
                .frame(minWidth: 400)
                .layoutPriority(1)
        }
    }
}
