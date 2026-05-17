import Foundation
import Combine

/// Persists archived session IDs in UserDefaults.
/// Pure local state — does not sync with the server.
@MainActor
final class ArchivedSessionsStore: ObservableObject {
    static let shared = ArchivedSessionsStore()

    @Published var archivedIDs: Set<String> = []

    @Published var showArchived: Bool = false

    private let defaultsKey = "archivedSessionIDs"
    private let showArchivedKey = "showArchivedSessions"
    private let store: UserDefaults

    private init(store: UserDefaults = .standard) {
        self.store = store
        load()
    }

    // MARK: - Public API

    func toggleArchive(sessionID: String) {
        if archivedIDs.contains(sessionID) {
            archivedIDs.remove(sessionID)
        } else {
            archivedIDs.insert(sessionID)
        }
        save()
    }

    func isArchived(_ sessionID: String) -> Bool {
        archivedIDs.contains(sessionID)
    }

    func toggleShowArchived() {
        showArchived.toggle()
        store.set(showArchived, forKey: showArchivedKey)
    }

    // MARK: - Persistence

    private func load() {
        if let data = store.data(forKey: defaultsKey),
           let ids = try? JSONDecoder().decode(Set<String>.self, from: data) {
            archivedIDs = ids
        }
        showArchived = store.bool(forKey: showArchivedKey)
    }

    private func save() {
        if let data = try? JSONEncoder().encode(archivedIDs) {
            store.set(data, forKey: defaultsKey)
        }
    }
}
