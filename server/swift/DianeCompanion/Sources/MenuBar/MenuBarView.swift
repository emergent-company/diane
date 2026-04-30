import SwiftUI
import AppKit

struct MenuBarView: View {
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var updateChecker: UpdateChecker
    @EnvironmentObject var serverConfig: ServerConfiguration
    @EnvironmentObject var cliManager: CLIManager
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Update banner
            if updateChecker.updateAvailable, let version = updateChecker.latestVersion {
                updateBanner(version: version)
                Divider().padding(.vertical, 6)
            }

            // Header: icon + app name + version
            headerSection
                .padding(.horizontal, 14)
                .padding(.top, 12)
                .padding(.bottom, 8)

            Divider()

            // Server connection status
            statusSection
                .padding(.horizontal, 14)
                .padding(.vertical, 10)

            Divider()

            // Footer: Quit (left) + Open App (right) (task 4.4)
            footerSection
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
        }
        .frame(width: 320)
    }

    // MARK: - Header

    private var headerSection: some View {
        HStack(spacing: 8) {
            Image(systemName: headerIcon)
                .foregroundStyle(headerColor)
                .font(.system(size: 14, weight: .medium))
            Text("Diane")
                .font(.headline)
            Spacer()
            if let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String {
                Text("v\(version)")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .monospacedDigit()
            }
            if statusMonitor.isChecking {
                ProgressView().controlSize(.mini)
            }
        }
    }

    private var headerIcon: String {
        switch statusMonitor.connectionState {
        case .connected:    return "brain.head.profile"
        case .disconnected: return "brain"
        case .error:        return "brain.head.profile.fill"
        case .unknown:      return "brain"
        }
    }

    private var headerColor: Color {
        switch statusMonitor.connectionState {
        case .connected:    return .primary
        case .disconnected: return .secondary
        case .error:        return .orange
        case .unknown:      return .secondary
        }
    }

    // MARK: - Server Status

    private var statusSection: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)
            Text(statusMonitor.statusLabel)
                .font(.subheadline)
                .fontWeight(.medium)
            Spacer()
            if statusMonitor.isChecking {
                ProgressView().controlSize(.mini)
            }
        }
    }

    private var statusColor: Color {
        switch statusMonitor.connectionState {
        case .connected:    return .green
        case .disconnected: return .secondary
        case .error:        return .orange
        case .unknown:      return .secondary
        }
    }

    // MARK: - Update banner

    private func updateBanner(version: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: updateChecker.isUpdating ? "arrow.down.to.line.circle" : "arrow.up.circle.fill")
                .foregroundStyle(.orange)
                .font(.system(size: 16))

            VStack(alignment: .leading, spacing: 2) {
                if updateChecker.isUpdating {
                    Text(updateChecker.updateOutput)
                        .font(.caption)
                        .fontWeight(.semibold)
                } else if let current = updateChecker.currentVersion {
                    Text("\(current)  →  \(version)")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .monospacedDigit()
                } else {
                    Text("Update available: \(version)")
                        .font(.caption)
                        .fontWeight(.semibold)
                }
                Text("Update ready")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if updateChecker.isUpdating {
                ProgressView()
                    .progressViewStyle(.circular)
                    .controlSize(.small)
            } else {
                Button(action: { updateChecker.performUpdate() }) {
                    Text("Update")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .foregroundStyle(.white)
                        .padding(.horizontal, Design.Padding.badgeH + 4)
                        .padding(.vertical, Design.Spacing.xs)
                        .background(Design.Semantic.warning)
                        .cornerRadius(Design.CornerRadius.medium)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, Design.Padding.card)
        .padding(.vertical, Design.Padding.banner)
        .background(Design.Semantic.warning.opacity(0.08))
    }

    // MARK: - Footer (task 4.4: Open App button)

    private var footerSection: some View {
        HStack(spacing: 0) {
            footerButton("Quit", role: .destructive) {
                NSApplication.shared.terminate(nil)
            }

            Spacer()

            // Task 4.4: Open App button opens the main application window
            footerButton("Open App") {
                openMainWindow()
            }
        }
    }

    @ViewBuilder
    private func footerButton(
        _ label: String,
        role: ButtonRole? = nil,
        action: @escaping () -> Void
    ) -> some View {
        Button(role: role, action: action) {
            Text(label)
                .font(.subheadline)
                .padding(.horizontal, 6)
                .padding(.vertical, 4)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .foregroundStyle(role == .destructive ? Color.red : Color.primary)
    }

    // MARK: - Actions

    private func openMainWindow() {
        openWindow(id: "main")
        NSApp.activate(ignoringOtherApps: true)
    }
}

// MARK: - Previews

#Preview("Connected") {
    MenuBarView()
        .environmentObject(AppState())
        .environmentObject(StatusMonitor.forPreviews(connectionState: .connected, isLocalReachable: true))
        .environmentObject(ServerConfiguration())
        .environmentObject(EmergentAPIClient())
        .environmentObject(UpdateChecker.forPreviews())
        .environmentObject(CLIManager())
        .frame(width: 320, height: 200)
}

#Preview("Disconnected") {
    MenuBarView()
        .environmentObject(AppState())
        .environmentObject(StatusMonitor.forPreviews(connectionState: .disconnected, isLocalReachable: false))
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
            .environmentObject(StatusMonitor.forPreviews(connectionState: .connected, isLocalReachable: true))
            .environmentObject(ServerConfiguration())
            .environmentObject(EmergentAPIClient())
            .environmentObject(UpdateChecker.forPreviews())
            .environmentObject(CLIManager())

        Divider()

        MenuBarView()
            .environmentObject(AppState())
            .environmentObject(StatusMonitor.forPreviews(connectionState: .disconnected, isLocalReachable: false))
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
    .frame(width: 320, height: 700)
}
