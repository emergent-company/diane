import SwiftUI

/// Permissions management view — shows all macOS permissions with status,
/// app-level feature toggles, and a "Test" button that actually tries the API.
///
/// macOS permissions work implicitly: the system prompts when an API is first
/// accessed. The Test button triggers this by attempting a real operation —
/// listing calendars, fetching contacts, sending a test notification, etc.
struct PermissionsView: View {
    @StateObject private var manager = PermissionManager()
    @State private var selectedGuide: PermissionType? = nil
    @State private var testResults: [PermissionType: String] = [:]
    @State private var testingTypes: Set<PermissionType> = []

    private let columns = [
        GridItem(.adaptive(minimum: 160, maximum: 220), spacing: 12)
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                Text("Permissions")
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
                if manager.isRefreshing {
                    ProgressView()
                        .controlSize(.mini)
                        .scaleEffect(0.7)
                }
                Button("Refresh") {
                    Task { await manager.asyncRefresh() }
                }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)

            Divider()

            ScrollView {
                LazyVGrid(columns: columns, spacing: 16) {
                    ForEach(manager.permissions) { permission in
                        permissionCard(permission)
                    }
                }
                .padding(16)
            }

            Divider()

            // Summary footer
            HStack {
                let active = manager.permissions.filter { $0.featureStatus.isUsable }.count
                let total = manager.permissions.count
                Text("\(active)/\(total) features active")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Text("Tap Test to trigger macOS permission prompt")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
        .navigationTitle("Permissions")
        .sheet(item: $selectedGuide) { type in
            SetupGuideView(permissionType: type)
        }
    }

    // MARK: - Permission Card

