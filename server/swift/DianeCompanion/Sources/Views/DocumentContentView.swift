import SwiftUI
import AppKit

// MARK: - Content Segment Model

/// A slice of the document, either pre-chunk prose or a named chunk block.
private struct ContentSegment: Identifiable {
    enum Kind {
        case prelude           // text before the first chunk
        case chunk(DocumentChunk)
    }
    let id: String
    let text: String
    let kind: Kind
}

// MARK: - Scroll Coordinator

/// Shared object that lets the chunk sidebar drive scrolling in the content area.
/// The content ScrollViewReader registers its proxy here; sidebar rows call scrollTo().
private final class ContentScrollCoordinator: ObservableObject {
    var scrollTo: ((String) -> Void)?

    func jump(to id: String) {
        scrollTo?(id)
    }
}

// MARK: - DocumentContentView

/// Full-text viewer window for a document.
/// HSplitView: chunk ToC sidebar (left) + scrollable text with inline chunk dividers (right).
struct DocumentContentView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var apiClient: EmergentAPIClient

    @StateObject private var scrollCoordinator = ContentScrollCoordinator()

    @State private var loadedDocument: Document? = nil
    @State private var chunks: [DocumentChunk] = []
    @State private var isLoading = false
    @State private var errorMessage: String? = nil
    @State private var segments: [ContentSegment] = []
    @State private var sidebarCollapsed = true   // default collapsed; opens when chunks exist

    var body: some View {
        Group {
            if isLoading {
                LoadingStateView(message: "Loading document…")
            } else if let err = errorMessage {
                VStack(spacing: 16) {
                    ErrorBannerView(message: err, retryAction: { Task { await loadContent() } })
                }
                .padding()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let doc = loadedDocument {
                documentBody(doc)
            } else {
                EmptyStateView(
                    title: "No Document Selected",
                    icon: "doc.text",
                    description: "Open the content viewer from a document's detail panel."
                )
            }
        }
        .navigationTitle(loadedDocument?.filename ?? appState.contentViewDocument?.filename ?? "Document Content")
        .toolbar {
            ToolbarItem(placement: .navigation) {
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        sidebarCollapsed.toggle()
                    }
                } label: {
                    Image(systemName: sidebarCollapsed ? "sidebar.left" : "sidebar.left")
                        .symbolVariant(sidebarCollapsed ? .none : .fill)
                }
                .help(sidebarCollapsed ? "Show chunks panel" : "Hide chunks panel")
            }
            ToolbarItem(placement: .primaryAction) {
                Button {
                    copyAllContent()
                } label: {
                    Label("Copy All", systemImage: "doc.on.doc")
                }
                .help("Copy full document content to clipboard")
                .disabled(loadedDocument?.content == nil)
            }
        }
        .task(id: appState.contentViewDocument?.id) {
            await loadContent()
        }
    }

    // MARK: - Document Body

    @ViewBuilder
    private func documentBody(_ doc: Document) -> some View {
        HStack(spacing: 0) {
            if !sidebarCollapsed {
                chunkSidebar
                    .frame(width: 180)
                    .transition(.move(edge: .leading))

                Divider()
            }

            contentArea(doc)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: - Chunk Sidebar

    private var chunkSidebar: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("CHUNKS")
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)

            Divider()

            if chunks.isEmpty {
                EmptyStateView(
                    title: "No Chunks",
                    icon: "rectangle.split.1x2",
                    description: "This document has not been chunked yet."
                )
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(chunks) { chunk in
                            ChunkRowView(chunk: chunk) {
                                scrollCoordinator.jump(to: "chunk-\(chunk.index)")
                            }
                        }
                    }
                }
            }
        }
        .background(Color(nsColor: .controlBackgroundColor))
    }

    // MARK: - Content Area

    @ViewBuilder
    private func contentArea(_ doc: Document) -> some View {
        if segments.isEmpty {
            EmptyStateView(
                title: "No Text Content",
                icon: "doc.slash",
                description: "This document has no extractable text content."
            )
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(segments) { segment in
                            segmentView(segment)
                                .id(segment.id)
                        }
                    }
                    .padding(16)
                }
                .onAppear {
                    scrollCoordinator.scrollTo = { id in
                        withAnimation(.easeInOut(duration: 0.25)) {
                            proxy.scrollTo(id, anchor: .top)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Segment View

    @ViewBuilder
    private func segmentView(_ segment: ContentSegment) -> some View {
        switch segment.kind {
        case .prelude:
            Text(segment.text)
                .font(.body)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.bottom, 8)

        case .chunk(let chunk):
            VStack(alignment: .leading, spacing: 6) {
                // Inline divider label
                HStack(spacing: 6) {
                    Rectangle()
                        .frame(width: 18, height: 1)
                        .foregroundStyle(.secondary.opacity(0.4))
                    Text("Chunk \(chunk.index + 1)")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .foregroundStyle(.secondary)
                    Rectangle()
                        .frame(height: 1)
                        .foregroundStyle(.secondary.opacity(0.25))
                    if chunk.hasEmbedding {
                        Image(systemName: "circle.fill")
                            .font(.system(size: 6))
                            .foregroundStyle(.green)
                    }
                }
                .padding(.top, 12)
                .padding(.bottom, 4)

                Text(segment.text)
                    .font(.body)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.bottom, 8)
            }
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func loadContent() async {
        guard let doc = appState.contentViewDocument,
              let projectId = doc.projectId else { return }

        isLoading = true
        errorMessage = nil
        loadedDocument = nil
        segments = []
        chunks = []

        do {
            async let docFetch = apiClient.fetchDocument(projectID: projectId, documentID: doc.id)
            async let chunksFetch = apiClient.fetchDocumentChunks(projectID: projectId, documentID: doc.id)

            let (fetchedDoc, fetchedChunks) = try await (docFetch, chunksFetch)
            loadedDocument = fetchedDoc
            let sortedChunks = fetchedChunks.sorted { $0.index < $1.index }
            chunks = sortedChunks
            segments = buildSegments(content: fetchedDoc.content ?? "", chunks: sortedChunks)
            // Expand sidebar automatically only when there are chunks
            sidebarCollapsed = sortedChunks.isEmpty
        } catch {
            errorMessage = error.localizedDescription
        }

        isLoading = false
    }

    // MARK: - Segment Building

    private func buildSegments(content: String, chunks: [DocumentChunk]) -> [ContentSegment] {
        guard !content.isEmpty else { return [] }
        guard !chunks.isEmpty else {
            return [ContentSegment(id: "prelude", text: content, kind: .prelude)]
        }

        let allHaveOffsets = chunks.allSatisfy {
            $0.metadata?.startOffset != nil && $0.metadata?.endOffset != nil
        }

        if allHaveOffsets {
            return buildSegmentsFromOffsets(content: content, chunks: chunks)
        }
        return buildSegmentsFromSubstringSearch(content: content, chunks: chunks)
    }

    private func buildSegmentsFromOffsets(content: String, chunks: [DocumentChunk]) -> [ContentSegment] {
        var result: [ContentSegment] = []
        var cursor = content.startIndex
        let totalChars = content.count

        for chunk in chunks {
            guard let startOff = chunk.metadata?.startOffset,
                  let endOff   = chunk.metadata?.endOffset else { continue }

            let startIdx = content.index(content.startIndex, offsetBy: min(startOff, totalChars))
            let endIdx   = content.index(content.startIndex, offsetBy: min(endOff,   totalChars))

            if cursor < startIdx {
                let prelude = String(content[cursor..<startIdx])
                if !prelude.isAllWhitespace {
                    result.append(ContentSegment(id: "prelude-\(startOff)", text: prelude, kind: .prelude))
                }
            }

            let chunkText = startIdx < endIdx ? String(content[startIdx..<endIdx]) : chunk.text
            result.append(ContentSegment(id: "chunk-\(chunk.index)", text: chunkText, kind: .chunk(chunk)))
            cursor = endIdx
        }

        if cursor < content.endIndex {
            let trailing = String(content[cursor...])
            if !trailing.isAllWhitespace {
                result.append(ContentSegment(id: "prelude-tail", text: trailing, kind: .prelude))
            }
        }

        return result.isEmpty ? [ContentSegment(id: "prelude", text: content, kind: .prelude)] : result
    }

    private func buildSegmentsFromSubstringSearch(content: String, chunks: [DocumentChunk]) -> [ContentSegment] {
        var result: [ContentSegment] = []
        var searchStart = content.startIndex

        for chunk in chunks {
            guard let range = content.range(of: chunk.text, range: searchStart..<content.endIndex) else {
                continue
            }

            if searchStart < range.lowerBound {
                let prelude = String(content[searchStart..<range.lowerBound])
                if !prelude.isAllWhitespace {
                    let offset = content.distance(from: content.startIndex, to: range.lowerBound)
                    result.append(ContentSegment(id: "prelude-\(offset)", text: prelude, kind: .prelude))
                }
            }

            result.append(ContentSegment(id: "chunk-\(chunk.index)", text: chunk.text, kind: .chunk(chunk)))
            searchStart = range.upperBound
        }

        if searchStart < content.endIndex {
            let trailing = String(content[searchStart...])
            if !trailing.isAllWhitespace {
                result.append(ContentSegment(id: "prelude-tail", text: trailing, kind: .prelude))
            }
        }

        return result.isEmpty ? [ContentSegment(id: "prelude", text: content, kind: .prelude)] : result
    }

    // MARK: - Clipboard

    private func copyAllContent() {
        guard let text = loadedDocument?.content else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
}

// MARK: - Chunk Row

private struct ChunkRowView: View {
    let chunk: DocumentChunk
    let onTap: () -> Void
    @State private var isHovered = false

    var body: some View {
        Button(action: onTap) {
            HStack(alignment: .center, spacing: 6) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Chunk \(chunk.index + 1)")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.primary)
                    Text("\(chunk.size) chars")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 4)
                if chunk.hasEmbedding {
                    Image(systemName: "circle.fill")
                        .font(.system(size: 7))
                        .foregroundStyle(.green)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(isHovered ? Color.primary.opacity(0.06) : Color.clear)
        }
        .buttonStyle(.plain)
        .contentShape(Rectangle())
        .onHover { isHovered = $0 }

        Divider().padding(.leading, 12)
    }
}

// MARK: - String helper

private extension String {
    var isAllWhitespace: Bool { allSatisfy(\.isWhitespace) }
}
