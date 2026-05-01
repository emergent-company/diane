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
public struct AnyCodable: Codable, @unchecked Sendable {
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
        switch value {
        case let b as Bool:
            var container = encoder.singleValueContainer()
            try container.encode(b)
        case let i as Int:
            var container = encoder.singleValueContainer()
            try container.encode(i)
        case let d as Double:
            var container = encoder.singleValueContainer()
            try container.encode(d)
        case let s as String:
            var container = encoder.singleValueContainer()
            try container.encode(s)
        case let arr as [Any]:
            var container = encoder.unkeyedContainer()
            for item in arr {
                try container.encode(AnyCodable(item))
            }
        case let dict as [String: Any]:
            var container = encoder.container(keyedBy: AnyCodingKey.self)
            for (key, val) in dict {
                if let k = AnyCodingKey(stringValue: key) {
                    try container.encode(AnyCodable(val), forKey: k)
                }
            }
        default:
            var container = encoder.singleValueContainer()
            try container.encodeNil()
        }
    }
}

/// Dynamic coding key used by AnyCodable for dictionary encoding.
private struct AnyCodingKey: CodingKey {
    var stringValue: String
    var intValue: Int?

    init?(stringValue: String) { self.stringValue = stringValue; self.intValue = nil }
    init?(intValue: Int) { self.stringValue = "\(intValue)"; self.intValue = intValue }
}

// MARK: - Agent Definitions

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

// MARK: - MCP Server (local API format from diane serve)

public struct MCPServer: Identifiable, Codable, Hashable, Sendable {
    public let name: String
    public let enabled: Bool
    public let type: String     // "stdio" | "http" | "sse" | "streamable-http"
    public let url: String?
    public let command: String?
    public let args: [String]?
    public let env: [String: String]?
    public let timeout: Int?

    public var id: String { name }

    public func hash(into hasher: inout Hasher) { hasher.combine(name) }
    public static func == (lhs: MCPServer, rhs: MCPServer) -> Bool { lhs.name == rhs.name }
}

public struct MCPTool: Identifiable, Codable, Sendable {
    public let id: String
    public let name: String
    public let description: String?
}

public struct MCPPrompt: Identifiable, Codable, Sendable {
    public let id: String
    public let name: String
    public let description: String?
    public let arguments: [MCPPromptArgument]?

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.id = (try? container.decodeIfPresent(String.self, forKey: .id)) ?? ""
        self.name = try container.decode(String.self, forKey: .name)
        self.description = try container.decodeIfPresent(String.self, forKey: .description)
        self.arguments = try container.decodeIfPresent([MCPPromptArgument].self, forKey: .arguments)
    }

    enum CodingKeys: String, CodingKey {
        case id, name, description, arguments
    }
}

public struct MCPPromptArgument: Identifiable, Codable, Sendable {
    public let name: String
    public let description: String?
    public let required: Bool?

    public var id: String { name }
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

// MARK: - Agent Stats (from local /api/stats)

public struct AgentStatsSummary: Identifiable, Codable, Sendable {
    public let agentName: String
    public let agentId: String?
    public let agentDescription: String?
    public let agentFlowType: String?
    public let totalRuns: Int
    public let successRuns: Int
    public let errorRuns: Int
    public let avgDurationMs: Double
    public let avgStepCount: Double
    public let avgToolCalls: Double
    public let avgInputTokens: Double
    public let avgOutputTokens: Double
    public let totalDurationMs: Int
    public let totalInputTokens: Int
    public let totalOutputTokens: Int
    public let totalCostUsd: Double
    public let avgCostUsd: Double
    public let successRate: Double

    /// The display name: uses the matched agent definition name (set by the
    /// API when a definition is found), falls back to the raw run name.
    public var displayName: String {
        if agentId != nil { return agentName }
        // Remove trailing timestamp-like suffix: -<digits>
        if let range = agentName.range(of: "-\\d+$", options: .regularExpression) {
            return String(agentName[..<range.lowerBound])
        }
        return agentName
    }

    public var id: String { agentName }

