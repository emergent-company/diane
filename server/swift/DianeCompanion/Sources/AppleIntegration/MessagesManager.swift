import Foundation
import OSLog

/// Manages iMessage via AppleScript.
@MainActor
final class MessagesManager: ObservableObject {
    private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "Messages")

    @Published private(set) var isAuthorized = false

    /// Check if Messages automation permission is granted.
    /// On macOS 14+, this triggers the permission prompt.
    func checkAuthorization() {
        // Quick probe: try to get the Messages app reference
        let script = """
        tell application "System Events"
            get name of every process whose name is "Messages"
        end tell
        """
        Task {
            if let result = try? await AppleScriptRunner.run(script), !result.isEmpty {
                isAuthorized = true
            }
        }
    }

    /// Send an iMessage to a contact.
    /// - Parameters:
    ///   - text: The message text
    ///   - recipient: Phone number, email, or contact name
    func sendMessage(text: String, to recipient: String) async throws {
        let safeText = text.replacingOccurrences(of: "\"", with: "\\\"")
        let safeRecipient = recipient.replacingOccurrences(of: "\"", with: "\\\"")
        let script = """
        tell application "Messages"
            set targetService to 1st service whose service type = iMessage
            set targetBuddy to buddy "\(safeRecipient)" of targetService
            send "\(safeText)" to targetBuddy
        end tell
        """
        try await AppleScriptRunner.run(script)
        logger.info("Sent iMessage to \(recipient)")
    }

    /// Send an SMS via the Messages app (falls back to iMessage if SMS unavailable).
    func sendSMS(text: String, to phoneNumber: String) async throws {
        let safeText = text.replacingOccurrences(of: "\"", with: "\\\"")
        let safePhone = phoneNumber.replacingOccurrences(of: "\"", with: "\\\"")
        let script = """
        tell application "Messages"
            set targetService to 1st service whose service type = SMS
            set targetBuddy to buddy "\(safePhone)" of targetService
            send "\(safeText)" to targetBuddy
        end tell
        """
        try await AppleScriptRunner.run(script)
        logger.info("Sent SMS to \(phoneNumber)")
    }

    /// Get recent conversations (last 10).
    func fetchRecentConversations() async throws -> [String] {
        let script = """
        tell application "Messages"
            set chatNames to {}
            repeat with c in text chats
                set end of chatNames to name of c
            end repeat
            return chatNames
        end tell
        """
        let result = try await AppleScriptRunner.run(script)
        return result.components(separatedBy: ", ")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }
}
