import Foundation

enum ConnectionState: Equatable {
    case unknown
    case connected
    case disconnected
    case error(String)

    static func == (lhs: ConnectionState, rhs: ConnectionState) -> Bool {
        switch (lhs, rhs) {
        case (.unknown, .unknown), (.connected, .connected), (.disconnected, .disconnected):
            return true
        case (.error(let a), .error(let b)):
            return a == b
        default:
            return false
        }
    }

    var description: String {
        switch self {
        case .unknown:          return "Unknown"
        case .connected:        return "Connected"
        case .disconnected:     return "Disconnected"
        case .error(let msg):   return msg
        }
    }

    var isConnected: Bool { self == .connected }
}