    enum CodingKeys: String, CodingKey {
        case agentName        = "agent_name"
        case agentId          = "agent_id"
        case agentDescription = "agent_description"
        case agentFlowType    = "agent_flow_type"
        case totalRuns        = "total_runs"
        case successRuns      = "success_runs"
        case errorRuns        = "error_runs"
        case avgDurationMs    = "avg_duration_ms"
        case avgStepCount     = "avg_step_count"
        case avgToolCalls     = "avg_tool_calls"
        case avgInputTokens   = "avg_input_tokens"
        case avgOutputTokens  = "avg_output_tokens"
        case totalDurationMs  = "total_duration_ms"
        case totalInputTokens = "total_input_tokens"
        case totalOutputTokens = "total_output_tokens"
        case totalCostUsd     = "total_cost_usd"
        case avgCostUsd       = "avg_cost_usd"
        case successRate      = "success_rate"
    }
}

public struct AgentStatsTotals: Codable, Sendable {
    public let totalRuns: Int
    public let totalSuccess: Int
    public let totalErrors: Int
    public let totalDurationMs: Int
    public let totalInputTokens: Int
    public let totalOutputTokens: Int
    public let totalCostUsd: Double
    public let overallAvgDurationMs: Double
    public let overallSuccessRate: Double

    enum CodingKeys: String, CodingKey {
        case totalRuns           = "total_runs"
        case totalSuccess        = "total_success"
        case totalErrors         = "total_errors"
        case totalDurationMs     = "total_duration_ms"
        case totalInputTokens    = "total_input_tokens"
        case totalOutputTokens   = "total_output_tokens"
        case totalCostUsd        = "total_cost_usd"
        case overallAvgDurationMs = "overall_avg_duration_ms"
        case overallSuccessRate  = "overall_success_rate"
    }
}

public struct AgentStatsResponse: Codable, Sendable {
    public let agents: [AgentStatsSummary]
    public let totals: AgentStatsTotals
    public let hours: Int
}

// MARK: - Provider Stats (from GET /api/stats/providers)

public struct ProviderStatsSummary: Identifiable, Codable, Sendable {
    public let providerName: String
    public let modelName: String
    public let totalRuns: Int
    public let successRuns: Int
    public let errorRuns: Int
    public let totalInputTokens: UInt64
    public let totalOutputTokens: UInt64
    public let totalCostUsd: Double

    public var id: String { "\(providerName)|\(modelName)" }

    enum CodingKeys: String, CodingKey {
        case providerName     = "provider_name"
        case modelName        = "model_name"
        case totalRuns        = "total_runs"
        case successRuns      = "success_runs"
        case errorRuns        = "error_runs"
        case totalInputTokens = "total_input_tokens"
        case totalOutputTokens = "total_output_tokens"
        case totalCostUsd     = "total_cost_usd"
    }
}

public struct ProviderStatsResponse: Codable, Sendable {
    public let providers: [ProviderStatsSummary]
    public let totalRuns: Int
    public let totalSuccess: Int
    public let totalErrors: Int
    public let totalInputTokens: UInt64
    public let totalOutputTokens: UInt64
    public let totalCostUsd: Double
    public let hours: Int

    enum CodingKeys: String, CodingKey {
        case providers        = "providers"
        case totalRuns        = "total_runs"
        case totalSuccess     = "total_success"
        case totalErrors      = "total_errors"
        case totalInputTokens = "total_input_tokens"
        case totalOutputTokens = "total_output_tokens"
        case totalCostUsd     = "total_cost_usd"
        case hours            = "hours"
    }
}

// MARK: - Graph Object Stats (from GET /api/stats/objects)

public struct TypeCountInfo: Codable, Sendable, Identifiable {
    public let typeName: String
    public let count: Int

    public var id: String { typeName }

    enum CodingKeys: String, CodingKey {
        case typeName = "type_name"
        case count    = "count"
    }
}

public struct GraphObjectStatsResponse: Codable, Sendable {
    public let total: Int
    public let byType: [TypeCountInfo]

