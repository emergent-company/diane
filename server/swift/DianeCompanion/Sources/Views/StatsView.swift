import SwiftUI

/// Stats Dashboard — agent run statistics + provider usage from local diane API.
struct StatsView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var updateChecker: UpdateChecker

    @State private var stats: AgentStatsResponse? = nil
    @State private var providerStats: ProviderStatsResponse? = nil
    @State private var projectProviders: [ProjectProviderInfo]? = nil
    @State private var graphObjectStats: GraphObjectStatsResponse? = nil
    @State private var serverStatus: DianeAPIClient.ServerStatus? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var selectedHours: Int = 168
    @State private var dismissVersionBanner = false

    private let hourOptions = [(24, "24h"), (168, "7d"), (720, "30d")]

    private static nonisolated(unsafe) let isoFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static nonisolated(unsafe) let isoFormatterNoFractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Design.Spacing.lg) {
                timeRangePicker

                versionMismatchBanner

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

                    if let gs = graphObjectStats {
                        graphObjectsSection(gs)
                        topGraphTypesSection(gs)
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

    // MARK: - Version Mismatch Banner

    /// Shows a warning when the CLI version differs significantly from the companion app version.
    @ViewBuilder
    private var versionMismatchBanner: some View {
        if !dismissVersionBanner, let cliVer = serverStatus?.version, let appVer = updateChecker.currentVersion {
            let cliParts = parseVersion(cliVer)
            let appParts = parseVersion(appVer)
            if cliParts.count >= 2 && appParts.count >= 2 {
                let mismatch = cliParts[0] != appParts[0] || cliParts[1] != appParts[1]
                if mismatch {
                    HStack(spacing: Design.Spacing.sm) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                        Text("Version mismatch: CLI **\(cliVer)** \u{2192} Companion **\(appVer)**")
                            .font(.subheadline)
                        Spacer()
                        Button(action: { dismissVersionBanner = true }) {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                    .padding(.horizontal, Design.Spacing.sm)
                    .padding(.vertical, 8)
                    .background(.orange.opacity(0.1))
                    .cornerRadius(Design.CornerRadius.medium)
                }
            }
        }
    }

    /// Parse a version string like "1.38.69" or "v1.38.69" into [1, 38, 69].
    private func parseVersion(_ raw: String) -> [Int] {
        let s = raw.hasPrefix("v") ? String(raw.dropFirst()) : raw
        return s.split(separator: ".").compactMap { Int($0) }
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
        guard let date = Self.isoFormatter.date(from: isoDate) ?? Self.isoFormatterNoFractional.date(from: isoDate) else {
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

    // MARK: - Graph Objects

    private func graphObjectsSection(_ gs: GraphObjectStatsResponse) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack {
                Image(systemName: "square.stack.3d.up.fill")
                    .foregroundStyle(.cyan)
                Text("Graph Objects")
                    .font(.headline)
                Spacer()
                Text("\(gs.total) total")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(.top, Design.Spacing.sm)

            LazyVGrid(columns: [GridItem(.adaptive(minimum: 140, maximum: 180), spacing: Design.Spacing.md)], spacing: Design.Spacing.md) {
                ForEach(gs.byType) { tc in
                    VStack(spacing: Design.Spacing.xxs) {
                        Text("\(tc.count)")
                            .font(.title3)
                            .fontWeight(.semibold)
                        Text(tc.typeName)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, Design.Spacing.sm)
                    .cardStyle(cornerRadius: Design.CornerRadius.medium)
                }
            }
        }
    }

    // MARK: - Top Graph Types

    private func topGraphTypesSection(_ gs: GraphObjectStatsResponse) -> some View {
        let top5 = gs.byType.prefix(5)
        guard !top5.isEmpty else { return AnyView(EmptyView()) }

        let maxCount = top5.map(\.count).max() ?? 1
        let barMaxHeight: CGFloat = 60
        let minBarHeight: CGFloat = 4

        return AnyView(VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack {
                Image(systemName: "trophy.fill")
                    .foregroundStyle(.yellow)
                Text("Top Graph Types")
                    .font(.headline)
                Spacer()
            }
            .padding(.top, Design.Spacing.sm)

            // Vertical bar chart
            HStack(alignment: .bottom, spacing: Design.Spacing.lg) {
                ForEach(Array(top5)) { tc in
                    VStack(spacing: 3) {
                        // Count label above bar
                        Text("\(tc.count)")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()

                        // Vertical bar with background track
                        ZStack(alignment: .bottom) {
                            // Background track
                            RoundedRectangle(cornerRadius: 3)
                                .fill(.cyan.opacity(0.1))
                                .frame(width: 20, height: barMaxHeight)

                            // Filled portion
                            RoundedRectangle(cornerRadius: 3)
                                .fill(.cyan.opacity(0.5))
                                .frame(
                                    width: 20,
                                    height: max(CGFloat(tc.count) / CGFloat(maxCount) * barMaxHeight, minBarHeight)
                                )
                        }

                        // Type name label at bottom
                        Text(tc.typeName)
                            .font(.caption2)
                            .fontWeight(.medium)
                            .lineLimit(1)
                            .frame(width: 56)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        })
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
            async let graphTask = dianeAPI.fetchGraphObjectStats()
            let (st, s, p, pp, gs) = try await (statusTask, statsTask, providersTask, projectTask, graphTask)
            serverStatus = st
            stats = s
            providerStats = p
            projectProviders = pp
            graphObjectStats = gs
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
        .environmentObject(UpdateChecker())
        .frame(width: 800, height: 600)
}
