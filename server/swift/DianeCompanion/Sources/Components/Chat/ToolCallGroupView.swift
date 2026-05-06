import SwiftUI

// MARK: - Tool Call Group View

/// Displays tool usage in a Discord-inspired compact inline format.
///
/// Discord shows bot tool interactions as compact blocks with:
/// - An icon representing the tool category
/// - The tool name as a label
/// - Collapsed/expanded arguments
/// - Status indicator (success, running, error)
///
/// This view renders tool calls in a similar style — compact by default,
/// expandable to see arguments, with status coloring.
struct ToolCallGroupView: View {
    let toolCalls: [ToolCall]
    var isLatest: Bool = false

    @State private var isExpanded: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            // Summary header — always visible
            Button(action: { withAnimation(.easeInOut(duration: 0.2)) { isExpanded.toggle() } }) {
                HStack(spacing: 6) {
                    // Chevron
                    Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(.secondary)

                    // Icon
                    Image(systemName: "wrench.and.screwdriver.fill")
                        .font(.system(size: 9))
                        .foregroundStyle(.purple)

                    // Summary text
                    if toolCalls.count == 1 {
                        Text(toolCalls[0].name)
                            .font(.system(size: 11, weight: .medium))
                            .foregroundStyle(.purple)
                            .lineLimit(1)
                    } else {
                        Text("\\(toolCalls.count) tool calls")
                            .font(.system(size: 11, weight: .medium))
                            .foregroundStyle(.secondary)
                    }

                    Spacer(minLength: 4)

                    // Status indicator
                    if isLatest {
                        HStack(spacing: 3) {
                            Circle()
                                .fill(Color.purple)
                                .frame(width: 5, height: 5)
                            Text("Used")
                                .font(.system(size: 9))
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
            }
            .buttonStyle(.plain)

            // Tool details — expanded
            if isExpanded {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(toolCalls) { tc in
                        toolCallDetail(tc)
                    }
                }
                .padding(.leading, 16)
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(8)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color.purple.opacity(0.04))
                .overlay(
                    RoundedRectangle(cornerRadius: 8)
                        .stroke(Color.purple.opacity(0.1), lineWidth: 0.5)
                )
        )
    }

    // MARK: - Tool Call Detail Row

    @ViewBuilder
    private func toolCallDetail(_ tc: ToolCall) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                // Tool name badge
                HStack(spacing: 3) {
                    Image(systemName: "function")
                        .font(.system(size: 8))
                    Text(tc.name)
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                }
                .foregroundStyle(.purple)
                .padding(.horizontal, 5)
                .padding(.vertical, 2)
                .background(Color.purple.opacity(0.08))
                .cornerRadius(4)

                Spacer(minLength: 4)

                // Tool ID (truncated)
                if !tc.id.isEmpty {
                    Text(tc.id.suffix(8))
                        .font(.system(size: 8, design: .monospaced))
                        .foregroundStyle(.tertiary)
                        .help(tc.id)
                }
            }

            // Arguments (if any)
            if let args = tc.arguments, !args.isEmpty, !isMinimalArgs(args) {
                Text(formatToolArgs(args))
                    .font(.system(size: 9, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .lineLimit(5)
                    .padding(6)
                    .background(Color.primary.opacity(0.03))
                    .cornerRadius(4)
            }
        }
        .padding(.vertical, 2)
    }

    // MARK: - Helpers

    /// Check if arguments are minimal/empty like "{}" or "[]".
    private func isMinimalArgs(_ args: String) -> Bool {
        let trimmed = args.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed == "{}" || trimmed == "[]" || trimmed.isEmpty
    }

    /// Format tool arguments as pretty-printed JSON.
    private func formatToolArgs(_ raw: String) -> String {
        guard let data = raw.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data),
              let pretty = try? JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys, .withoutEscapingSlashes]),
              let str = String(data: pretty, encoding: .utf8)
        else { return raw }
        return str
    }
}

// MARK: - Previews

#Preview("Single Tool Call") {
    ToolCallGroupView(toolCalls: [
        ToolCall(id: "call_abc123", name: "search_files", arguments: "{\"pattern\": \"*.swift\", \"path\": \".\"}")
    ], isLatest: true)
    .padding()
    .frame(width: 350)
}

#Preview("Multiple Tool Calls") {
    ToolCallGroupView(toolCalls: [
        ToolCall(id: "call_1", name: "web_search", arguments: "{\"query\": \"SwiftUI chat\"}"),
        ToolCall(id: "call_2", name: "read_file", arguments: "{\"path\": \"file.swift\"}"),
    ])
    .padding()
    .frame(width: 350)
}

#Preview("Empty Args") {
    ToolCallGroupView(toolCalls: [
        ToolCall(id: "call_xyz", name: "get_weather", arguments: "{\"location\": \"London\"}")
    ])
    .padding()
    .frame(width: 350)
}
