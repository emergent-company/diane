import Foundation
import OSLog

/// Polls both the local Diane API and remote Memory Platform and publishes connection state.
/// Uses a fixed 30-second polling interval.
@MainActor
final class StatusMonitor: ObservableObject {
    @Published private(set) var connectionState: ConnectionState = .unknown
    @Published private(set) var lastChecked: Date?
    @Published private(set) var isChecking = false
    @Published var isPaused = false
    @Published private(set) var isLocalAPIReachable = false
    @Published private(set) var isRemoteReachable = false

    private var timer: Timer?
    private var localHealthURL: URL?
    private var remoteHealthURL: URL?
    private let session: URLSession
    private let pollingInterval: TimeInterval = 30

    init() {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 5
        session = URLSession(configuration: config)
    }

    deinit { timer?.invalidate() }

    // MARK: - Public

    var statusLabel: String {
        if isLocalAPIReachable { return "Local API" }
        if isRemoteReachable { return "Remote" }
        return connectionState.description
    }

    var statusColor: Color {
        if isLocalAPIReachable { return .green }
        if isRemoteReachable { return .yellow }
        switch connectionState {
        case .connected: return .green
        case .disconnected: return .secondary
        case .error: return .orange
        case .unknown: return .secondary
        }
    }

    func configure(from serverConfig: ServerConfiguration) {
        stopPolling()
        localHealthURL = URL(string: "http://127.0.0.1:8890/api/status")
        guard let base = serverConfig.baseURL else {
            remoteHealthURL = nil
            connectionState = .unknown
            startPolling(interval: pollingInterval)
            return
        }
        remoteHealthURL = base.appendingPathComponent("health")
        startPolling(interval: pollingInterval)
    }

    func checkNow() {
        Task { await performCheck() }
    }

    // MARK: - Private

    private func startPolling(interval: TimeInterval) {
        Task { await performCheck() }
        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            self?.checkNow()
        }
    }

    private func stopPolling() {
        timer?.invalidate()
        timer = nil
    }

    private func performCheck() async {
        guard !isPaused else { return }
        isChecking = true
        defer {
            isChecking = false
            lastChecked = Date()
        }

        // Check local API first
        var localOK = false
        if let localURL = localHealthURL {
            localOK = await checkOne(url: localURL)
        }
        isLocalAPIReachable = localOK

        // Check remote server
        var remoteOK = false
        if let remoteURL = remoteHealthURL {
            remoteOK = await checkOne(url: remoteURL)
        }
        isRemoteReachable = remoteOK

        // Overall state
        if localOK {
            connectionState = .connected
        } else if remoteOK {
            connectionState = .connected
        } else if remoteHealthURL == nil && localHealthURL == nil {
            connectionState = .unknown
        } else {
            connectionState = .disconnected
        }
    }

    private func checkOne(url: URL) async -> Bool {
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.timeoutInterval = 5
        do {
            let (_, response) = try await session.data(for: request)
            if let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) {
                return true
            }
        } catch {}
        return false
    }
}

import SwiftUI
