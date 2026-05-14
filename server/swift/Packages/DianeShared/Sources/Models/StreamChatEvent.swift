import Foundation

public struct StreamChatEvent: Codable, Sendable {
    public let type: String
    public let content: String?
    public let name: String?
    public let role: String?
    public let sessionID: String?
    public let runID: String?
    public let message: String?

    public init(type: String, content: String?, name: String?, role: String?,
                sessionID: String?, runID: String?, message: String?) {
        self.type = type; self.content = content; self.name = name
        self.role = role; self.sessionID = sessionID; self.runID = runID
        self.message = message
    }
}
