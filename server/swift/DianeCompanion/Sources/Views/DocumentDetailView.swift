import SwiftUI

/// Rich detail panel for a selected document.
/// Shows all available metadata grouped into logical sections.
struct DocumentDetailView: View {
    let document: Document

    @EnvironmentObject private var appState: AppState
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                headerSection
                processingSection
                storageSection
                contentSection
                identifiersSection
            }
            .padding(16)
        }
        .navigationTitle(document.filename)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    appState.contentViewDocument = document
                    openWindow(id: "document-content")
                } label: {
                    Label("View Content", systemImage: "text.alignleft")
                }
                .help("Open full text content viewer")
            }
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: fileIcon)
                    .font(.title2)
                    .foregroundStyle(.secondary)
                    .frame(width: 28)
                VStack(alignment: .leading, spacing: 3) {
                    Text(document.filename)
                        .font(.headline)
                        .textSelection(.enabled)
                    if let mime = document.mimeType {
                        Text(mime)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if let size = document.fileSizeBytes {
                        Text(formatBytes(size))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    // MARK: - Processing Status

    @ViewBuilder
    private var processingSection: some View {
        let hasConversion = document.conversionStatus != nil
        let hasExtraction = document.extractionStatus != nil
        if hasConversion || hasExtraction {
            DetailSectionView(title: "Processing") {
                if let status = document.conversionStatus {
                    DetailRowView(label: "Conversion", value: status, valueColor: statusColor(status))
                }
                if let status = document.extractionStatus {
                    DetailRowView(label: "Extraction", value: status, valueColor: statusColor(status))
                }
                if let syncVersion = document.syncVersion {
                    DetailRowView(label: "Sync Version", value: "\(syncVersion)")
                }
            }
        }
    }

    // MARK: - Content Stats

    @ViewBuilder
    private var contentSection: some View {
        let hasAny = document.chunks != nil || document.embeddedChunks != nil || document.totalChars != nil
        if hasAny {
            DetailSectionView(title: "Content") {
                if let chars = document.totalChars {
                    DetailRowView(label: "Characters", value: chars.formatted())
                }
                if let chunks = document.chunks {
                    DetailRowView(label: "Chunks", value: chunks.formatted())
                }
                if let embedded = document.embeddedChunks {
                    DetailRowView(label: "Embedded Chunks", value: embedded.formatted())
                }
            }
        }
    }

    // MARK: - Storage

    @ViewBuilder
    private var storageSection: some View {
        let hasAny = document.sourceType != nil || document.storageKey != nil
        if hasAny {
            DetailSectionView(title: "Storage") {
                if let sourceType = document.sourceType {
                    DetailRowView(label: "Source", value: sourceType)
                }
                if let key = document.storageKey {
                    DetailRowView(label: "Storage Key", value: key, monospaced: true, truncate: true)
                }
                if let hash = document.fileHash ?? document.contentHash {
                    DetailRowView(label: "Hash", value: hash, monospaced: true, truncate: true)
                }
            }
        }
    }

    // MARK: - Identifiers & Timestamps

    private var identifiersSection: some View {
        DetailSectionView(title: "Info") {
            DetailRowView(label: "ID", value: document.id, monospaced: true, truncate: true)
            if let createdAt = document.createdAt {
                DetailRowView(label: "Created", value: formatDate(createdAt))
            }
            if let updatedAt = document.updatedAt {
                DetailRowView(label: "Updated", value: formatDate(updatedAt))
            }
        }
    }

    // MARK: - Helpers

    private var fileIcon: String {
        guard let mime = document.mimeType else { return "doc.text" }
        if mime.hasPrefix("audio/") { return "waveform" }
        if mime.hasPrefix("video/") { return "film" }
        if mime.hasPrefix("image/") { return "photo" }
        if mime == "application/pdf" { return "doc.richtext" }
        return "doc.text"
    }

    private func formatBytes(_ bytes: Int) -> String {
        let formatter = ByteCountFormatter()
        formatter.allowedUnits = [.useKB, .useMB, .useGB]
        formatter.countStyle = .file
        return formatter.string(fromByteCount: Int64(bytes))
    }

    private func formatDate(_ iso: String) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: iso) {
            let display = DateFormatter()
            display.dateStyle = .medium
            display.timeStyle = .short
            return display.string(from: date)
        }
        return iso
    }

    private func statusColor(_ status: String) -> Color {
        switch status {
        case "pending":      return .orange
        case "processing":   return .blue
        case "completed":    return .green
        case "failed":       return .red
        case "not_required": return .secondary
        default:             return .primary
        }
    }
}

// MARK: - Reusable section + row components

struct DetailSectionView<Content: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(title.uppercased())
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .padding(.bottom, 6)

            VStack(spacing: 0) {
                content()
            }
            .background(Color.primary.opacity(0.04))
            .clipShape(RoundedRectangle(cornerRadius: 8))
        }
    }
}

struct DetailRowView: View {
    let label: String
    let value: String
    var monospaced: Bool = false
    var truncate: Bool = false
    var valueColor: Color = .primary

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 110, alignment: .leading)
                .padding(.vertical, 7)

            Divider()
                .frame(height: 20)
                .padding(.top, 7)

            Group {
                if monospaced {
                    Text(value)
                        .font(.system(.caption, design: .monospaced))
                } else {
                    Text(value)
                        .font(.caption)
                }
            }
            .foregroundStyle(valueColor)
            .lineLimit(truncate ? 1 : nil)
            .truncationMode(.middle)
            .textSelection(.enabled)
            .padding(.vertical, 7)

            Spacer(minLength: 0)
        }
        .padding(.horizontal, 10)

        Divider()
            .padding(.leading, 10)
    }
}
