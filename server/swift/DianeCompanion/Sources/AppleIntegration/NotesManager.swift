import Foundation
import OSLog

/// Creates Apple Notes via AppleScript.
@MainActor
final class NotesManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Notes")
    
    func createNote(title: String, body: String) async throws {
        let escapedTitle = title.replacingOccurrences(of: "\"", with: "\\\"")
        let escapedBody = body.replacingOccurrences(of: "\"", with: "\\\"")
        let script = """
        tell application "Notes"
            tell account "iCloud"
                make new note with properties {name:"\(escapedTitle)", body:"\(escapedBody)"}
            end tell
        end tell
        """
        try await AppleScriptRunner.run(script)
        logger.info("Created note: \(title)")
    }
}
