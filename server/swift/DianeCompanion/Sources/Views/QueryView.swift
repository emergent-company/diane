import SwiftUI

/// Query view — semantic search against the graph using /api/graph/search.
struct QueryView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var queryText: String = ""
    @State private var result: QueryResult? = nil
    @State private var selectedItem: QueryResultItem? = nil
    @State private var isRunning = false
    @State private var error: String? = nil

    var body: some View {
        HSplitView {
            // Left: query editor + results list
            VStack(spacing: 0) {
                editorPane
                    .frame(minHeight: 100, idealHeight: 140, maxHeight: 260)
                Divider()
                resultsPane
            }
            .frame(minWidth: 320)

            // Right: detail panel
            if let item = selectedItem {
                objectDetailPanel(item)
                    .frame(minWidth: 260)
            } else {
                EmptyStateView(
                    title: "Select a Result",
                    icon: "doc.text.magnifyingglass",
                    description: "Select a result to view the full object."
                )
                .frame(minWidth: 260)
            }
        }
        .navigationTitle("Query")
    }

    // MARK: - Editor

    private var editorPane: some View {
        VStack(spacing: 0) {
            TextEditor(text: $queryText)
                .font(.system(.body, design: .monospaced))
                .padding(8)
                .frame(maxWidth: .infinity, maxHeight: .infinity)

            Divider()

            HStack {
                if let projectName = appState.selectedProject?.name {
                    Text(projectName)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    Text("No project selected")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
                Spacer()
                if let elapsed = result?.meta?.elapsedMs {
                    Text(String(format: "%.0fms", elapsed))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Button("Run") {
                    Task { await runQuery() }
                }
                .disabled(queryText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                          || appState.activeProjectID == nil
                          || isRunning)
                .keyboardShortcut(.return, modifiers: .command)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
    }

    // MARK: - Results list

    @ViewBuilder
    private var resultsPane: some View {
        if isRunning {
            LoadingStateView(message: "Running query…")
        } else if let err = error {
            VStack {
                ErrorBannerView(message: err) {
                    Task { await runQuery() }
                }
                .padding()
                Spacer()
            }
        } else if let items = result?.data, !items.isEmpty {
            resultsList(items)
        } else if result != nil {
            EmptyStateView(
                title: "No Results",
                icon: "magnifyingglass",
                description: "The query returned no matches."
            )
        } else {
            EmptyStateView(
                title: "Run a Search",
                icon: "magnifyingglass",
                description: "Enter a search query and press Cmd+Return."
            )
        }
    }

    private func resultsList(_ items: [QueryResultItem]) -> some View {
        VStack(spacing: 0) {
            List(items, selection: $selectedItem) { item in
                resultRow(item)
                    .tag(item)
            }
            .listStyle(.plain)

            Divider()
            HStack {
                let total = result?.total ?? items.count
                Text("\(total) result\(total == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
    }

    private func resultRow(_ item: QueryResultItem) -> some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    if let type = item.object.type {
                        Text(type)
                            .font(.caption2)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(Color.accentColor.opacity(0.15))
                            .cornerRadius(3)
                    }
                    Text(item.object.id)
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
                if let key = item.object.properties?["key"]?.value as? String {
                    Text(key)
                        .font(.caption)
                        .lineLimit(1)
                }
            }
            Spacer()
            if let score = item.score {
                Text(String(format: "%.2f", score))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
        }
        .padding(.vertical, 2)
    }

    // MARK: - Object detail panel

    private func objectDetailPanel(_ item: QueryResultItem) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        if let type = item.object.type {
                            Text(type)
                                .font(.caption)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Color.accentColor.opacity(0.15))
                                .cornerRadius(4)
                        }
                        if let score = item.score {
                            Text("score \(String(format: "%.3f", score))")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Text(item.object.id)
                        .font(.system(.caption2, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            if let props = item.object.properties, !props.isEmpty {
                List {
                    Section("Properties") {
                        ForEach(props.keys.sorted(), id: \.self) { key in
                            HStack(alignment: .top) {
                                Text(key)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .frame(width: 100, alignment: .leading)
                                Text(propString(props[key]))
                                    .font(.system(.caption, design: .monospaced))
                                    .lineLimit(4)
                                    .multilineTextAlignment(.leading)
                            }
                            .padding(.vertical, 1)
                        }
                    }
                }
                .listStyle(.plain)
            } else {
                EmptyStateView(
                    title: "No Properties",
                    icon: "square.dashed",
                    description: "This object has no properties."
                )
            }
        }
    }

    private func propString(_ v: AnyCodable?) -> String {
        guard let v else { return "—" }
        switch v.value {
        case let s as String: return s
        case let n as Int:    return "\(n)"
        case let d as Double: return "\(d)"
        case let b as Bool:   return b ? "true" : "false"
        default:              return "\(v.value)"
        }
    }

    // MARK: - Run

    @MainActor
    private func runQuery() async {
        guard let projectID = appState.activeProjectID else { return }
        let q = queryText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !q.isEmpty else { return }
        isRunning = true
        error = nil
        selectedItem = nil
        do {
            result = try await apiClient.executeQuery(projectID: projectID, query: q)
        } catch {
            self.error = error.localizedDescription
            result = nil
        }
        isRunning = false
    }
}
