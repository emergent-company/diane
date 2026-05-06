import SwiftUI

/// Account Status view — shows server connection info and aggregated stats.
///
/// Task 6.2, 6.3
struct AccountStatusView: View {
    @EnvironmentObject var apiClient: EmergentAPIClient
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var serverConfig: ServerConfiguration

    @State private var stats: AccountStats? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var lastUpdated: Date? = nil

    var body: some View {
        VStack(spacing: 0) {
            if isLoading && stats == nil {
                LoadingStateView(message: "Loading account stats…")
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        if let err = error {
                            ErrorBannerView(message: err) {
                                Task { await load() }
                            }
                            .padding(.horizontal)
                            .padding(.top)
                        }

                        // Connection info card
                        connectionCard
                            .padding(.horizontal)
                            .padding(.top, error == nil ? 16 : 0)

                        // Aggregated stats grid
                        if let s = stats {
                            aggregatedStatsGrid(s)
                                .padding(.horizontal)
                        }
                    }
                }

                Divider()
                HStack {
                    if let ts = lastUpdated {
                        Text("Last updated: \(ts, style: .relative) ago")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    } else {
                        Text("Last updated: just now")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Refresh") { Task { await load() } }
                        .font(.caption)
                        .buttonStyle(.borderless)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
            }
        }
        .navigationTitle("Account Status")
        .task { await load() }
    }

    // MARK: - Connection card

    private var connectionCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Connection")
                .font(.subheadline)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)

            Divider()

            HStack {
                Text("Server URL")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Text(serverConfig.serverURL.isEmpty ? "Not configured" : serverConfig.serverURL)
                    .font(.system(.caption, design: .monospaced))
                    .lineLimit(1)
                    .truncationMode(.middle)
            }

            HStack {
                Text("Status")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                HStack(spacing: 4) {
                    Circle()
                        .fill(connectionStatusColor)
                        .frame(width: 7, height: 7)
                    Text(statusMonitor.statusLabel)
                        .font(.caption)
                }
            }

            if let s = stats, let latency = s.latencyMs {
                HStack {
                    Text("Latency")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Text(String(format: "%.0fms", latency))
                        .font(.system(.caption, design: .monospaced))
                }
            }

            if let s = stats, let version = s.serverVersion {
                HStack {
                    Text("Server Version")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Text(version)
                        .font(.system(.caption, design: .monospaced))
                }
            }
        }
        .padding(12)
        .background(.background.opacity(0.8))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(Color.primary.opacity(0.08), lineWidth: 1)
        )
    }

    private var connectionStatusColor: Color {
        switch statusMonitor.connectionState {
        case .connected:    return .green
        case .disconnected: return .secondary
        case .error:        return .orange
        case .unknown:      return .secondary
        }
    }

    // MARK: - Aggregated stats (task 6.3: render 0 gracefully)

    private func aggregatedStatsGrid(_ s: AccountStats) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Aggregated Stats")
                .font(.subheadline)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)

            LazyVGrid(columns: [
                GridItem(.flexible()),
                GridItem(.flexible()),
                GridItem(.flexible())
            ], spacing: 12) {
                StatCardView(
                    title: "Total Projects",
                    value: s.totalProjects.formatted(),
                    icon: "folder",
                    tint: .blue
                )
                StatCardView(
                    title: "Total Objects",
                    value: s.totalObjects.formatted(),
                    icon: "cube",
                    tint: .purple
                )
                StatCardView(
                    title: "Total Relations",
                    value: s.totalRelations.formatted(),
                    icon: "arrow.left.and.right",
                    tint: .green
                )
                StatCardView(
                    title: "Total API Requests",
                    value: s.totalApiRequests.formatted(),
                    icon: "arrow.up.arrow.down",
                    tint: .orange
                )
                if let latency = s.avgLatencyMs {
                    StatCardView(
                        title: "Avg. Latency",
                        value: String(format: "%.0fms", latency),
                        icon: "stopwatch",
                        tint: .pink
                    )
                }
            }
        }
    }

    @MainActor
    private func load() async {
        if stats == nil { isLoading = true }
        do {
            stats = try await apiClient.fetchAccountStats()
            lastUpdated = Date()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}
