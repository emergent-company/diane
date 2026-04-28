import SwiftUI

/// A generic three-column layout container matching the standard Emergent app
/// UI pattern: a sidebar list on the left and a detail panel on the right.
///
/// This does NOT manage the outer sidebar — it fills the content + detail
/// columns (i.e., it is used inside a `NavigationSplitView` detail pane).
///
/// Usage:
/// ```swift
/// ThreeColumnDetailView(items: traces, selection: $selectedTrace) { trace in
///     TraceRowView(trace: trace)
/// } detail: { trace in
///     TraceDetailView(trace: trace)
/// }
/// ```
struct ThreeColumnDetailView<Item: Identifiable & Hashable,
                              ListContent: View,
                              DetailContent: View>: View {

    let items: [Item]
    @Binding var selection: Item?

    @ViewBuilder let listContent: (Item) -> ListContent
    @ViewBuilder let detail: (Item) -> DetailContent

    /// Optional footer label (e.g. "12 objects found")
    var footerLabel: String? = nil

    var body: some View {
        HSplitView {
            // Left: list pane
            VStack(spacing: 0) {
                List(items, selection: $selection) { item in
                    listContent(item)
                        .tag(item)
                }
                .listStyle(.inset)

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
            .frame(minWidth: 260, idealWidth: 320)

            // Right: detail pane
            Group {
                if let item = selection {
                    detail(item)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    EmptyStateView(
                        title: "Nothing selected",
                        icon: "sidebar.right",
                        description: "Select an item from the list to view its details."
                    )
                }
            }
            .frame(minWidth: 280)
        }
    }
}
