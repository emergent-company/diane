import Foundation
import OSLog
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
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "CLI")
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
        logger.debug("CLIManager: Starting detectAndSetup")
        status = .settingUp
        setupOutput = "Starting CLI setup...\n"
        
        await checkPathEnvironment()
        await detectConflicts()
        
        do {
            try await setupSymlink()
            if let version = await fetchBundledVersion() {
                status = .ready(version: version)
                appendOutput("✓ CLI ready (\(version)).\n")
                logger.info("CLIManager: CLI ready with version \(version).")
            } else {
                status = .error("Bundled CLI version unknown")
                appendOutput("✗ Failed to determine bundled CLI version.\n")
            }
        } catch {
            status = .error(error.localizedDescription)
            appendOutput("✗ Setup failed: \(error.localizedDescription)\n")
            logger.error("CLIManager: Setup failed: \(error.localizedDescription)")
        }
        logger.debug("CLIManager: Finished detectAndSetup")
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
            logger.error("CLIManager: Bundled CLI not found.")
            throw NSError(domain: "CLIManager", code: 1, userInfo: [NSLocalizedDescriptionKey: "Bundled CLI not found"])
        }
        
        appendOutput("Found bundled CLI at: \(bundledURL.path)\n")
        
        let localBinDir = AppConstants.CLIPaths.localBinDir
        let targetPath = AppConstants.CLIPaths.installTarget
        
        let fm = FileManager.default
        
        // Ensure ~/.local/bin exists
        if !fm.fileExists(atPath: localBinDir) {
            appendOutput("Creating directory: \(localBinDir)\n")
            try fm.createDirectory(atPath: localBinDir, withIntermediateDirectories: true)
        }
        
        // Check existing symlink
        var needsUpdate = true
        if fm.fileExists(atPath: targetPath) || symlinkExists(at: targetPath) {
            if let destination = try? fm.destinationOfSymbolicLink(atPath: targetPath), destination == bundledURL.path {
                needsUpdate = false
                appendOutput("Symlink already correct.\n")
            } else {
                appendOutput("Removing existing symlink/file at \(targetPath)\n")
                try fm.removeItem(atPath: targetPath)
            }
        }
        
        if needsUpdate {
            appendOutput("Creating symlink...\n")
            try fm.createSymbolicLink(atPath: targetPath, withDestinationPath: bundledURL.path)
            appendOutput("Symlink created.\n")
            logger.info("CLIManager: Symlink created to \(bundledURL.path).")
        }
    }
    
    private func symlinkExists(at path: String) -> Bool {
        let fm = FileManager.default
        guard let attributes = try? fm.attributesOfItem(atPath: path) else { return false }
        return attributes[.type] as? FileAttributeType == .typeSymbolicLink
    }

    // MARK: - Conflict & PATH Detection

    private func checkPathEnvironment() async {
        logger.debug("CLIManager: Checking PATH environment.")
        // macOS GUI apps don't inherit interactive PATH easily.
        // We'll spawn a bash login shell to check if ~/.local/bin is in its PATH.
        if let envPath = await runCommand("/bin/bash", args: ["-l", "-c", "echo $PATH"]) {
            isLocalBinInPath = envPath.contains("/.local/bin")
            if !isLocalBinInPath {
                appendOutput("Warning: ~/.local/bin is not in PATH.\n")
                logger.debug("CLIManager: ~/.local/bin not found in PATH.")
            }
        }
    }
    
    private func detectConflicts() async {
        logger.debug("CLIManager: Detecting CLI conflicts.")
        conflicts = []
        let fm = FileManager.default
        
        // Exclude our target path
        let conflictCandidates = AppConstants.CLIPaths.candidates.filter { $0 != AppConstants.CLIPaths.installTarget }
        
        for candidate in conflictCandidates {
            if fm.fileExists(atPath: candidate) || symlinkExists(at: candidate) {
                appendOutput("Warning: Conflicting CLI found at \(candidate)\n")
                conflicts.append(CLIConflict(path: candidate, description: "An independent installation of diane was found at \(candidate). This may conflict with the bundled version."))
                logger.debug("CLIManager: Conflict detected at \(candidate).")
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
