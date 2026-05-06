import Foundation
import OSLog

/// Sends emails via AppleScript through the Mail app.
@MainActor
final class MailManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Mail")
    
    func sendEmail(to: String, subject: String, body: String) async throws {
        let escapedSubject = subject.replacingOccurrences(of: "\"", with: "\\\"")
        let escapedBody = body.replacingOccurrences(of: "\"", with: "\\\"")
        let script = """
        tell application "Mail"
            set newMessage to make new outgoing message with properties {subject:"\(escapedSubject)", content:"\(escapedBody)", visible:true}
            tell newMessage
                make new to recipient at end of recipients with properties {address:"\(to)"}
                send
            end tell
        end tell
        """
        try await AppleScriptRunner.run(script)
        logger.info("Sent email to \(to)")
    }
}
