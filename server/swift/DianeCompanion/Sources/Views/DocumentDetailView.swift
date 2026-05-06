import SwiftUI

/// Detailed view of a document with extraction summary and graph objects.
struct DocumentDetailView: View {
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration

    let document: Document

    @State private var extractionSummary: ExtractionSummary?
    @State private var extractionError: String?
    @State private var isLoadingExtraction = true
    @State private var branchObjects: [GraphObject] = []
    @State private var isLoadingBranchObjects = false
    @State private var showContent = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Design.Spacing.md) {
                headerSection
                    .cardStyle()
                metadataSection
                    .cardStyle()
                extractionSection
                    .cardStyle()
                if showContent, let content = document.content, !content.isEmpty {
                    contentSection(content)
                        .cardStyle()
                }
            }
            .padding(Design.Padding.card)
        }
        .navigationTitle(document.filename)
        .task { await loadExtraction() }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack {
                Image(systemName: iconForMime(document.mimeType))
                    .font(.title)
                    .foregroundStyle(.secondary)
                Spacer()
                extractionStatusBadge
            }

            Text(document.filename)
                .font(.title2)
                .fontWeight(.semibold)

            if let mime = document.mimeType {
                Text(mime)
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    // MARK: - Metadata

    private var metadataSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            Text("Details")
                .font(.headline)

            DetailRow(label: "Size", value: formattedSize)
            DetailRow(label: "Chunks", value: "\(document.chunks ?? 0) / embedded: \(document.embeddedChunks ?? 0)")
            DetailRow(label: "Characters", value: "\(document.totalChars ?? 0)")
            DetailRow(label: "Created", value: formattedDate(document.createdAt))
            DetailRow(label: "Conversion", value: document.conversionStatus ?? "-")
            DetailRow(label: "Extraction", value: document.extractionStatus ?? "-")
            extractionBranchRow

            if let objs = document.objectsCreated, objs > 0 {
                DetailRow(label: "Objects Created", value: "\(objs)")
            }
            if let rels = document.relationshipsCreated, rels > 0 {
                DetailRow(label: "Relationships", value: "\(rels)")
            }

            if let content = document.content, !content.isEmpty {
                Toggle(isOn: $showContent) {
                    Text("Show Content")
                        .font(.subheadline)
                }
                .toggleStyle(.switch)
                .controlSize(.small)
            }
        }
    }

    // MARK: - Extraction

    @ViewBuilder
    private var extractionSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            Text("Extraction Results")
                .font(.headline)

            if isLoadingExtraction {
                HStack {
                    ProgressView().controlSize(.small)
                    Text("Loading extraction results…")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            } else if let err = extractionError {
                VStack(alignment: .leading, spacing: 4) {
                    Label(err, systemImage: "info.circle")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text("Extraction will run automatically after upload. Check back later.")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
            } else if let summary = extractionSummary {
                // Branch name
                if let jobId = extractionSummary?.jobId {
                    let branch = "extraction/\(document.id)/\(jobId)"
                    Label(branch, systemImage: "arrow.tree.branch")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                // Summary counts
                HStack(spacing: Design.Spacing.lg) {
                    StatBadge(value: "\(summary.objectsCreated)", label: "Objects", icon: "cube.fill")
                    StatBadge(value: "\(summary.relationshipsCreated)", label: "Relationships", icon: "arrow.triangle.branch")
                    StatBadge(value: "\(summary.chunksProcessed)/\(summary.totalChunks)", label: "Chunks", icon: "doc.on.doc")
                }
                .padding(.vertical, Design.Spacing.xs)

                if summary.hasErrors, let err = summary.errorSummary {
                    Label(err, systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }

                // Objects by type
                if let byType = summary.objectsByType, !byType.isEmpty {
                    Divider()
                    Text("Objects by Type")
                        .font(.subheadline)
                        .fontWeight(.medium)

                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 140))], spacing: Design.Spacing.xs) {
                        ForEach(byType.sorted(by: { $0.key < $1.key }), id: \.key) { type, count in
                            HStack(spacing: 4) {
                                Image(systemName: iconForObjectType(type))
                                    .foregroundStyle(.secondary)
                                Text(type)
                                    .font(.caption)
                                    .fontWeight(.medium)
                                Spacer()
                                Text("\(count)")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .monospacedDigit()
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(Design.Surface.cardBackground.opacity(0.5))
                            .cornerRadius(Design.CornerRadius.small)
                        }
                    }
                }

                // Extracted objects list
                if !branchObjects.isEmpty {
                    Divider()
                    Text("Extracted Objects (\(branchObjects.count))")
                        .font(.subheadline)
                        .fontWeight(.medium)

                    LazyVStack(spacing: Design.Spacing.xs) {
                        ForEach(branchObjects, id: \.id) { obj in
                            HStack(spacing: 6) {
                                Image(systemName: iconForObjectType(obj.type ?? ""))
                                    .foregroundStyle(.secondary)
                                    .frame(width: 16)
                                VStack(alignment: .leading, spacing: 1) {
                                    Text(obj.displayName)
                                        .font(.caption)
                                        .fontWeight(.medium)
                                    Text(obj.type ?? "")
                                        .font(.caption2)
                                        .foregroundStyle(.tertiary)
                                }
                                Spacer()
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(Design.Surface.cardBackground.opacity(0.3))
                            .cornerRadius(Design.CornerRadius.small)
                        }
                    }
                } else if !isLoadingBranchObjects, extractionSummary != nil {
                    Text("No extracted objects visible on this branch.")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }

                if isLoadingBranchObjects {
                    HStack {
                        ProgressView().controlSize(.small)
                        Text("Loading objects…")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                Text("Completed: \(formattedDate(summary.completedAt))")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .padding(.top, 2)

            } else {
                Text("No extraction data available.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Content

    private func contentSection(_ content: String) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            Text("Content Preview")
                .font(.headline)

            ScrollView([.vertical]) {
                Text(content)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .textSelection(.enabled)
            }
            .frame(maxHeight: 400)
            .background(Design.Surface.cardBackground.opacity(0.3))
            .cornerRadius(Design.CornerRadius.small)
        }
    }

    // MARK: - Helpers

    @ViewBuilder
    private var extractionBranchRow: some View {
        if let summary = extractionSummary, !summary.jobId.isEmpty {
            let branch = "extraction/\(document.id)/\(summary.jobId)"
            DetailRow(label: "Branch", value: branch)
        }
    }

    @ViewBuilder
    private var extractionStatusBadge: some View {
        let status = document.extractionStatus ?? ""
        switch status {
        case "completed":
            Label("Extracted", systemImage: "checkmark.seal.fill")
                .font(.caption)
                .foregroundStyle(.green)
                .badgeStyle(color: .green)
        case "processing", "extracting":
            Label("Extracting…", systemImage: "arrow.triangle.2.circlepath")
                .font(.caption)
                .foregroundStyle(.orange)
                .badgeStyle(color: .orange)
        case "pending":
            Label("Pending", systemImage: "clock")
                .font(.caption)
                .badgeStyle()
        case "failed", "dead_letter":
            Label("Failed", systemImage: "exclamationmark.triangle")
                .font(.caption)
                .foregroundStyle(.red)
                .badgeStyle(color: .red)
        default:
            EmptyView()
        }
    }

    private func loadExtraction() async {
        isLoadingExtraction = true
        extractionError = nil
        do {
            let summary = try await apiClient.fetchExtractionSummary(
                projectID: serverConfig.projectID,
                documentID: document.id
            )
            extractionSummary = summary
            isLoadingBranchObjects = true

            // Prefer objectIds from extraction summary (always works, survives branch merge)
            if let ids = summary.objectIds, !ids.isEmpty {
                var objects: [GraphObject] = []
                for objId in ids {
                    if let obj = try? await apiClient.fetchObject(id: objId) {
                        objects.append(obj)
                    }
                }
                branchObjects = objects
            } else {
                // Fallback: try fetching from extraction branch (pre-v0.40.58 docs)
                let branch = "extraction/\(document.id)/\(summary.jobId)"
                let objects = try? await apiClient.fetchBranchObjects(
                    projectID: serverConfig.projectID,
                    branch: branch
                )
                branchObjects = objects?.filter {
                    $0.properties?["_extraction_job_id"]?.stringValue == summary.jobId
                } ?? []
            }
            isLoadingBranchObjects = false
        } catch {
            extractionError = error.localizedDescription
        }
        isLoadingExtraction = false
    }

    private var formattedSize: String {
        guard let bytes = document.fileSizeBytes else { return "-" }
        let formatter = ByteCountFormatter()
        formatter.countStyle = .file
        return formatter.string(fromByteCount: Int64(bytes))
    }

    private func formattedDate(_ iso: String?) -> String {
        guard let iso = iso else { return "-" }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: iso) {
            return date.formatted(date: .abbreviated, time: .shortened)
        }
        return iso
    }

    private func iconForMime(_ mime: String?) -> String {
        guard let m = mime else { return "doc.text" }
        if m.contains("pdf") { return "doc.richtext" }
        if m.contains("word") { return "doc.text" }
        if m.contains("text") || m.contains("markdown") { return "doc.plaintext" }
        return "doc.text"
    }

    private func iconForObjectType(_ type: String) -> String {
        switch type.lowercased() {
        case "person": return "person.fill"
        case "company", "organization": return "building.2.fill"
        case "place", "location": return "mappin.and.ellipse"
        case "car", "vehicle": return "car.fill"
        case "document": return "doc.text.fill"
        case "financialtransaction", "transaction": return "dollarsign.circle"
        case "project": return "folder.fill"
        case "service": return "gearshape.2.fill"
        case "item": return "shippingbox.fill"
        default: return "cube.fill"
        }
    }
}

// MARK: - Supporting Views

private struct DetailRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 100, alignment: .leading)
            Text(value)
                .font(.subheadline)
                .foregroundStyle(.primary)
            Spacer()
        }
    }
}

