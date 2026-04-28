import Foundation

// MARK: - Project

/// A project on the Emergent server.
/// Field names match the server's camelCase JSON (orgId, stats.objectCount, etc.)
public struct Project: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let orgId: String?

    // No CodingKeys needed — field names match the JSON directly (id, name, orgId)

    public init(id: String, name: String, orgId: String? = nil) {
        self.id = id; self.name = name; self.orgId = orgId
    }
}

// MARK: - Project Stats (fetched from /api/type-registry/projects/{id}/stats + /api/documents)

/// Stats assembled from two endpoints:
///  - GET /api/type-registry/projects/{projectId}/stats  → object/type counts
///  - GET /api/documents?limit=1  (X-Project-ID header)  → document total
public struct ProjectStats: Codable, Hashable, Sendable {
    public let totalObjects: Int
    public let totalTypes: Int
    public let enabledTypes: Int
    public let typesWithObjects: Int
    public let totalDocuments: Int

    public init(totalObjects: Int, totalTypes: Int, enabledTypes: Int,
                typesWithObjects: Int, totalDocuments: Int) {
        self.totalObjects = totalObjects
        self.totalTypes = totalTypes
        self.enabledTypes = enabledTypes
        self.typesWithObjects = typesWithObjects
        self.totalDocuments = totalDocuments
    }

    // Removed: objectCount, relationshipCount, documentCount, queuedJobs, runningJobs
    // — the server never embeds stats in /api/projects; real counts come from separate endpoints.
}


// MARK: - Account Stats

public struct AccountStats: Codable, Sendable {
    public let serverURL: String
    public let serverVersion: String?
    public let latencyMs: Double?
    public let totalProjects: Int
    public let totalObjects: Int
    public let totalRelations: Int
    public let totalApiRequests: Int
    public let avgLatencyMs: Double?

    enum CodingKeys: String, CodingKey {
        case serverURL       = "server_url"
        case serverVersion   = "server_version"
        case latencyMs       = "latency_ms"
        case totalProjects   = "total_projects"
        case totalObjects    = "total_objects"
        case totalRelations  = "total_relations"
        case totalApiRequests = "total_api_requests"
        case avgLatencyMs    = "avg_latency_ms"
    }
}

// MARK: - Trace / Extraction Job

/// An extraction job — maps to the server's `/monitoring/extraction-jobs` endpoint.
public struct Trace: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let status: String
    public let spanCount: Int?
    public let createdAt: String?
    public let updatedAt: String?
    public let sourceType: String?
    public let documentID: String?
    public let errorMessage: String?

    enum CodingKeys: String, CodingKey {
        case id, status
        case spanCount   = "span_count"
        case createdAt   = "created_at"
        case updatedAt   = "updated_at"
        case sourceType  = "source_type"
        case documentID  = "document_id"
        case errorMessage = "error_message"
    }
}

public struct TraceDetail: Codable, Sendable {
    public let id: String
    public let status: String
    public let logs: [String]?
    public let llmCalls: [LLMCall]?
}

public struct LLMCall: Identifiable, Codable, Sendable {
    public let id: String
    public let model: String?
    public let prompt: String?
    public let response: String?
    public let durationMs: Double?

    enum CodingKeys: String, CodingKey {
        case id, model, prompt, response
        case durationMs = "duration_ms"
    }
}

// MARK: - Worker

/// Represents a processing worker node.
public struct Worker: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let status: WorkerStatus
    public let currentJobID: String?
    public let cpuPercent: Double?
    public let memoryMB: Double?
    public let lastSeenAt: String?

    enum CodingKeys: String, CodingKey {
        case id, status
        case currentJobID = "current_job_id"
        case cpuPercent   = "cpu_percent"
        case memoryMB     = "memory_mb"
        case lastSeenAt   = "last_seen_at"
    }
}

public enum WorkerStatus: String, Codable, Sendable {
    case idle    = "idle"
    case busy    = "busy"
    case offline = "offline"
    case unknown = "unknown"

    public var displayLabel: String {
        switch self {
        case .idle:    return "Idle"
        case .busy:    return "Busy"
        case .offline: return "Offline"
        case .unknown: return "Unknown"
        }
    }

    public var systemIcon: String {
        switch self {
        case .idle:    return "checkmark.circle.fill"
        case .busy:    return "gearshape.fill"
        case .offline: return "exclamationmark.triangle.fill"
        case .unknown: return "questionmark.circle"
        }
    }
}

// MARK: - Object (Graph)

public struct GraphObject: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let type: String?
    public let score: Double?
    public let properties: [String: AnyCodable]?
    public let createdAt: String?

    enum CodingKeys: String, CodingKey {
        case id, type, score, properties
        case createdAt = "created_at"
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: GraphObject, rhs: GraphObject) -> Bool { lhs.id == rhs.id }
}

