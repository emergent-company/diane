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

            // Projects list (task 4.1–4.3)
            projectsSection
                .padding(.horizontal, 14)
                .padding(.vertical, 10)

            Divider()

            // Footer: Quit (left) + Open App (right) (task 4.4)
            footerSection
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
        }
        .frame(width: 320)
        .task {
            await loadProjects()
        }
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
        switch statusMonitor.connectionState {
        case .connected:    return .green
        case .disconnected: return .secondary
        case .error:        return .orange
        case .unknown:      return .secondary
        }
    }

    // MARK: - Projects Section (tasks 4.1, 4.2, 4.3)

    @ViewBuilder
    private var projectsSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Projects")
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .textCase(.uppercase)

            if appState.isLoadingProjects {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.mini)
                    Text("Loading projects…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            } else if let error = appState.projectLoadError {
                // Task 4.2: Inline error with retry
                HStack(spacing: 6) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                        .font(.caption)
                    Text(error)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Spacer()
                    Button {
                        Task { await loadProjects() }
                    } label: {
                        Image(systemName: "arrow.clockwise")
                            .font(.caption)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                }
            } else if appState.projects.isEmpty {
                Text("No projects found")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .italic()
            } else {
                // Task 4.3: Single line per project with stats
                ForEach(appState.projects) { project in
                    projectRow(project)
                }
            }
        }
    }

    private func projectRow(_ project: Project) -> some View {
        HStack(spacing: 4) {
            Image(systemName: "doc.text")
                .foregroundStyle(.secondary)
                .font(.caption2)
                .frame(width: 12)
            Text(project.name)
                .font(.caption)
                .lineLimit(1)
            Spacer()
        }
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

    @MainActor
    private func loadProjects() async {
        guard statusMonitor.connectionState == .connected else { return }
        appState.isLoadingProjects = true
        appState.projectLoadError = nil
        do {
            appState.projects = try await apiClient.fetchProjects()
        } catch {
            // Task 4.2: Show inline error message
            appState.projectLoadError = "Failed to load projects"
        }
        appState.isLoadingProjects = false
    }
}
