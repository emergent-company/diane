import SwiftUI
import Combine

/// Document Browser — two-column layout: list on the left, rich detail panel on the right.
/// Uses debounced search to avoid backend spam.
struct DocumentBrowserView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var searchText: String = ""
    @State private var documents: [Document] = []
    @State private var selectedDocument: Document? = nil
    @State private var isLoading = false
    @State private var error: String? = nil

    @State private var searchSubject = PassthroughSubject<String, Never>()
    @State private var cancellables = Set<AnyCancellable>()

    var body: some View {
        NavigationSplitView {
            listColumn
                .navigationSplitViewColumnWidth(min: 220, ideal: 260, max: 320)
        } detail: {
            if let doc = selectedDocument {
                DocumentDetailView(document: doc)
            } else {
                EmptyStateView(
                    title: "No Selection",
                    icon: "doc.text",
                    description: "Select a document to view its details."
                )
            }
        }
        .navigationTitle("Documents")
        .task {
            setupDebounce()
            await performSearch("")
        }
        .onChange(of: appState.selectedProject) { _ in
            documents = []
            selectedDocument = nil
            Task { await performSearch(searchText) }
        }
    }

    // MARK: - List column

    private var listColumn: some View {
        VStack(spacing: 0) {
            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await performSearch(searchText) }
                }
                .padding(8)
            }

            // Search bar
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                    .imageScale(.small)
                TextField("Search documents…", text: $searchText)
                    .textFieldStyle(.plain)
                    .onChange(of: searchText) { newValue in
                        searchSubject.send(newValue)
                    }
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.secondary)
                            .imageScale(.small)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .background(Color.primary.opacity(0.04))

            Divider()

            if isLoading {
                LoadingStateView(message: "Searching…")
            } else if documents.isEmpty && !searchText.isEmpty {
                EmptyStateView(
                    title: "No Results",
                    icon: "doc.text",
                    description: "No documents match '\(searchText)'."
                )
            } else if documents.isEmpty {
                EmptyStateView(
                    title: "No Documents",
                    icon: "doc.text",
                    description: "No documents found in this project."
                )
            } else {
                List(documents, id: \.id, selection: $selectedDocument) { doc in
                    documentRow(doc)
                        .tag(doc)
                }
                .listStyle(.plain)
            }

            Divider()
            HStack {
                Text(countLabel)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
        }
    }

    private var countLabel: String {
        if isLoading { return "Loading…" }
        if documents.isEmpty { return "No documents" }
        return "\(documents.count) document\(documents.count == 1 ? "" : "s")"
    }

    // MARK: - Document Row

    private func documentRow(_ doc: Document) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(doc.filename)
                .font(.subheadline)
                .lineLimit(1)
            HStack(spacing: 6) {
                if let mime = doc.mimeType {
                    Label(mimeTypeLabel(mime), systemImage: mimeTypeIcon(mime))
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .labelStyle(.titleAndIcon)
                } else if let sourceType = doc.sourceType {
                    Text(sourceType)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                if let status = doc.conversionStatus, status != "not_required" {
                    statusBadge(status)
                }
            }
        }
        .padding(.vertical, 2)
    }

    // MARK: - Helpers

    private func mimeTypeLabel(_ mime: String) -> String {
        switch mime {
        case "audio/mpeg", "audio/mp4", "audio/wav": return "Audio"
        case "video/mp4", "video/mpeg":              return "Video"
        case "application/pdf":                      return "PDF"
        case "text/plain":                           return "Text"
        case "text/markdown":                        return "Markdown"
        case "application/json":                     return "JSON"
        default:
            if mime.hasPrefix("image/") { return "Image" }
            if mime.hasPrefix("text/")  { return "Text" }
            return mime
        }
    }

    private func mimeTypeIcon(_ mime: String) -> String {
        switch mime {
        case let m where m.hasPrefix("audio/"): return "waveform"
        case let m where m.hasPrefix("video/"): return "film"
        case "application/pdf":                 return "doc.richtext"
        case let m where m.hasPrefix("image/"): return "photo"
        default:                                return "doc.text"
        }
    }

    @ViewBuilder
    private func statusBadge(_ status: String) -> some View {
        let (label, color) = statusInfo(status)
        Text(label)
            .font(.caption2)
            .foregroundStyle(color)
    }

    private func statusInfo(_ status: String) -> (String, Color) {
        switch status {
        case "pending":    return ("Pending", .orange)
        case "processing": return ("Processing", .blue)
        case "completed":  return ("Done", .green)
        case "failed":     return ("Failed", .red)
        default:           return (status, .secondary)
        }
    }

    // MARK: - Debounce

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
    private func performSearch(_ query: String) async {
        guard let projectID = appState.activeProjectID else { return }
        isLoading = true
        error = nil
        do {
            documents = try await apiClient.searchDocuments(projectID: projectID, query: query)
            if !documents.contains(where: { $0.id == selectedDocument?.id }) {
                selectedDocument = nil
            }
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}

// Document needs Hashable/Equatable for selection binding
extension Document: Hashable, Equatable {
    public static func == (lhs: Document, rhs: Document) -> Bool { lhs.id == rhs.id }
    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
}
