import Foundation
import DianeShared

/// Manages iOS device registration as a phone node in the Memory Platform.
@MainActor
public final class NodeRegistrationService {
    public static let shared = NodeRegistrationService()

    private let defaults = UserDefaults(suiteName: "group.com.emergent-company.diane-companion")
    private let key = "device-uuid"

    public private(set) var instanceID: String = ""
    public private(set) var status: NodeStatus = .unregistered

    public enum NodeStatus: String {
        case unregistered
        case registered
        case failed
    }

    private init() {}

    /// Register this device as a phone node.
    public func register(apiClient: EmergentAPIClient) async throws {
        // Generate or load UUID
        if let existing = defaults?.string(forKey: key) {
            instanceID = existing
        } else {
            instanceID = "ios-" + UUID().uuidString.lowercased().prefix(8)
            defaults?.set(instanceID, forKey: key)
        }

        // Send registration (best effort)
        do {
            _ = try await apiClient.registerNode(
                instanceID: instanceID,
                nodeType: "phone",
                mode: "passive",
                capabilities: ["push_notifications"]
            )
            status = .registered
        } catch {
            status = .failed
            throw error
        }
    }

    /// Send a heartbeat while app is in foreground.
    public func sendHeartbeat(apiClient: EmergentAPIClient) async {
        guard status == .registered else { return }
        try? await apiClient.sendNodeHeartbeat(instanceID: instanceID)
    }
}

// MARK: - EmergentAPIClient extensions

extension EmergentAPIClient {
    func registerNode(instanceID: String, nodeType: String, mode: String, capabilities: [String]) async throws -> Bool {
        let body: [String: Any] = [
            "instance_id": instanceID,
            "node_type": nodeType,
            "mode": mode,
            "capabilities": capabilities,
            "metadata": [
                "device_name": await UIDevice.current.name,
                "device_model": await UIDevice.current.model,
                "os_version": await UIDevice.current.systemVersion
            ]
        ]
        let jsonData = try JSONSerialization.data(withJSONObject: body)
        let _ = try await http.post("/api/nodes", body: jsonData)
        return true
    }

    func sendNodeHeartbeat(instanceID: String) async throws {
        let body = try JSONSerialization.data(withJSONObject: ["status": "online"])
        _ = try await http.put("/api/nodes/\(instanceID)/heartbeat", body: body)
    }
}

// MARK: - UIKit import for device info

#if canImport(UIKit)
import UIKit
#endif
