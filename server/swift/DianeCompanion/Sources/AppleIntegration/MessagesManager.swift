import Foundation
import OSLog

/// Sends iMessages via AppleScript.
@MainActor
final class MessagesManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Messages")
    
    func sendMessage(text: String, to handle: String) async throws {
        let escapedText = text.replacingOccurrences(of: "\"", with: "\\\"")
        let script = """
        tell application "Messages"
            set targetService to 1st service whose service type = iMessage
            set targetBuddy to buddy "\(escapedText)" of targetService
            send "\(escapedText)" to targetBuddy
        end tell
        """
        try await AppleScriptRunner.run(script)
        logger.info("Sent iMessage to \(handle)")
    }
}
