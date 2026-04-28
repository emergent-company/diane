import Foundation
import SwiftUI

/// Persistent app configuration backed by UserDefaults.
/// Injected into the view hierarchy via `.environmentObject()`.
@MainActor
final class ServerConfiguration: ObservableObject {
    @Published var serverURL: String {
        didSet { UserDefaults.standard.set(serverURL, forKey: Keys.serverURL) }
    }

    @Published var apiKey: String {
        didSet { UserDefaults.standard.set(apiKey, forKey: Keys.apiKey) }
    }

    @Published var launchAtLogin: Bool {
        didSet { UserDefaults.standard.set(launchAtLogin, forKey: Keys.launchAtLogin) }
    }

    @Published var pollInterval: TimeInterval {
        didSet { UserDefaults.standard.set(pollInterval, forKey: Keys.pollInterval) }
    }

    var isConfigured: Bool { !serverURL.isEmpty }

    var baseURL: URL? {
        guard !serverURL.isEmpty else { return nil }
        return URL(string: serverURL)
    }

    enum Keys {
        static let serverURL     = "serverURL"
        static let apiKey        = "apiKey"
        static let launchAtLogin = "launchAtLogin"
        static let pollInterval  = "pollInterval"
    }

    init() {
        self.serverURL     = UserDefaults.standard.string(forKey: Keys.serverURL) ?? ""
        self.apiKey        = UserDefaults.standard.string(forKey: Keys.apiKey) ?? ""
        self.launchAtLogin = UserDefaults.standard.bool(forKey: Keys.launchAtLogin)
        let stored         = UserDefaults.standard.double(forKey: Keys.pollInterval)
        self.pollInterval  = stored > 0 ? stored : 10
    }
}
