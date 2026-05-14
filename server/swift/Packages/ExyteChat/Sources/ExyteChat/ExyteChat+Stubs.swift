//
//  ExyteChat+Stubs.swift
//  ExyteChat
//
//  Stub types replacing removed external dependencies (ExyteMediaPicker, GiphyUISDK).
//

import Foundation
import SwiftUI

// MARK: - Media stub (replaces ExyteMediaPicker.Media)
public struct Media: Identifiable, Sendable, Equatable {
    public let id: String
    public let url: URL?
    public let type: MediaType
    
    public enum MediaType: String, Sendable {
        case image, video, document
    }
    
    public init(id: String = UUID().uuidString, url: URL? = nil, type: MediaType = .image) {
        self.id = id
        self.url = url
        self.type = type
    }
}

// MARK: - Giphy stub (replaces GiphyUISDK)
public struct GPHMedia: Sendable {
    public let id: String
    public let url: URL?
    
    public init(id: String = "", url: URL? = nil) {
        self.id = id
        self.url = url
    }
}

// MARK: - MediaPicker stub types
public enum MediaPickerMode: Sendable {
    case photos, albums
}

public struct MediaPickerSelectionParameters: Sendable {
    public var selectionLimit: Int?
    public var showFullscreenPreview: Bool = false
    
    public init(selectionLimit: Int? = nil, showFullscreenPreview: Bool = false) {
        self.selectionLimit = selectionLimit
        self.showFullscreenPreview = showFullscreenPreview
    }
}

public typealias MediaPickerLiveCameraStyle = LiveCameraCellStyle
public typealias MediaPickerOrientationHandler = () -> Void

public struct LiveCameraCellStyle: Sendable {
    public init() {}
}

/// Typedef for the original ExyteMediaPicker parameters typealias
public struct MediaPickerCutomizationParameters: Sendable {
    public var selectionParameters = MediaPickerSelectionParameters()
    public var liveCameraStyle: MediaPickerLiveCameraStyle = .init()
    public var orientationHandler: MediaPickerOrientationHandler = {}
    
    public init() {}
}
