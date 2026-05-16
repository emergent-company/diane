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
    case photos, albums, camera, cameraSelection
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
public typealias MediaPickerOrientationHandler = @Sendable () -> Void

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

// MARK: - MediaPicker View stub

public struct MediaPicker<AlbumSelectionContent: View, CameraSelectionContent: View>: View {
    @Binding var isPresented: Bool
    let onMediaPicked: ([Media]) -> Void
    
    public init(
        isPresented: Binding<Bool>,
        onMediaPicked: @escaping ([Media]) -> Void,
        @ViewBuilder albumSelectionBuilder: @escaping () -> AlbumSelectionContent = { EmptyView() as! AlbumSelectionContent },
        @ViewBuilder cameraSelectionBuilder: @escaping () -> CameraSelectionContent = { EmptyView() as! CameraSelectionContent }
    ) {
        self._isPresented = isPresented
        self.onMediaPicked = onMediaPicked
    }
    
    public var body: some View {
        EmptyView()
    }
    
    public func didPressCancelCamera(_ action: @escaping () -> Void) -> Self { self }
    public func currentFullscreenMedia(_ binding: Binding<Media?>) -> Self { self }
    public func pickerMode(_ binding: Binding<MediaPickerMode>) -> Self { self }
    public func setMediaPickerParameters(_ params: MediaPickerCutomizationParameters) -> Self { self }
}

/// Stub for ExyteMediaPicker's pickerTheme
public extension EnvironmentValues {
    var pickerTheme: MediaPickerTheme {
        get { self[MediaPickerThemeKey.self] }
        set { self[MediaPickerThemeKey.self] = newValue }
    }
}

// MARK: - MediaPickerTheme stub

public struct MediaPickerTheme {
    public struct Main {
        public var pickerText: Color = .primary
        public var pickerBackground: Color = .clear
        public var fullscreenPhotoBackground: Color = .clear
        
        public init(pickerText: Color = .primary, pickerBackground: Color = .clear, fullscreenPhotoBackground: Color = .clear) {
            self.pickerText = pickerText
            self.pickerBackground = pickerBackground
            self.fullscreenPhotoBackground = fullscreenPhotoBackground
        }
    }

    public struct Selection {
        public var accent: Color = .accentColor
        
        public init(accent: Color = .accentColor) {
            self.accent = accent
        }
    }

    public var main: Main
    public var selection: Selection
    
    public init(main: Main = .init(), selection: Selection = .init()) {
        self.main = main
        self.selection = selection
    }
}

public enum MediaPickerThemeKey: EnvironmentKey {
    public static let defaultValue = MediaPickerTheme()
}

public enum MediaPickerThemeIsOverriddenKey: EnvironmentKey {
    public static let defaultValue = false
}

public extension EnvironmentValues {
    var mediaPickerTheme: MediaPickerTheme {
        get { self[MediaPickerThemeKey.self] }
        set { self[MediaPickerThemeKey.self] = newValue }
    }
    
    var mediaPickerThemeIsOverridden: Bool {
        get { self[MediaPickerThemeIsOverriddenKey.self] }
        set { self[MediaPickerThemeIsOverriddenKey.self] = newValue }
    }
}

extension View {
    public func mediaPickerTheme(
        _ theme: MediaPickerTheme
    ) -> some View {
        self
            .environment(\.mediaPickerTheme, theme)
            .environment(\.mediaPickerThemeIsOverridden, true)
    }

    public func mediaPickerTheme(
        main: MediaPickerTheme.Main,
        selection: MediaPickerTheme.Selection
    ) -> some View {
        self.mediaPickerTheme(MediaPickerTheme(main: main, selection: selection))
    }
}
