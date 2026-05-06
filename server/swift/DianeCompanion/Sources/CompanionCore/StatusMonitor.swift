import Foundation

/// Polls both the local Diane API and remote Memory Platform and publishes connection state.
/// Uses a fixed 30-second polling interval.
/// On initial startup, enters a "connecting" phase with aggressive retries (2s for 20s)
/// so the app has time to wait for diane serve to finish booting after an upgrade.
@MainActor
final class StatusMonitor: ObservableObject {
    @Published private(set) var connectionState: ConnectionState = .unknown
    @Published private(set) var lastChecked: Date?
    @Published private(set) var isChecking = false
    @Published private(set) var isConnecting = false
    @Published var isPaused = false
    @Published private(set) var isLocalAPIReachable = false
    @Published private(set) var isRemoteReachable = false

    private nonisolated(unsafe) var timer: Timer?
    private nonisolated(unsafe) var connectTimer: Timer?
    private var localHealthURL: URL?
    private var remoteHealthURL: URL?
    private let session: URLSession
    private let pollingInterval: TimeInterval = 30

    /// Aggressive retry phase: poll every N seconds for up to totalDuration seconds.
    private let connectInterval: TimeInterval = 2
    private let connectDuration: TimeInterval = 20
    private var connectElapsed: TimeInterval = 0

    init() {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 5
        session = URLSession(configuration: config)
    }

    deinit {
        timer?.invalidate()
        connectTimer?.invalidate()
    }

    // MARK: - Public

    var statusLabel: String {
        if isConnecting { return "Connecting" }
        if isLocalAPIReachable { return "Local API" }
        if isRemoteReachable { return "Remote" }
        return connectionState.description
    }

    var statusColor: Color {
        if isConnecting { return .orange }
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
        connectTimer?.invalidate()
        connectTimer = nil

        localHealthURL = URL(string: "http://127.0.0.1:8890/api/status")
        guard let base = serverConfig.baseURL else {
            remoteHealthURL = nil
            connectionState = .unknown
            startPolling(interval: pollingInterval)
            return
        }
        remoteHealthURL = base.appendingPathComponent("health")
        startConnectingPhase()
    }

    func checkNow() {
        Task { await performCheck() }
    }

    /// Restart the aggressive connecting phase (e.g. after user taps Retry).
    func restartConnectingPhase() {
        connectTimer?.invalidate()
        connectTimer = nil
        connectElapsed = 0
        startConnectingPhase()
    }

    // MARK: - Private

    private func startConnectingPhase() {
        isConnecting = true
        stopPolling()

        // Run first check immediately
        Task { await performCheck() }

        // Aggressive retry: poll every 2s for up to 20s
        connectTimer = Timer.scheduledTimer(withTimeInterval: connectInterval, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            Task { @MainActor in
                self.connectElapsed += self.connectInterval
                await self.performCheck()

                // Once reachable or timeout, switch to normal polling
                if self.isLocalAPIReachable || self.connectElapsed >= self.connectDuration {
                    self.finishConnectingPhase()
                }
            }
        }
    }

    private func finishConnectingPhase() {
        connectTimer?.invalidate()
        connectTimer = nil
        isConnecting = false
        connectElapsed = 0
        startPolling(interval: pollingInterval)
    }

    private func startPolling(interval: TimeInterval) {
        Task { await performCheck() }
        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            Task { @MainActor in
                self.checkNow()
            }
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

extension StatusMonitor {
    /// Create an instance pre-configured for preview canvases.
    /// Can only live in this file because `@Published private(set)` properties
    /// are file-private for writes.
    static func forPreviews(
        connectionState: ConnectionState = .connected,
        isLocalReachable: Bool = true,
        isConnecting: Bool = false
    ) -> StatusMonitor {
        let monitor = StatusMonitor()
        monitor.connectionState = connectionState
        monitor.isLocalAPIReachable = isLocalReachable
        monitor.isConnecting = isConnecting
        return monitor
    }
}
