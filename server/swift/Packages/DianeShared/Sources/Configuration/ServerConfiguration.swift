import Foundation

public final class ServerConfiguration: @unchecked Sendable {
    public var serverURL: String {
        didSet { persist(value: serverURL, defaultsKey: Keys.serverURL, kcKey: .serverURL) }
    }

    public var apiKey: String {
        didSet { persist(value: apiKey, defaultsKey: Keys.apiKey, kcKey: .apiKey) }
    }

    public var projectID: String {
        didSet { persist(value: projectID, defaultsKey: Keys.projectID, kcKey: .projectID) }
    }

    public var isConfigured: Bool { !serverURL.isEmpty && !apiKey.isEmpty }

    public var baseURL: URL? {
        guard !serverURL.isEmpty else { return nil }
        return URL(string: serverURL)
    }

    enum Keys {
        static let serverURL = "serverURL"
        static let apiKey = "apiKey"
        static let projectID = "projectID"
    }

    private let store: UserDefaults

    public init(userDefaults: UserDefaults = .standard) {
        self.store = userDefaults

        // Migrate legacy UserDefaults → Keychain on first access
        KeychainService.migrateFromUserDefaults(userDefaults: userDefaults)

        // Prefer Keychain, fall back to UserDefaults for backward compat
        self.serverURL = KeychainService.get(.serverURL) ?? userDefaults.string(forKey: Keys.serverURL) ?? ""
        self.apiKey = KeychainService.get(.apiKey) ?? userDefaults.string(forKey: Keys.apiKey) ?? ""
        self.projectID = KeychainService.get(.projectID) ?? userDefaults.string(forKey: Keys.projectID) ?? ""
    }

    // MARK: - Persistence

    private func persist(value: String, defaultsKey: String, kcKey: KeychainService.Key) {
        KeychainService.set(value, for: kcKey)
    }
}
