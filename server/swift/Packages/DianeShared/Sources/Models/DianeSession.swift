import Foundation

public struct DianeSession: Identifiable, Codable, Sendable, Hashable {
    public let id: String
    public let entityID: String?
    public let title: String?
    public let status: String?
    public let messageCount: Int?
    public let createdAt: String?
    public let updatedAt: String?
    public let agentName: String?

    enum CodingKeys: String, CodingKey {
        case id, title, status, messageCount = "message_count"
        case entityID = "entity_id", createdAt = "created_at", updatedAt = "updated_at"
        case agentName = "agent_name"
    }

    public init(id: String, title: String? = nil, status: String? = nil,
                messageCount: Int? = nil, entityID: String? = nil,
                createdAt: String? = nil, updatedAt: String? = nil,
                agentName: String? = nil) {
        self.id = id
        self.title = title
        self.status = status
        self.messageCount = messageCount
        self.entityID = entityID
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.agentName = agentName
    }

    // Dual-format decoder supporting both flat and graph formats
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)

        // Try flat format first
        if let flatID = try? container.decode(String.self, forKey: .id) {
            self.id = flatID
            self.entityID = try? container.decodeIfPresent(String.self, forKey: .entityID)
            self.title = try? container.decodeIfPresent(String.self, forKey: .title)
            self.status = try? container.decodeIfPresent(String.self, forKey: .status)
            self.messageCount = try? container.decodeIfPresent(Int.self, forKey: .messageCount)
            self.createdAt = try? container.decodeIfPresent(String.self, forKey: .createdAt)
            self.updatedAt = try? container.decodeIfPresent(String.self, forKey: .updatedAt)
            self.agentName = try? container.decodeIfPresent(String.self, forKey: .agentName)
            return
        }

        // Fall back to graph format
        self.entityID = try container.decode(String.self, forKey: .entityID)
        self.id = try container.decode(String.self, forKey: .id)

        // Extract from properties dict
        if let props = try? decoder.container(keyedBy: DynamicKey.self) {
            self.title = try? props.decodeIfPresent(String.self, forKey: DynamicKey(stringValue: "title")!)
            self.status = try? props.decodeIfPresent(String.self, forKey: DynamicKey(stringValue: "status")!)
            self.messageCount = try? props.decodeIfPresent(Int.self, forKey: DynamicKey(stringValue: "message_count")!)
            self.createdAt = try? props.decodeIfPresent(String.self, forKey: DynamicKey(stringValue: "created_at")!)
            self.updatedAt = try? props.decodeIfPresent(String.self, forKey: DynamicKey(stringValue: "updated_at")!)
            self.agentName = try? props.decodeIfPresent(String.self, forKey: DynamicKey(stringValue: "agent_name")!)
        } else {
            self.title = nil; self.status = nil; self.messageCount = nil
            self.createdAt = nil; self.updatedAt = nil; self.agentName = nil
        }
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: DianeSession, rhs: DianeSession) -> Bool { lhs.id == rhs.id }
}

// For graph format dynamic keys
struct DynamicKey: CodingKey {
    var stringValue: String
    var intValue: Int?
    init?(stringValue: String) { self.stringValue = stringValue; self.intValue = nil }
    init?(intValue: Int) { self.stringValue = "\(intValue)"; self.intValue = intValue }
}
