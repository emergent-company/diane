import SwiftUI

/// Stats Dashboard — agent run statistics + provider usage from local diane API.
struct StatsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient

    @State private var stats: AgentStatsResponse? = nil
    @State private var providerStats: ProviderStatsResponse? = nil
    @State private var projectProviders: [ProjectProviderInfo]? = nil
    @State private var serverStatus: DianeAPIClient.ServerStatus? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var selectedHours: Int = 168

    private let hourOptions = [(24, "24h"), (168, "7d"), (720, "30d")]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Design.Spacing.lg) {
                timeRangePicker

                if let status = serverStatus {
                    serverStatusBar(status)
                }

                if let err = error {
                    ErrorBannerView(message: err) {
                        Task { await load() }
                    }
                }

                if isLoading && stats == nil && providerStats == nil {
                    VStack(spacing: Design.Spacing.md) {
                        ProgressView()
                        Text("Loading stats…")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.top, 60)
                } else {
                    if let s = stats {
                        summaryCardsSection(totals: s.totals)
                    }

                    if let pp = projectProviders, !pp.isEmpty {
                        projectProvidersSection(providers: pp)
                    }

                    if let ps = providerStats, !ps.providers.isEmpty {
                        providerUsageSection(providers: ps.providers, totals: ps)
                    }

                    if let s = stats {
                        agentBreakdownSection(agents: s.agents, hours: s.hours)
                            .padding(.top, Design.Spacing.sm)
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
        .onChange(of: selectedHours) { _, _ in
            Task { await load() }
        }
    }

    // MARK: - Server Status Bar

    private func serverStatusBar(_ status: DianeAPIClient.ServerStatus) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            Image(systemName: "server.rack")
                .foregroundStyle(.secondary)
                .font(.caption)

            if let ver = status.version {
                HStack(spacing: 3) {
                    Text("Version:")
                        .foregroundStyle(.secondary)
                    Text(ver)
                        .fontWeight(.medium)
                }
                .font(.caption)
            }

            if let started = status.startedAt {
                HStack(spacing: 3) {
                    Text("Up:")
                        .foregroundStyle(.secondary)
                    Text(uptimeString(from: started))
                        .fontWeight(.medium)
                }
                .font(.caption)
            }

            Spacer()
        }
        .padding(.horizontal, Design.Spacing.sm)
        .padding(.vertical, 6)
        .background(Design.Surface.cardBackground)
        .cornerRadius(Design.CornerRadius.medium)
    }

    private func uptimeString(from isoDate: String) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = formatter.date(from: isoDate) ?? ISO8601DateFormatter().date(from: isoDate) else {
            return "—"
        }
        let interval = Date().timeIntervalSince(date)
        let days = Int(interval) / 86400
        let hours = (Int(interval) % 86400) / 3600
        let minutes = (Int(interval) % 3600) / 60
        if days > 0 { return "\(days)d \(hours)h \(minutes)m" }
        if hours > 0 { return "\(hours)h \(minutes)m" }
        return "\(minutes)m"
    }

    // MARK: - Summary Cards

    private func summaryCardsSection(totals: AgentStatsTotals) -> some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 160, maximum: 220), spacing: Design.Spacing.md)], spacing: Design.Spacing.md) {
            SummaryCardView(
                title: "Total Runs",
                value: "\(totals.totalRuns)",
                icon: "arrow.triangle.branch",
                color: .blue
            )
            SummaryCardView(
                title: "Success Rate",
                value: String(format: "%.1f%%", totals.overallSuccessRate),
                icon: "checkmark.circle.fill",
                color: .green
            )
            SummaryCardView(
                title: "Avg Duration",
                value: formatDuration(totals.overallAvgDurationMs),
                icon: "clock.fill",
                color: .orange
            )
            SummaryCardView(
                title: "Total Tokens",
                value: formatCount(totals.totalInputTokens + totals.totalOutputTokens),
                icon: "textformat.size",
                color: .purple
            )
            SummaryCardView(
                title: "Total Cost",
                value: formatCost(totals.totalCostUsd),
                icon: "dollarsign.circle.fill",
                color: .yellow
            )
        }
    }

    // MARK: - Project-Level Providers

    private func projectProvidersSection(providers: [ProjectProviderInfo]) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack {
                Image(systemName: "gearshape.2")
                    .foregroundStyle(.teal)
                Text("Configured Providers")
                    .font(.headline)
                Spacer()
                Text("\(providers.count) provider\(providers.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(.top, Design.Spacing.sm)

            LazyVGrid(columns: [GridItem(.adaptive(minimum: 200, maximum: 280), spacing: Design.Spacing.md)], spacing: Design.Spacing.md) {
                ForEach(providers) { provider in
                    projectProviderCard(provider)
                }
            }
        }
    }

    private func projectProviderCard(_ p: ProjectProviderInfo) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack(spacing: Design.Spacing.sm) {
                Image(systemName: providerIcon(p.provider))
                    .font(.title3)
                    .foregroundStyle(providerColor(p.provider))
                Text(providerDisplayName(p.provider))
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
            }

            if let model = p.generativeModel, !model.isEmpty {
                LabelRowView(icon: "sparkle", label: "Model", value: model)
            }
            if let embed = p.embeddingModel, !embed.isEmpty {
                LabelRowView(icon: "square.text.square", label: "Embed", value: embed)
            }
            if let url = p.baseUrl, !url.isEmpty {
                LabelRowView(icon: "link", label: "URL", value: url)
                    .help(url)
            }
        }
        .cardStyle(cornerRadius: Design.CornerRadius.medium)
    }

    // MARK: - Provider Usage

    private func providerUsageSection(providers: [ProviderStatsSummary], totals: ProviderStatsResponse) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack {
                Image(systemName: "cpu")
                    .foregroundStyle(.indigo)
                Text("Provider Usage")
                    .font(.headline)
            }
            .padding(.top, Design.Spacing.sm)

            let grouped = Dictionary(grouping: providers, by: { $0.providerName.lowercased() })
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 200, maximum: 280), spacing: Design.Spacing.md)], spacing: Design.Spacing.md) {
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
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack(spacing: Design.Spacing.sm) {
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
                    HStack(spacing: Design.Spacing.xs) {
                        Circle()
                            .fill(Color.primary.opacity(0.15))
                            .frame(width: 4, height: 4)
                        Text(model)
                            .font(.caption2)
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                    }
                }
            } else {
                Text("\(models.count) models")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            HStack {
                LabelRowView(icon: "dollarsign.circle", label: "Cost", value: formatCost(totalCost))
                Spacer()
            }
        }
        .cardStyle(cornerRadius: Design.CornerRadius.medium)
    }

    // MARK: - Per-Agent Breakdown

    private func agentBreakdownSection(agents: [AgentStatsSummary], hours: Int) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            Text("Per-Agent Breakdown")
                .font(.headline)
                .padding(.top, Design.Spacing.sm)

            if agents.isEmpty {
                Text("No agent runs in this period.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.top, Design.Spacing.xs)
            } else {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 300, maximum: 480), spacing: Design.Spacing.md)], spacing: Design.Spacing.md) {
                    ForEach(agents) { agent in
                        AgentStatsCardView(agent: agent)
                    }
                }
            }
        }
    }

    // MARK: - Provider Helpers

    private func providerIcon(_ name: String) -> String {
        switch name.lowercased() {
        case _ where name.contains("openai"):   return "sparkles.square"
        case _ where name.contains("anthropic"): return "brain"
        case _ where name.contains("google"):    return "leaf"
        case _ where name.contains("mistral"):   return "wind"
        case _ where name.contains("gemini"):    return "sparkle.magnifyingglass"
        default:                                 return "globe"
        }
    }

    private func providerColor(_ name: String) -> Color {
        switch name.lowercased() {
        case _ where name.contains("openai"):   return .green
        case _ where name.contains("anthropic"): return .purple
        case _ where name.contains("google"):    return .blue
        case _ where name.contains("mistral"):   return .orange
        case _ where name.contains("gemini"):    return .yellow
        default:                                  return .secondary
        }
    }

    private func providerDisplayName(_ name: String) -> String {
        switch name.lowercased() {
        case "openai":   return "OpenAI"
        case "anthropic": return "Anthropic"
        case "google", "vertex": return "Google Vertex"
        case "mistral":  return "Mistral AI"
        case "gemini":   return "Gemini"
        default:         return name
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        error = nil
        do {
            async let statusTask = dianeAPI.fetchServerStatus()
            async let statsTask = dianeAPI.fetchAgentStats(hours: selectedHours)
            async let providersTask = dianeAPI.fetchProviderStats(hours: selectedHours)
            async let projectTask = dianeAPI.fetchProjectProviders()
            let (st, s, p, pp) = try await (statusTask, statsTask, providersTask, projectTask)
            serverStatus = st
            stats = s
            providerStats = p
            projectProviders = pp
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - Previews

#Preview {
    StatsView()
        .environmentObject(AppState())
        .environmentObject(DianeAPIClient())
        .frame(width: 800, height: 600)
}
