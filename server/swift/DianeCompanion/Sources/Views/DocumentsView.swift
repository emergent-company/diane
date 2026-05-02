import SwiftUI

/// Lists uploaded documents with extraction status and upload capability.
struct DocumentsView: View {
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration

    @State private var documents: [Document] = []
    @State private var isLoading = true
    @State private var errorMessage: String?
    @State private var searchText = ""
    @State private var selection: Document?

    @State private var isUploading = false
    @State private var uploadProgressMessage = ""
    @State private var uploadError: String?
    @State private var showUploadError = false

    var body: some View {
        NavigationSplitView {
            listContent
                .navigationTitle("Documents")
                .toolbar { uploadToolbar }
                .task { await loadDocuments() }
                .searchable(text: $searchText, prompt: "Search documents…")
                .overlay { uploadingOverlay }
                .alert("Upload Error", isPresented: $showUploadError, actions: {
                    Button("OK") { uploadError = nil }
                }, message: {
                    Text(uploadError ?? "Unknown error")
                })
        } detail: {
            detailContent
        }
    }

    // MARK: - Upload Toolbar

    @ToolbarContentBuilder
    private var uploadToolbar: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button {
                pickAndUploadFile()
            } label: {
                Label("Upload", systemImage: "plus")
            }
            .disabled(isUploading)
            .help("Upload a document for extraction")
        }
    }

    // MARK: - Upload Overlay

    @ViewBuilder
    private var uploadingOverlay: some View {
        if isUploading {
            ZStack {
                Color.black.opacity(0.2)
                    .ignoresSafeArea()

                VStack(spacing: Design.Spacing.md) {
                    ProgressView()
                        .controlSize(.large)
                    Text(uploadProgressMessage)
                        .font(.headline)
                    Text("Processing and extracting…")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .padding(Design.Spacing.lg)
                .background(.regularMaterial)
                .cornerRadius(Design.CornerRadius.medium)
                .shadow(radius: 8)
            }
        }
    }

    // MARK: - List

    @ViewBuilder
    private var listContent: some View {
        if isLoading {
            LoadingStateView(message: "Loading documents…")
        } else if let err = errorMessage {
            ErrorBannerView(message: err) { Task { await loadDocuments() } }
        } else if filteredDocs.isEmpty {
            EmptyStateView(
                title: "No Documents",
                icon: "doc.text",
                description: "Upload documents to extract objects and knowledge."
            )
        } else {
            List(filteredDocs, selection: $selection) { doc in
                DocumentRowView(document: doc)
                    .tag(doc)
            }
            .listStyle(.plain)
            .refreshable { await loadDocuments() }
        }
    }

    // MARK: - Detail

    @ViewBuilder
    private var detailContent: some View {
        if let doc = selection {
            DocumentDetailView(document: doc)
                .environmentObject(apiClient)
                .environmentObject(serverConfig)
                .id(doc.id)
        } else {
            EmptyStateView(
                title: "Select a Document",
                icon: "doc.text.fill",
                description: "Choose a document from the list to view its details and extracted objects."
            )
        }
    }

    // MARK: - Data

    private var filteredDocs: [Document] {
        if searchText.isEmpty { return documents }
        return documents.filter {
            $0.filename.localizedCaseInsensitiveContains(searchText)
        }
    }

    /// Opens an NSOpenPanel for file selection, then uploads the chosen file.
    private func pickAndUploadFile() {
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = false
        panel.canChooseDirectories = false
        panel.allowedContentTypes = [
            .pdf, .text, .plainText,
            .init(filenameExtension: "md") ?? .plainText,
            .init(filenameExtension: "docx") ?? .data,
            .init(filenameExtension: "doc") ?? .data,
            .init(filenameExtension: "csv") ?? .data,
            .init(filenameExtension: "json") ?? .data,
            .init(filenameExtension: "rtf") ?? .data,
        ]
        panel.message = "Select a document to upload and extract"
        panel.prompt = "Upload"

        guard panel.runModal() == .OK, let url = panel.url else { return }

        Task { await performUpload(fileURL: url) }
    }

    /// Performs the actual upload and refreshes the list.
    private func performUpload(fileURL: URL) async {
        isUploading = true
        uploadProgressMessage = "Uploading \(fileURL.lastPathComponent)…"

        do {
            let doc = try await apiClient.uploadDocument(
                fileURL: fileURL,
                projectID: serverConfig.projectID,
                autoExtract: true
            )
            uploadProgressMessage = "Upload complete — refreshing list…"

            // Wait a moment for processing to start, then refresh
            try await Task.sleep(nanoseconds: 1_000_000_000)
            await loadDocuments()

            // Auto-select the uploaded document
            if let found = documents.first(where: { $0.id == doc.id }) {
                selection = found
            }
        } catch {
            uploadError = error.localizedDescription
            showUploadError = true
        }

        isUploading = false
        uploadProgressMessage = ""
    }

    private func loadDocuments() async {
        isLoading = true
        errorMessage = nil
        do {
            documents = try await apiClient.searchDocuments(
                projectID: serverConfig.projectID,
                query: "",
                limit: 50
            )
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - Document Row

private struct DocumentRowView: View {
    let document: Document

    var body: some View {
        HStack(spacing: Design.Spacing.sm) {
            // Icon based on MIME type
            Image(systemName: iconForMime(document.mimeType))
                .font(.title3)
                .foregroundStyle(.secondary)

            VStack(alignment: .leading, spacing: 2) {
                Text(document.filename)
                    .lineLimit(1)
                    .fontWeight(.medium)

                HStack(spacing: 6) {
                    statusBadge
                    if let size = document.fileSizeBytes {
                        Text(sizeFormatted(size))
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
            }

            Spacer()

            if let objs = document.objectsCreated, objs > 0 {
                Text("\(objs) obj")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .badgeStyle()
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder
    private var statusBadge: some View {
        let status = document.extractionStatus ?? document.processingStatus ?? ""
        switch status {
        case "completed":
            Label("Extracted", systemImage: "checkmark.circle.fill")
                .font(.caption)
                .foregroundStyle(.green)
        case "processing", "extracting":
            Label("Extracting", systemImage: "arrow.triangle.2.circlepath")
                .font(.caption)
                .foregroundStyle(.orange)
        case "pending":
            Label("Pending", systemImage: "clock")
                .font(.caption)
                .foregroundStyle(.secondary)
        case "failed", "dead_letter":
            Label("Failed", systemImage: "exclamationmark.triangle.fill")
                .font(.caption)
                .foregroundStyle(.red)
        default:
            if document.conversionStatus == "failed" {
                Label("Conv. Failed", systemImage: "xmark.circle")
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
    }

    private func iconForMime(_ mime: String?) -> String {
        guard let m = mime else { return "doc.text" }
        if m.contains("pdf") { return "doc.richtext" }
        if m.contains("wordprocessingml") || m.contains("docx") { return "doc.text" }
        if m.contains("spreadsheet") { return "tablecells" }
        if m.contains("text") || m.contains("markdown") { return "doc.plaintext" }
        return "doc.text"
    }

    private func sizeFormatted(_ bytes: Int) -> String {
        let formatter = ByteCountFormatter()
        formatter.countStyle = .file
        return formatter.string(fromByteCount: Int64(bytes))
    }
}

// MARK: - Previews

#Preview {
    DocumentsView()
        .environmentObject(EmergentAPIClient())
        .environmentObject(ServerConfiguration())
}
