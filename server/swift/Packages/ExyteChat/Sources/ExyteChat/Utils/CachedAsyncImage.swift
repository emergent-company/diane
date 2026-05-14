//
//  Created by Aman Kumar on 26/08/25.
//

import SwiftUI

/// A view that asynchronously loads and displays an image using SwiftUI's built-in AsyncImage.
/// This replaces the Kingfisher-based CachedAsyncImage from the original ExyteChat library.
@available(iOS 15.0, macOS 12.0, tvOS 15.0, watchOS 8.0, *)
public struct CachedAsyncImage<Content>: View where Content: View {

    @State private var phase: AsyncImagePhase

    private let url: URL?
    private let scale: CGFloat
    private let transaction: Transaction
    private let content: (AsyncImagePhase) -> Content

    public var body: some View {
        content(phase)
            .task(id: url, load)
    }

    /// Loads and displays an image from the specified URL.
    public init(url: URL?, scale: CGFloat = 1) where Content == Image {
        self.init(url: url, scale: scale) { phase in
            phase.image ?? Image(systemName: "photo")
        }
    }

    /// Loads and displays a modifiable image with placeholder.
    public init<I, P>(
        url: URL?,
        scale: CGFloat = 1,
        @ViewBuilder content: @escaping (Image) -> I,
        @ViewBuilder placeholder: @escaping () -> P
    ) where Content == _ConditionalContent<I, P>, I: View, P: View {
        self.init(url: url, scale: scale) { phase in
            if let image = phase.image {
                content(image)
            } else {
                placeholder()
            }
        }
    }

    /// Loads and displays a modifiable image in phases.
    public init(
        url: URL?,
        scale: CGFloat = 1,
        transaction: Transaction = Transaction(),
        @ViewBuilder content: @escaping (AsyncImagePhase) -> Content
    ) {
        self.url = url
        self.scale = scale
        self.transaction = transaction
        self.content = content
        self._phase = State(wrappedValue: .empty)
    }

    @Sendable
    private func load() async {
        guard let url = url else {
            withAnimation(transaction.animation) { phase = .empty }
            return
        }

        do {
            let (data, _) = try await URLSession.shared.data(from: url)
            #if os(macOS)
            guard let nsImage = NSImage(data: data) else {
                throw URLError(.cannotDecodeContentData)
            }
            let image = Image(nsImage: nsImage)
            #else
            guard let uiImage = UIImage(data: data) else {
                throw URLError(.cannotDecodeContentData)
            }
            let image = Image(uiImage: uiImage)
            #endif

            withAnimation(transaction.animation) {
                phase = .success(image)
            }
        } catch {
            withAnimation(transaction.animation) {
                phase = .failure(error)
            }
        }
    }
}