    enum CodingKeys: String, CodingKey {
        case total  = "total"
        case byType = "by_type"
    }
}

// MARK: - Project-Level Providers (from GET /api/providers)

public struct ProjectProviderInfo: Codable, Sendable, Identifiable {
    public let provider: String
    public let baseUrl: String?
    public let generativeModel: String?
    public let embeddingModel: String?

    public var id: String { provider }

    enum CodingKeys: String, CodingKey {
        case provider        = "provider"
        case baseUrl         = "base_url"
        case generativeModel = "generative_model"
        case embeddingModel  = "embedding_model"
    }
}

// MARK: - Session Aggregates (from GET /api/sessions/{id})

public struct SessionRunAggregates: Codable, Sendable {
    public let totalRuns: Int
    public let totalInputTokens: Int64
    public let totalOutputTokens: Int64
    public let estimatedCostUsd: Double
    public let agentNames: [String]?

    enum CodingKeys: String, CodingKey {
        case totalRuns        = "total_runs"
        case totalInputTokens = "total_input_tokens"
        case totalOutputTokens = "total_output_tokens"
        case estimatedCostUsd = "estimated_cost_usd"
        case agentNames       = "agent_names"
    }
}

/// Response from GET /api/sessions/{id} — session metadata + aggregated run stats.
public struct SessionDetailResponse: Codable, Sendable {
    public let id: String
    public let key: String?
    public let title: String?
    public let status: String?
    public let messageCount: Int
    public let totalTokens: Int
    public let createdAt: String?
    public let updatedAt: String?
    public let aggregates: SessionRunAggregates?

    enum CodingKeys: String, CodingKey {
        case id, key, title, status
        case messageCount = "message_count"
        case totalTokens  = "total_tokens"
        case createdAt    = "created_at"
        case updatedAt    = "updated_at"
        case aggregates
    }
}

// MARK: - Chat Send Response

/// Response from POST /api/chat/send — session metadata + run messages.
struct ChatSendResponse: Codable, Sendable {
    let sessionID: String
    let runID: String
    let messages: [DianeMessage]
    let success: Bool
    let error: String?

    enum CodingKeys: String, CodingKey {
        case sessionID = "session_id"
        case runID     = "run_id"
        case messages, success, error
    }
}

// MARK: - GraphObject JSON Helpers
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

// MARK: - GraphObject JSON Helpers

/// Internal model mirroring the GraphObject JSON format returned by
/// the Memory Platform's graph session API.
private struct GraphObjectJSON: Decodable {
    let entityID: String
    let key: String?
    let createdAt: String?
    let properties: [String: AnyValue]?
    
    enum CodingKeys: String, CodingKey {
        case entityID = "entity_id"
        case key
        case createdAt = "created_at"
        case properties
    }
}

/// Wrapper for the ListSessions / ListMessages response format.
private struct ListResponse<T: Decodable>: Decodable {
    let items: [T]
}

/// Type-erased value for decoding mixed-type JSON properties.
private enum AnyValue: Decodable {
    case string(String)
    case int(Int)
    case double(Double)
    case bool(Bool)
    case null
    
    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let str = try? container.decode(String.self) {
            self = .string(str)
        } else if let i = try? container.decode(Int.self) {
            self = .int(i)
        } else if let d = try? container.decode(Double.self) {
            self = .double(d)
        } else if let b = try? container.decode(Bool.self) {
            self = .bool(b)
        } else if container.decodeNil() {
            self = .null
        } else {
            self = .null
        }
    }
    
    var stringValue: String? {
        switch self { case .string(let s): return s; default: return nil }
    }
    
