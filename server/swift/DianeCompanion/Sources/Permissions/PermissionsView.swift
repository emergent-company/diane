import SwiftUI

/// Permissions management view — shows all macOS permissions with status and actions.
struct PermissionsView: View {
    @StateObject private var manager = PermissionManager()
    
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Permissions")
                .font(.subheadline)
                .fontWeight(.semibold)
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
            
            Divider()
            
            List(manager.permissions) { permission in
                permissionRow(permission)
            }
            .listStyle(.plain)
            
            Divider()
            
            HStack {
                Text("\(manager.permissions.filter(\.status.isGranted).count)/\(manager.permissions.count) granted")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Button("Refresh") { manager.refresh() }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
        .navigationTitle("Permissions")
    }
    
    private func permissionRow(_ permission: PermissionInfo) -> some View {
        HStack(spacing: 10) {
            Image(systemName: permission.type.systemIcon)
                .foregroundStyle(permission.status.isGranted ? .green : .secondary)
                .frame(width: 20)
            
            VStack(alignment: .leading, spacing: 2) {
                Text(permission.type.displayName)
                    .font(.subheadline)
                Text(permission.type.description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            
            Spacer()
            
            switch permission.status {
            case .granted:
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                    .font(.title3)
            case .denied:
                Button("Open Settings") {
                    manager.openSystemSettings(permission.type)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
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
        .padding(.vertical, 4)
    }
}
