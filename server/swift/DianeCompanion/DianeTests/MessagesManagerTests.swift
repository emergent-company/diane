import XCTest
@testable import Diane

/// Mock MessagesManager for testing MessagesView logic.
@MainActor
final class MockMessagesManager: MessagesManagerProtocol {
    var isAuthorized: Bool = true
    var sentMessages: [(text: String, recipient: String)] = []
    var sentSMSs: [(text: String, phoneNumber: String)] = []
    var recentConversations: [String] = ["Mom", "Work Chat", "John"]
    var shouldThrowOnSend = false
    var shouldThrowOnFetch = false

    func checkAuthorization() {
        // Already set via `isAuthorized`
    }

    func sendMessage(text: String, to recipient: String) async throws {
        if shouldThrowOnSend { throw NSError(domain: "test", code: 1, userInfo: [NSLocalizedDescriptionKey: "Send failed"]) }
        sentMessages.append((text, recipient))
    }

    func sendSMS(text: String, to phoneNumber: String) async throws {
        if shouldThrowOnSend { throw NSError(domain: "test", code: 1, userInfo: [NSLocalizedDescriptionKey: "SMS failed"]) }
        sentSMSs.append((text, phoneNumber))
    }

    func fetchRecentConversations() async throws -> [String] {
        if shouldThrowOnFetch { throw NSError(domain: "test", code: 1, userInfo: [NSLocalizedDescriptionKey: "Fetch failed"]) }
        return recentConversations
    }
}

@MainActor
final class MessagesManagerTests: XCTestCase {

    func testProtocolConformance() {
        let manager = MessagesManager()
        // Compile-time check: manager conforms to MessagesManagerProtocol
        let proto: MessagesManagerProtocol = manager
        XCTAssertNotNil(proto, "MessagesManager should conform to MessagesManagerProtocol")
    }

    func testMockSendMessageSuccess() async throws {
        let mock = MockMessagesManager()
        try await mock.sendMessage(text: "Hello", to: "user@example.com")
        XCTAssertEqual(mock.sentMessages.count, 1)
        XCTAssertEqual(mock.sentMessages[0].text, "Hello")
        XCTAssertEqual(mock.sentMessages[0].recipient, "user@example.com")
    }

    func testMockSendMessageFailure() async {
        let mock = MockMessagesManager()
        mock.shouldThrowOnSend = true
        do {
            try await mock.sendMessage(text: "Hello", to: "user@example.com")
            XCTFail("Should have thrown")
        } catch {
            XCTAssertEqual(error.localizedDescription, "Send failed")
        }
    }

    func testMockSendSMSSuccess() async throws {
        let mock = MockMessagesManager()
        try await mock.sendSMS(text: "Hello", to: "+1234567890")
        XCTAssertEqual(mock.sentSMSs.count, 1)
        XCTAssertEqual(mock.sentSMSs[0].text, "Hello")
        XCTAssertEqual(mock.sentSMSs[0].phoneNumber, "+1234567890")
    }

    func testMockFetchConversationsSuccess() async throws {
        let mock = MockMessagesManager()
        let convos = try await mock.fetchRecentConversations()
        XCTAssertEqual(convos, ["Mom", "Work Chat", "John"])
    }

    func testMockFetchConversationsFailure() async {
        let mock = MockMessagesManager()
        mock.shouldThrowOnFetch = true
        do {
            _ = try await mock.fetchRecentConversations()
            XCTFail("Should have thrown")
        } catch {
            XCTAssertEqual(error.localizedDescription, "Fetch failed")
        }
    }

    func testMockAuthorizationState() {
        let mock = MockMessagesManager()
        XCTAssertTrue(mock.isAuthorized)

        mock.isAuthorized = false
        XCTAssertFalse(mock.isAuthorized)

        mock.checkAuthorization()
        // Should not crash — no-op in mock
    }

    func testMockMultipleMessages() async throws {
        let mock = MockMessagesManager()
        try await mock.sendMessage(text: "First", to: "a@b.com")
        try await mock.sendMessage(text: "Second", to: "c@d.com")
        try await mock.sendMessage(text: "Third", to: "e@f.com")
        XCTAssertEqual(mock.sentMessages.count, 3)
        XCTAssertEqual(mock.sentMessages.map(\.text), ["First", "Second", "Third"])
        XCTAssertEqual(mock.sentMessages.map(\.recipient), ["a@b.com", "c@d.com", "e@f.com"])
    }
}