    var intValue: Int? {
        switch self { case .int(let i): return i; case .double(let d): return Int(d); default: return nil }
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
    let updatedAt: String?
    
    // Server-side properties we don't store as fields but decode gracefully
    private let properties: [String: AnyValue]?
    
    func hash(into hasher: inout Hasher) { hasher.combine(id) }
    static func == (lhs: DianeSession, rhs: DianeSession) -> Bool { lhs.id == rhs.id }
    
    enum CodingKeys: String, CodingKey {
        case id, key, title, status
        case messageCount = "message_count"
        case totalTokens = "total_tokens"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case properties
        case entityID = "entity_id"
    }
    
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        // Support both flat (id) and graph (entity_id) formats
        if let flatID = try? container.decode(String.self, forKey: .id) {
            self.id = flatID
        } else {
            self.id = try container.decode(String.self, forKey: .entityID)
        }
        self.key = try container.decodeIfPresent(String.self, forKey: .key)
        self.createdAt = try container.decodeIfPresent(String.self, forKey: .createdAt)
        self.properties = try container.decodeIfPresent([String: AnyValue].self, forKey: .properties)
        
        // Flat JSON format
        if let t = try? container.decodeIfPresent(String.self, forKey: .title) {
            self.title = t
        } else {
            self.title = self.properties?["title"]?.stringValue
        }
        if let s = try? container.decodeIfPresent(String.self, forKey: .status) {
            self.status = s
        } else {
            self.status = self.properties?["status"]?.stringValue
        }
        if let mc = try? container.decodeIfPresent(Int.self, forKey: .messageCount) {
            self.messageCount = mc
        } else {
            self.messageCount = self.properties?["message_count"]?.intValue
        }
        if let tt = try? container.decodeIfPresent(Int.self, forKey: .totalTokens) {
            self.totalTokens = tt
        } else {
            self.totalTokens = self.properties?["total_tokens"]?.intValue
        }
        if let ua = try? container.decodeIfPresent(String.self, forKey: .updatedAt) {
            self.updatedAt = ua
        } else {
            self.updatedAt = self.properties?["updated_at"]?.stringValue ?? self.properties?["last_active_at"]?.stringValue
        }
    }
    
    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encodeIfPresent(key, forKey: .key)
        try container.encodeIfPresent(title, forKey: .title)
        try container.encodeIfPresent(status, forKey: .status)
        try container.encodeIfPresent(messageCount, forKey: .messageCount)
        try container.encodeIfPresent(totalTokens, forKey: .totalTokens)
        try container.encodeIfPresent(createdAt, forKey: .createdAt)
        try container.encodeIfPresent(updatedAt, forKey: .updatedAt)
    }
}

// MARK: - Tool Call

public struct ToolCall: Identifiable, Codable, Sendable {
    public let id: String
    public let name: String
    public let arguments: String?
    
    public enum CodingKeys: String, CodingKey {
        case id, name, arguments
    }
    
    public init(id: String, name: String, arguments: String? = nil) {
        self.id = id
        self.name = name
        self.arguments = arguments
    }
}

// MARK: - Diane Message

public struct DianeMessage: Identifiable, Codable, Sendable {
    public let id: String
    public let role: String
    public let content: String
    public let sequenceNumber: Int?
    public let tokenCount: Int?
    public let toolCalls: [ToolCall]?
    public let reasoningContent: String?
    public let createdAt: String?

    /// Memberwise initializer for creating messages locally (optimistic UI, error bubbles).
    public init(
        id: String,
        role: String,
        content: String,
        sequenceNumber: Int? = nil,
        tokenCount: Int? = nil,
        toolCalls: [ToolCall]? = nil,
        reasoningContent: String? = nil,
        createdAt: String? = nil
    ) {
        self.id = id
        self.role = role
        self.content = content
        self.sequenceNumber = sequenceNumber
        self.tokenCount = tokenCount
        self.toolCalls = toolCalls
        self.reasoningContent = reasoningContent
        self.createdAt = createdAt
    }

    enum CodingKeys: String, CodingKey {
        case id, role, content
        case sequenceNumber = "sequence_number"
        case tokenCount = "token_count"
        case toolCalls = "tool_calls"
        case reasoningContent = "reasoning_content"
        case createdAt = "created_at"
        case entityID = "entity_id"
        case properties
    }
    
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        // Support both flat (id) and graph (entity_id) formats
        if let flatID = try? container.decode(String.self, forKey: .id) {
            self.id = flatID
        } else {
            self.id = try container.decode(String.self, forKey: .entityID)
        }
        
        let props = try container.decodeIfPresent([String: AnyValue].self, forKey: .properties)
        
