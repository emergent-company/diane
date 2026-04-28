import Foundation

/// Polls the Emergent server health endpoint and publishes connection state.
/// Uses a fixed 30-second polling interval.
@MainActor
final class StatusMonitor: ObservableObject {
    @Published private(set) var connectionState: ConnectionState = .unknown
    @Published private(set) var lastChecked: Date?
    @Published private(set) var isChecking = false
    @Published var isPaused = false

    private var timer: Timer?
    private var healthURL: URL?
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

    var statusLabel: String { connectionState.description }

    func configure(from serverConfig: ServerConfiguration) {
        stopPolling()
        guard let base = serverConfig.baseURL else {
            healthURL = nil
            connectionState = .unknown
            return
        }
        healthURL = base.appendingPathComponent("health")
        startPolling(interval: pollingInterval)
    }

    func checkNow() {
        guard let url = healthURL else { return }
        Task { await performCheck(url: url) }
    }

    // MARK: - Private

    private func startPolling(interval: TimeInterval) {
        checkNow()
        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            self?.checkNow()
        }
    }

    private func stopPolling() {
        timer?.invalidate()
        timer = nil
    }

    private func performCheck(url: URL) async {
        guard !isPaused else { return }
        isChecking = true
        defer {
            isChecking = false
            lastChecked = Date()
        }
        do {
            var request = URLRequest(url: url)
            request.httpMethod = "GET"
            let (_, response) = try await session.data(for: request)
            if let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) {
                connectionState = .connected
            } else {
                connectionState = .disconnected
            }
        } catch let urlError as URLError {
            switch urlError.code {
            case .timedOut:
                connectionState = .error("Timed out")
            case .cannotConnectToHost, .networkConnectionLost, .notConnectedToInternet:
                connectionState = .disconnected
            default:
                connectionState = .error(urlError.localizedDescription)
            }
        } catch {
            connectionState = .error(error.localizedDescription)
        }
    }
}
