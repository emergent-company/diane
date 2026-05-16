#if os(iOS)
//
//  Created by Alex.M on 16.06.2022.
//

import SwiftUI

public struct AttachmentCell: View {

    @Environment(\.chatTheme) var theme

    let attachment: Attachment
    let size: CGSize
    let showCancel: Bool
    let onTap: (_ attachment: Attachment, _ isCancel: Bool) -> Void

    public init(
        attachment: Attachment, size: CGSize, showCancel: Bool = false,
        onTap: @escaping (_ attachment: Attachment, _ isCancel: Bool) -> Void
    ) {
        self.attachment = attachment
        self.size = size
        self.showCancel = showCancel
        self.onTap = onTap
    }

    public var body: some View {
        ZStack {
            switch attachment.type {
            case .image:
                ZStack {
                    content
                    uploadStatusOverlay
                }
            case .video:
                ZStack {
                    content
                    if let status = attachment.fullUploadStatus, case .complete = status {
                        playOverlay
                    } else if attachment.fullUploadStatus == nil {
                        playOverlay
                    } else {
                        uploadStatusOverlay
                    }
                }
            }
        }
        .frame(width: size.width, height: size.height)
        .contentShape(Rectangle())
        .simultaneousGesture(attachmentTapGesture)
    }

    @ViewBuilder
    private var playOverlay: some View {
        VStack {
            Spacer()
            theme.images.message.playVideo
                .resizable()
                .foregroundColor(.white)
                .frame(width: 36, height: 36)
            Spacer()
        }
    }

    @ViewBuilder
    private var uploadStatusOverlay: some View {
        if let status = attachment.fullUploadStatus {
            switch status {
            case .inProgress(.none):
                uploadingOverlay(percent: nil)
            case .inProgress(let percent?):
                uploadingOverlay(percent: percent)
            case .complete:
                EmptyView()
            case .cancelled:
                cancelledOverlay
            case .error:
                errorOverlay
            }
        }
    }

    @ViewBuilder
    private func uploadingOverlay(percent: Int?) -> some View {
        Color.white.opacity(0.8)
        if showCancel {
            theme.images.message.cancel
                .resizable()
                .symbolRenderingMode(.palette)
                .foregroundStyle(.white, .black.opacity(0.4))
                .frame(width: 36, height: 36)
        }
        VStack {
            HStack {
                Spacer()
                if let percent {
                    AttachmentUploadStatusCapsuleView(percent)
                        .padding(4)
                } else {
                    AttachmentUploadStatusCapsuleView()
                        .padding(4)
                }
            }
            Spacer()
        }
    }

    @ViewBuilder
    private var cancelledOverlay: some View {
        Color.white.opacity(0.8)
        VStack {
            HStack {
                Spacer()
                theme.images.message.cancel
                    .resizable()
                    .symbolRenderingMode(.palette)
                    .foregroundStyle(.white, .black.opacity(0.4))
                    .frame(width: 26, height: 26)
                    .padding(4)
            }
            Spacer()
        }
    }

    @ViewBuilder
    private var errorOverlay: some View {
        Color.white.opacity(0.8)
        VStack {
            HStack {
                Spacer()
                theme.images.message.error
                    .resizable()
                    .symbolRenderingMode(.palette)
                    .foregroundStyle(.white, .black.opacity(0.4))
                    .frame(width: 26, height: 26)
                    .padding(4)
            }
            Spacer()
        }
    }

    private var attachmentTapGesture: AnyGesture<Void>? {
        if let status = attachment.fullUploadStatus {
            switch status {
            case .cancelled: return nil
            case .error: return nil
            case .inProgress(_):
                if showCancel {
                    return AnyGesture(TapGesture().onEnded { onTap(attachment, true) })
                }
                else {
                    // only the sender can cancel an upload attachment
                    return nil
                }
            case .complete: return AnyGesture(TapGesture().onEnded { onTap(attachment, false) })
            }
        }

        // attachments are uploaded before displayed so show play button
        return AnyGesture(TapGesture().onEnded { onTap(attachment, false) })

    }

    var content: some View {
        AsyncImageView(attachment: attachment, size: size)
    }
}

struct AsyncImageView: View {

    @Environment(\.chatTheme) var theme

    let attachment: Attachment
    let size: CGSize

    var body: some View {
        CachedAsyncImage(
            url: attachment.thumbnail,
            cacheKey: attachment.thumbnailCacheKey
        ) { imageView in
            imageView
                .resizable()
                .scaledToFill()
                .frame(width: size.width, height: size.height)
                .clipped()
        } placeholder: {
            ZStack {
                Rectangle()
                    .foregroundColor(theme.colors.inputBG)
                    .frame(width: size.width, height: size.height)
                ActivityIndicator(size: 30, showBackground: false)
            }
        }
    }
}
#endif