        // Decode from flat JSON first, fall back to graph properties
        self.role = (try? container.decodeIfPresent(String.self, forKey: .role))
            ?? props?["role"]?.stringValue ?? ""
        self.content = (try? container.decodeIfPresent(String.self, forKey: .content))
            ?? props?["content"]?.stringValue ?? ""
        self.sequenceNumber = (try? container.decodeIfPresent(Int.self, forKey: .sequenceNumber))
            ?? props?["sequence_number"]?.intValue
        self.tokenCount = (try? container.decodeIfPresent(Int.self, forKey: .tokenCount))
            ?? props?["token_count"]?.intValue
        self.createdAt = (try? container.decodeIfPresent(String.self, forKey: .createdAt))
        
        // Reasoning content: check flat JSON first, then graph properties
        self.reasoningContent = (try? container.decodeIfPresent(String.self, forKey: .reasoningContent))
            ?? props?["reasoningContent"]?.stringValue
            ?? props?["reasoning_content"]?.stringValue
        
        // Tool calls: check flat JSON first, then graph properties
        if let flatTCs = try? container.decodeIfPresent([ToolCall].self, forKey: .toolCalls) {
            self.toolCalls = flatTCs.isEmpty ? nil : flatTCs
        } else if let rawTCs = props?["toolCalls"] {
            self.toolCalls = Self.decodeToolCalls(from: rawTCs)
        } else {
            self.toolCalls = nil
        }
    }
    
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(role, forKey: .role)
        try container.encode(content, forKey: .content)
        try container.encodeIfPresent(sequenceNumber, forKey: .sequenceNumber)
        try container.encodeIfPresent(tokenCount, forKey: .tokenCount)
        try container.encodeIfPresent(toolCalls, forKey: .toolCalls)
        try container.encodeIfPresent(reasoningContent, forKey: .reasoningContent)
        try container.encodeIfPresent(createdAt, forKey: .createdAt)
    }
    
    /// Decode tool calls from the graph properties `toolCalls` field, which is stored as an array of dictionaries.
    private static func decodeToolCalls(from raw: AnyValue?) -> [ToolCall]? {
        guard raw != nil else { return nil }
        // The toolCalls property is stored as a JSON array in graph properties
        // It might come through as a JSON string or as nested values
        return nil // Handled below via the Array-typed properties
    }
}

// MARK: - Tool Call Parsing from Graph Properties

extension DianeMessage {
    /// Attempt to extract tool calls from raw graph properties.
    /// The properties map stores toolCalls as an untyped `Any` from the JSON decoder.
    static func toolCalls(fromRaw value: Any?) -> [ToolCall]? {
        guard let value = value else { return nil }
        if let arr = value as? [[String: Any]] {
            return arr.compactMap { dict in
                guard let id = dict["id"] as? String ?? (dict["id"] as? String),
                      let name = dict["name"] as? String else { return nil }
                let args: String?
                if let s = dict["arguments"] as? String { args = s }
                else if let d = dict["arguments"] {
                    args = (try? JSONSerialization.data(withJSONObject: d, options: .fragmentsAllowed))
                        .flatMap { String(data: $0, encoding: .utf8) }
                } else { args = nil }
                return ToolCall(id: id, name: name, arguments: args)
            }.nilIfEmpty
        }
        if let arr = value as? [Any] {
            return arr.compactMap { item in
                guard let dict = item as? [String: Any],
                      let id = dict["id"] as? String,
                      let name = dict["name"] as? String else { return nil }
                let args: String?
                if let s = dict["arguments"] as? String { args = s }
                else if let d = dict["arguments"] {
                    args = (try? JSONSerialization.data(withJSONObject: d, options: .fragmentsAllowed))
                        .flatMap { String(data: $0, encoding: .utf8) }
                } else { args = nil }
                return ToolCall(id: id, name: name, arguments: args)
            }.nilIfEmpty
        }
        return nil
    }
}

// MARK: - Array Extension

private extension Array {
    var nilIfEmpty: Self? { isEmpty ? nil : self }
}

