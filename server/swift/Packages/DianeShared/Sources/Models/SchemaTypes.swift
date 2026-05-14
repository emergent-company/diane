import Foundation

// MARK: - Schema Response

public struct SchemaResponse: Codable, Sendable {
    public let types: [SchemaType]?
    public let relationships: [SchemaRelationship]?
    public let enums: [SchemaEnum]?
}

// MARK: - Schema Type

public struct SchemaType: Codable, Sendable, Identifiable {
    public let name: String
    public let fields: [SchemaField]?
    public let description: String?

    public var id: String { name }
}

// MARK: - Schema Field

public struct SchemaField: Codable, Sendable {
    public let name: String
    public let type: String
    public let required: Bool?
    public let description: String?
    public let defaultValue: String?
    public let isList: Bool?

    enum CodingKeys: String, CodingKey {
        case name, type, required, description
        case defaultValue = "default_value"
        case isList = "is_list"
    }
}

// MARK: - Schema Relationship

public struct SchemaRelationship: Codable, Sendable {
    public let name: String
    public let fromType: String
    public let toType: String
    public let type: String // "one_to_one", "one_to_many", "many_to_many"
    public let description: String?

    enum CodingKeys: String, CodingKey {
        case name, type, description
        case fromType = "from_type"
        case toType = "to_type"
    }
}

// MARK: - Schema Enum

public struct SchemaEnum: Codable, Sendable, Identifiable {
    public let name: String
    public let values: [String]
    public let description: String?

    public var id: String { name }
}

// MARK: - Generic API Response Wrappers

public struct APIResponse<T: Codable & Sendable>: Codable, Sendable {
    public let success: Bool
    public let data: T?
    public let error: String?
    public let message: String?
}

public struct ListResponse<T: Codable & Sendable>: Codable, Sendable {
    public let data: [T]
    public let total: Int?
    public let hasMore: Bool?

    enum CodingKeys: String, CodingKey {
        case data, total
        case hasMore = "has_more"
    }
}
