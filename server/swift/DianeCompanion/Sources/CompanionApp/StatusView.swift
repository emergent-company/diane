import SwiftUI

// MARK: - CLI Status Row (used in MenuBarView)

struct CLIStatusRow: View {
    @EnvironmentObject var cliManager: CLIManager

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: "terminal")
                    .foregroundStyle(.secondary)
                    .frame(width: 14)

                switch cliManager.status {
                case .ready(let version):
                    Text("CLI: \(version)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    
                    if !cliManager.conflicts.isEmpty {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.yellow)
                            .help("CLI Conflict Detected: \n\(cliManager.conflicts.first?.description ?? "")")
                    } else if !cliManager.isLocalBinInPath {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                            .help("~/.local/bin is not in your PATH. Please add it to your shell profile.")
                    } else {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                            .font(.caption)
                    }

                case .notSetup:
                    Text("CLI Setup Required")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Setup") {
                        Task { await cliManager.repair() }
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.mini)

                case .settingUp:
                    Text("Setting up CLI…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    ProgressView().controlSize(.mini)

                case .error(let reason):
                    Text("Setup failed")
                        .font(.caption)
                        .foregroundStyle(.red)
                        .help(reason)
                    Spacer()
                    Button("Repair") {
                        Task { await cliManager.repair() }
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.mini)
                    .tint(.orange)
                }
            }

            if case .settingUp = cliManager.status, !cliManager.setupOutput.isEmpty {
                ScrollView {
                    Text(cliManager.setupOutput)
                        .font(.system(.caption2, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(maxHeight: 60)
                .background(Color(NSColor.textBackgroundColor))
                .cornerRadius(Design.CornerRadius.small)
            }
        }
    }
}
