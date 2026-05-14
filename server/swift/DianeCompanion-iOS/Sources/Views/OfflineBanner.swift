import SwiftUI

/// Offline banner displayed at the top of content views when the device has no network.
struct OfflineBanner: View {
    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "wifi.slash")
                .font(.caption)
            Text("No Connection")
                .font(.caption)
                .fontWeight(.medium)
            Spacer()
            Text("Showing cached data")
                .font(.caption2)
                .foregroundColor(.secondary)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color.orange.opacity(0.15))
        .foregroundColor(.orange)
    }
}

/// View modifier that shows an offline banner when not connected.
struct OfflineBannerModifier: ViewModifier {
    @StateObject private var monitor = NetworkMonitorObserved()

    func body(content: Content) -> some View {
        VStack(spacing: 0) {
            if !monitor.isConnected {
                OfflineBanner()
            }
            content
        }
    }
}

extension View {
    func withOfflineBanner() -> some View {
        modifier(OfflineBannerModifier())
    }
}

/// Observable wrapper around NetworkMonitor for SwiftUI.
final class NetworkMonitorObserved: ObservableObject {
    @Published var isConnected = NetworkMonitor.shared.isConnected

    init() {
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(connectivityChanged),
            name: NetworkMonitor.connectivityChanged,
            object: nil
        )
        isConnected = NetworkMonitor.shared.isConnected
    }

    @objc private func connectivityChanged(_ notification: Notification) {
        if let connected = notification.userInfo?["isConnected"] as? Bool {
            isConnected = connected
        }
    }

    deinit {
        NotificationCenter.default.removeObserver(self)
    }
}
