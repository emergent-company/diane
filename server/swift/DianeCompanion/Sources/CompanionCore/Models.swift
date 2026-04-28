import Foundation

// MARK: - MCP Server

public struct MCPServer: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let serverType: String?  // "sse" | "stdio"
    public let url: String?
    public let status: String?
    public let tools: [MCPTool]?

    enum CodingKeys: String, CodingKey {
        case id, name, url, status, tools
        case serverType = "server_type"
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: MCPServer, rhs: MCPServer) -> Bool { lhs.id == rhs.id }
}

public struct MCPTool: Identifiable, Codable, Sendable {
    public let id: String
    public let name: String
    public let description: String?
}

// MARK: - Diane Session (for Companion session log viewing)

struct DianeSession: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let key: String?
    let title: String?
    let status: String?
    let messageCount: Int?
    let totalTokens: Int?
    let createdAt: String?

    enum CodingKeys: String, CodingKey {
        case id, key, title, status
        case messageCount = "message_count"
        case totalTokens = "total_tokens"
        case createdAt = "created_at"
    }

    func hash(into hasher: inout Hasher) { hasher.combine(id) }
    static func == (lhs: DianeSession, rhs: DianeSession) -> Bool { lhs.id == rhs.id }
}

struct DianeMessage: Identifiable, Codable, Sendable {
    let id: String
    let role: String
    let content: String
    let sequenceNumber: Int?
    let tokenCount: Int?

    enum CodingKeys: String, CodingKey {
        case id, role, content
        case sequenceNumber = "sequence_number"
        case tokenCount = "token_count"
    }
}

// MARK: - MCP Relay Session

struct RelaySession: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let instanceID: String?
    let nodeName: String?
    let toolCount: Int?
    let connectedAt: String?
    let lastSeenAt: String?

    enum CodingKeys: String, CodingKey {
        case id
        case instanceID = "instance_id"
        case nodeName = "node_name"
        case toolCount = "tool_count"
        case connectedAt = "connected_at"
        case lastSeenAt = "last_seen_at"
    }

    func hash(into hasher: inout Hasher) { hasher.combine(id) }
    static func == (lhs: RelaySession, rhs: RelaySession) -> Bool { lhs.id == rhs.id }
}
