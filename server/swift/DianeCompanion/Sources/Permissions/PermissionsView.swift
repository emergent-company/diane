import SwiftUI

/// Permissions management view — shows all macOS permissions with status, actions, and step-by-step guides.
struct PermissionsView: View {
    @StateObject private var manager = PermissionManager()
    @State private var selectedGuide: PermissionType? = nil

    private let columns = [
        GridItem(.adaptive(minimum: 140, maximum: 200), spacing: 12)
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
                Text("Auto-refreshes every 15s")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                Button("Refresh") { manager.refresh() }
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

            HStack {
                Text("\(manager.permissions.filter(\.status.isGranted).count)/\(manager.permissions.count) granted")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
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
        VStack(spacing: 12) {
            // Icon
            Image(systemName: permission.type.systemIcon)
                .font(.title2)
                .foregroundStyle(permission.status.isGranted ? .green : .secondary.opacity(0.6))

            // Name
            Text(permission.type.displayName)
                .font(.subheadline)
                .fontWeight(.medium)
                .lineLimit(1)

            // Description
            Text(permission.type.description)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .lineLimit(2)

            Spacer()

            // Status & action
            switch permission.status {
            case .granted:
                Label("Granted", systemImage: "checkmark.circle.fill")
                    .font(.caption)
                    .foregroundStyle(.green)
            case .denied:
                VStack(spacing: 4) {
                    Button("Open Settings") {
                        manager.openSystemSettings(permission.type)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)

                    Button("Show Guide") {
                        selectedGuide = permission.type
                    }
                    .font(.caption2)
                    .buttonStyle(.borderless)
                    .foregroundStyle(.secondary)
                }
            case .notDetermined:
                Button("Request") {
                    Task { await manager.request(permission.type) }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
            case .restricted:
                Text("Restricted")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .frame(minHeight: 180)
        .background(
            RoundedRectangle(cornerRadius: 10)
                .fill(Color.primary.opacity(0.03))
                .overlay(
                    RoundedRectangle(cornerRadius: 10)
                        .stroke(
                            permission.status.isGranted ? Color.green.opacity(0.2) : Color.secondary.opacity(0.1),
                            lineWidth: 1
                        )
                )
        )
    }
}

// MARK: - Setup Guide Sheet

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
                    // Description
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Why this is needed")
                            .font(.subheadline)
                            .fontWeight(.semibold)
                        Text(permissionType.description)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    // Step-by-step guide
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Setup Instructions")
                            .font(.subheadline)
                            .fontWeight(.semibold)
                        Text(permissionType.setupGuide)
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineSpacing(4)
                    }

                    // Quick action
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
