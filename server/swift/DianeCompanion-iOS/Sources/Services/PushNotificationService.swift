import Foundation
import UIKit
import UserNotifications

/// Manages Apple Push Notification service registration and token management.
public final class PushNotificationService: NSObject, @unchecked Sendable {
    public static let shared = PushNotificationService()

    private enum UserDefaultsKey {
        static let pushEnabled = "com.emergent.diane.pushEnabled"
    }

    public private(set) var token: String?

    /// Whether the user has opted in to push notifications (persisted).
    public private(set) var isRegistered: Bool {
        get { _isRegistered }
        set { _isRegistered = newValue }
    }

    private var _isRegistered: Bool

    private override init() {
        _isRegistered = UserDefaults.standard.bool(forKey: UserDefaultsKey.pushEnabled)
        super.init()
    }

    /// Request notification authorization and register for remote notifications.
    public func register() {
        let center = UNUserNotificationCenter.current()
        center.requestAuthorization(options: [.alert, .badge, .sound]) { granted, _ in
            DispatchQueue.main.async {
                if granted {
                    UIApplication.shared.registerForRemoteNotifications()
                    self._isRegistered = true
                    UserDefaults.standard.set(true, forKey: UserDefaultsKey.pushEnabled)
                }
            }
        }
    }

    /// Unregister from remote notifications and persist the preference.
    public func unregister() {
        UIApplication.shared.unregisterForRemoteNotifications()
        _isRegistered = false
        token = nil
        UserDefaults.standard.set(false, forKey: UserDefaultsKey.pushEnabled)
    }

    /// Called by AppDelegate when a push token is received.
    public func didRegisterForRemoteNotifications(deviceToken: Data) {
        let tokenString = deviceToken.map { String(format: "%02.2hhx", $0) }.joined()
        token = tokenString
        _isRegistered = true
        UserDefaults.standard.set(true, forKey: UserDefaultsKey.pushEnabled)
    }

    /// Called when push registration fails.
    public func didFailToRegisterForRemoteNotifications(error: Error) {
        print("Push registration failed: \(error.localizedDescription)")
    }
}
