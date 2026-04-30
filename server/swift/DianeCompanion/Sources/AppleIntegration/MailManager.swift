import Foundation

/// Manages Apple Mail via AppleScript.
@MainActor
final class MailManager: ObservableObject {

    @Published private(set) var isAuthorized = false

    func checkAuthorization() {
        let script = """
        tell application "System Events"
            get name of every process whose name is "Mail"
        end tell
        """
        Task {
            if let result = try? await AppleScriptRunner.run(script), !result.isEmpty {
                isAuthorized = true
            }
        }
    }

    /// Send an email via the Mail app.
    func sendEmail(to: String, subject: String, body: String) async throws {
        let safeTo = to.replacingOccurrences(of: "\"", with: "\\\"")
        let safeSubject = subject.replacingOccurrences(of: "\"", with: "\\\"")
        let safeBody = body.replacingOccurrences(of: "\"", with: "\\\"")
        let script = """
        tell application "Mail"
            set newMessage to make new outgoing message with properties {subject:"\(safeSubject)", content:"\(safeBody)", visible:true}
            tell newMessage
                make new to recipient at end of recipients with properties {address:"\(safeTo)"}
                send
            end tell
        end tell
        """
        try await AppleScriptRunner.run(script)
        logInfo("Sent email to \(to)", category: "Mail")
    }

    /// Fetch recent inbox messages summary.
    func fetchInbox(count: Int = 20) async throws -> [MailMessage] {
        let script = """
        tell application "Mail"
            set msgs to messages of inbox
            set resultList to {}
            repeat with i from 1 to \(count)
                if i > count of msgs then exit repeat
                set msg to item i of msgs
                set end of resultList to (subject of msg) & "|" & (sender of msg) & "|" & (date received of msg as string)
            end repeat
            return resultList
        end tell
        """
        let result = try await AppleScriptRunner.run(script)
        let lines = result.components(separatedBy: ", ")
        return lines.compactMap { line -> MailMessage? in
            let parts = line.components(separatedBy: "|")
            guard parts.count >= 1 else { return nil }
            return MailMessage(
                id: UUID().uuidString,
                subject: parts.indices.contains(0) ? parts[0] : nil,
                sender: parts.indices.contains(1) ? parts[1] : nil,
                dateString: parts.indices.contains(2) ? parts[2] : nil
            )
        }
    }
}

/// A summary model for a Mail message.
struct MailMessage: Identifiable, Sendable {
    let id: String
    let subject: String?
    let sender: String?
    let dateString: String?
}
