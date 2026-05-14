import Foundation
import UIKit
import UserNotifications

/// Manages Apple Push Notification service registration and token management.
public final class PushNotificationService: NSObject, @unchecked Sendable {
    public static let shared = PushNotificationService()

    public private(set) var token: String?
    public private(set) var isRegistered = false

    private override init() {
        super.init()
    }

    /// Request notification authorization and register for remote notifications.
    public func register() {
        let center = UNUserNotificationCenter.current()
        center.requestAuthorization(options: [.alert, .badge, .sound]) { granted, _ in
            DispatchQueue.main.async {
                if granted {
                    UIApplication.shared.registerForRemoteNotifications()
                    self.isRegistered = true
                }
            }
        }
    }

    /// Called by AppDelegate when a push token is received.
    public func didRegisterForRemoteNotifications(deviceToken: Data) {
        let tokenString = deviceToken.map { String(format: "%02.2hhx", $0) }.joined()
        token = tokenString
    }

    /// Called when push registration fails.
    public func didFailToRegisterForRemoteNotifications(error: Error) {
        print("Push registration failed: \(error.localizedDescription)")
    }
}
