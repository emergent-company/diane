import SwiftUI

/// Workers / Diagnostics view — shows server diagnostics since there is no
/// dedicated workers endpoint in this version of the server.
struct WorkersView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State var diagnostics: ServerDiagnostics? = nil
    @State var isLoading = false
    @State var error: String? = nil
    @State var lastUpdated: Date? = nil

    var body: some View {
        VStack(spacing: 0) {
            if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding(8)
            }

            if isLoading && diagnostics == nil {
                LoadingStateView(message: "Loading diagnostics…")
            } else if let diag = diagnostics {
                diagnosticsContent(diag)
            } else {
                EmptyStateView(
                    title: "No Data",
                    icon: "gearshape.2",
                    description: "Could not load server diagnostics."
                )
            }

            Divider()
            HStack {
                Text("Server Diagnostics")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                if let ts = lastUpdated {
                    Text("Updated \(ts, style: .relative) ago")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Button("Refresh") {
                    Task { await load() }
                }
                .font(.caption)
                .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
        .navigationTitle("Workers")
        .task { await load() }
    }

    @ViewBuilder
    private func diagnosticsContent(_ diag: ServerDiagnostics) -> some View {
        List {
            Section("Server") {
                if let version = diag.server?.version {
                    detailRow(label: "Version", value: version)
                }
                if let env = diag.server?.environment {
                    detailRow(label: "Environment", value: env)
                }
                if let uptime = diag.uptime {
                    detailRow(label: "Uptime", value: uptime)
                }
                if let ts = diag.timestamp {
                    detailRow(label: "Timestamp", value: ts)
                }
            }

            if let pool = diag.database?.pool {
                Section("Database Pool") {
                    if let total = pool.totalConns {
                        detailRow(label: "Total Connections", value: "\(total)")
                    }
                    if let idle = pool.idleConns {
                        detailRow(label: "Idle Connections", value: "\(idle)")
                    }
                    if let max = pool.maxConns {
                        detailRow(label: "Max Connections", value: "\(max)")
                    }
                }
            }
        }
        .listStyle(.plain)
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 140, alignment: .leading)
            Text(value)
                .font(.system(.caption, design: .monospaced))
        }
    }

    @MainActor
    func load(with testClient: EmergentAPIClient? = nil) async {
        let client = testClient ?? apiClient
        if diagnostics == nil { isLoading = true }
        let result = await Self.fetchDiagnostics(client: client)
        diagnostics = result.diagnostics
        error = result.error
        if result.diagnostics != nil { lastUpdated = Date() }
        isLoading = false
    }

    /// Pure function for testing.
    static func fetchDiagnostics(client: EmergentAPIClient) async -> (diagnostics: ServerDiagnostics?, error: String?) {
        do {
            let d = try await client.fetchDiagnostics()
            return (d, nil)
        } catch {
            return (nil, error.localizedDescription)
        }
    }
}
