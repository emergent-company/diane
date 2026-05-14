import Foundation
import Network

/// Monitors network connectivity status using NWPathMonitor.
/// Posts notifications when connectivity changes.
public final class NetworkMonitor: @unchecked Sendable {
    public static let shared = NetworkMonitor()

    private let monitor = NWPathMonitor()
    private let queue = DispatchQueue(label: "com.emergent-company.diane.network", qos: .utility)

    public private(set) var isConnected = true
    public private(set) var isExpensive = false

    /// Notification posted when connectivity changes. Check `isConnected` on receipt.
    public static let connectivityChanged = Notification.Name("com.diane.network.connectivityChanged")

    private init() {
        monitor.pathUpdateHandler = { [weak self] path in
            guard let self else { return }
            let wasConnected = self.isConnected
            self.isConnected = path.status == .satisfied
            self.isExpensive = path.isExpensive
            if wasConnected != self.isConnected {
                DispatchQueue.main.async {
                    NotificationCenter.default.post(
                        name: Self.connectivityChanged,
                        object: nil,
                        userInfo: ["isConnected": self.isConnected]
                    )
                }
            }
        }
    }

    public func start() {
        monitor.start(queue: queue)
    }

    public func stop() {
        monitor.cancel()
    }

    deinit {
        monitor.cancel()
    }
}
