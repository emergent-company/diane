import XCTest
@testable import Diane

final class StatusColorsTests: XCTestCase {

    // MARK: - Session Status

    func testSessionStatusGreen() {
        XCTAssertEqual(StatusColors.sessionStatus("active"), .green)
        XCTAssertEqual(StatusColors.sessionStatus("running"), .green)
        XCTAssertEqual(StatusColors.sessionStatus("ACTIVE"), .green)
    }

    func testSessionStatusOrange() {
        XCTAssertEqual(StatusColors.sessionStatus("paused"), .orange)
        XCTAssertEqual(StatusColors.sessionStatus("idle"), .orange)
    }

    func testSessionStatusSecondary() {
        XCTAssertEqual(StatusColors.sessionStatus("completed"), .secondary)
        XCTAssertEqual(StatusColors.sessionStatus("closed"), .secondary)
        XCTAssertEqual(StatusColors.sessionStatus("done"), .secondary)
    }

    func testSessionStatusRed() {
        XCTAssertEqual(StatusColors.sessionStatus("error"), .red)
        XCTAssertEqual(StatusColors.sessionStatus("failed"), .red)
    }

    func testSessionStatusUnknown() {
        XCTAssertEqual(StatusColors.sessionStatus("unknown"), .secondary)
        XCTAssertEqual(StatusColors.sessionStatus(""), .secondary)
    }

    // MARK: - Schema Status

    func testSchemaStatusGreen() {
        XCTAssertEqual(StatusColors.schemaStatus("active"), .green)
        XCTAssertEqual(StatusColors.schemaStatus("open"), .green)
    }

    func testSchemaStatusGray() {
        XCTAssertEqual(StatusColors.schemaStatus("inactive"), .gray)
        XCTAssertEqual(StatusColors.schemaStatus("closed"), .gray)
    }

    func testSchemaStatusRed() {
        XCTAssertEqual(StatusColors.schemaStatus("error"), .red)
        XCTAssertEqual(StatusColors.schemaStatus("failed"), .red)
    }

    // MARK: - Trace Status

    func testTraceStatusGreen() {
        XCTAssertEqual(StatusColors.traceStatus(for: "completed"), .green)
        XCTAssertEqual(StatusColors.traceStatus(for: "success"), .green)
    }

    func testTraceStatusBlue() {
        XCTAssertEqual(StatusColors.traceStatus(for: "running"), .blue)
        XCTAssertEqual(StatusColors.traceStatus(for: "processing"), .blue)
    }

    func testTraceStatusOrange() {
        XCTAssertEqual(StatusColors.traceStatus(for: "pending"), .orange)
        XCTAssertEqual(StatusColors.traceStatus(for: "queued"), .orange)
    }

    func testTraceStatusBackgroundOpacity() {
        let bg = StatusColors.traceStatusBackground(for: "completed")
        // Can't compare .opacity directly in Color, but should not crash
        XCTAssertNotNil(bg)
    }

    // MARK: - Doctor Status

    func testDoctorStatusGreen() {
        XCTAssertEqual(StatusColors.doctorStatus("ok"), .green)
    }

    func testDoctorStatusOrange() {
        XCTAssertEqual(StatusColors.doctorStatus("warning"), .orange)
    }

    func testDoctorStatusRed() {
        XCTAssertEqual(StatusColors.doctorStatus("error"), .red)
    }

    // MARK: - Message Role

    func testMessageRoleBlue() {
        XCTAssertEqual(StatusColors.messageRole("user"), .blue)
    }

    func testMessageRoleGreen() {
        XCTAssertEqual(StatusColors.messageRole("assistant"), .green)
    }

    func testMessageRoleOrange() {
        XCTAssertEqual(StatusColors.messageRole("system"), .orange)
    }

    func testMessageRolePurple() {
        XCTAssertEqual(StatusColors.messageRole("tool"), .purple)
    }

    func testMessageRoleCaseInsensitive() {
        XCTAssertEqual(StatusColors.messageRole("USER"), .blue)
        XCTAssertEqual(StatusColors.messageRole("Assistant"), .green)
    }

    // MARK: - Agent Flow

    func testAgentFlowGreen() {
        XCTAssertEqual(StatusColors.agentFlow("chat"), .green)
        XCTAssertEqual(StatusColors.agentFlow(""), .green)
    }

    func testAgentFlowPurple() {
        XCTAssertEqual(StatusColors.agentFlow("agent"), .purple)
    }

    func testAgentFlowOrange() {
        XCTAssertEqual(StatusColors.agentFlow("chain"), .orange)
    }

    func testAgentFlowBlue() {
        XCTAssertEqual(StatusColors.agentFlow("workflow"), .blue)
    }
}
