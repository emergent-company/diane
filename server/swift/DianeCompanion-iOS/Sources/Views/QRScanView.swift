import AVFoundation
import SwiftUI

public struct AuthCodePayload: Decodable, Equatable {
    public let mpURL: String
    public let apiKey: String
    public let projectID: String
    enum CodingKeys: String, CodingKey {
        case mpURL = "mp_url"
        case apiKey = "api_key"
        case projectID = "project_id"
    }
}

struct QRScanView: View {
    var onScan: (AuthCodePayload) -> Void
    var onCancel: () -> Void
    @State private var scanError: String?

    var body: some View {
        ZStack {
            CameraPreview { payload in onScan(payload) } onError: { e in scanError = e }
                .edgesIgnoringSafeArea(.all)
            VStack {
                Spacer()
                Text("Point your camera at the QR code\ndisplayed in the Diane macOS app.")
                    .font(.subheadline).foregroundColor(.white).multilineTextAlignment(.center)
                    .padding().background(.ultraThinMaterial).cornerRadius(12).padding(.bottom, 40)
            }
            VStack {
                HStack { Spacer(); Button(action: onCancel) { Image(systemName: "xmark.circle.fill").font(.title).foregroundColor(.white).padding() } }; Spacer()
            }
            if let e = scanError {
                VStack {
                    Image(systemName: "exclamationmark.triangle.fill").font(.largeTitle).foregroundColor(.yellow)
                    Text(e).foregroundColor(.white).font(.subheadline)
                    Button("Try Again") { scanError = nil }.buttonStyle(.borderedProminent).padding(.top, 8)
                }.padding().background(.ultraThinMaterial).cornerRadius(16)
            }
        }
    }
}

private struct CameraPreview: UIViewControllerRepresentable {
    let onScan: (AuthCodePayload) -> Void
    let onError: (String) -> Void
    func makeUIViewController(context: Context) -> ScannerViewController {
        let vc = ScannerViewController()
        vc.onScan = { p in Task { @MainActor in onScan(p) } }
        vc.onError = { m in Task { @MainActor in onError(m) } }
        return vc
    }
    func updateUIViewController(_: ScannerViewController, context: Context) {}
}

@MainActor
final class ScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onScan: ((AuthCodePayload) -> Void)?
    var onError: ((String) -> Void)?
    private var captureSession: AVCaptureSession?
    private var previewLayer: AVCaptureVideoPreviewLayer?
    private var hasStarted = false

    override func viewDidLoad() { super.viewDidLoad(); setupCamera() }
    override func viewWillAppear(_ a: Bool) { super.viewWillAppear(a); if !hasStarted { startSession() } }
    override func viewDidDisappear(_ a: Bool) { super.viewDidDisappear(a); stopSession() }

    private func setupCamera() {
        guard let device = AVCaptureDevice.default(for: .video) else { onError?("Camera not available"); return }
        let session = AVCaptureSession(); self.captureSession = session
        do {
            let input = try AVCaptureDeviceInput(device: device)
            guard session.canAddInput(input) else { onError?("Cannot add camera input"); return }
            session.addInput(input)
        } catch { onError?("Camera error: \(error.localizedDescription)"); return }
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { onError?("Cannot add metadata output"); return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]
        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        preview.frame = view.bounds
        view.layer.addSublayer(preview)
        previewLayer = preview
    }

    private func startSession() {
        guard !hasStarted, let s = captureSession else { return }
        hasStarted = true
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in self?.captureSession?.startRunning() }
    }
    private func stopSession() {
        guard hasStarted, let s = captureSession else { return }; hasStarted = false; s.stopRunning()
    }

    nonisolated func metadataOutput(_: AVCaptureMetadataOutput, didOutput mos: [AVMetadataObject], from _: AVCaptureConnection) {
        guard let o = mos.first as? AVMetadataMachineReadableCodeObject, let raw = o.stringValue,
              let d = raw.data(using: .utf8), let p = try? JSONDecoder().decode(AuthCodePayload.self, from: d) else { return }
        Task { @MainActor [weak self] in self?.stopSession(); AudioServicesPlaySystemSound(kSystemSoundID_Vibrate); self?.onScan?(p) }
    }

    override func viewDidLayoutSubviews() { super.viewDidLayoutSubviews(); previewLayer?.frame = view.bounds }
}
