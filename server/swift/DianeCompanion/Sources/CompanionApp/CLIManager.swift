import Foundation
import Combine

enum CLIStatus {
    case ready(version: String)
    case notSetup
    case settingUp
    case error(String)
}

struct CLIConflict {
    let path: String
    let description: String
}

@MainActor
final class CLIManager: ObservableObject {
    @Published private(set) var status: CLIStatus = .notSetup
    @Published private(set) var setupOutput: String = ""
    @Published private(set) var conflicts: [CLIConflict] = []
    @Published private(set) var isLocalBinInPath: Bool = true
    
    var installedVersion: String? {
        if case .ready(let v) = status { return v }
        return nil
    }

    init() {
        Task { await detectAndSetup() }
    }

    // MARK: - Setup & Detection

    func detectAndSetup() async {
        logDebug("CLIManager: Starting detectAndSetup", category: "CLI")
        status = .settingUp
        setupOutput = "Starting CLI setup...\n"
        
        await checkPathEnvironment()
        await detectConflicts()
        
        do {
            try await setupSymlink()
            if let version = await fetchBundledVersion() {
                status = .ready(version: version)
                appendOutput("✓ CLI ready (\(version)).\n")
                logInfo("CLIManager: CLI ready with version \(version).", category: "CLI")
            } else {
                status = .error("Bundled CLI version unknown")
                appendOutput("✗ Failed to determine bundled CLI version.\n")
            }
        } catch {
            status = .error(error.localizedDescription)
            appendOutput("✗ Setup failed: \(error.localizedDescription)\n")
            logError("CLIManager: Setup failed: \(error.localizedDescription)", category: "CLI")
        }
        logDebug("CLIManager: Finished detectAndSetup", category: "CLI")
    }
    
    func repair() async {
        await detectAndSetup()
    }

    // MARK: - Symlinking

    private func setupSymlink() async throws {
        guard let resourceURL = Bundle.main.resourceURL,
              let files = try? FileManager.default.contentsOfDirectory(at: resourceURL, includingPropertiesForKeys: nil),
              let bundledURL = files.first(where: { $0.lastPathComponent == "diane" })
        else {
            logError("CLIManager: Bundled CLI not found.", category: "CLI")
            throw NSError(domain: "CLIManager", code: 1, userInfo: [NSLocalizedDescriptionKey: "Bundled CLI not found"])
        }
        
        appendOutput("Found bundled CLI at: \(bundledURL.path)\n")
        
        // Create symlinks at BOTH locations so there's only one real binary
        let targets: [(dir: String, link: String, label: String)] = [
            (AppConstants.CLIPaths.localBinDir, AppConstants.CLIPaths.installTarget, "~/.local/bin/diane"),
            (AppConstants.CLIPaths.dianeBinDir, AppConstants.CLIPaths.dianeTarget, "~/.diane/bin/diane"),
        ]
        
        for (dir, link, label) in targets {
            try installSymlink(bundledURL: bundledURL, dir: dir, link: link, label: label)
        }
    }
    
    private func installSymlink(bundledURL: URL, dir: String, link: String, label: String) throws {
        let fm = FileManager.default
        
        // Ensure directory exists
        if !fm.fileExists(atPath: dir) {
            appendOutput("Creating directory: \(dir)\n")
            try fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
        }
        
        // Check existing file/symlink
        var needsUpdate = true
        if fm.fileExists(atPath: link) || (try? fm.attributesOfItem(atPath: link))?[.type] as? FileAttributeType == .typeSymbolicLink {
            // Check if it's a symlink pointing to the right place
            if let destination = try? fm.destinationOfSymbolicLink(atPath: link), destination == bundledURL.path {
                needsUpdate = false
                appendOutput("Symlink at \(label) already correct.\n")
            } else {
                appendOutput("Removing existing at \(label)\n")
                try fm.removeItem(atPath: link)
            }
        }
        
        if needsUpdate {
            appendOutput("Creating symlink at \(label) → bundled binary...\n")
            try fm.createSymbolicLink(atPath: link, withDestinationPath: bundledURL.path)
            logInfo("CLIManager: Symlink created at \(label) → \(bundledURL.path).", category: "CLI")
        }
    }

    // MARK: - Conflict & PATH Detection

    private func checkPathEnvironment() async {
        logDebug("CLIManager: Checking PATH environment.", category: "CLI")
        // macOS GUI apps don't inherit interactive PATH easily.
        // We'll spawn a bash login shell to check if ~/.local/bin is in its PATH.
        if let envPath = await runCommand("/bin/bash", args: ["-l", "-c", "echo $PATH"]) {
            isLocalBinInPath = envPath.contains("/.local/bin")
            if !isLocalBinInPath {
                appendOutput("Warning: ~/.local/bin is not in PATH.\n")
                logDebug("CLIManager: ~/.local/bin not found in PATH.", category: "CLI")
            }
        }
    }
    
    private func detectConflicts() async {
        logDebug("CLIManager: Detecting CLI conflicts.", category: "CLI")
        conflicts = []
        let fm = FileManager.default
        
        // Exclude our target paths (both ~/.local/bin/diane and ~/.diane/bin/diane)
        let excludePaths: Set<String> = [AppConstants.CLIPaths.installTarget, AppConstants.CLIPaths.dianeTarget]
        let conflictCandidates = AppConstants.CLIPaths.candidates.filter { !excludePaths.contains($0) }
        
        for candidate in conflictCandidates {
            if fm.fileExists(atPath: candidate) || ((try? fm.attributesOfItem(atPath: candidate))?[.type] as? FileAttributeType == .typeSymbolicLink) {
                appendOutput("Warning: Conflicting CLI found at \(candidate)\n")
                conflicts.append(CLIConflict(path: candidate, description: "An independent installation of diane was found at \(candidate). This may conflict with the bundled version."))
                logDebug("CLIManager: Conflict detected at \(candidate).", category: "CLI")
            }
        }
    }

    // MARK: - Version Fetching

    private func fetchBundledVersion() async -> String? {
        guard let bundledURL = Bundle.main.url(forResource: "diane", withExtension: nil) else { return nil }
        if let output = await runCommand(bundledURL.path, args: ["version"]) {
            return parseVersion(output)
        }
        return nil
    }

    private func parseVersion(_ raw: String) -> String {
        for line in raw.components(separatedBy: .newlines) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.lowercased().hasPrefix("version:") {
                let value = trimmed
                    .dropFirst("version:".count)
                    .trimmingCharacters(in: .whitespaces)
                if !value.isEmpty { return value }
            } else if trimmed.lowercased().hasPrefix("diane version") {
                // handle simple output format from mock
                let value = trimmed
                    .dropFirst("diane version".count)
                    .trimmingCharacters(in: .whitespaces)
                if !value.isEmpty { return value }
            }
        }
        let fallback = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return fallback.isEmpty ? "unknown" : fallback
    }

    // MARK: - Process helpers

    @discardableResult
    private func runCommand(_ path: String, args: [String]) async -> String? {
        await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .utility).async {
                let process = Process()
                process.executableURL = URL(fileURLWithPath: path)
                process.arguments = args
                let pipe = Pipe()
                process.standardOutput = pipe
                process.standardError = Pipe()
                do {
                    try process.run()
                    process.waitUntilExit()
                    let data = pipe.fileHandleForReading.readDataToEndOfFile()
                    continuation.resume(returning: String(data: data, encoding: .utf8))
                } catch {
                    continuation.resume(returning: nil)
                }
            }
        }
    }

    private func appendOutput(_ text: String) {
        setupOutput += text
    }
}
