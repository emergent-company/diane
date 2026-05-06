import Foundation
import Sentry

/// A single diagnostic check from `diane doctor --json` (CLI format).
struct CLIDoctorCheck: Decodable, Identifiable {
    let name: String
    let status: String   // "pass", "fail", "warn", "skip"
    let detail: String?

    var id: String { name }

    var isPassed: Bool { status == "pass" }
    var isFailed: Bool { status == "fail" }
    var isWarning: Bool { status == "warn" }
    var isSkipped: Bool { status == "skip" }
}

/// Complete JSON report from `diane doctor --json`.
struct CLIDoctorReport: Decodable {
    let version: String
    let passed: Int
    let failed: Int
    let warnings: Int
    let total: Int
    let checks: [CLIDoctorCheck]
}

/// Result of a self-test run.
enum SelfTestState: Equatable {
    case idle
    case running
    case completed(CLIDoctorReport)
    case failed(String)

    static func == (lhs: SelfTestState, rhs: SelfTestState) -> Bool {
        switch (lhs, rhs) {
        case (.idle, .idle), (.running, .running):
            return true
        case let (.completed(a), .completed(b)):
            return a.version == b.version && a.total == b.total && a.failed == b.failed
        case let (.failed(a), .failed(b)):
            return a == b
        default:
            return false
        }
    }
}

/// Runs `diane doctor --json` and publishes results.
/// Reports failures to Sentry for automated monitoring.
@MainActor
final class SelfTestManager: ObservableObject {
    @Published private(set) var state: SelfTestState = .idle
    @Published private(set) var lastRunVersion: String?

    /// Whether to auto-run self-test on next launch after detecting an upgrade.
    private var pendingPostUpgradeTest = false

    private let defaults = UserDefaults.standard
    private static let lastSeenVersionKey = "com.diane.selftest.last_seen_version"

    /// Check if a post-upgrade self-test is needed.
    /// Call this on app startup, passing the installed version.
    func checkPostUpgrade(installedVersion: String) async {
        let lastSeen = defaults.string(forKey: SelfTestManager.lastSeenVersionKey)

        if let last = lastSeen, last != installedVersion, last != "1.0", last != "unknown" {
            logInfo("SelfTest: Version changed \(last) → \(installedVersion) — scheduling post-upgrade self-test", category: "SelfTest")
            pendingPostUpgradeTest = true
        }

        defaults.set(installedVersion, forKey: SelfTestManager.lastSeenVersionKey)
    }

    /// Run the self-test now by executing `diane doctor --json`.
    func run() async {
        state = .running

        guard let bundledURL = Bundle.main.url(forResource: "diane", withExtension: nil) else {
            state = .failed("Bundled diane binary not found")
            logError("SelfTest: Bundled diane binary not found", category: "SelfTest")
            SentrySDK.capture(message: "Self-test failed: bundled binary not found")
            return
        }

        logInfo("SelfTest: Running doctor --json", category: "SelfTest")

        do {
            let output = try await runCommand(bundledURL.path, arguments: ["doctor", "--json"])
            guard let data = output.data(using: .utf8) else {
                state = .failed("Failed to decode doctor output as UTF-8")
                return
            }

            let report = try JSONDecoder().decode(CLIDoctorReport.self, from: data)
            logInfo("SelfTest: \(report.passed)/\(report.total) passed, \(report.failed) failed, \(report.warnings) warnings", category: "SelfTest")

            // Add Sentry breadcrumb with overview
            let crumb = Breadcrumb()
            crumb.level = report.failed > 0 ? .error : .info
            crumb.category = "SelfTest"
            crumb.message = "Self-test: \(report.passed)/\(report.total) passed, \(report.failed) failed"
            crumb.data = [
                "passed": report.passed,
                "failed": report.failed,
                "warnings": report.warnings,
                "total": report.total,
                "version": report.version
            ]
            SentrySDK.addBreadcrumb(crumb)

            // Report individual failures to Sentry as captured errors
            for check in report.checks where check.isFailed {
                let error = NSError(
                    domain: "SelfTest",
                    code: 1001,
                    userInfo: [
                        NSLocalizedDescriptionKey: "Self-test check failed: \(check.name)",
                        "check_name": check.name,
                        "detail": check.detail ?? "",
                        "diane_version": report.version
                    ]
                )
                SentrySDK.capture(error: error)
            }

            state = .completed(report)
            lastRunVersion = report.version
            pendingPostUpgradeTest = false

        } catch {
            logError("SelfTest: Failed: \(error.localizedDescription)", category: "SelfTest")
            state = .failed(error.localizedDescription)

            SentrySDK.capture(error: NSError(
                domain: "SelfTest",
                code: 1000,
                userInfo: [
                    NSLocalizedDescriptionKey: "Self-test execution failed",
                    NSUnderlyingErrorKey: error
                ]
            ))
        }
    }

    /// Run if a post-upgrade test is pending (detected version change).
    func runIfPending() async {
        if pendingPostUpgradeTest {
            logInfo("SelfTest: Running post-upgrade self-test", category: "SelfTest")
            await run()
        }
    }

    /// Reset state (e.g. after viewing results).
    func reset() {
        state = .idle
    }

    // MARK: - Process helper

    private func runCommand(_ path: String, arguments: [String]) async throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = arguments

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .utility).async {
                do {
                    try process.run()
                    process.waitUntilExit()

                    let outData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
                    let errData = stderrPipe.fileHandleForReading.readDataToEndOfFile()

                    if process.terminationStatus != 0 {
                        let errText = String(data: errData, encoding: .utf8) ?? ""
                        continuation.resume(throwing: NSError(
                            domain: "SelfTest",
                            code: Int(process.terminationStatus),
                            userInfo: [NSLocalizedDescriptionKey: errText.isEmpty ? "Exit code \(process.terminationStatus)" : errText]
                        ))
                    } else {
                        let output = String(data: outData, encoding: .utf8) ?? ""
                        continuation.resume(returning: output)
                    }
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }
}
