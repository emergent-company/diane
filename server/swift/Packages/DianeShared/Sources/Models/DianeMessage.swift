import Foundation

public struct DianeMessage: Identifiable, Codable, Sendable, Hashable {
    public let id: String
    public let role: String
    public let content: String
    public let createdAt: String?
    public let toolCalls: [ToolCall]?
    public let reasoningContent: String?

    /// Optional content type hint. One of: "html", "markdown", "text", or nil for auto-detect.
    public let contentType: String?

    /// Delivery status for the state machine (sending → sent → read, or queued/failed/streaming)
    public let sendStatus: SendStatus?

    /// User-facing error message when sendStatus == .failed
    public let errorMessage: String?

    public enum SendStatus: String, Codable, Sendable, CaseIterable {
        /// Created locally, waiting for network to send
        case queued
        /// Currently being sent to server
        case sending
        /// Delivered to server, awaiting processing
        case sent
        /// Server/AI acknowledged and started processing
        case read
        /// All retries exhausted — message could not be sent
        case failed
        /// Auto-retry in progress
        case retrying
        /// Assistant message actively being streamed
        case streaming
    }

    enum CodingKeys: String, CodingKey {
        case id, role, content
        case createdAt = "created_at"
        case toolCalls = "tool_calls"
        case reasoningContent = "reasoning_content"
        case contentType = "content_type"
        case sendStatus = "send_status"
        case errorMessage = "error_message"
    }

    public init(id: String, role: String, content: String, createdAt: String? = nil,
                toolCalls: [ToolCall]? = nil, reasoningContent: String? = nil,
                contentType: String? = nil,
                sendStatus: SendStatus? = nil, errorMessage: String? = nil) {
        self.id = id; self.role = role; self.content = content
        self.createdAt = createdAt; self.toolCalls = toolCalls
        self.reasoningContent = reasoningContent
        self.contentType = contentType
        self.sendStatus = sendStatus
        self.errorMessage = errorMessage
    }

    public struct ToolCall: Codable, Sendable, Hashable {
        public let name: String
        public let arguments: String?
        public let result: String?
        public init(name: String, arguments: String? = nil, result: String? = nil) {
            self.name = name; self.arguments = arguments; self.result = result
        }
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: DianeMessage, rhs: DianeMessage) -> Bool { lhs.id == rhs.id }
}
