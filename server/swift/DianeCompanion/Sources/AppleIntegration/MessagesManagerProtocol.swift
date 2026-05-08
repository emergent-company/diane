import Foundation

/// Protocol for iMessage management, enabling test mocking.
@MainActor
protocol MessagesManagerProtocol: AnyObject {
    var isAuthorized: Bool { get }
    func checkAuthorization()
    func sendMessage(text: String, to recipient: String) async throws
    func sendSMS(text: String, to phoneNumber: String) async throws
    func fetchRecentConversations() async throws -> [String]
}
