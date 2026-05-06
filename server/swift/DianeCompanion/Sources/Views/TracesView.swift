import SwiftUI

/// Traces view — lists extraction jobs with auto-refresh.
/// Background refresh fails silently (task 8.3).
///
/// Tasks 8.1, 8.2, 8.3
struct TracesView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var traces: [Trace] = []
    @State private var selectedTrace: Trace? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var lastUpdated: Date? = nil
    @State private var refreshTask: Task<Void, Never>? = nil

    // Task 8.2: Background refresh interval (30 seconds)
    private let refreshInterval: TimeInterval = 30

    var body: some View {
        HSplitView {
            // Left: traces list
            tracesList
                .frame(minWidth: 300)

            // Right: detail panel
            if let trace = selectedTrace {
                traceDetailPanel(trace)
                    .frame(minWidth: 280)
            } else {
                EmptyStateView(
                    title: "Select a Trace",
                    icon: "chart.bar.doc.horizontal",
                    description: "Select a trace from the list to view its details."
                )
                .frame(minWidth: 280)
            }
        }
        .navigationTitle("Traces")
        .task {
            await load(isBackground: false)
            startAutoRefresh()
        }
        .onChange(of: appState.selectedProject) { _ in
            traces = []
            selectedTrace = nil
            Task { await load(isBackground: false) }
        }
        .onDisappear {
            refreshTask?.cancel()
        }
    }

    // MARK: - Traces List

    @ViewBuilder
    private var tracesList: some View {
        VStack(spacing: 0) {
            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load(isBackground: false) }
                }
                .padding(8)
            }

            if isLoading && traces.isEmpty {
                LoadingStateView(message: "Loading traces…")
            } else if traces.isEmpty {
                EmptyStateView(
                    title: "No Traces",
                    icon: "chart.bar.doc.horizontal",
                    description: "No traces recorded yet for this project."
                )
            } else {
                List(traces, selection: $selectedTrace) { trace in
                    traceRow(trace)
                        .tag(trace)
                }
                .listStyle(.plain)
            }

            Divider()
            HStack {
                Text("Showing \(traces.count) trace\(traces.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                if let ts = lastUpdated {
                    Text("Updated \(ts, style: .relative) ago")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Button("Refresh") {
                    Task { await load(isBackground: false) }
                }
                .font(.caption)
                .buttonStyle(.borderless)
                .keyboardShortcut("r", modifiers: .command)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
    }

    private func traceRow(_ trace: Trace) -> some View {
        HStack(spacing: 8) {
            statusCircle(for: trace.status)
            VStack(alignment: .leading, spacing: 2) {
                Text(trace.id)
                    .font(.system(.caption, design: .monospaced))
                    .lineLimit(1)
                HStack(spacing: 8) {
                    if let spans = trace.spanCount {
                        Text("\(spans) span\(spans == 1 ? "" : "s")")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    if let sourceType = trace.sourceType {
                        Text(sourceType)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
            Spacer()
            Text(trace.status)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(statusBackground(for: trace.status))
                .clipShape(Capsule())
        }
        .padding(.vertical, 2)
    }

    private func statusCircle(for status: String) -> some View {
        Circle()
            .fill(statusColor(for: status))
            .frame(width: 7, height: 7)
    }

    private func statusColor(for status: String) -> Color {
        switch status.lowercased() {
        case "completed", "success": return .green
        case "running", "processing": return .blue
        case "failed", "error": return .red
        case "pending", "queued": return .orange
        default: return .secondary
        }
    }

    private func statusBackground(for status: String) -> Color {
        statusColor(for: status).opacity(0.15)
    }

    // MARK: - Trace Detail Panel

    private func traceDetailPanel(_ trace: Trace) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text(trace.id)
                        .font(.system(.subheadline, design: .monospaced))
                        .fontWeight(.semibold)
                        .lineLimit(1)
                    HStack(spacing: 6) {
                        statusCircle(for: trace.status)
                        Text(trace.status)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            List {
                if let spans = trace.spanCount {
                    detailRow(label: "Spans", value: "\(spans)")
                }
                if let sourceType = trace.sourceType {
                    detailRow(label: "Source Type", value: sourceType)
                }
                if let docID = trace.documentID {
                    detailRow(label: "Document ID", value: docID)
                }
                if let createdAt = trace.createdAt {
                    detailRow(label: "Created", value: createdAt)
                }
                if let updatedAt = trace.updatedAt {
                    detailRow(label: "Updated", value: updatedAt)
                }
                if let errMsg = trace.errorMessage {
                    Section("Error") {
                        Text(errMsg)
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.red)
                    }
                }
            }
            .listStyle(.plain)
        }
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 100, alignment: .leading)
            Text(value)
                .font(.system(.caption, design: .monospaced))
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }

    // MARK: - Data loading

    @MainActor
    private func load(isBackground: Bool) async {
        guard let projectID = appState.activeProjectID else { return }
        if !isBackground { isLoading = true }
        do {
            traces = try await apiClient.fetchTraces(projectID: projectID)
            lastUpdated = Date()
            if !isBackground { error = nil }
        } catch {
            if !isBackground {
                // Task 8.3: Only show errors on manual refresh, fail silently in background
                self.error = error.localizedDescription
            }
            // Background failures are silently ignored
        }
        if !isBackground { isLoading = false }
    }

    // Task 8.2: Background auto-refresh
    private func startAutoRefresh() {
        refreshTask?.cancel()
        refreshTask = Task {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(refreshInterval * 1_000_000_000))
                guard !Task.isCancelled else { break }
                await load(isBackground: true)
            }
        }
    }
}