private struct StatBadge: View {
    let value: String
    let label: String
    let icon: String

    var body: some View {
        VStack(spacing: 2) {
            Image(systemName: icon)
                .font(.title3)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.title3)
                .fontWeight(.semibold)
                .monospacedDigit()
            Text(label)
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, Design.Spacing.xs)
        .background(Design.Surface.cardBackground.opacity(0.3))
        .cornerRadius(Design.CornerRadius.small)
    }
}

// MARK: - Previews

#Preview {
    DocumentDetailView(
        document: Document(
            id: "test",
            projectId: "proj",
            filename: "camping-trailer-agreement.pdf",
            mimeType: "application/pdf",
            fileHash: nil,
            contentHash: nil,
            sourceType: "upload",
            conversionStatus: "completed",
            extractionStatus: "completed",
            processingStatus: "completed",
            storageKey: nil,
            storageUrl: nil,
            fileSizeBytes: 1_151_992,
            syncVersion: nil,
            chunks: 7,
            embeddedChunks: 7,
            totalChars: 12345,
            objectsCreated: 9,
            relationshipsCreated: 12,
            content: nil,
            createdAt: "2026-05-02T10:53:00Z",
            updatedAt: nil
        )
    )
    .environmentObject(EmergentAPIClient())
    .environmentObject(ServerConfiguration())
    .frame(width: 500, height: 700)
}
