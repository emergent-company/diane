import Foundation

// MARK: - MCP Server

public struct MCPServer: Codable, Sendable, Identifiable, Hashable {
    public let id: String
    public let name: String
    public let url: String?
    public let status: String?
    public let tools: [MCPTool]?
    public let prompts: [MCPPrompt]?
    public let enabled: Bool?

    public init(id: String, name: String, url: String? = nil, status: String? = nil,
                tools: [MCPTool]? = nil, prompts: [MCPPrompt]? = nil, enabled: Bool? = nil) {
        self.id = id; self.name = name; self.url = url; self.status = status
        self.tools = tools; self.prompts = prompts; self.enabled = enabled
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: MCPServer, rhs: MCPServer) -> Bool { lhs.id == rhs.id }
}

// MARK: - MCP Tool

public struct MCPTool: Codable, Sendable, Identifiable, Hashable {
    public let name: String
    public let description: String?
    public let inputSchema: [String: AnyCodable]?

    public var id: String { name }

    enum CodingKeys: String, CodingKey {
        case name, description
        case inputSchema = "input_schema"
    }

    public init(name: String, description: String? = nil, inputSchema: [String: AnyCodable]? = nil) {
        self.name = name; self.description = description; self.inputSchema = inputSchema
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(name) }
    public static func == (lhs: MCPTool, rhs: MCPTool) -> Bool { lhs.name == rhs.name }
}

// MARK: - MCP Tool Info (from API responses)

public struct MCPToolInfo: Codable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let description: String?
    public let serverID: String?
    public let parameters: [String: AnyCodable]?

    enum CodingKeys: String, CodingKey {
        case id, name, description
        case serverID = "server_id"
        case parameters
    }

    public init(id: String, name: String, description: String? = nil,
                serverID: String? = nil, parameters: [String: AnyCodable]? = nil) {
        self.id = id; self.name = name; self.description = description
        self.serverID = serverID; self.parameters = parameters
    }
}

// MARK: - MCP Prompt

public struct MCPPrompt: Codable, Sendable, Identifiable, Hashable {
    public let name: String
    public let description: String?
    public let arguments: [MCPPromptArgument]?

    public var id: String { name }

    public init(name: String, description: String? = nil, arguments: [MCPPromptArgument]? = nil) {
        self.name = name; self.description = description; self.arguments = arguments
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(name) }
    public static func == (lhs: MCPPrompt, rhs: MCPPrompt) -> Bool { lhs.name == rhs.name }
}

public struct MCPPromptArgument: Codable, Sendable, Hashable {
    public let name: String
    public let description: String?
    public let required: Bool?

    public init(name: String, description: String? = nil, required: Bool? = nil) {
        self.name = name; self.description = description; self.required = required
    }
}

// MARK: - AnyCodable helper

public struct AnyCodable: Codable, @unchecked Sendable, Hashable {
    public let value: Any

    public init(_ value: Any) { self.value = value }

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let intVal = try? container.decode(Int.self) { value = intVal }
        else if let doubleVal = try? container.decode(Double.self) { value = doubleVal }
        else if let boolVal = try? container.decode(Bool.self) { value = boolVal }
        else if let stringVal = try? container.decode(String.self) { value = stringVal }
        else if let arrayVal = try? container.decode([AnyCodable].self) { value = arrayVal.map { $0.value } }
        else if let dictVal = try? container.decode([String: AnyCodable].self) { value = dictVal.mapValues { $0.value } }
        else { throw DecodingError.dataCorruptedError(in: container, debugDescription: "AnyCodable: unsupported type") }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        if let intVal = value as? Int { try container.encode(intVal) }
        else if let doubleVal = value as? Double { try container.encode(doubleVal) }
        else if let boolVal = value as? Bool { try container.encode(boolVal) }
        else if let stringVal = value as? String { try container.encode(stringVal) }
        else if let arrayVal = value as? [Any] { try container.encode(arrayVal.map { AnyCodable($0) }) }
        else if let dictVal = value as? [String: Any] { try container.encode(dictVal.mapValues { AnyCodable($0) }) }
        else { throw EncodingError.invalidValue(value, .init(codingPath: container.codingPath, debugDescription: "AnyCodable: unsupported type")) }
    }

    public func hash(into hasher: inout Hasher) {
        if let intVal = value as? Int { hasher.combine(intVal) }
        else if let stringVal = value as? String { hasher.combine(stringVal) }
        else if let boolVal = value as? Bool { hasher.combine(boolVal) }
    }

    public static func == (lhs: AnyCodable, rhs: AnyCodable) -> Bool {
        if let l = lhs.value as? Int, let r = rhs.value as? Int { return l == r }
        if let l = lhs.value as? String, let r = rhs.value as? String { return l == r }
        if let l = lhs.value as? Bool, let r = rhs.value as? Bool { return l == r }
        return false
    }
}
