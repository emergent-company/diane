import SwiftUI

// MARK: - Design Tokens

public enum DesignTokens {
    // MARK: - Spacing
    public static let spacingXXS: CGFloat = 2
    public static let spacingXS: CGFloat = 4
    public static let spacingSM: CGFloat = 8
    public static let spacingMD: CGFloat = 12
    public static let spacingLG: CGFloat = 16
    public static let spacingXL: CGFloat = 24
    public static let spacingXXL: CGFloat = 32
    public static let spacingXXXL: CGFloat = 48

    // MARK: - Corner Radius
    public static let radiusSM: CGFloat = 4
    public static let radiusMD: CGFloat = 8
    public static let radiusLG: CGFloat = 12
    public static let radiusXL: CGFloat = 16
    public static let radiusPill: CGFloat = 999

    // MARK: - Font Sizes
    public static let fontSizeXS: CGFloat = 10
    public static let fontSizeSM: CGFloat = 12
    public static let fontSizeMD: CGFloat = 14
    public static let fontSizeLG: CGFloat = 16
    public static let fontSizeXL: CGFloat = 20
    public static let fontSizeXXL: CGFloat = 24
    public static let fontSizeXXXL: CGFloat = 32

    // MARK: - Icon Sizes
    public static let iconSM: CGFloat = 12
    public static let iconMD: CGFloat = 16
    public static let iconLG: CGFloat = 20
    public static let iconXL: CGFloat = 24

    // MARK: - Opacity
    public static let opacityDisabled: Double = 0.4
    public static let opacitySubtle: Double = 0.6
    public static let opacityOverlay: Double = 0.7

    // MARK: - Animation Durations
    public static let animationFast: Double = 0.15
    public static let animationNormal: Double = 0.3
    public static let animationSlow: Double = 0.5

    // MARK: - Line Limits
    public static let lineLimitSM: Int = 1
    public static let lineLimitMD: Int = 2
    public static let lineLimitLG: Int = 5

    // MARK: - Min Heights
    public static let minTouchTarget: CGFloat = 44
    public static let minRowHeight: CGFloat = 48
    public static let minToolbarHeight: CGFloat = 40
}

// MARK: - Padding Convenience

extension EdgeInsets {
    public static let listRowPadding = EdgeInsets(
        top: DesignTokens.spacingSM,
        leading: DesignTokens.spacingLG,
        bottom: DesignTokens.spacingSM,
        trailing: DesignTokens.spacingLG
    )

    public static let sectionPadding = EdgeInsets(
        top: DesignTokens.spacingLG,
        leading: DesignTokens.spacingXL,
        bottom: DesignTokens.spacingLG,
        trailing: DesignTokens.spacingXL
    )
}
