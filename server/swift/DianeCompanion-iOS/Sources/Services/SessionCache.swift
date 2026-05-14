import Foundation
import DianeShared

/// Local cache for sessions and messages to support offline viewing.
/// Persists to UserDefaults as JSON blobs.
public final class SessionCache: @unchecked Sendable {
    public static let shared = SessionCache()

    private let defaults = UserDefaults(suiteName: "group.com.emergent-company.diane-companion")
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    private enum Keys {
        static let sessions = "cached-sessions"
        static let messagesPrefix = "cached-messages-"
        static let lastReadPrefix = "last-read-"
    }

    private init() {}

    // MARK: - Sessions

    public func cacheSessions(_ sessions: [DianeSession]) {
        guard let data = try? encoder.encode(sessions) else { return }
        defaults?.set(data, forKey: Keys.sessions)
    }

    public func loadCachedSessions() -> [DianeSession] {
        guard let data = defaults?.data(forKey: Keys.sessions),
              let sessions = try? decoder.decode([DianeSession].self, from: data) else {
            return []
        }
        return sessions
    }

    // MARK: - Messages

    public func cacheMessages(_ messages: [DianeMessage], for sessionID: String) {
        guard let data = try? encoder.encode(messages) else { return }
        defaults?.set(data, forKey: Keys.messagesPrefix + sessionID)
    }

    public func loadCachedMessages(for sessionID: String) -> [DianeMessage] {
        guard let data = defaults?.data(forKey: Keys.messagesPrefix + sessionID),
              let messages = try? decoder.decode([DianeMessage].self, from: data) else {
            return []
        }
        return messages
    }

    // MARK: - Badge / Last Read Tracking

    public func markRead(sessionID: String) {
        let now = ISO8601DateFormatter().string(from: Date())
        defaults?.set(now, forKey: Keys.lastReadPrefix + sessionID)
    }

    public func lastReadDate(sessionID: String) -> Date? {
        guard let dateStr = defaults?.string(forKey: Keys.lastReadPrefix + sessionID) else {
            return nil
        }
        return ISO8601DateFormatter().date(from: dateStr)
    }

    public func unreadCount(sessionID: String, messages: [DianeMessage]) -> Int {
        guard let lastRead = lastReadDate(sessionID: sessionID) else {
            // Never read — count non-user messages
            return messages.filter { $0.role != "user" && $0.role != "error" }.count
        }
        return messages.filter { msg in
            guard msg.role != "user" && msg.role != "error",
                  let created = msg.createdAt,
                  let createdDate = ISO8601DateFormatter().date(from: created) else {
                return false
            }
            return createdDate > lastRead
        }.count
    }

    /// Total unread count across all sessions.
    public func totalUnreadCount(sessions: [DianeSession]) -> Int {
        var total = 0
        for session in sessions {
            let msgs = loadCachedMessages(for: session.id)
            total += unreadCount(sessionID: session.id, messages: msgs)
        }
        return total
    }
}
