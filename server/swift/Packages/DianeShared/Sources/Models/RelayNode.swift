import Foundation

public struct RelayNode: Codable, Sendable, Identifiable, Hashable {
    public let id: String
    public let name: String
    public let url: String?
    public let status: String?
    public let nodeType: String?
    public let region: String?
    public let lastHeartbeat: String?
    public let connectedAgents: Int?
    public let load: Double?

    enum CodingKeys: String, CodingKey {
        case id, name, url, status
        case nodeType = "node_type"
        case region
        case lastHeartbeat = "last_heartbeat"
        case connectedAgents = "connected_agents"
        case load
    }

    public init(id: String, name: String, url: String? = nil, status: String? = nil,
                nodeType: String? = nil, region: String? = nil, lastHeartbeat: String? = nil,
                connectedAgents: Int? = nil, load: Double? = nil) {
        self.id = id; self.name = name; self.url = url; self.status = status
        self.nodeType = nodeType; self.region = region; self.lastHeartbeat = lastHeartbeat
        self.connectedAgents = connectedAgents; self.load = load
    }

    public func hash(into hasher: inout Hasher) { hasher.combine(id) }
    public static func == (lhs: RelayNode, rhs: RelayNode) -> Bool { lhs.id == rhs.id }
}
