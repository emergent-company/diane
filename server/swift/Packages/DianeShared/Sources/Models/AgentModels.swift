import Foundation

// MARK: - Agent Definition

public struct AgentDef: Codable, Sendable, Identifiable, Hashable {
    public let id: String
    public let name: String
    public let description: String?
    public let model: String?
    public let provider: String?
    public let tools: [String]?
    public let maxTokens: Int?
    public let temperature: Double?
    public let systemPrompt: String?
    public let isActive: Bool?

    enum CodingKeys: String, CodingKey {
        case id, name, description, model, provider, tools
        case maxTokens = "max_tokens"
        case temperature
        case systemPrompt = "system_prompt"
        case isActive = "is_active"
    }

    public init(id: String, name: String, description: String? = nil, model: String? = nil,
                provider: String? = nil, tools: [String]? = nil, maxTokens: Int? = nil,
                temperature: Double? = nil, systemPrompt: String? = nil, isActive: Bool? = nil) {
        self.id = id; self.name = name; self.description = description
        self.model = model; self.provider = provider; self.tools = tools
        self.maxTokens = maxTokens; self.temperature = temperature
        self.systemPrompt = systemPrompt; self.isActive = isActive
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: AgentDef, rhs: AgentDef) -> Bool { lhs.id == rhs.id }
}

// MARK: - Agent (runtime instance)

public struct AgentInfo: Codable, Sendable, Identifiable, Hashable {
    public let id: String
    public let name: String
    public let status: String?
    public let sessionCount: Int?
    public let agentDefID: String?
    public let createdAt: String?
    public let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, name, status
        case sessionCount = "session_count"
        case agentDefID = "agent_def_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    public init(id: String, name: String, status: String? = nil, sessionCount: Int? = nil,
                agentDefID: String? = nil, createdAt: String? = nil, updatedAt: String? = nil) {
        self.id = id; self.name = name; self.status = status
        self.sessionCount = sessionCount; self.agentDefID = agentDefID
        self.createdAt = createdAt; self.updatedAt = updatedAt
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: AgentInfo, rhs: AgentInfo) -> Bool { lhs.id == rhs.id }
}

// MARK: - Agent Detail (full definition + runtime info)

public struct AgentDetail: Codable, Sendable, Identifiable {
    public let id: String
    public let info: AgentInfo?
    public let def: AgentDef?

    enum CodingKeys: String, CodingKey {
        case id, info, def
    }

    public init(id: String, info: AgentInfo? = nil, def: AgentDef? = nil) {
        self.id = id; self.info = info; self.def = def
    }
}
