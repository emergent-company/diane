import Foundation

public final class ServerConfiguration: @unchecked Sendable {
    public var serverURL: String {
        didSet { store.set(serverURL, forKey: Keys.serverURL) }
    }
    public var apiKey: String {
        didSet { store.set(apiKey, forKey: Keys.apiKey) }
    }
    public var projectID: String {
        didSet { store.set(projectID, forKey: Keys.projectID) }
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
        self.serverURL = userDefaults.string(forKey: Keys.serverURL) ?? ""
        self.apiKey = userDefaults.string(forKey: Keys.apiKey) ?? ""
        self.projectID = userDefaults.string(forKey: Keys.projectID) ?? ""
    }
}