// MARK: - Agent Definition

struct AgentDef: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let name: String
    let description: String?
    let flowType: String
    let visibility: String
    let isDefault: Bool
    let toolCount: Int
    let createdAt: String?
    let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, name, description
        case flowType = "flow_type"
        case visibility
        case isDefault = "is_default"
        case toolCount = "tool_count"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
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

// MARK: - Graph Schema (from GET /api/schema)

public struct SchemaProperty: Codable, Sendable, Identifiable {
    public let name: String
    public let type: String
    public let description: String
    public let enumValues: [String]?
    
    public var id: String { name }
    
    enum CodingKeys: String, CodingKey {
        case name, type, description
        case enumValues = "enum_values"
    }
}

public struct SchemaNodeType: Codable, Sendable, Identifiable {
    public let typeName: String
    public let label: String
    public let description: String
    public let namespace: String?
    public let properties: [SchemaProperty]
    public let objectCount: Int
    public let relationshipCount: Int
    
    public var id: String { typeName }
    
    enum CodingKeys: String, CodingKey {
        case typeName = "type_name"
        case label, description, namespace, properties
        case objectCount = "object_count"
        case relationshipCount = "relationship_count"
    }
}

public struct SchemaRelationship: Codable, Sendable, Identifiable {
    public let name: String
    public let label: String
    public let inverseLabel: String
    public let description: String
    public let sourceType: String
    public let targetType: String
    
    public var id: String { name }
    
    enum CodingKeys: String, CodingKey {
        case name, label, description
        case inverseLabel = "inverse_label"
        case sourceType = "source_type"
        case targetType = "target_type"
    }
}

public struct SchemaResponse: Codable, Sendable {
    public let nodeTypes: [SchemaNodeType]
    public let relationships: [SchemaRelationship]
    
    enum CodingKeys: String, CodingKey {
        case nodeTypes = "node_types"
        case relationships
    }
}

/// Lightweight summary of a graph object, returned by GET /api/schema/objects/{typeName}.
public struct SchemaObjectSummary: Codable, Sendable, Identifiable {
    public let entityID: String
    public let key: String?
    public let type: String
    public let createdAt: String
    public let relationshipCount: Int
    public let title: String?
    public let status: String?
    
    public var id: String { entityID }
    
    enum CodingKeys: String, CodingKey {
        case entityID = "entity_id"
        case key, type
        case createdAt = "created_at"
        case relationshipCount = "relationship_count"
        case title, status
    }
}

/// Response from GET /api/schema/objects/{typeName}.
public struct SchemaObjectsResponse: Codable, Sendable {
    public let typeName: String
    public let total: Int
    public let objects: [SchemaObjectSummary]
    
    enum CodingKeys: String, CodingKey {
        case typeName = "type_name"
        case total, objects
    }
}

// MARK: - Doctor Check

/// Response from GET /api/doctor
public struct DoctorResponse: Codable, Sendable {
    public let ok: Bool
    public let version: String?
    public let results: [DoctorCheckItem]
}

/// A single diagnostic check from /api/doctor
public struct DoctorCheckItem: Codable, Sendable, Identifiable {
    public let check: String
    public let status: String   // "ok", "warning", "error"
    public let message: String
    public let details: [String: String]?

    public var id: String { check }
    
    /// Display icon name based on status
    public var iconName: String {
        switch status {
        case "ok":      return "checkmark.circle.fill"
        case "warning": return "exclamationmark.triangle.fill"
        case "error":   return "xmark.circle.fill"
        default:        return "questionmark.circle"
        }
    }
    
    /// Human-readable label for the check name
    public var displayName: String {
        switch check {
        case "config_file":     return "Config File"
        case "api_token":       return "API Token"
        case "sdk_connection":  return "SDK Connection"
        case "project_info":    return "Project Info"
        case "agent_definitions": return "Agent Definitions"
        case "session_crud":    return "Session CRUD"
        case "memory_search":   return "Memory Search"
        case "server_version":  return "Server Version"
        default:                return check.replacingOccurrences(of: "_", with: " ").capitalized
        }
    }
}
