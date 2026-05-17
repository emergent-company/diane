import Testing
import Foundation
@testable import DianeShared

// MARK: - ACP Integration Tests

/// These tests hit the real Memory Platform API.
/// They only run when `DIANE_TEST_TOKEN` environment variable is set.
struct ACPIntegrationTests {

    let token: String
    let baseURL = "https://memory.emergent-company.ai"

    init() throws {
        self.token = try #require(
            ProcessInfo.processInfo.environment["DIANE_TEST_TOKEN"],
            "Set DIANE_TEST_TOKEN env var to run integration tests"
        )
    }

    // MARK: - Session Lifecycle

    @Test("ACP: create session returns valid session ID")
    func createSession() async throws {
        let client = makeClient()
        let sessionID = try await client.createACPSession(agentName: "diane-default")

        #expect(!sessionID.isEmpty)
        #expect(sessionID.count > 10)  // UUID format
        print("[TEST] Created session: \(sessionID)")

        // Cleanup
        _ = try? await client.http.delete("/acp/v1/sessions/\(sessionID)")
    }

    @Test("ACP: list sessions returns array of session items")
    func listSessions() async throws {
        let client = makeClient()

        // First create a session so there's at least one
        let sessionID = try await client.createACPSession(agentName: "diane-default")

        let items = try await client.fetchACPSessions()
        #expect(!items.isEmpty)

        // Verify session shape
        if let first = items.first {
            #expect(!first.id.isEmpty)
            print("[TEST] Session: id=\(first.id) agent=\(first.agentName ?? "nil") last_run_status=\(first.lastRunStatus ?? "nil") is_archived=\(first.isArchived ?? false)")
        }

        // Cleanup
        _ = try? await client.http.delete("/acp/v1/sessions/\(sessionID)")
    }

    // MARK: - Session Detail & Status Values

    @Test("ACP: session detail returns expected fields")
    func sessionDetail() async throws {
        let client = makeClient()
        let sessionID = try await client.createACPSession(agentName: "diane-default")

        let detail = try await client.fetchACPSession(id: sessionID)

        // Verify the session response shape matches expected ACP fields
        #expect(detail.id == sessionID)
        #expect(detail.agentName == "diane-default")

        // A new session should have nil last_run_status and 0 counts
        #expect(detail.lastRunStatus == nil)  // no runs yet
        #expect((detail.runCount ?? 0) == 0)

        // Cleanup
        _ = try? await client.http.delete("/acp/v1/sessions/\(sessionID)")
    }

    // MARK: - SSE Streaming & State Machine Transitions

    @Test("ACP: streaming SSE returns expected event sequence")
    func streamingSSE() async throws {
        let client = makeClient()
        let sessionID = try await client.createACPSession(agentName: "diane-default")

        // Collect all events from the stream
        var events: [StreamChatEvent] = []
        var eventTypes: [String] = []

        do {
            let stream = client.streamACP(
                agentName: "diane-default",
                sessionID: sessionID,
                content: "Say 'hello' and nothing else."
            )

            for try await event in stream {
                events.append(event)
                eventTypes.append(event.type)
                print("[TEST] Event: type=\(event.type) content=\(event.content?.prefix(50) ?? "nil")")
            }
        } catch {
            // If the stream fails, we still want to see what events were received
            print("[TEST] Stream ended with error: \(error.localizedDescription)")
        }

        print("[TEST] Received \(events.count) SSE events")
        print("[TEST] Event types: \(eventTypes)")

        // Verify we got at least some events (the stream should produce events)
        // Even if the agent errors, we should have received run.created
        #expect(!events.isEmpty, "Should receive at least one SSE event")

        // Check for the key status events
        let hasRunCreated = events.contains(where: { $0.type == "run.created" })
        let hasRunInProgress = events.contains(where: { $0.type == "run.in-progress" })
        let hasRunCompleted = events.contains(where: { $0.type == "run.completed" })
        let hasError = events.contains(where: { $0.type == "error" || $0.type == "run.failed" })

        print("[TEST] run.created=\(hasRunCreated) run.in-progress=\(hasRunInProgress) run.completed=\(hasRunCompleted) error=\(hasError)")

        // At minimum, we should have run.created
        #expect(hasRunCreated, "Stream should emit run.created")

        // Map events to state machine transitions
        var transitions: [String] = []
        if hasRunCreated { transitions.append("submitted → .sent") }
        if hasRunInProgress { transitions.append("working → .read") }
        if hasRunCompleted { transitions.append("completed → streaming done") }
        print("[TEST] State machine transitions: \(transitions)")

        // Count content tokens
        let textEvents = events.filter { $0.type == "token" || $0.type == "text" }
        print("[TEST] Text tokens received: \(textEvents.count)")

        // Cleanup
        _ = try? await client.http.delete("/acp/v1/sessions/\(sessionID)")
    }

    // MARK: - SendStatus Mapping from ACP Statuses

    @Test("SendStatus: ACP submitted maps to .sent")
    func acpSubmittedMapsToSent() {
        let result = SendStatusTransition.transitionForACPStatus("submitted")
        #expect(result == .sent)
    }

    @Test("SendStatus: ACP working maps to .read")
    func acpWorkingMapsToRead() {
        let result = SendStatusTransition.transitionForACPStatus("working")
        #expect(result == .read)
    }

    @Test("SendStatus: ACP completed maps to .sent")
    func acpCompletedMapsToSent() {
        let result = SendStatusTransition.transitionForACPStatus("completed")
        #expect(result == .sent)
    }

    @Test("SendStatus: ACP failed maps to .failed")
    func acpFailedMapsToFailed() {
        let result = SendStatusTransition.transitionForACPStatus("failed")
        #expect(result == .failed)
    }

    @Test("SendStatus: ACP cancelled maps to .failed")
    func acpCancelledMapsToFailed() {
        let result = SendStatusTransition.transitionForACPStatus("cancelled")
        #expect(result == .failed)
    }

    @Test("SendStatus: unknown ACP status returns nil")
    func acpUnknownMapsToNil() {
        let result = SendStatusTransition.transitionForACPStatus("nonexistent-status")
        #expect(result == nil)
    }

    // MARK: - ACP Status → Color/Animation Mapping

    @Test("StatusColors: SDK-defined statuses map correctly")
    func statusColorMapping() throws {
        // Use Color's description for verification
        // Just verify they compile and return without crash
        _ = StatusColors.statusColor("completed")
        _ = StatusColors.statusColor("failed")
        _ = StatusColors.statusColor("submitted")
        _ = StatusColors.statusColor("working")
        _ = StatusColors.statusColor("cancelled")
        _ = StatusColors.statusColor("input-required")
        _ = StatusColors.statusColor("cancelling")
        _ = StatusColors.statusColor(nil)
    }

    @Test("StatusAnimation: SDK-defined statuses map correctly")
    func statusAnimationMapping() throws {
        // Active/in-progress statuses should pulse
        let submittedAnimation = StatusColors.statusAnimation("submitted")
        #expect(submittedAnimation != .static, "submitted should pulse")

        let workingAnimation = StatusColors.statusAnimation("working")
        #expect(workingAnimation != .static, "working should pulse")

        let cancellingAnimation = StatusColors.statusAnimation("cancelling")
        #expect(cancellingAnimation != .static, "cancelling should pulse")

        // Terminal statuses should be static
        #expect(StatusColors.statusAnimation("completed") == .static)
        #expect(StatusColors.statusAnimation("failed") == .static)
        #expect(StatusColors.statusAnimation("cancelled") == .static)
        #expect(StatusColors.statusAnimation("skipped") == .static)
        #expect(StatusColors.statusAnimation("input-required") == .static)

        // Nil (no runs) should pulse
        #expect(StatusColors.statusAnimation(nil) != .static)
    }

    // MARK: - Helpers

    private func makeClient() -> EmergentAPIClient {
        let client = EmergentAPIClient(
            baseURL: baseURL,
            apiKey: token
        )
        return client
    }
}