/// Type-erased Codable wrapper for mixed-type JSON values.
public struct AnyCodable: Codable, Sendable {
    public let value: Any

    public init(_ value: Any) { self.value = value }

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let bool = try? container.decode(Bool.self)   { value = bool; return }
        if let int  = try? container.decode(Int.self)    { value = int;  return }
        if let dbl  = try? container.decode(Double.self) { value = dbl;  return }
        if let str  = try? container.decode(String.self) { value = str;  return }
        if let arr  = try? container.decode([AnyCodable].self) { value = arr.map(\.value); return }
        if let dict = try? container.decode([String: AnyCodable].self) {
            value = dict.mapValues(\.value); return
        }
        value = NSNull()
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch value {
        case let b as Bool:   try container.encode(b)
        case let i as Int:    try container.encode(i)
        case let d as Double: try container.encode(d)
        case let s as String: try container.encode(s)
        default: try container.encodeNil()
        }
    }
}

// MARK: - Agent

public struct Agent: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let triggerType: String?
    public let schedule: String?
    public let prompt: String?
    public let isActive: Bool
    public let capabilities: [String]?

    enum CodingKeys: String, CodingKey {
        case id, name, schedule, prompt
        case triggerType = "trigger_type"
        case isActive    = "is_active"
        case capabilities
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: Agent, rhs: Agent) -> Bool { lhs.id == rhs.id }
}

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

// MARK: - User Profile

public struct UserProfile: Codable, Sendable {
    public let id: String
    public let name: String?
    public let email: String?
    public let role: String?
    public let apiKey: String?
    public let apiKeyCreatedAt: String?
    public let apiKeyLastUsed: String?

    enum CodingKeys: String, CodingKey {
        case id, name, email, role
        case apiKey        = "api_key"
        case apiKeyCreatedAt = "api_key_created_at"
        case apiKeyLastUsed  = "api_key_last_used"
    }
}

// MARK: - Document

/// A document stored in the Emergent platform.
public struct Document: Identifiable, Codable, Sendable {
    public let id: String
    public let projectId: String?
    public let filename: String
    public let mimeType: String?
    public let fileHash: String?
    public let contentHash: String?
    public let sourceType: String?
    public let conversionStatus: String?
    public let extractionStatus: String?
    public let storageKey: String?
    public let storageUrl: String?
    public let fileSizeBytes: Int?
    public let syncVersion: Int?
    public let chunks: Int?
    public let embeddedChunks: Int?
    public let totalChars: Int?
    public let content: String?
    public let createdAt: String?
    public let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, filename, content
        case projectId        = "projectId"
        case mimeType         = "mimeType"
        case fileHash         = "fileHash"
        case contentHash      = "contentHash"
        case sourceType       = "source_type"
        case conversionStatus = "conversionStatus"
        case extractionStatus = "extractionStatus"
        case storageKey       = "storageKey"
        case storageUrl       = "storageUrl"
        case fileSizeBytes    = "fileSizeBytes"
        case syncVersion      = "syncVersion"
        case chunks           = "chunks"
        case embeddedChunks   = "embeddedChunks"
        case totalChars       = "totalChars"
        case createdAt        = "created_at"
        case updatedAt        = "updated_at"
    }
}

// MARK: - Document Chunks

/// Decoded from GET /api/chunks?documentId={id}
/// Metadata contains optional character offsets for inline placement.
public struct DocumentChunk: Identifiable, Codable, Sendable {
    public let id: String
    public let documentId: String
    public let index: Int
    public let text: String
    public let size: Int
    public let hasEmbedding: Bool
    public let metadata: ChunkMetadata?
    public let createdAt: String?

    public struct ChunkMetadata: Codable, Sendable {
        public let strategy: String?
        public let startOffset: Int?
        public let endOffset: Int?
        public let boundaryType: String?
    }
}

public struct ChunksResponse: Codable, Sendable {
    public let data: [DocumentChunk]
    public let totalCount: Int
}

// MARK: - Server Diagnostics

/// Decoded from GET /api/diagnostics
public struct ServerDiagnostics: Codable, Sendable {
    public let timestamp: String?
    public let uptime: String?
    public let server: ServerInfo?
    public let database: DatabaseInfo?

    public struct ServerInfo: Codable, Sendable {
        public let version: String?
        public let environment: String?
    }

    public struct DatabaseInfo: Codable, Sendable {
        public let pool: DBPool?

        public struct DBPool: Codable, Sendable {
            public let totalConns: Int?
            public let idleConns: Int?
            public let maxConns: Int?

