import Testing
import Foundation
@testable import DianeShared

// MARK: - SendStatus State Machine Unit Tests

struct DianeMessageTests {

    // MARK: - SendStatus transitions

    @Test("SendStatus state machine transitions forward correctly")
    func testSendStatusForwardTransitions() throws {
        // queued → sending → sent → read
        #expect(SendStatusTransition.canTransition(from: .queued, to: .sending) == true)
        #expect(SendStatusTransition.canTransition(from: .sending, to: .sent) == true)
        #expect(SendStatusTransition.canTransition(from: .sent, to: .read) == true)
    }

    @Test("SendStatus failure allowed from any non-terminal state")
    func testSendStatusFailureFromAnyState() throws {
        for fromState in [DianeMessage.SendStatus.queued, .sending, .sent, .retrying] {
            #expect(SendStatusTransition.canTransition(from: fromState, to: .failed) == true,
                    "Failed transition should be allowed from \(fromState)")
        }
    }

    @Test("SendStatus retry only allowed from failed")
    func testSendStatusRetryOnlyFromFailed() throws {
        #expect(SendStatusTransition.canTransition(from: .failed, to: .retrying) == true)
        #expect(SendStatusTransition.canTransition(from: .sending, to: .retrying) == false)
        #expect(SendStatusTransition.canTransition(from: .queued, to: .retrying) == false)
    }

    @Test("SendStatus no transitions from terminal states")
    func testSendStatusNoTransitionsFromTerminal() throws {
        let terminalStates: [DianeMessage.SendStatus] = [.read, .failed, .streaming, .sent]
        for fromState in terminalStates {
            for toState in DianeMessage.SendStatus.allCases {
                if fromState == .sent && toState == .read {
                    // sent → read is the only allowed transition from sent
                    continue
                }
                #expect(SendStatusTransition.canTransition(from: fromState, to: toState) == false,
                        "\(fromState) → \(toState) should be disallowed")
            }
        }
    }

    @Test("sent → read is the only valid sent transition")
    func testSentOnlyGoesToRead() throws {
        for toState in DianeMessage.SendStatus.allCases where toState != .read {
            #expect(SendStatusTransition.canTransition(from: .sent, to: toState) == false,
                    "sent should only transition to read, not \(toState)")
        }
    }

    @Test("streaming is assistant-only, transitions to sent or failed")
    func testStreamingTransitions() throws {
        #expect(SendStatusTransition.canTransition(from: .streaming, to: .sent) == true)
        #expect(SendStatusTransition.canTransition(from: .streaming, to: .failed) == true)
        #expect(SendStatusTransition.canTransition(from: .streaming, to: .read) == false)
        #expect(SendStatusTransition.canTransition(from: .streaming, to: .sending) == false)
    }

    // MARK: - Model encoding/decoding

    @Test("DianeMessage encodes and decodes SendStatus correctly")
    func testMessageSendStatusEncoding() throws {
        let encoder = JSONEncoder()
        let decoder = JSONDecoder()

        for status in DianeMessage.SendStatus.allCases {
            let msg = DianeMessage(
                id: "test-\(status.rawValue)",
                role: "user",
                content: "hello",
                createdAt: DateUtils.formatISO8601(),
                sendStatus: status
            )

            let data = try encoder.encode(msg)
            let decoded = try decoder.decode(DianeMessage.self, from: data)

            #expect(decoded.id == msg.id)
            #expect(decoded.sendStatus == status)
            #expect(decoded.role == "user")
        }
    }

    @Test("DianeMessage encodes and decodes errorMessage correctly")
    func testMessageErrorEncoding() throws {
        let msg = DianeMessage(
            id: "test-error",
            role: "user",
            content: "original text",
            sendStatus: .failed,
            errorMessage: "Network error: connection refused"
        )

        let data = try JSONEncoder().encode(msg)
        let decoded = try JSONDecoder().decode(DianeMessage.self, from: data)

        #expect(decoded.sendStatus == .failed)
        #expect(decoded.errorMessage == "Network error: connection refused")
        #expect(decoded.content == "original text")
    }

    @Test("DianeMessage backward compatible — old format without sendStatus")
    func testMessageBackwardCompatible() throws {
        let oldJSON = """
        {"id": "old-msg", "role": "assistant", "content": "hello"}
        """.data(using: .utf8)!

        let decoded = try JSONDecoder().decode(DianeMessage.self, from: oldJSON)
        #expect(decoded.id == "old-msg")
        #expect(decoded.sendStatus == nil)  // old messages don't have it
        #expect(decoded.errorMessage == nil)
    }
}

// MARK: - State Machine Validation Helper

/// Validates ACP SSE event → SendStatus transitions
public enum SendStatusTransition {

    /// Returns true if the transition from `from` to `to` is valid
    public static func canTransition(from: DianeMessage.SendStatus, to: DianeMessage.SendStatus) -> Bool {
        switch (from, to) {
        // Forward flow
        case (.queued, .sending):   return true
        case (.sending, .sent):     return true
        case (.sent, .read):        return true

        // Error flow — can fail from any active state
        case (.queued, .failed):    return true
        case (.sending, .failed):   return true
        case (.sent, .failed):      return true
        case (.retrying, .failed):  return true
        case (.streaming, .failed): return true

        // Retry — failed → retrying → (back to sending)
        case (.failed, .retrying):  return true
        case (.retrying, .sending): return true

        // Streaming lifecycle
        case (.streaming, .sent):   return true

        // Queued → failed directly (offline, never attempted)
        case (.queued, .failed):    return true

        default: return false
        }
    }

    /// Returns true if the status is terminal (no further transitions expected)
    public static func isTerminal(_ status: DianeMessage.SendStatus) -> Bool {
        switch status {
        case .read, .failed: return true
        case .sent: return false  // sent → read is still expected
        default: return false
        }
    }

    /// Maps an ACP SSE event status string to the corresponding SendStatus transition
    public static func transitionForACPStatus(_ status: String) -> DianeMessage.SendStatus? {
        switch status {
        case "submitted":   return .sent     // run.created → message delivered
        case "working":     return .read     // run.in-progress → AI processing
        case "completed":   return .sent     // run.completed → assistant message done
        case "failed":      return .failed   // run.failed → error
        case "cancelled":   return .failed   // run.cancelled → stopped with error
        default:            return nil
        }
    }
}
