import SwiftUI

/// System page — app updates, doctor diagnostics, and server info.
struct SystemView: View {
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var statusMonitor: StatusMonitor
    @EnvironmentObject var updateChecker: UpdateChecker

    // Doctor state
    @State private var doctorResult: DoctorResponse? = nil
    @State private var isDoctorRunning = false
    @State private var doctorError: String? = nil

    // Server status
    @State private var serverStatus: DianeAPIClient.ServerStatus? = nil
    @State private var statusError: String? = nil

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Design.Spacing.lg) {
                // ── Update Section ──
                updateSection
                    .cardStyle()

                // ── Doctor Section ──
                doctorSection
                    .cardStyle()

                // ── System Info Section ──
                systemInfoSection
                    .cardStyle()
            }
            .padding()
        }
        .navigationTitle("System")
        .task { await loadStatus() }
    }

    // MARK: - Update Section

    private var updateSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.md) {
            sectionHeader("🧰 Update", icon: "arrow.up.circle")

            HStack(spacing: Design.Spacing.lg) {
                VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                    Text("Current Version")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(updateChecker.currentVersion ?? "—")
                        .font(.body.monospacedDigit())
                        .fontWeight(.medium)
                }

                VStack(alignment: .leading, spacing: Design.Spacing.xxs) {
                    Text("Latest Release")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(updateChecker.latestVersion ?? "—")
                        .font(.body.monospacedDigit())
                        .fontWeight(.medium)
                }

                Spacer()

                if updateChecker.isChecking {
                    ProgressView()
                        .controlSize(.small)
                }
            }

            if updateChecker.updateAvailable {
                updateAvailableBanner
            }

            if updateChecker.isUpdating {
                updatingBanner
            }

            HStack(spacing: Design.Spacing.sm) {
                Button(action: {
                    Task { await updateChecker.checkForUpdates() }
                }) {
                    Label("Check for Updates", systemImage: "arrow.triangle.2.circlepath")
                }
                .buttonStyle(.bordered)
                .disabled(updateChecker.isChecking || updateChecker.isUpdating)

                if updateChecker.updateAvailable && !updateChecker.isUpdating {
                    Button(action: { updateChecker.performUpdate() }) {
                        Label("Update Now", systemImage: "arrow.down.to.line.compact")
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(.orange)
                }
            }
        }
    }

    private var updateAvailableBanner: some View {
        HStack(spacing: Design.Spacing.sm) {
            Image(systemName: "arrow.up.circle.fill")
                .foregroundStyle(.orange)
                .font(.system(size: Design.IconSize.medium))
            Text("Update available: \(updateChecker.currentVersion ?? "?") → \(updateChecker.latestVersion ?? "?")")
                .font(.subheadline)
                .fontWeight(.medium)
                .foregroundStyle(.secondary)
        }
        .padding(Design.Padding.banner)
        .background(Color.orange.opacity(0.08))
        .cornerRadius(Design.CornerRadius.medium)
    }

    private var updatingBanner: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.xs) {
            HStack(spacing: Design.Spacing.sm) {
                ProgressView()
                    .controlSize(.small)
                Text(updateChecker.updateOutput)
                    .font(.subheadline)
                    .fontWeight(.medium)
            }
            if updateChecker.downloadProgress > 0 && updateChecker.downloadProgress < 1.0 {
                ProgressView(value: updateChecker.downloadProgress)
                    .progressViewStyle(.linear)
                    .tint(.orange)
            }
        }
        .padding(Design.Padding.banner)
        .background(Design.Surface.elevatedBackground)
        .cornerRadius(Design.CornerRadius.medium)
    }

    // MARK: - Doctor Section

    private var doctorSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.md) {
            sectionHeader("🔍 Doctor Check", icon: "stethoscope")

            if let error = doctorError {
                ErrorBannerView(message: error) {
                    Task { await runDoctor() }
                }
            }

            if let result = doctorResult {
                VStack(alignment: .leading, spacing: Design.Spacing.sm) {
                    // Overall status
                    HStack(spacing: Design.Spacing.sm) {
                        Image(systemName: result.ok ? "checkmark.seal.fill" : "exclamationmark.triangle.fill")
                            .foregroundStyle(result.ok ? .green : .orange)
                            .font(.system(size: Design.IconSize.medium))
                        Text(result.ok ? "All checks passed" : "Some issues found")
                            .font(.subheadline)
                            .fontWeight(.medium)
                    }

                    // Check items
                    ForEach(result.results) { item in
                        doctorCheckRow(item)
                    }

                    // Server version
                    if let ver = result.version {
                        Text("Server: v\(ver)")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
            } else if isDoctorRunning {
                VStack(spacing: Design.Spacing.sm) {
                    ProgressView()
                        .controlSize(.large)
                    Text("Running diagnostics…")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, Design.Spacing.lg)
            }

            Button(action: {
                Task { await runDoctor() }
            }) {
                Label(isDoctorRunning ? "Running…" : "Run Doctor", systemImage: "play.fill")
            }
            .buttonStyle(.bordered)
            .disabled(isDoctorRunning)
        }
    }

    @ViewBuilder
    private func doctorCheckRow(_ item: DoctorCheckItem) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            Image(systemName: item.iconName)
                .foregroundStyle(doctorStatusColor(item.status))
                .font(.system(size: Design.IconSize.small))
                .frame(width: 18)

            VStack(alignment: .leading, spacing: 1) {
                Text(item.displayName)
                    .font(.callout)
                    .fontWeight(.medium)
                Text(item.message)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            statusBadge(item.status)
        }
        .padding(.vertical, 4)
    }

    private func doctorStatusColor(_ status: String) -> Color {
        switch status {
        case "ok":      return .green
        case "warning": return .orange
        case "error":   return .red
        default:        return .secondary
        }
    }

    private func statusBadge(_ status: String) -> some View {
        Group {
            switch status {
            case "ok":
                Text("Pass")
                    .foregroundStyle(.green)
                    .badgeStyle(color: .green)
            case "warning":
                Text("Warn")
                    .foregroundStyle(.orange)
                    .badgeStyle(color: .orange)
            case "error":
                Text("Fail")
                    .foregroundStyle(.red)
                    .badgeStyle(color: .red)
            default:
                Text(status)
                    .foregroundStyle(.secondary)
                    .badgeStyle()
            }
        }
    }

    // MARK: - System Info Section

    private var systemInfoSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.md) {
            sectionHeader("🖥️ System Info", icon: "info.circle")

            if let error = statusError {
                ErrorBannerView(message: error) {
                    Task { await loadStatus() }
                }
            }

            if let status = serverStatus {
                infoRow(label: "Server Version", value: status.version ?? "—")
                // CLI / App version match
                let cliVersion = status.version ?? "—"
                let appVersion = updateChecker.currentVersion ?? "—"
                let versionMatch = cliVersion == appVersion || 
                    (appVersion.hasPrefix("dev") && cliVersion.hasPrefix("vdev"))
                infoRow(label: "CLI / App Match",
                        value: versionMatch ? "✅ \(cliVersion) = \(appVersion)" : "⚠️ \(cliVersion) ≠ \(appVersion)",
                        valueColor: versionMatch ? .green : .orange)
                infoRow(label: "Project ID", value: status.projectID ?? "—")
                infoRow(label: "Server URL", value: status.serverURL ?? "—")
                if let started = status.startedAt {
                    infoRow(label: "Started", value: friendlyDate(started))
                }
            }

            Divider()
                .padding(.vertical, Design.Spacing.xs)

            infoRow(label: "Local API", value: statusMonitor.isLocalAPIReachable ? "Connected" : "Unreachable",
                    valueColor: statusMonitor.isLocalAPIReachable ? .green : .secondary)
            infoRow(label: "Remote Server", value: statusMonitor.isRemoteReachable ? "Connected" : "Unreachable",
                    valueColor: statusMonitor.isRemoteReachable ? .green : .secondary)

            if let last = statusMonitor.lastChecked {
                infoRow(label: "Last Checked", value: friendlyDate(last))
            }
        }
    }

    private func infoRow(label: String, value: String, valueColor: Color = .primary) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 120, alignment: .leading)
            Text(value)
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(valueColor)
            Spacer()
        }
    }

    // MARK: - Helpers

    private func sectionHeader(_ title: String, icon: String) -> some View {
        HStack(spacing: Design.Spacing.xs) {
            Image(systemName: icon)
                .foregroundStyle(.secondary)
            Text(title)
                .font(.headline)
        }
    }

    private func friendlyDate(_ iso: String) -> String {
        // Try ISO8601 parsing
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: iso) {
            return Self.friendlyFormatter.string(from: date)
        }
        formatter.formatOptions = [.withInternetDateTime]
        if let date = formatter.date(from: iso) {
            return Self.friendlyFormatter.string(from: date)
        }
        return iso
    }

    private func friendlyDate(_ date: Date) -> String {
        Self.friendlyFormatter.string(from: date)
    }

    private static let friendlyFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "MMM d, yyyy  h:mm a"
        return f
    }()

    // MARK: - Actions

    private func loadStatus() async {
        do {
            serverStatus = try await dianeAPI.fetchServerStatus()
            statusError = nil
        } catch {
            statusError = error.localizedDescription
        }
    }

    private func runDoctor() async {
        isDoctorRunning = true
        doctorError = nil
        defer { isDoctorRunning = false }

        do {
            doctorResult = try await dianeAPI.fetchDoctorReport()
        } catch {
            doctorError = error.localizedDescription
        }
    }
}

// MARK: - Previews

#Preview {
    SystemView()
        .environmentObject(DianeAPIClient())
        .environmentObject(StatusMonitor())
        .environmentObject(UpdateChecker())
        .frame(width: 600, height: 700)
}
