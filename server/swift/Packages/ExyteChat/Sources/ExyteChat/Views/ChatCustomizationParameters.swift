//
//  ChatCustomizationParameters.swift
//  Chat
//
//  Created by Alisa Mylnikova on 02.04.2026.
//

import SwiftUI

#if os(iOS)
#if os(macOS)
typealias UIEdgeInsets = NSEdgeInsets
#endif

struct ChatCustomizationParameters {
    var isListAboveInputView: Bool = true
    var showScrollToBottomButton: Bool = true
    var showNetworkConnectionProblem: Bool = false
    var showDateHeaders: Bool = true
    var isScrollEnabled: Bool = true
    var showMessageMenuOnLongPress: Bool = true
#if os(iOS)
    var keyboardDismissMode: UIScrollView.KeyboardDismissMode = .none
#endif
    var messageMenuAnimationDuration: CGFloat = 0.3
    var contentInsets: UIEdgeInsets = UIEdgeInsets(top: 0, left: 0, bottom: 0, right: 0)

    var externalContentOffset: CGPoint? // External → Internal
    var onContentOffsetChange: ((CGPoint) -> Void)? // Internal → External
    var scrollToMessageID: String?
    var onWillDisplayCell: ((Message) -> Void)?
    var onTransactionReady: ((TableUpdateTransaction) -> Void)?

    var paginationHandler: PaginationHandler?
    var localization = ChatLocalization.defaultLocalization // these can be localized in the Localizable.strings files
    var reactionDelegate: ReactionDelegate?
    var listSwipeActions = ListSwipeActions()
}

struct MessageCustomizationParameters {
    var showTimeView = true
    var showUsername = false
    var linkPreviewLimit = 8
    var shouldShowPreviewForLink: (URL) -> Bool = { _ in true }
#if os(iOS)
    var font = UIFontMetrics.default.scaledFont(for: UIFont.systemFont(ofSize: 15))
#else
    var font = NSFont.systemFont(ofSize: 15)
#endif

    // avatar
    var showAvatar = true
    var avatarSize: CGFloat = 32
    var tapAvatarClosure: ChatView.TapAvatarClosure?
    var avatarBuilder: ((User)->(AnyView))?
}

struct InputViewCustomizationParameters {
    var externalInputText: String? // External → Internal
    var onInputTextChange: ((String) -> Void)? // Internal → External
    var availableInputs: [AvailableInputType] = [.text, .audio, .media]
    var recorderSettings = RecorderSettings()
    var mediaPickerParameters = MediaPickerParameters()
}

#if os(iOS)
public typealias MediaPickerParameters = MediaPickerCutomizationParameters
#endif
#endif
