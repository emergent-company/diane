import Foundation

public struct DianeMessage: Identifiable, Codable, Sendable, Hashable {
    public let id: String
    public let role: String
    public let content: String
    public let createdAt: String?
    public let toolCalls: [ToolCall]?
    public let reasoningContent: String?

    enum CodingKeys: String, CodingKey {
        case id, role, content
        case createdAt = "created_at"
        case toolCalls = "tool_calls"
        case reasoningContent = "reasoning_content"
    }

    public init(id: String, role: String, content: String, createdAt: String? = nil,
                toolCalls: [ToolCall]? = nil, reasoningContent: String? = nil) {
        self.id = id; self.role = role; self.content = content
        self.createdAt = createdAt; self.toolCalls = toolCalls
        self.reasoningContent = reasoningContent
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
