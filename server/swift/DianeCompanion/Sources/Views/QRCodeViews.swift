import SwiftUI
import CoreImage

struct QRCodePopoverView: View {
    let authJSON: String
    var body: some View {
        VStack(spacing: 16) {
            Text("Pair iOS Device").font(.title3).fontWeight(.semibold)
            if let image = QRCodeGenerator.generate(from: authJSON) {
                Image(nsImage: image).interpolation(.none).resizable().frame(width: 240, height: 240).padding()
            } else {
                Image(systemName: "qrcode").font(.system(size: 120)).foregroundStyle(.secondary)
            }
            Text("Scan this code with the Diane iOS app\nto authenticate this device.").font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
        }.padding(24).frame(width: 340)
    }
}

enum QRCodeGenerator {
    static func generate(from string: String) -> NSImage? {
        guard let data = string.data(using: .utf8) else { return nil }
        let filter = CIFilter(name: "CIQRCodeGenerator")
        filter?.setValue(data, forKey: "inputMessage")
        filter?.setValue("M", forKey: "inputCorrectionLevel")
        guard let ciImage = filter?.outputImage else { return nil }
        let scaled = ciImage.transformed(by: CGAffineTransform(scaleX: 10, y: 10))
        let rep = NSCIImageRep(ciImage: scaled)
        let nsImage = NSImage(size: rep.size)
        nsImage.addRepresentation(rep)
        return nsImage
    }
}
