import SwiftUI

/// Stats Dashboard — agent run statistics from local diane API.
struct StatsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient

    @State private var stats: AgentStatsResponse? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var selectedHours: Int = 24

    private let hourOptions = [(24, "24h"), (168, "7d"), (720, "30d")]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                // Time range picker
                timeRangePicker

                if let err = error {
                    ErrorBannerView(message: err) {
                        Task { await load() }
                    }
                }

                if isLoading && stats == nil {
                    VStack(spacing: 12) {
                        ProgressView()
                        Text("Loading stats…")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.top, 60)
                } else if let s = stats {
                    // Summary cards
                    summaryCardsSection(totals: s.totals)

                    // Per-agent breakdown
                    agentBreakdownSection(agents: s.agents, hours: s.hours)
                } else {
                    EmptyStateView(
                        title: "No Stats Yet",
                        icon: "chart.bar.fill",
                        description: "No agent run data recorded yet."
                    )
                    .padding(.top, 60)
                }
            }
            .padding()
        }
        .navigationTitle("Dashboard")
        .task { await load() }
    }

    // MARK: - Time Range Picker

    private var timeRangePicker: some View {
        Picker("Time Range", selection: $selectedHours) {
            ForEach(hourOptions, id: \.0) { hours, label in
                Text(label).tag(hours)
            }
        }
        .pickerStyle(.segmented)
        .frame(maxWidth: 280)
        .onChange(of: selectedHours) { _ in
            Task { await load() }
        }
    }

    // MARK: - Summary Cards

    private func summaryCardsSection(totals: AgentStatsTotals) -> some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 160, maximum: 220), spacing: 12)], spacing: 12) {
            summaryCard(
                title: "Total Runs",
                value: "\(totals.totalRuns)",
                icon: "arrow.triangle.branch",
                color: .blue
            )
            summaryCard(
                title: "Success Rate",
                value: String(format: "%.1f%%", totals.overallSuccessRate),
                icon: "checkmark.circle.fill",
                color: .green
            )
            summaryCard(
                title: "Avg Duration",
                value: formatDuration(totals.overallAvgDurationMs),
                icon: "clock.fill",
                color: .orange
            )
            summaryCard(
                title: "Total Tokens",
                value: formatCount(totals.totalInputTokens + totals.totalOutputTokens),
                icon: "textformat.size",
                color: .purple
            )
        }
    }

    private func summaryCard(title: String, value: String, icon: String, color: Color) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .foregroundStyle(color)
                    .font(.system(size: 14, weight: .semibold))
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.title2)
                .fontWeight(.bold)
                .monospacedDigit()
                .foregroundStyle(.primary)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.primary.opacity(0.04))
        .cornerRadius(10)
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(Color.primary.opacity(0.06), lineWidth: 1)
        )
    }

    // MARK: - Agent Breakdown

    private func agentBreakdownSection(agents: [AgentStatsSummary], hours: Int) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Per-Agent Breakdown")
                .font(.headline)
                .padding(.top, 8)

            if agents.isEmpty {
                Text("No agent runs in this period.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.top, 4)
            } else {
                ForEach(agents) { agent in
                    agentStatsRow(agent)
                }
            }
        }
    }

    private func agentStatsRow(_ agent: AgentStatsSummary) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            // Header
            HStack {
                Circle()
                    .fill(agent.successRate >= 80 ? Color.green :
                          agent.successRate >= 50 ? Color.orange : Color.red)
                    .frame(width: 8, height: 8)
                Text(agent.agentName)
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
                Text("\(agent.totalRuns) runs")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }

            // Success rate bar
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(Color.primary.opacity(0.08))
                        .frame(height: 6)
                    RoundedRectangle(cornerRadius: 3)
                        .fill(successBarColor(agent.successRate))
                        .frame(width: max(geo.size.width * CGFloat(agent.successRate / 100), 2), height: 6)
                }
            }
            .frame(height: 6)

            // Detail metrics
            HStack(spacing: 16) {
                metricItem("✅ \(agent.successRuns)")
                metricItem("❌ \(agent.errorRuns)")
                metricItem(formatDuration(agent.avgDurationMs))
                metricItem("\(String(format: "%.0f", agent.avgInputTokens)) in")
                metricItem("\(String(format: "%.0f", agent.avgOutputTokens)) out")
            }
        }
        .padding(12)
        .background(Color.primary.opacity(0.03))
        .cornerRadius(8)
    }

    private func metricItem(_ text: String) -> some View {
        Text(text)
            .font(.caption2)
            .foregroundStyle(.secondary)
            .monospacedDigit()
    }

    private func successBarColor(_ rate: Double) -> Color {
        if rate >= 80 { return .green }
        if rate >= 50 { return .orange }
        return .red
    }

    // MARK: - Formatters

    private func formatDuration(_ ms: Double) -> String {
        if ms >= 60000 {
            return String(format: "%.1fm", ms / 60000)
        } else if ms >= 1000 {
            return String(format: "%.1fs", ms / 1000)
        } else {
            return String(format: "%.0fms", ms)
        }
    }

    private func formatCount(_ count: Int) -> String {
        if count >= 1_000_000 {
            return String(format: "%.1fM", Double(count) / 1_000_000)
        } else if count >= 1_000 {
            return String(format: "%.1fK", Double(count) / 1_000)
        }
        return "\(count)"
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        do {
            stats = try await dianeAPI.fetchAgentStats(hours: selectedHours)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}
