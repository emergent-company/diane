import UIKit

/// Manages application icon badge number based on unread message count.
public final class BadgeManager: @unchecked Sendable {
    public static let shared = BadgeManager()

    private init() {}

    /// Update the app icon badge to reflect total unread count.
    public func updateBadge(count: Int) {
        DispatchQueue.main.async {
            UIApplication.shared.applicationIconBadgeNumber = count
        }
    }

    /// Clear the badge (e.g., when user opens the app).
    public func clearBadge() {
        DispatchQueue.main.async {
            UIApplication.shared.applicationIconBadgeNumber = 0
        }
    }
}
