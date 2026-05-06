import Foundation

/// Captures errors from the companion app and reports them to the Diane Go server
/// (which creates a GitHub issue via the bugreport endpoint).
///
/// ### Crash handling strategy
/// - **NSException** (ObjC crashes): caught via `NSSetUncaughtExceptionHandler` on init.
///   Writes crash context to disk immediately (stack traces, app version, OS).
/// - **Error reports**: runtime errors are sent in real-time via `report(error:...)`.
/// - **Deferred delivery**: crash context written to disk is sent on next app launch.
///   This works because:
///   1. The Go server is already running when the companion app restarts
///   2. We detect and flush pending crash reports in `sendPendingReports()`
///
/// No Mach exception handler — those (SIGSEGV, SIGABRT, etc.) require PLCrashReporter
/// or Sentry and are out of scope for this lightweight solution.
final class ErrorReporter: @unchecked Sendable {
    static let shared = ErrorReporter()

    private let reportQueue = DispatchQueue(label: "com.emergent-company.error-reporter", qos: .utility)
    private let reportsDir: URL
    private let localAPIURL = URL(string: "http://127.0.0.1:8890/api/bugreport")!

    private var previousExceptionHandler: NSUncaughtExceptionHandler?

    private init() {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        reportsDir = appSupport.appendingPathComponent("Diane/crash_reports")
        try? FileManager.default.createDirectory(at: reportsDir, withIntermediateDirectories: true)

        // Install crash handler — save previous handler so we can chain
        previousExceptionHandler = NSGetUncaughtExceptionHandler()
        NSSetUncaughtExceptionHandler { exception in
            ErrorReporter.shared.handleException(exception)
        }
    }

    // MARK: - Public API

    /// Report a recoverable runtime error (API failure, connection issue, etc.).
    /// Sends immediately to the local API server.
    func report(
        title: String,
        body: String,
        severity: String = "medium",
        category: String = "General",
        logLines: [String] = [],
        labels: String = ""
    ) {
        let osVersion = ProcessInfo.processInfo.operatingSystemVersionString
        let appVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown"
        let logSnippet = logLines.suffix(20).joined(separator: "\n")

        var allLabels = "companion-app"
        if !labels.isEmpty {
            allLabels += "," + labels
        }

        let payload: [String: String] = [
            "title": title,
            "body": body,
            "severity": severity,
            "app_version": appVersion,
            "os_version": osVersion,
            "labels": allLabels,
            "log_snippet": logSnippet,
        ]

        reportQueue.async { [self] in
            sendReport(payload: payload)
        }
    }

    /// Send any pending crash reports that were written to disk from a previous session.
    /// Call this on app startup (before or after the Go server is confirmed running).
    func sendPendingReports() {
        reportQueue.async { [self] in
            guard let files = try? FileManager.default.contentsOfDirectory(
                at: reportsDir,
                includingPropertiesForKeys: [.contentModificationDateKey],
                options: [.skipsHiddenFiles]
            ) else { return }

            // Sort by date — oldest first
            let sorted = files
                .filter { $0.pathExtension == "json" }
                .sorted { a, b in
                    let dateA = (try? a.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
                    let dateB = (try? b.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
                    return dateA < dateB
                }

            for file in sorted {
                guard let data = try? Data(contentsOf: file),
                      let payload = try? JSONSerialization.jsonObject(with: data) as? [String: String] else {
                    // Remove corrupted files
                    try? FileManager.default.removeItem(at: file)
                    continue
                }

                // Wait a bit between reports to avoid flooding
                Thread.sleep(forTimeInterval: 2)
                sendReport(payload: payload)

                // Remove after sending (or attempted — we don't retry deferred
                // reports since the user can always re-report)
                try? FileManager.default.removeItem(at: file)
            }
        }
    }

    // MARK: - Private

    /// Handle an uncaught NSException — write crash context to disk
    private func handleException(_ exception: NSException) {
        let appVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown"
        let osVersion = ProcessInfo.processInfo.operatingSystemVersionString

        let title = "Crash: \(exception.name.rawValue)"
        let body = """
        **Reason:** \(exception.reason ?? "Unknown")

        **Stack trace:**
        ```
        \((exception.callStackSymbols.prefix(20).joined(separator: "\n")))
        ```

        **User info:** \(exception.userInfo ?? [:])
        """

        let payload: [String: String] = [
            "title": title,
            "body": body,
            "severity": "critical",
            "app_version": appVersion,
            "os_version": osVersion,
            "labels": "companion-app,crash",
        ]

        // Write to disk — the app is about to crash so async send won't work
        let filename = "crash_\(ISO8601DateFormatter().string(from: Date())).json"
        let fileURL = reportsDir.appendingPathComponent(filename)

        // Sanitize filename to avoid issues with colons on macOS
        let safeFilename = filename.replacingOccurrences(of: ":", with: "-")
        let safeURL = reportsDir.appendingPathComponent(safeFilename)

        if let data = try? JSONSerialization.data(withJSONObject: payload, options: [.prettyPrinted]) {
            try? data.write(to: safeURL, options: [.atomic])
        }
    }

    /// POST a report payload to the local Diane API server
    private func sendReport(payload: [String: String]) {
        guard let body = try? JSONSerialization.data(withJSONObject: payload) else { return }

        var request = URLRequest(url: localAPIURL)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = body
        request.timeoutInterval = 10

        let semaphore = DispatchSemaphore(value: 0)
        let task = URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                AppLogger.shared.error("ErrorReporter: send failed: \(error.localizedDescription)", category: "ErrorReporter")
            } else if let http = response as? HTTPURLResponse, http.statusCode == 200 {
                if let data = data, let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                    let url = json["issue_url"] as? String ?? "unknown"
                    AppLogger.shared.info("ErrorReporter: issue created: \(url)", category: "ErrorReporter")
                }
            } else {
                let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                let respBody = data.flatMap { String(data: $0, encoding: .utf8) } ?? ""
                AppLogger.shared.warning("ErrorReporter: HTTP \(status): \(respBody)", category: "ErrorReporter")
            }
            semaphore.signal()
        }
        task.resume()
        _ = semaphore.wait(timeout: .now() + 15)
    }
}

// MARK: - Convenience global functions

/// Report a runtime error to the bugreport endpoint.
func reportError(
    title: String,
    body: String,
    severity: String = "medium",
    category: String = "General",
    labels: String = ""
) {
    // Collect recent log lines for context
    // Note: AppLogger writes to file but we can't efficiently tail it here.
    // A future improvement could read the last N lines from the log file.
    ErrorReporter.shared.report(
        title: title,
        body: body,
        severity: severity,
        category: category,
        labels: labels
    )
}
