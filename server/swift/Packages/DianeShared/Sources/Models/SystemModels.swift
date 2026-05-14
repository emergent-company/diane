import Foundation

// MARK: - Project

public struct Project: Codable, Sendable, Identifiable, Hashable {
    public let id: String
    public let name: String
    public let description: String?
    public let status: String?
    public let createdAt: String?
    public let updatedAt: String?
    public let agentCount: Int?
    public let sessionCount: Int?

    enum CodingKeys: String, CodingKey {
        case id, name, description, status
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case agentCount = "agent_count"
        case sessionCount = "session_count"
    }

    public init(id: String, name: String, description: String? = nil, status: String? = nil,
                createdAt: String? = nil, updatedAt: String? = nil,
                agentCount: Int? = nil, sessionCount: Int? = nil) {
        self.id = id; self.name = name; self.description = description; self.status = status
        self.createdAt = createdAt; self.updatedAt = updatedAt
        self.agentCount = agentCount; self.sessionCount = sessionCount
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: Project, rhs: Project) -> Bool { lhs.id == rhs.id }
}

// MARK: - Project Stats

public struct ProjectStats: Codable, Sendable {
    public let totalSessions: Int?
    public let totalMessages: Int?
    public let activeAgents: Int?
    public let totalProjects: Int?
    public let sessionsToday: Int?
    public let messagesToday: Int?

    enum CodingKeys: String, CodingKey {
        case totalSessions = "total_sessions"
        case totalMessages = "total_messages"
        case activeAgents = "active_agents"
        case totalProjects = "total_projects"
        case sessionsToday = "sessions_today"
        case messagesToday = "messages_today"
    }

    public init(totalSessions: Int? = nil, totalMessages: Int? = nil, activeAgents: Int? = nil,
                totalProjects: Int? = nil, sessionsToday: Int? = nil, messagesToday: Int? = nil) {
        self.totalSessions = totalSessions; self.totalMessages = totalMessages
        self.activeAgents = activeAgents; self.totalProjects = totalProjects
        self.sessionsToday = sessionsToday; self.messagesToday = messagesToday
    }
}

// MARK: - Document

public struct Document: Codable, Sendable, Identifiable, Hashable {
    public let id: String
    public let title: String?
    public let content: String?
    public let fileType: String?
    public let size: Int?
    public let projectID: String?
    public let createdAt: String?
    public let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, title, content
        case fileType = "file_type"
        case size
        case projectID = "project_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    public init(id: String, title: String? = nil, content: String? = nil, fileType: String? = nil,
                size: Int? = nil, projectID: String? = nil, createdAt: String? = nil, updatedAt: String? = nil) {
        self.id = id; self.title = title; self.content = content; self.fileType = fileType
        self.size = size; self.projectID = projectID; self.createdAt = createdAt; self.updatedAt = updatedAt
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: Document, rhs: Document) -> Bool { lhs.id == rhs.id }
}

// MARK: - Worker

public struct Worker: Codable, Sendable, Identifiable, Hashable {
    public let id: String
    public let name: String?
    public let status: String?
    public let workerType: String?
    public let lastHeartbeat: String?
    public let taskCount: Int?

    enum CodingKeys: String, CodingKey {
        case id, name, status
        case workerType = "worker_type"
        case lastHeartbeat = "last_heartbeat"
        case taskCount = "task_count"
    }

    public init(id: String, name: String? = nil, status: String? = nil, workerType: String? = nil,
                lastHeartbeat: String? = nil, taskCount: Int? = nil) {
        self.id = id; self.name = name; self.status = status; self.workerType = workerType
        self.lastHeartbeat = lastHeartbeat; self.taskCount = taskCount
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: Worker, rhs: Worker) -> Bool { lhs.id == rhs.id }
}

// MARK: - API Key

public struct APIKey: Codable, Sendable, Identifiable {
    public let id: String
    public let name: String?
    public let keyPrefix: String?
    public let createdAt: String?
    public let lastUsedAt: String?
    public let isActive: Bool?

    enum CodingKeys: String, CodingKey {
        case id, name
        case keyPrefix = "key_prefix"
        case createdAt = "created_at"
        case lastUsedAt = "last_used_at"
        case isActive = "is_active"
    }
}

// MARK: - Paginated Response

public struct PaginatedResponse<T: Codable & Sendable>: Codable, Sendable {
    public let data: [T]
    public let total: Int?
    public let page: Int?
    public let pageSize: Int?

    enum CodingKeys: String, CodingKey {
        case data, total, page
        case pageSize = "page_size"
    }
}

// MARK: - Health / Status

public struct HealthStatus: Codable, Sendable {
    public let status: String
    public let version: String?
    public let uptime: Double?
    public let activeConnections: Int?

    enum CodingKeys: String, CodingKey {
        case status, version, uptime
        case activeConnections = "active_connections"
    }
}
