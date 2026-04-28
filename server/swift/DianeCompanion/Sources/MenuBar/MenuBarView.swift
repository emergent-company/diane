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
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Circle()
                    .fill(statusColor)
                    .frame(width: 8, height: 8)
                Text(serverConfig.isConfigured ? serverConfig.serverURL : "Not configured")
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer()
                Text(statusMonitor.statusLabel)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var statusColor: Color {
        statusMonitor.statusColor
    }

    // MARK: - Update banner

    private func updateBanner(version: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "arrow.up.circle.fill")
                .foregroundStyle(.orange)
                .font(.system(size: 16))

            VStack(alignment: .leading, spacing: 2) {
                if let current = updateChecker.currentVersion {
                    Text("\(current)  →  \(version)")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .monospacedDigit()
                } else {
                    Text("Update available: \(version)")
                        .font(.caption)
                        .fontWeight(.semibold)
                }
                Text("CLI update ready")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if updateChecker.isUpdating {
                ProgressView().controlSize(.small)
                if !updateChecker.updateOutput.isEmpty {
                    Text(updateChecker.updateOutput)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            } else {
                Button(action: { Task { await updateChecker.performUpdate() } }) {
                    Text("Update")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .foregroundStyle(.white)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 4)
                        .background(Color.orange)
                        .cornerRadius(5)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(Color.orange.opacity(0.08))
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
