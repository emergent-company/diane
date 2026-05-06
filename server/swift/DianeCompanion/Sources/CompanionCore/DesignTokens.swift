import SwiftUI

// MARK: - Diane Design Tokens

/// Central design constants for the Diane companion app.
///
/// Use these everywhere instead of hardcoded values so a single change
/// propagates across the entire UI. Three-tier corner radius system:
///   • `.small`  (4)  — badges, tags, status pills
///   • `.medium` (8)  — summary headers, grouped sections, inner panels
///   • `.large`  (12) — cards, full panels, dialogs
///
/// Usage:
///   .cornerRadius(Design.CornerRadius.small)
///   VStack(spacing: Design.Spacing.sm) { ... }
///   .padding(Design.Padding.card)
///   .background(Design.Surface.cardBackground)
///   .cardStyle()  // shortcut for card + border
///   .badgeStyle(color: .green)
enum Design {
    // MARK: - Corner Radii

    enum CornerRadius {
        /// Badges, tags, status pills (replaces 3, 4)
        static let small: CGFloat = 4
        /// Summary headers, grouped sections, inner panels (replaces 5, 6, 8)
        static let medium: CGFloat = 8
        /// Cards, panels, dialogs (replaces 10, 12)
        static let large: CGFloat = 12
    }

    // MARK: - Spacing

    enum Spacing {
        /// Icon–label gaps, inline micro spacing (replaces 2)
        static let xxs: CGFloat = 2
        /// Tight HStack gaps, badge element gaps (replaces 3, 4)
        static let xs: CGFloat = 4
        /// Vertical item spacing, label rows (replaces 6, 8)
        static let sm: CGFloat = 8
        /// Card internal VStack spacing (replaces 10, 12)
        static let md: CGFloat = 12
        /// Section spacing in scroll views (replaces 16, 20)
        static let lg: CGFloat = 16
        /// Large section gaps, group box spacing (replaces 24)
        static let xl: CGFloat = 24
    }

    // MARK: - Padding

    enum Padding {
        /// Card content padding (replaces 14)
        static let card: CGFloat = 14
        /// Section headers, summary bars (replaces 12)
        static let sectionHeader: CGFloat = 12
        /// Badge horizontal padding
        static let badgeH: CGFloat = 6
        /// Badge vertical padding
        static let badgeV: CGFloat = 2
        /// Inline banner / error bar padding
        static let banner: CGFloat = 10
    }

    // MARK: - Surface (backgrounds & borders)

    enum Surface {
        /// Default card background: light tint
        static let cardBackground: Color = .primary.opacity(0.04)
        /// Elevated/secondary card background
        static let elevatedBackground: Color = .primary.opacity(0.03)
        /// Card border stroke
        static let border: Color = .primary.opacity(0.06)
        /// Subtle hover/tint fill
        static let tint: Color = .primary.opacity(0.12)
        /// Active badge background
        static let badgeFill: Color = .secondary.opacity(0.1)
    }

    // MARK: - Icon Sizes

    enum IconSize {
        /// Tiny icon in metric box / label row (replaces 9)
        static let tiny: CGFloat = 9
        /// Small icon in badge / inline indicator (replaces 10–12)
        static let small: CGFloat = 12
        /// Medium icon in summary header (replaces 14)
        static let medium: CGFloat = 14
        /// Large icon in empty state (replaces 32)
        static let large: CGFloat = 32
    }

    // MARK: - Semantic Colors

    enum Semantic {
        static let success = Color.green
        static let warning = Color.orange
        static let error = Color.red
        static let info = Color.blue
        static let inactive = Color.gray
    }
}

// MARK: - ViewModifiers

/// Card style: background + rounded corners + subtle border.
struct CardStyleModifier: ViewModifier {
    var cornerRadius: CGFloat = Design.CornerRadius.large

    func body(content: Content) -> some View {
        content
            .padding(Design.Padding.card)
            .background(Design.Surface.cardBackground)
            .cornerRadius(cornerRadius)
            .overlay(
                RoundedRectangle(cornerRadius: cornerRadius)
                    .stroke(Design.Surface.border, lineWidth: 1)
            )
    }
}

/// Badge / tag pill style with a tinted background.
struct BadgeStyleModifier: ViewModifier {
    var color: Color = .secondary

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, Design.Padding.badgeH)
            .padding(.vertical, Design.Padding.badgeV)
            .background(color.opacity(0.1))
            .cornerRadius(Design.CornerRadius.small)
    }
}

extension View {
    /// Wrap the view in a standard Diane card with background, border, and corner radius.
    /// - Parameter cornerRadius: defaults to `.large` (12); use `.medium` (8) for smaller cards.
    func cardStyle(cornerRadius: CGFloat = Design.CornerRadius.large) -> some View {
        modifier(CardStyleModifier(cornerRadius: cornerRadius))
    }

    /// Apply a tinted badge/pill appearance.
    /// - Parameter color: the semantic tint color (e.g. `.green`, `.orange`).
    func badgeStyle(color: Color = .secondary) -> some View {
        modifier(BadgeStyleModifier(color: color))
    }
}