            enum CodingKeys: String, CodingKey {
                case totalConns = "total_conns"
                case idleConns  = "idle_conns"
                case maxConns   = "max_conns"
            }
        }
    }
}

// MARK: - Embedding Status

/// Running/paused state for a single embedding pipeline worker.
/// Decoded from the nested objects/relationships/sweep keys in GET /api/embeddings/status
public struct EmbeddingWorkerState: Codable, Sendable {
    public let running: Bool
    public let paused: Bool
}

/// Configuration for the embedding pipeline, decoded from the `config` key in GET /api/embeddings/status
public struct EmbeddingConfig: Codable, Sendable {
    public let batchSize: Int?
    public let concurrency: Int?
    public let intervalMs: Int?
    public let minConcurrency: Int?
    public let maxConcurrency: Int?
    public let currentConcurrency: Int?
    public let healthScore: Double?
    public let enableAdaptiveScaling: Bool?

    enum CodingKeys: String, CodingKey {
        case batchSize            = "batch_size"
        case concurrency          = "concurrency"
        case intervalMs           = "interval_ms"
        case minConcurrency       = "min_concurrency"
        case maxConcurrency       = "max_concurrency"
        case currentConcurrency   = "current_concurrency"
        case healthScore          = "health_score"
        case enableAdaptiveScaling = "enable_adaptive_scaling"
    }
}

/// Response from GET /api/embeddings/status
public struct EmbeddingStatus: Codable, Sendable {
    public let objects: EmbeddingWorkerState?
    public let relationships: EmbeddingWorkerState?
    public let sweep: EmbeddingWorkerState?
    public let config: EmbeddingConfig?
    // No CodingKeys needed — JSON keys match Swift field names directly
}

/// An embedding policy for a project, decoded from GET /api/graph/embedding-policies
public struct EmbeddingPolicy: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let projectId: String?
    public let name: String
    public let description: String?
    public let objectTypes: [String]?
    public let fields: [String]?
    public let template: String?
    public let model: String?
    public let active: Bool

    enum CodingKeys: String, CodingKey {
        case id, name, description, template, model, active
        case projectId   = "project_id"
        case objectTypes = "object_types"
        case fields      = "fields"
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: EmbeddingPolicy, rhs: EmbeddingPolicy) -> Bool { lhs.id == rhs.id }
}

// MARK: - Provider Credentials

/// An org-level LLM provider credential (no secret fields — API returns public-safe representation).
/// Decoded from GET /api/v1/organizations/{orgId}/providers/credentials
public struct OrgCredential: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let orgId: String
    public let provider: String
    public let gcpProject: String?
    public let location: String?
    public let createdAt: String?
    public let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, provider
        case orgId      = "orgId"
        case gcpProject = "gcpProject"
        case location   = "location"
        case createdAt  = "createdAt"
        case updatedAt  = "updatedAt"
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: OrgCredential, rhs: OrgCredential) -> Bool { lhs.id == rhs.id }
}

/// A per-project provider policy.
/// Decoded from GET /api/v1/projects/{projectId}/providers/policies
public struct ProjectPolicy: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let projectId: String
    public let provider: String
    public let policy: String          // "none" | "organization" | "project"
    public let gcpProject: String?
    public let location: String?
    public let embeddingModel: String?
    public let generativeModel: String?
    public let createdAt: String?
    public let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, provider, policy
        case projectId       = "projectId"
        case gcpProject      = "gcpProject"
        case location        = "location"
        case embeddingModel  = "embeddingModel"
        case generativeModel = "generativeModel"
        case createdAt       = "createdAt"
        case updatedAt       = "updatedAt"
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: ProjectPolicy, rhs: ProjectPolicy) -> Bool { lhs.id == rhs.id }
}

// MARK: - Query Result

/// Response from POST /api/graph/search
/// Returns ranked graph objects with semantic + lexical scores.
public struct QueryResult: Codable, Sendable {
    public let data: [QueryResultItem]?
    public let total: Int?
    public let hasMore: Bool?
    public let meta: QueryResultMeta?
}

public struct QueryResultItem: Codable, Identifiable, Hashable, Sendable {
    public var id: String { object.id }
    public let object: GraphObject
    public let score: Double?
    public let lexicalScore: Double?

    public func hash(into hasher: inout Hasher) { hasher.combine(object.id) }
    public static func == (lhs: QueryResultItem, rhs: QueryResultItem) -> Bool { lhs.object.id == rhs.object.id }
}

public struct QueryResultMeta: Codable, Sendable {
    public let elapsedMs: Double?

    enum CodingKeys: String, CodingKey {
        case elapsedMs = "elapsed_ms"
    }
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
