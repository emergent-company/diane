import SwiftUI
import Combine

/// Objects Browser — search and inspect graph objects.
/// Uses debounced search to avoid backend spam.
///
/// Tasks 7.3, 7.5
struct ObjectsBrowserView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State var searchText: String = ""
    @State var objects: [GraphObject] = []
    @State var selectedObject: GraphObject? = nil
    @State var isLoading = false
    @State var error: String? = nil

    // Task 7.5: Debounce via Combine publisher
    @State private var searchSubject = PassthroughSubject<String, Never>()
    @State private var cancellables = Set<AnyCancellable>()

    var body: some View {
        HSplitView {
            // Left: search + list
            VStack(spacing: 0) {
                if let err = error {
                    ErrorBannerView(message: err) {
                        Task { await performSearch(searchText) }
                    }
                    .padding(8)
                }

                // Search bar
                HStack {
                    Image(systemName: "magnifyingglass")
                        .foregroundStyle(.secondary)
                    TextField("Search objects…", text: $searchText)
                        .textFieldStyle(.plain)
                        .onChange(of: searchText) {
                            searchSubject.send(searchText)
                        }
                    if !searchText.isEmpty {
                        Button {
                            searchText = ""
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(8)
                .background(Color.primary.opacity(0.04))

                Divider()

                if isLoading {
                    LoadingStateView(message: "Searching…")
                } else if objects.isEmpty && !searchText.isEmpty {
                    // Task 7.5: No results empty state
                    EmptyStateView(
                        title: "No Results",
                        icon: "cube",
                        description: "No objects found for '\(searchText)'."
                    )
                } else if objects.isEmpty {
                    EmptyStateView(
                        title: "Search Objects",
                        icon: "cube",
                        description: "Type to search for objects in this project."
                    )
                } else {
                    List(objects, selection: $selectedObject) { object in
                        objectRow(object)
                            .tag(object)
                    }
                    .listStyle(.plain)
                }

                Divider()
                HStack {
                    Text(objects.isEmpty ? "No objects found" : "\(objects.count) object\(objects.count == 1 ? "" : "s") found")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
            }
            .frame(minWidth: 260)

            // Right: detail panel
            if let obj = selectedObject {
                objectDetailPanel(obj)
                    .frame(minWidth: 260)
            } else {
                EmptyStateView(
                    title: "No Selection",
                    icon: "cube",
                    description: "Select an object to view its properties."
                )
                .frame(minWidth: 260)
            }
        }
        .navigationTitle("Objects")
        .task {
            setupDebounce()
            guard let projectID = appState.activeProjectID, !projectID.isEmpty else { return }
            await performSearch("")
        }
        .onChange(of: appState.selectedProject) {
            objects = []
            selectedObject = nil
            Task { await performSearch(searchText) }
        }
    }

    // MARK: - Object Row

    private func objectRow(_ object: GraphObject) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(object.id)
                    .font(.system(.caption, design: .monospaced))
                    .lineLimit(1)
                Spacer()
                if let score = object.score {
                    Text(String(format: "%.2f", score))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .monospacedDigit()
                }
            }
            if let type = object.type {
                Text(type)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }

    // MARK: - Detail Panel

    private func objectDetailPanel(_ obj: GraphObject) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(obj.id)
                        .font(.system(.subheadline, design: .monospaced))
                        .fontWeight(.semibold)
                        .lineLimit(1)
                    if let type = obj.type {
                        Text(type)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            Text("Properties")
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 12)
                .padding(.top, 8)

            JSONPropertyViewer(properties: obj.properties)
        }
    }

    // MARK: - Debounce setup (task 7.5)

    private func setupDebounce() {
        searchSubject
            .debounce(for: .milliseconds(300), scheduler: DispatchQueue.main)
            .removeDuplicates()
            .sink { query in
                Task { await self.performSearch(query) }
            }
            .store(in: &cancellables)
    }

    @MainActor
    func performSearch(_ query: String, using testClient: EmergentAPIClient? = nil, projectID: String? = nil) async {
        guard let pid = projectID ?? appState.activeProjectID else { return }
        isLoading = true
        error = nil
        let client = testClient ?? apiClient
        let result = await Self.searchObjects(client: client, projectID: pid, query: query)
        objects = result.objects
        error = result.error
        if !objects.contains(where: { $0.id == selectedObject?.id }) {
            selectedObject = nil
        }
        isLoading = false
    }

    /// Pure function for testing.
    static func searchObjects(client: EmergentAPIClient, projectID: String, query: String) async -> (objects: [GraphObject], error: String?) {
        do {
            let objs = try await client.searchObjects(projectID: projectID, query: query)
            return (objs, nil)
        } catch {
            return ([], error.localizedDescription)
        }
    }
}
