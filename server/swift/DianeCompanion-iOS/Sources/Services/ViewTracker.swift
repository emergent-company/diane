import SwiftUI
import Sentry

/// Tracks view appearances and user actions in Sentry breadcrumbs for dev debugging.
///
/// Call `ViewTracker.track("MyView")` from `.onAppear` or use the `.sentryView("MyView")`
/// view modifier for automatic tracking. Every tracked view becomes a breadcrumb in Sentry,
/// giving you a full navigation timeline before any error or crash.
enum ViewTracker {

    /// Log a view appearance as a Sentry breadcrumb.
    /// - Parameters:
    ///   - name: A descriptive name for the view (e.g. "SessionsList").
    ///   - data: Optional contextual data (e.g. ["session_id": "abc123"]).
    static func track(_ name: String, data: [String: Any]? = nil) {
        let crumb = Breadcrumb()
        crumb.category = "navigation"
        crumb.type = "navigation"
        crumb.message = name
        crumb.data = data ?? [:]
        SentrySDK.addBreadcrumb(crumb)
    }

    /// Log a user action (button tap, toggle, selection) as a Sentry breadcrumb.
    /// - Parameters:
    ///   - action: e.g. "tapped_refresh", "toggled_auto_update".
    ///   - data: Optional contextual data.
    static func action(_ action: String, data: [String: Any]? = nil) {
        let crumb = Breadcrumb()
        crumb.category = "user.action"
        crumb.type = "user"
        crumb.message = action
        crumb.data = data ?? [:]
        SentrySDK.addBreadcrumb(crumb)
    }
}

// MARK: - View Modifier

/// SwiftUI modifier that automatically tracks view appearances in Sentry.
struct SentryViewModifier: ViewModifier {
    let name: String
    let data: [String: Any]?

    func body(content: Content) -> some View {
        content.onAppear {
            ViewTracker.track(name, data: data)
        }
    }
}

extension View {
    /// Attach this to any view to log its appearance as a Sentry breadcrumb.
    /// Usage: `MyView().sentryView("MyView")`
    func sentryView(_ name: String, data: [String: Any]? = nil) -> some View {
        modifier(SentryViewModifier(name: name, data: data))
    }
}
