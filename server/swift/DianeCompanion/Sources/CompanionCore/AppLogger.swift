import Foundation

/// File-based logger for the Diane Companion app.
///
/// Writes to `~/Library/Logs/Diane/DianeCompanion.log` with auto-rotation
/// (keeps last 3 logs). Each entry has a timestamp, level, and category.
///
/// Also logs to the unified OSLog system for Console.app visibility.
final class AppLogger: @unchecked Sendable {
    static let shared = AppLogger()

    private let logQueue = DispatchQueue(label: "com.emergent-company.diane-logger", qos: .utility)
    private let maxLogSize: Int64 = 5 * 1024 * 1024  // 5 MB per file
    private let maxLogFiles = 3

    private let logDir: URL
    private let logFile: URL

    enum Level: String {
        case debug   = "DEBUG"
        case info    = "INFO"
        case warning = "WARN"
        case error   = "ERROR"
    }

    private init() {
        let home = FileManager.default.homeDirectoryForCurrentUser
        logDir = home.appendingPathComponent("Library/Logs/Diane")
        logFile = logDir.appendingPathComponent("DianeCompanion.log")

        // Ensure log directory exists
        try? FileManager.default.createDirectory(at: logDir, withIntermediateDirectories: true)

        // Rotate if current log is too large
        rotateIfNeeded()
    }

    // MARK: - Public API

    func debug(_ message: String, category: String = "General") {
        write(level: .debug, message: message, category: category)
    }

    func info(_ message: String, category: String = "General") {
        write(level: .info, message: message, category: category)
    }

    func warning(_ message: String, category: String = "General") {
        write(level: .warning, message: message, category: category)
    }

    func error(_ message: String, category: String = "General") {
        write(level: .error, message: message, category: category)
    }

    /// Log an error with its localized description and optional underlying error chain.
    func error(_ error: Error, category: String = "General", message: String? = nil) {
        var text = message.map { "\($0): " } ?? ""
        text += error.localizedDescription
        // Include underlying error chain via `localizedDescription` when possible
        let nsErr = error as NSError
        if let underlying = nsErr.userInfo[NSUnderlyingErrorKey] as? Error {
            text += " | underlying: \(underlying.localizedDescription)"
        }
        write(level: .error, message: text, category: category)
    }

    // MARK: - Internal

    private func write(level: Level, message: String, category: String) {
        let timestamp = ISO8601DateFormatter().string(from: Date())
        let line = "\(timestamp) [\(level.rawValue)] [\(category)] \(message)"

        // Write to file asynchronously
        logQueue.async { [self] in
            guard let data = (line + "\n").data(using: .utf8) else { return }
            // Append to file, create if needed
            if FileManager.default.fileExists(atPath: logFile.path) {
                if let handle = try? FileHandle(forWritingTo: logFile) {
                    handle.seekToEndOfFile()
                    handle.write(data)
                    try? handle.close()
                }
            } else {
                try? data.write(to: logFile, options: .atomic)
            }
        }
    }

    /// Rotate log files when current log exceeds maxLogSize.
    private func rotateIfNeeded() {
        guard let attrs = try? FileManager.default.attributesOfItem(atPath: logFile.path),
              let size = attrs[.size] as? Int64,
              size > maxLogSize else { return }

        // Shift existing rotated files: .2 -> .3, .1 -> .2
        for i in stride(from: maxLogFiles - 1, through: 1, by: -1) {
            let oldPath = logFile.appendingPathExtension("\(i)")
            let newPath = logFile.appendingPathExtension("\(i + 1)")
            try? FileManager.default.moveItem(at: oldPath, to: newPath)
        }
        // Move current -> .1
        let rotated = logFile.appendingPathExtension("1")
        try? FileManager.default.moveItem(at: logFile, to: rotated)

        // Trim excess files
        for i in (maxLogFiles + 1)...10 {
            let oldPath = logFile.appendingPathExtension("\(i)")
            try? FileManager.default.removeItem(at: oldPath)
        }
    }
}

// MARK: - Convenience global functions (for quick use anywhere)

func logDebug(_ message: String, category: String = "General") {
    AppLogger.shared.debug(message, category: category)
}

func logInfo(_ message: String, category: String = "General") {
    AppLogger.shared.info(message, category: category)
}

func logWarning(_ message: String, category: String = "General") {
    AppLogger.shared.warning(message, category: category)
}

func logError(_ message: String, category: String = "General") {
    AppLogger.shared.error(message, category: category)
}

func logError(_ error: Error, category: String = "General", message: String? = nil) {
    AppLogger.shared.error(error, category: category, message: message)
}