    private func permissionCard(_ permission: PermissionInfo) -> some View {
        let ft = permission.featureStatus
        let isTesting = testingTypes.contains(permission.type)

        return VStack(spacing: 0) {
            // Icon + Toggle row
            HStack(spacing: 8) {
                Image(systemName: permission.type.systemIcon)
                    .font(.title3)
                    .foregroundStyle(ft == .active ? .green : .secondary.opacity(0.6))

                Spacer(minLength: 4)

                Toggle(isOn: Binding(
                    get: { permission.featureEnabled },
                    set: { manager.setFeatureEnabled($0, for: permission.type) }
                )) { }
                .toggleStyle(.switch)
                .controlSize(.small)
            }

            Spacer().frame(height: 10)

            // Name
            Text(permission.type.displayName)
                .font(.subheadline)
                .fontWeight(.medium)
                .frame(maxWidth: .infinity, alignment: .leading)

            // Description
            Text(permission.type.description)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .lineLimit(2)

            Spacer().frame(height: 10)

            // Combined feature status badge
            featureBadge(ft)

            Spacer().frame(height: 6)

            // OS permission sub-line
            HStack(spacing: 4) {
                Image(systemName: permission.status.isGranted ? "checkmark.circle.fill" : "xmark.circle.fill")
                    .font(.system(size: 9))
                Text(permission.status.isGranted ? "macOS: Granted" : "macOS: Denied")
                    .font(.caption2)
            }
            .foregroundStyle(permission.status.isGranted ? .green : .secondary)

            // Test result message
            if let result = testResults[permission.type], !result.isEmpty {
                Spacer().frame(height: 4)
                Text(result)
                    .font(.caption2)
                    .foregroundStyle(result.contains("✓") || result.contains("works") || result.contains("Found") ? .green : .secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .lineLimit(3)
            }

            Spacer().frame(height: 6)

            // Actions row
            if permission.featureEnabled {
                HStack(spacing: 8) {
                    // Test button — triggers actual macOS permission dialog
                    if isTesting {
                        ProgressView()
                            .controlSize(.mini)
                            .scaleEffect(0.6)
                    } else {
                        Button("Test") {
                            runTest(permission.type)
                        }
                        .buttonStyle(.borderedProminent)
                        .controlSize(.small)
                    }

                    Button("Settings") {
                        manager.openSystemSettings(permission.type)
                    }
                    .buttonStyle(.borderless)
                    .font(.caption2)
                    .foregroundStyle(.secondary)

                    Button("Guide") {
                        selectedGuide = permission.type
                    }
                    .buttonStyle(.borderless)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                }
            }
        }
        .padding(12)
        .frame(minHeight: 200)
        .background(
            RoundedRectangle(cornerRadius: Design.CornerRadius.large)
                .fill(cardBackground(ft))
                .overlay(
                    RoundedRectangle(cornerRadius: Design.CornerRadius.large)
                        .stroke(cardBorder(ft), lineWidth: 1)
                )
        )
    }

    // MARK: - Test Action

    private func runTest(_ type: PermissionType) {
        testingTypes.insert(type)
        testResults[type] = ""
        Task {
            let result = await manager.test(type)
            testResults[type] = result
            testingTypes.remove(type)
        }
    }

    // MARK: - Feature status badge

    @ViewBuilder
    private func featureBadge(_ status: FeatureStatus) -> some View {
        HStack(spacing: 4) {
            Image(systemName: statusIcon(status))
                .font(.system(size: 10, weight: .semibold))
            Text(statusLabel(status))
                .font(.caption)
                .fontWeight(.medium)
        }
        .foregroundStyle(statusColor(status))
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(statusColor(status).opacity(0.1))
        .cornerRadius(4)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func statusLabel(_ s: FeatureStatus) -> String {
        switch s {
        case .active:          return "Active"
        case .disabled:        return "Disabled"
        case .needsPermission: return "Needs Permission"
        case .notDetermined:   return "Not Requested"
        case .restricted:      return "Restricted"
        }
    }

    private func statusIcon(_ s: FeatureStatus) -> String {
        switch s {
        case .active:          return "checkmark.circle.fill"
        case .disabled:        return "power"
        case .needsPermission: return "exclamationmark.triangle.fill"
        case .notDetermined:   return "questionmark.circle"
        case .restricted:      return "lock.fill"
        }
    }

    private func statusColor(_ s: FeatureStatus) -> Color {
        switch s {
        case .active:          return .green
        case .disabled:        return .secondary
        case .needsPermission: return .orange
        case .notDetermined:   return .blue
        case .restricted:      return .red
        }
    }

    // MARK: - Card theming

    private func cardBackground(_ s: FeatureStatus) -> Color {
        switch s {
        case .active:          return Design.Surface.cardBackground
        case .disabled:        return Design.Surface.cardBackground.opacity(0.5)
        case .needsPermission: return Design.Surface.cardBackground
        case .notDetermined:   return Design.Surface.cardBackground
        case .restricted:      return Design.Surface.cardBackground
        }
    }

    private func cardBorder(_ s: FeatureStatus) -> Color {
        switch s {
        case .active:          return .green.opacity(0.2)
        case .disabled:        return .secondary.opacity(0.08)
        case .needsPermission: return .orange.opacity(0.25)
        case .notDetermined:   return Design.Surface.border
        case .restricted:      return .red.opacity(0.2)
        }
    }
}

// MARK: - Setup Guide Sheet (unchanged)

struct SetupGuideView: View {
    @Environment(\.dismiss) private var dismiss
    let permissionType: PermissionType

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Image(systemName: permissionType.systemIcon)
                    .font(.title3)
                    .foregroundStyle(.orange)
                Text("\(permissionType.displayName) Setup Guide")
                    .font(.headline)
                Spacer()
                Button("Done") { dismiss() }
                    .buttonStyle(.borderless)
            }
            .padding()

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Why this is needed")
                            .font(.subheadline)
                            .fontWeight(.semibold)
                        Text(permissionType.description)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    VStack(alignment: .leading, spacing: 4) {
                        Text("Setup Instructions")
                            .font(.subheadline)
                            .fontWeight(.semibold)
                        Text(permissionType.setupGuide)
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineSpacing(4)
                    }

                    HStack(spacing: 12) {
                        Button("Open System Settings") {
                            if let url = permissionType.settingsURL {
                                NSWorkspace.shared.open(url)
                            }
                        }
                        .buttonStyle(.borderedProminent)
                        .controlSize(.small)

                        Button("Check Again") {
                            dismiss()
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                    }
                }
                .padding()
            }

            Divider()

            HStack {
                Spacer()
                Button("Close") { dismiss() }
                    .buttonStyle(.bordered)
                    .keyboardShortcut(.escape)
            }
            .padding()
        }
        .frame(width: 420, height: 450)
    }
}
