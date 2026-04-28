import Foundation

/// App-wide constants. Configuration and UserDefaults keys live in ServerConfiguration.
enum AppConstants {
    enum CLIPaths {
        static let candidates = [
            "/usr/local/bin/diane",
            "/opt/homebrew/bin/diane",
            "/usr/bin/diane",
            "\(NSHomeDirectory())/.local/bin/diane",
        ]
        static let localBinDir   = "\(NSHomeDirectory())/.local/bin"
        static let installTarget = "\(NSHomeDirectory())/.local/bin/diane"
    }
    
    // Paths for bundled resources
    static let companionAppVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0"
    
    // Permissions
    static let permissionCheckInterval: TimeInterval = 30
}
