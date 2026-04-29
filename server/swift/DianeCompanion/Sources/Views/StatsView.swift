import SwiftUI

/// Stats Dashboard — agent run statistics + provider usage from local diane API.
struct StatsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient

    @State private var stats: AgentStatsResponse? = nil
    @State private var providerStats: ProviderStatsResponse? = nil
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

                if isLoading && stats == nil && providerStats == nil {
                    VStack(spacing: 12) {
                        ProgressView()
                        Text("Loading stats…")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.top, 60)
                } else {
                    // Summary cards
                    if let s = stats {
                        summaryCardsSection(totals: s.totals)
                    }

                    // Provider usage section
                    if let ps = providerStats, !ps.providers.isEmpty {
                        providerUsageSection(providers: ps.providers, totals: ps)
                    }

                    // Per-agent breakdown
                    if let s = stats {
                        agentBreakdownSection(agents: s.agents, hours: s.hours)
                            .padding(.top, 8)
                    }

                    if stats == nil && providerStats == nil {
                        EmptyStateView(
                            title: "No Stats Yet",
                            icon: "chart.bar.fill",
                            description: "No agent run data recorded yet."
                        )
                        .padding(.top, 60)
                    }
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
            summaryCard(
                title: "Total Cost",
                value: formatCost(totals.totalCostUsd),
                icon: "dollarsign.circle.fill",
                color: .yellow
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

    // MARK: - Provider Usage

    private func providerUsageSection(providers: [ProviderStatsSummary], totals: ProviderStatsResponse) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: "cpu")
                    .foregroundStyle(.indigo)
                Text("Provider Usage")
                    .font(.headline)
            }
            .padding(.top, 8)

            // Provider summary mini-cards
            let grouped = Dictionary(grouping: providers, by: { $0.providerName.lowercased() })
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 200, maximum: 280), spacing: 12)], spacing: 12) {
                ForEach(Array(grouped.keys.sorted()), id: \.self) { key in
                    let items = grouped[key]!
                    let totalRuns = items.reduce(0) { $0 + $1.totalRuns }
                    let totalCost = items.reduce(0.0) { $0 + $1.totalCostUsd }
                    providerGroupCard(
                        providerName: items[0].providerName,
                        models: items.map { $0.modelName },
                        totalRuns: totalRuns,
                        totalCost: totalCost
                    )
                }
            }
        }
    }

    private func providerGroupCard(providerName: String, models: [String], totalRuns: Int, totalCost: Double) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: providerIcon(providerName))
                    .font(.title3)
                    .foregroundStyle(providerColor(providerName))
                Text(providerDisplayName(providerName))
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
                Text("\(totalRuns) runs")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }

            if models.count <= 3 {
                ForEach(models, id: \.self) { model in
                    HStack(spacing: 4) {
                        Circle()
                            .fill(Color.primary.opacity(0.15))
                            .frame(width: 4, height: 4)
                        Text(model)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
            } else {
                Text("\(models.count) models")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            HStack {
                Text(formatCost(totalCost))
                    .font(.caption)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                Spacer()
            }
        }
        .padding(12)
        .background(Color.primary.opacity(0.03))
        .cornerRadius(10)
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(Color.primary.opacity(0.06), lineWidth: 1)
        )
    }

    private func providerIcon(_ name: String) -> String {
        let lower = name.lowercased()
        if lower.contains("openai") { return "sparkles" }
        if lower.contains("anthropic") || lower.contains("claude") { return "brain.head.profile" }
        if lower.contains("google") || lower.contains("gemini") { return "leaf" }
        if lower.contains("meta") || lower.contains("llama") { return "m.square.fill" }
        if lower.contains("mistral") { return "cloud" }
        if lower.contains("deepseek") { return "magnifyingglass" }
        if lower.contains("xai") || lower.contains("grok") { return "x.square.fill" }
        return "cpu"
    }

    private func providerColor(_ name: String) -> Color {
        let lower = name.lowercased()
        if lower.contains("openai") { return .green }
        if lower.contains("anthropic") || lower.contains("claude") { return .orange }
        if lower.contains("google") || lower.contains("gemini") { return .blue }
        if lower.contains("meta") || lower.contains("llama") { return .purple }
        if lower.contains("mistral") { return .cyan }
        if lower.contains("deepseek") { return .red }
        if lower.contains("xai") || lower.contains("grok") { return .black }
        return .indigo
    }

    private func providerDisplayName(_ name: String) -> String {
        let lower = name.lowercased()
        if lower == "unknown" { return "Unknown Provider" }
        if lower == "openai" { return "OpenAI" }
        if lower == "anthropic" { return "Anthropic" }
        if lower == "google" || lower == "googleai" { return "Google AI" }
        if lower == "meta" { return "Meta" }
        if lower == "mistral" { return "Mistral AI" }
        if lower == "deepseek" { return "DeepSeek" }
        if lower == "xai" { return "xAI" }
        // Capitalize first letter of each word
        return name.split(separator: " ")
            .map { $0.prefix(1).uppercased() + $0.dropFirst() }
            .joined(separator: " ")
    }

    // MARK: - Agent Breakdown

    private func agentBreakdownSection(agents: [AgentStatsSummary], hours: Int) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: "person.2.circle")
                    .foregroundStyle(.blue)
                Text("Agents")
                    .font(.headline)
                Spacer()
                Text("Last \(hours == 24 ? "24h" : hours == 168 ? "7d" : "30d")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if agents.isEmpty {
                Text("No agent runs in this period.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.top, 4)
            } else {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 300, maximum: 480), spacing: 12)], spacing: 12) {
                    ForEach(agents) { agent in
                        agentWidgetCard(agent)
                    }
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
                Text(agent.displayName)
                    .font(.subheadline)
                    .fontWeight(.semibold)
                    .lineLimit(1)
                if let desc = agent.agentDescription, !desc.isEmpty {
                    Text(desc)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
                if let flow = agent.agentFlowType {
                    Text(flow)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 4)
                        .padding(.vertical, 1)
                        .background(Color.secondary.opacity(0.1))
                        .cornerRadius(3)
                }
                Spacer()
                Text("\(agent.totalRuns) runs")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
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

            // Metrics grid
            LazyVGrid(columns: [
                GridItem(.flexible()), GridItem(.flexible()),
                GridItem(.flexible()), GridItem(.flexible())
            ], spacing: 6) {
                metricBox(icon: "arrow.triangle.branch", value: "\(agent.totalRuns)", label: "Runs")
                metricBox(icon: "checkmark", value: "\(agent.successRuns)", label: "OK", color: .green)
                metricBox(icon: "xmark", value: "\(agent.errorRuns)", label: "Err", color: .red)
                metricBox(icon: "clock", value: formatDuration(agent.avgDurationMs), label: "Avg")
                metricBox(icon: "arrow.up.doc", value: formatCount(Int(agent.avgInputTokens)), label: "In")
                metricBox(icon: "arrow.down.doc", value: formatCount(Int(agent.avgOutputTokens)), label: "Out")
                metricBox(icon: "dollarsign", value: formatCost(agent.avgCostUsd), label: "/run")
                metricBox(icon: "percent", value: String(format: "%.0f%%", agent.successRate), label: "Rate")
            }
        }
        .padding(14)
        .background(Color.primary.opacity(0.03))
        .cornerRadius(12)
        .overlay(
            RoundedRectangle(cornerRadius: 12)
                .stroke(Color.primary.opacity(0.06), lineWidth: 1)
        )
    }

    private func statusBadge(_ agent: AgentStatsSummary) -> some View {
        HStack(spacing: 4) {
            Circle()
                .fill(agent.successRate >= 80 ? Color.green :
                      agent.successRate >= 50 ? Color.orange : Color.red)
                .frame(width: 6, height: 6)
            Text(agent.successRate >= 80 ? "Healthy" :
                 agent.successRate >= 50 ? "Degraded" : "Unhealthy")
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(Color.primary.opacity(0.06))
        .cornerRadius(6)
    }

    private func metricBox(icon: String, value: String, label: String, color: Color = .secondary) -> some View {
        VStack(spacing: 2) {
            HStack(spacing: 3) {
                Image(systemName: icon)
                    .font(.system(size: 9))
                    .foregroundStyle(color)
                Text(value)
                    .font(.caption)
                    .fontWeight(.medium)
                    .monospacedDigit()
                    .foregroundStyle(.primary)
            }
            Text(label)
                .font(.system(size: 9))
                .foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 4)
        .background(Color.primary.opacity(0.04))
        .cornerRadius(6)
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

    private func formatCost(_ usd: Double) -> String {
        if usd >= 100 {
            return String(format: "$%.2f", usd)
        } else if usd >= 1 {
            return String(format: "$%.3f", usd)
        } else if usd >= 0.001 {
            return String(format: "%.1f¢", usd * 100)
        } else {
            return String(format: "%.2f¢", usd * 100)
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        error = nil
        do {
            async let statsTask = dianeAPI.fetchAgentStats(hours: selectedHours)
            async let providersTask = dianeAPI.fetchProviderStats(hours: selectedHours)
            let (s, p) = try await (statsTask, providersTask)
            stats = s
            providerStats = p
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}
