import SwiftUI

/// A full-width card showing per-agent run statistics.
///
/// Displays agent name, description, flow type badge, a success rate bar,
/// and a metrics grid (runs, OK, Err, Avg, In, Out, /run, Rate).
///
/// ```swift
/// AgentStatsCardView(agent: agentSummary)
/// ```
struct AgentStatsCardView: View {
    let agent: AgentStatsSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Header row
            HStack {
                statusDot
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
                    flowBadge(flow)
                }
                Spacer()
                statusBadge
            }

            // Success rate bar
            successBar

            // Metrics grid
            LazyVGrid(columns: [
                GridItem(.flexible()), GridItem(.flexible()),
                GridItem(.flexible()), GridItem(.flexible())
            ], spacing: 6) {
                MetricBoxView(icon: "arrow.triangle.branch", value: "\(agent.totalRuns)", label: "Runs")
                MetricBoxView(icon: "checkmark", value: "\(agent.successRuns)", label: "OK", color: .green)
                MetricBoxView(icon: "xmark", value: "\(agent.errorRuns)", label: "Err", color: .red)
                MetricBoxView(icon: "clock", value: formatDuration(agent.avgDurationMs), label: "Avg")
                MetricBoxView(icon: "arrow.up.doc", value: formatCount(Int(agent.avgInputTokens)), label: "In")
                MetricBoxView(icon: "arrow.down.doc", value: formatCount(Int(agent.avgOutputTokens)), label: "Out")
                MetricBoxView(icon: "dollarsign", value: formatCost(agent.avgCostUsd), label: "/run")
                MetricBoxView(icon: "percent", value: String(format: "%.0f%%", agent.successRate), label: "Rate")
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

    // MARK: - Sub-views

    private var statusDot: some View {
        Circle()
            .fill(agent.successRate >= 80 ? Color.green :
                  agent.successRate >= 50 ? Color.orange : Color.red)
            .frame(width: 10, height: 10)
    }

    private var statusBadge: some View {
        HStack(spacing: 4) {
            statusDot
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

    private func flowBadge(_ flow: String) -> some View {
        Text(flow)
            .font(.caption2)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 4)
            .padding(.vertical, 1)
            .background(Color.secondary.opacity(0.1))
            .cornerRadius(3)
    }

    private var successBar: some View {
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
    }

    private func successBarColor(_ rate: Double) -> Color {
        if rate >= 80 { return .green }
        if rate >= 50 { return .orange }
        return .red
    }
}

// MARK: - Previews

#Preview {
    ScrollView {
        VStack(spacing: 12) {
            AgentStatsCardView(agent: AgentStatsSummary(
                agentName: "diane-default",
                agentId: "abc-123",
                agentDescription: "General-purpose AI assistant",
                agentFlowType: "single",
                totalRuns: 42,
                successRuns: 40,
                errorRuns: 2,
                avgDurationMs: 24135,
                avgStepCount: 0,
                avgToolCalls: 0,
                avgInputTokens: 12000,
                avgOutputTokens: 1000,
                totalDurationMs: 1000000,
                totalInputTokens: 500000,
                totalOutputTokens: 42000,
                totalCostUsd: 0.05,
                avgCostUsd: 0.0012,
                successRate: 95.2
            ))
            AgentStatsCardView(agent: AgentStatsSummary(
                agentName: "diane-codebase",
                agentId: nil,
                agentDescription: nil,
                agentFlowType: nil,
                totalRuns: 0,
                successRuns: 0,
                errorRuns: 0,
                avgDurationMs: 0,
                avgStepCount: 0,
                avgToolCalls: 0,
                avgInputTokens: 0,
                avgOutputTokens: 0,
                totalDurationMs: 0,
                totalInputTokens: 0,
                totalOutputTokens: 0,
                totalCostUsd: 0,
                avgCostUsd: 0,
                successRate: 0
            ))
        }
        .padding()
        .frame(width: 500)
    }
}
