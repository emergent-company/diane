import Foundation

/// Creates Apple Notes via AppleScript.
@MainActor
final class NotesManager: ObservableObject {
    
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
        logInfo("Created note: \(title)", category: "Notes")
    }
}
