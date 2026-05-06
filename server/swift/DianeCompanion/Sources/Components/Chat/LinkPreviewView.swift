import SwiftUI

// MARK: - Link Preview View

/// Renders a rich link preview card from Open Graph metadata.
///
/// Discord-style preview showing:
/// - Site favicon + site name (top row)
/// - Title (bold, linked)
/// - Description (truncated)
/// - Thumbnail image (right side, if available)
///
/// Tapping the card opens the URL in the default browser.
struct LinkPreviewView: View {
    let ogData: OpenGraphData

    @State private var faviconImage: NSImage?
    @State private var previewImage: NSImage?

    var body: some View {
        Button(action: openURL) {
            content
        }
        .buttonStyle(.plain)
        .task { await loadImages() }
    }

    // MARK: - Card Content

    @ViewBuilder
    private var content: some View {
        HStack(alignment: .top, spacing: 0) {
            // Text content
            VStack(alignment: .leading, spacing: 4) {
                // Site name + favicon
                HStack(spacing: 4) {
                    // Favicon
                    if let img = faviconImage {
                        Image(nsImage: img)
                            .resizable()
                            .frame(width: 14, height: 14)
                            .cornerRadius(2)
                    } else {
                        Image(systemName: "globe")
                            .font(.system(size: 10))
                            .foregroundStyle(.tertiary)
                    }

                    Text(ogData.displaySite)
                        .font(.system(size: 10))
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }

                // Title
                Text(ogData.displayTitle)
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(.primary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)

                // Description
                if let desc = ogData.description, !desc.isEmpty {
                    Text(desc)
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                }
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)

            // Thumbnail image (right side)
            if let img = previewImage {
                Image(nsImage: img)
                    .resizable()
                    .aspectRatio(contentMode: .fill)
                    .frame(width: 64, height: 64)
                    .clipped()
                    .cornerRadius(4)
                    .padding(8)
            }
        }
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color.primary.opacity(0.03))
                .overlay(
                    RoundedRectangle(cornerRadius: 8)
                        .stroke(Color.primary.opacity(0.08), lineWidth: 0.5)
                )
        )
        .contentShape(Rectangle())
        .cursor(.pointingHand)
    }

    // MARK: - Actions

    private func openURL() {
        NSWorkspace.shared.open(ogData.url)
    }

    // MARK: - Image Loading

    @MainActor
    private func loadImages() async {
        // Load favicon
        if let faviconURL = ogData.faviconURL {
            faviconImage = await loadImage(from: faviconURL)
        }

        // Load preview image
        if let imgURL = ogData.imageURL {
            previewImage = await loadImage(from: imgURL)
        }
    }

    private func loadImage(from url: URL) async -> NSImage? {
        var request = URLRequest(url: url)
        request.timeoutInterval = 5
        guard let (data, _) = try? await URLSession.shared.data(for: request),
              let image = NSImage(data: data)
        else { return nil }
        return image
    }
}

// MARK: - URL Auto-Detection + Preview Integration

/// A view that detects URLs in text, fetches OG metadata, and shows previews.
struct AutoLinkPreviewView: View {
    let text: String

    @State private var previews: [OpenGraphData] = []
    @State private var isLoading = false

    var body: some View {
        Group {
            if !previews.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(previews) { og in
                        LinkPreviewView(ogData: og)
                    }
                }
            }
        }
        .task(id: text) { await loadPreviews() }
    }

    @MainActor
    private func loadPreviews() async {
        let urls = OpenGraphService.extractURLs(from: text)
        guard !urls.isEmpty else {
            previews = []
            return
        }

        isLoading = true
        defer { isLoading = false }

        // Fetch up to 3 previews concurrently
        let limited = Array(urls.prefix(3))
        var results: [OpenGraphData] = []

        for url in limited {
            if let og = await OpenGraphService.fetch(url: url) {
                results.append(og)
            }
        }

        previews = results
    }
}

// MARK: - Previews

#Preview("Link Preview Card") {
    LinkPreviewView(ogData: OpenGraphData(
        id: "https://github.com",
        url: URL(string: "https://github.com")!,
        siteName: "GitHub",
        title: "Let's build from here · GitHub",
        description: "GitHub is the world's largest software development platform.",
        imageURL: nil,
        faviconURL: URL(string: "https://github.com/favicon.ico")
    ))
    .padding()
    .frame(width: 380)
}

#Preview("Auto Detect URLs") {
    AutoLinkPreviewView(text: "Check out https://github.com for more info and https://apple.com for Apple.")
        .padding()
        .frame(width: 380)
}
