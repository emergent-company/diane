import SwiftUI
import DianeShared

struct MCPServersView: View {
    @Environment(\.apiClient) private var apiClient

    @State private var servers: [MCPServer] = []
    @State private var selectedServer: MCPServer?
    @State private var isLoading = true
    @State private var error: String?
    @State private var isOffline = false

    var body: some View {
        VStack(spacing: 0) {
            if isOffline { OfflineBanner() }

            Group {
                if isLoading && servers.isEmpty {
                    List {
                        ForEach(0..<4, id: \.self) { _ in
                            MCPRowPlaceholder()
                        }
                    }
                    .listStyle(.insetGrouped)
                } else if let err = error, servers.isEmpty {
                    ContentUnavailableView(
                        "Could Not Load MCP Servers",
                        systemImage: "server.rack",
                        description: Text(err)
                    )
                } else if servers.isEmpty {
                    ContentUnavailableView(
                        "No MCP Servers",
                        systemImage: "server.rack",
                        description: Text("No MCP servers configured on this server.")
                    )
                } else {
                    List(servers) { server in
                        NavigationLink(value: server) {
                            MCPRow(server: server)
                        }
                    }
                    .listStyle(.insetGrouped)
                }
            }
        }
        .navigationTitle("MCP Servers")
        .task { await load() }
        .refreshable { await load() }
        .navigationDestination(for: MCPServer.self) { server in
            MCPDetailView(server: server)
        }
    }

    private func load() async {
        isLoading = true
        error = nil
        isOffline = false
        do {
            servers = try await apiClient.fetchMCPServers()
            cacheServers(servers)
        } catch {
            let cached = loadCachedServers()
            if cached.isEmpty {
                self.error = error.localizedDescription
            } else {
                servers = cached
                isOffline = true
            }
        }
        isLoading = false
    }

    private func cacheServers(_ servers: [MCPServer]) {
        if let data = try? JSONEncoder().encode(servers) {
            UserDefaults.standard.set(data, forKey: "cached-mcp-servers")
        }
    }

    private func loadCachedServers() -> [MCPServer] {
        guard let data = UserDefaults.standard.data(forKey: "cached-mcp-servers"),
              let servers = try? JSONDecoder().decode([MCPServer].self, from: data) else {
            return []
        }
        return servers
    }
}

// MARK: - MCP Row

struct MCPRow: View {
    let server: MCPServer

    var body: some View {
        HStack(spacing: DesignTokens.spacingMD) {
            // Status dot
            Circle()
                .fill(statusColor)
                .frame(width: 10, height: 10)

            VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
                Text(server.name)
                    .font(.body)
                    .lineLimit(1)

                HStack(spacing: DesignTokens.spacingSM) {
                    // Status label
                    Text(statusLabel)
                        .font(.caption)
                        .foregroundColor(statusColor)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(statusColor.opacity(0.1))
                        .cornerRadius(DesignTokens.radiusSM)

                    // Tool count
                    if let tools = server.tools {
                        Text("\(tools.count) tools")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                }
            }

            Spacer()

            if server.enabled != false {
                Image(systemName: "checkmark.circle.fill")
                    .font(.caption)
                    .foregroundColor(.green)
            }
        }
        .padding(.vertical, DesignTokens.spacingXS)
    }

    private var statusColor: Color {
        switch server.status?.lowercased() {
        case "running": return .green
        case "disabled": return .gray
        case "auth_required", "auth_expired": return .orange
        case "error": return .red
        default: return .secondary
        }
    }

    private var statusLabel: String {
        switch server.status?.lowercased() {
        case "running": return "Running"
        case "disabled": return "Disabled"
        case "auth_required": return "Auth Required"
        case "auth_expired": return "Auth Expired"
        case "error": return "Error"
        default: return server.status?.capitalized ?? "Unknown"
        }
    }
}

// MARK: - Placeholder

struct MCPRowPlaceholder: View {
    var body: some View {
        HStack(spacing: DesignTokens.spacingMD) {
            Circle()
                .fill(Color.gray.opacity(0.3))
                .frame(width: 10, height: 10)

            VStack(alignment: .leading, spacing: 4) {
                Text("Server Name")
                    .font(.body)
                Text("status · N tools")
                    .font(.caption)
            }
        }
        .redacted(reason: .placeholder)
    }
}

// MARK: - MCP Detail View

struct MCPDetailView: View {
    let server: MCPServer

    var body: some View {
        Form {
            Section("Info") {
                HStack {
                    Text("Name")
                        .foregroundColor(.secondary)
                    Spacer()
                    Text(server.name)
                }
                HStack {
                    Text("Status")
                        .foregroundColor(.secondary)
                    Spacer()
                    Label(statusLabel, systemImage: "circle.fill")
                        .font(.subheadline)
                        .foregroundColor(statusColor)
                }
                if let url = server.url, !url.isEmpty {
                    HStack(alignment: .top) {
                        Text("URL")
                            .foregroundColor(.secondary)
                        Spacer()
                        Text(url)
                            .font(.caption.monospaced())
                            .foregroundColor(.secondary)
                            .multilineTextAlignment(.trailing)
                    }
                }
                HStack {
                    Text("Enabled")
                        .foregroundColor(.secondary)
                    Spacer()
                    Text(server.enabled != false ? "Yes" : "No")
                }
            }

            if let tools = server.tools, !tools.isEmpty {
                Section("Tools (\(tools.count))") {
                    ForEach(tools) { tool in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(tool.name)
                                .font(.body.monospaced())
                                .lineLimit(1)
                            if let desc = tool.description, !desc.isEmpty {
                                Text(desc)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                    .lineLimit(2)
                            }
                        }
                    }
                }
            }

            if let prompts = server.prompts, !prompts.isEmpty {
                Section("Prompts (\(prompts.count))") {
                    ForEach(prompts) { prompt in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(prompt.name)
                                .font(.body.monospaced())
                            if let desc = prompt.description, !desc.isEmpty {
                                Text(desc)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            if let args = prompt.arguments, !args.isEmpty {
                                Text("\(args.count) arguments")
                                    .font(.caption2)
                                    .foregroundColor(.secondary)
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle(server.name)
        .navigationBarTitleDisplayMode(.inline)
    }

    private var statusColor: Color {
        switch server.status?.lowercased() {
        case "running": return .green
        case "disabled": return .gray
        case "auth_required", "auth_expired": return .orange
        case "error": return .red
        default: return .secondary
        }
    }

    private var statusLabel: String {
        switch server.status?.lowercased() {
        case "running": return "Running"
        case "disabled": return "Disabled"
        case "auth_required": return "Auth Required"
        case "auth_expired": return "Auth Expired"
        case "error": return "Error"
        default: return server.status?.capitalized ?? "Unknown"
        }
    }
}
