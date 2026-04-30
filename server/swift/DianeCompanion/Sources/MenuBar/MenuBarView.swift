import SwiftUI

/// The macOS menu bar icon, status popover, and update checker.
struct MenuBarView: View {
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var updateChecker: UpdateChecker
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var cliManager: CLIManager
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var apiClient: EmergentAPIClient

    var body: some View {
        VStack(spacing: 0) {
            connectionStatus
            Divider()
            serverInfo
            Divider()
            VStack(spacing: 0) {
                botStatus
            }
            if updateChecker.updateAvailable ?? false {
                Divider()
                updateBanner
            }
            Divider()
            HStack(spacing: 0) {
                quitButton
                Spacer()
                settingsButton
                Spacer()
                checkNowButton
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .frame(width: 280)
        .background(
            VisualEffectView(material: .menu, blendingMode: .behindWindow)
                .ignoresSafeArea()
        )
    }

    // MARK: - Connection Status

    private var connectionStatus: some View {
        HStack(spacing: 8) {
            Image(systemName: statusIcon)
                .foregroundStyle(statusColor)
                .font(.title3)
            VStack(alignment: .leading, spacing: 0) {
                if statusMonitor.connectionState == .connected {
                    Text("Connected")
                        .fontWeight(.medium)
                } else {
                    HStack(spacing: 4) {
                        Text("Disconnected")
                            .fontWeight(.medium)
                        if statusMonitor.isChecking {
                            ProgressView()
                                .scaleEffect(0.6)
                                .frame(width: 12, height: 12)
                        }
                    }
                }
                Text(statusDetail)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }

    private var statusIcon: String {
        switch statusMonitor.connectionState {
        case .connected:    return "brain.head.profile"
        case .disconnected: return "brain"
        case .error:        return "brain.head.profile.fill"
        }
    }

    private var statusColor: Color {
        switch statusMonitor.connectionState {
        case .connected:    return .primary
        case .disconnected: return .secondary
        case .error:        return .orange
        }
    }

    private var statusDetail: String {
        switch statusMonitor.connectionState {
        case .connected:
            if statusMonitor.isLocalAPIReachable {
                return "Local API reachable"
            }
            return "Remote server only"
        case .disconnected:
            if statusMonitor.isChecking {
                return "Checking…"
            }
            return "Tap to reconnect"
        case .error:
            return statusMonitor.lastError ?? "Unknown error"
        }
    }

    private var connectionDot: Color {
        switch statusMonitor.connectionState {
        case .connected:    return .green
        case .disconnected: return .secondary
        case .error:        return .orange
        }
    }

    // MARK: - Server Info

    private var serverInfo: some View {
        HStack(spacing: 6) {
            Image(systemName: "server.rack")
                .font(.caption2)
                .foregroundStyle(.secondary)
            Text(serverConfig.displayName)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
    }

    // MARK: - Bot Status

    private var botStatus: some View {
        HStack(spacing: 8) {
            Image(systemName: "dot.circle.fill")
                .foregroundStyle(statusDotColor)
                .font(.caption2)
            Text("Discord Bot")
                .font(.subheadline)
            Spacer()
            Text(botDetail)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
    }

    private var statusDotColor: Color {
        switch statusMonitor.connectionState {
        case .connected:    return .green
        case .disconnected: return .secondary
        case .error:        return .orange
        }
    }

    private var botDetail: String {
        let agentsCount = appState.agentCount
        if agentsCount > 0 {
            return "\(agentsCount) agent\(agentsCount == 1 ? "" : "s")"
        }
        return "—"
    }

    // MARK: - Update Banner

    private var updateBanner: some View {
        HStack(spacing: 8) {
            Image(systemName: updateChecker.isUpdating ?? false ? "arrow.triangle.2.circlepath" : "arrow.up.circle.fill")
                .foregroundStyle(updateChecker.isUpdating ?? false ? .blue : .green)
                .font(.title3)
            VStack(alignment: .leading, spacing: 0) {
                if updateChecker.isUpdating ?? false {
                    Text("Updating…")
                        .fontWeight(.medium)
                    if let output = updateChecker.updateOutput {
                        Text(output)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                } else {
                    Text("Update Available")
                        .fontWeight(.medium)
                    HStack(spacing: 4) {
                        Text(updateChecker.currentVersion ?? "")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .strikethrough()
                        Image(systemName: "arrow.right")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                        Text(updateChecker.latestVersion ?? "")
                            .font(.caption2)
                            .foregroundStyle(.green)
                            .fontWeight(.medium)
                    }
                }
            }
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }

    // MARK: - Bottom Actions

    private var quitButton: some View {
        Button("Quit") {
            NSApplication.shared.terminate(nil)
        }
        .buttonStyle(.borderless)
        .font(.subheadline)
    }

    private var settingsButton: some View {
        Button("Settings…") {
            // Open settings window
        }
        .buttonStyle(.borderless)
        .font(.subheadline)
    }

    private var checkNowButton: some View {
        Button("Check Now") {
            statusMonitor.checkNow()
        }
        .buttonStyle(.borderless)
        .font(.subheadline)
    }
}

// MARK: - Visual Effect View

/// NSVisualEffectView wrapper for menu bar background.
struct VisualEffectView: NSViewRepresentable {
    let material: NSVisualEffectView.Material
    let blendingMode: NSVisualEffectView.BlendingMode

    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.material = material
        view.blendingMode = blendingMode
        view.state = .active
        return view
    }

    func updateNSView(_ nsView: NSVisualEffectView, context: Context) {}
}

// MARK: - Previews

#Preview("Connected") {
    MenuBarView()
        .environmentObject(AppState())
        .environmentObject(StatusMonitor.forPreviews(connectionState: ConnectionState.connected, isLocalReachable: true))
        .environmentObject(ServerConfiguration())
        .environmentObject(EmergentAPIClient())
        .environmentObject(UpdateChecker.forPreviews())
        .environmentObject(CLIManager())
        .frame(width: 320, height: 200)
}

#Preview("Disconnected") {
    MenuBarView()
        .environmentObject(AppState())
        .environmentObject(StatusMonitor.forPreviews(connectionState: ConnectionState.disconnected, isLocalReachable: false))
        .environmentObject(ServerConfiguration())
        .environmentObject(EmergentAPIClient())
        .environmentObject(UpdateChecker.forPreviews())
        .environmentObject(CLIManager())
        .frame(width: 320, height: 200)
}

#Preview("Update Available") {
    MenuBarView()
        .environmentObject(AppState())
        .environmentObject(StatusMonitor.forPreviews())
        .environmentObject(ServerConfiguration())
        .environmentObject(EmergentAPIClient())
        .environmentObject(UpdateChecker.forPreviews(
            updateAvailable: true,
            currentVersion: "v1.12.3",
            latestVersion: "v1.13.0"
        ))
        .environmentObject(CLIManager())
        .frame(width: 320, height: 260)
}

#Preview("Update In Progress") {
    MenuBarView()
        .environmentObject(AppState())
        .environmentObject(StatusMonitor.forPreviews())
        .environmentObject(ServerConfiguration())
        .environmentObject(EmergentAPIClient())
        .environmentObject(UpdateChecker.forPreviews(
            updateAvailable: true,
            currentVersion: "v1.12.3",
            latestVersion: "v1.13.0",
            isUpdating: true,
            updateOutput: "Downloading v1.13.0… 45%"
        ))
        .environmentObject(CLIManager())
        .frame(width: 320, height: 260)
}

#Preview("Gallery") {
    VStack(spacing: 0) {
        MenuBarView()
            .environmentObject(AppState())
            .environmentObject(StatusMonitor.forPreviews(connectionState: ConnectionState.connected, isLocalReachable: true))
            .environmentObject(ServerConfiguration())
            .environmentObject(EmergentAPIClient())
            .environmentObject(UpdateChecker.forPreviews())
            .environmentObject(CLIManager())

        Divider()

        MenuBarView()
            .environmentObject(AppState())
            .environmentObject(StatusMonitor.forPreviews(connectionState: ConnectionState.disconnected, isLocalReachable: false))
            .environmentObject(ServerConfiguration())
            .environmentObject(EmergentAPIClient())
            .environmentObject(UpdateChecker.forPreviews())
            .environmentObject(CLIManager())

        Divider()

        MenuBarView()
            .environmentObject(AppState())
            .environmentObject(StatusMonitor.forPreviews())
            .environmentObject(ServerConfiguration())
            .environmentObject(EmergentAPIClient())
            .environmentObject(UpdateChecker.forPreviews(
                updateAvailable: true,
                currentVersion: "v1.12.3",
                latestVersion: "v1.13.0"
            ))
            .environmentObject(CLIManager())
    }
    .frame(width: 320)
}
