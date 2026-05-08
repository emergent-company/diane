import XCTest
import SwiftUI
@testable import Diane

// MARK: - SessionsView Formatting Logic

@MainActor
final class SessionsViewStatusColorTests: XCTestCase {
    let sut = SessionsView()

    func testStatusColorActiveGreen() {
        XCTAssertEqual(sut.statusColor("active"), .green)
    }

    func testStatusColorRunningGreen() {
        XCTAssertEqual(sut.statusColor("running"), .green)
    }

    func testStatusColorPausedOrange() {
        XCTAssertEqual(sut.statusColor("paused"), .orange)
    }

    func testStatusColorIdleOrange() {
        XCTAssertEqual(sut.statusColor("idle"), .orange)
    }

    func testStatusColorCompletedSecondary() {
        XCTAssertEqual(sut.statusColor("completed"), .secondary)
    }

    func testStatusColorClosedSecondary() {
        XCTAssertEqual(sut.statusColor("closed"), .secondary)
    }

    func testStatusColorDoneSecondary() {
        XCTAssertEqual(sut.statusColor("done"), .secondary)
    }

    func testStatusColorErrorRed() {
        XCTAssertEqual(sut.statusColor("error"), .red)
    }

    func testStatusColorFailedRed() {
        XCTAssertEqual(sut.statusColor("failed"), .red)
    }

    func testStatusColorUnknownDefaultsToSecondary() {
        XCTAssertEqual(sut.statusColor("unknown"), .secondary)
    }

    func testStatusColorCaseInsensitive() {
        XCTAssertEqual(sut.statusColor("ACTIVE"), .green)
        XCTAssertEqual(sut.statusColor("Error"), .red)
    }
}

@MainActor
final class SessionsViewSessionIDShortFormTests: XCTestCase {
    let sut = SessionsView()

    func testShortIdReturnsAsIs() {
        XCTAssertEqual(sut.sessionIDShortForm("abc123"), "abc123")
    }

    func testLongIdReturnsLast6Chars() {
        XCTAssertEqual(sut.sessionIDShortForm("session-abc123"), "abc123")
    }

    func testExact6CharIdReturnsAsIs() {
        XCTAssertEqual(sut.sessionIDShortForm("123456"), "123456")
    }

    func testEmptyIdReturnsEmpty() {
        XCTAssertEqual(sut.sessionIDShortForm(""), "")
    }
}

@MainActor
final class SessionsViewAgentShortNameTests: XCTestCase {
    let sut = SessionsView()

    func testDiscordPrefixStripped() {
        XCTAssertEqual(sut.agentShortName("discord-bot"), "bot")
    }

    func testDianePrefixStripped() {
        XCTAssertEqual(sut.agentShortName("diane-default"), "default")
    }

    func testAgentPrefixStripped() {
        XCTAssertEqual(sut.agentShortName("agent-researcher"), "researcher")
    }

    func testNoMatchingPrefixReturnsFullName() {
        XCTAssertEqual(sut.agentShortName("custom-agent"), "custom-agent")
    }

    func testEmptyNameReturnsEmpty() {
        XCTAssertEqual(sut.agentShortName(""), "")
    }

    func testOnlyPrefixReturnsEmpty() {
        XCTAssertEqual(sut.agentShortName("diane-"), "")
    }
}

@MainActor
final class SessionsViewBubbleColorTests: XCTestCase {
    let sut = SessionsView()

    func testUserBubbleBackground() {
        let color = sut.bubbleBackground(isUser: true, isSystem: false)
        XCTAssertEqual(color, Color.blue.opacity(0.12))
    }

    func testSystemBubbleBackgroundClear() {
        let color = sut.bubbleBackground(isUser: false, isSystem: true)
        XCTAssertEqual(color, Color.clear)
    }

    func testAssistantBubbleBackground() {
        let color = sut.bubbleBackground(isUser: false, isSystem: false)
        XCTAssertEqual(color, Color.primary.opacity(0.05))
    }

    func testUserBubbleTailColor() {
        let color = sut.bubbleTailColor(isUser: true, isSystem: false)
        XCTAssertEqual(color, Color.blue.opacity(0.12))
    }

    func testSystemBubbleTailColorClear() {
        let color = sut.bubbleTailColor(isUser: false, isSystem: true)
        XCTAssertEqual(color, Color.clear)
    }

    func testAssistantBubbleTailColor() {
        let color = sut.bubbleTailColor(isUser: false, isSystem: false)
        XCTAssertEqual(color, Color.primary.opacity(0.05))
    }

    func testUserSystemOverlapUserTakesPriority() {
        // When both true, isUser check fires first
        let bg = sut.bubbleBackground(isUser: true, isSystem: true)
        XCTAssertEqual(bg, Color.blue.opacity(0.12))
    }
}

@MainActor
final class SessionsViewFormatToolArgsTests: XCTestCase {
    let sut = SessionsView()

    func testValidJSONPrettyPrinted() {
        let raw = #"{"name":"test","value":42}"#
        let result = sut.formatToolArgs(raw)
        XCTAssertTrue(result.contains("\"name\""))
        XCTAssertTrue(result.contains("\"test\""))
    }

    func testInvalidJSONReturnsRaw() {
        let raw = "not-json-at-all"
        XCTAssertEqual(sut.formatToolArgs(raw), raw)
    }

    func testEmptyStringReturnsEmpty() {
        XCTAssertEqual(sut.formatToolArgs(""), "")
    }

    func testNestedJSONIndented() {
        let raw = #"{"outer":{"inner":true}}"#
        let result = sut.formatToolArgs(raw)
        // Sorted keys: inner before outer
        XCTAssertTrue(result.contains("\"inner\""))
        XCTAssertTrue(result.contains("\"outer\""))
    }

    func testArrayJSONFormatted() {
        let raw = #"[{"a":1},{"b":2}]"#
        let result = sut.formatToolArgs(raw)
        // Array is serialized with sorted keys
        XCTAssertTrue(result.contains("\"a\""))
        XCTAssertTrue(result.contains("\"b\""))
    }
}

@MainActor
final class SessionsViewRoleColorTests: XCTestCase {
    let sut = SessionsView()

    func testUserRoleBlue() {
        XCTAssertEqual(sut.roleColor("user"), .blue)
    }

    func testAssistantRoleGreen() {
        XCTAssertEqual(sut.roleColor("assistant"), .green)
    }

    func testSystemRoleOrange() {
        XCTAssertEqual(sut.roleColor("system"), .orange)
    }

    func testToolRolePurple() {
        XCTAssertEqual(sut.roleColor("tool"), .purple)
    }

    func testUnknownRoleSecondary() {
        XCTAssertEqual(sut.roleColor("unknown"), .secondary)
    }

    func testRoleColorCaseInsensitive() {
        XCTAssertEqual(sut.roleColor("User"), .blue)
        XCTAssertEqual(sut.roleColor("ASSISTANT"), .green)
    }
}

// MARK: - AgentsView Formatting Logic

@MainActor
final class AgentsViewAgentColorTests: XCTestCase {
    let sut = AgentsView()

    func testChatFlowGreen() {
        XCTAssertEqual(sut.agentColor("chat"), .green)
    }

    func testEmptyFlowGreen() {
        XCTAssertEqual(sut.agentColor(""), .green)
    }

    func testAgentFlowPurple() {
        XCTAssertEqual(sut.agentColor("agent"), .purple)
    }

    func testChainFlowOrange() {
        XCTAssertEqual(sut.agentColor("chain"), .orange)
    }

    func testWorkflowFlowBlue() {
        XCTAssertEqual(sut.agentColor("workflow"), .blue)
    }

    func testUnknownFlowSecondary() {
        XCTAssertEqual(sut.agentColor("custom"), .secondary)
    }

    func testAgentColorCaseInsensitive() {
        XCTAssertEqual(sut.agentColor("CHAT"), .green)
        XCTAssertEqual(sut.agentColor("Agent"), .purple)
    }
}

@MainActor
final class AgentsViewAgentIconTests: XCTestCase {
    let sut = AgentsView()

    func testChatIconMessage() {
        XCTAssertEqual(sut.agentIcon("chat"), "message")
    }

    func testEmptyIconMessage() {
        XCTAssertEqual(sut.agentIcon(""), "message")
    }

    func testAgentIconBrain() {
        XCTAssertEqual(sut.agentIcon("agent"), "brain.head.profile")
    }

    func testChainIconLink() {
        XCTAssertEqual(sut.agentIcon("chain"), "link")
    }

    func testWorkflowIconBranch() {
        XCTAssertEqual(sut.agentIcon("workflow"), "arrow.triangle.branch")
    }

    func testUnknownIconGearshape() {
        XCTAssertEqual(sut.agentIcon("custom"), "gearshape")
    }

    func testAgentIconCaseInsensitive() {
        XCTAssertEqual(sut.agentIcon("CHAIN"), "link")
    }
}

@MainActor
final class AgentsViewParseCommaListTests: XCTestCase {
    let sut = AgentsView()

    func testSimpleListSplits() {
        XCTAssertEqual(sut.parseCommaList("a,b,c"), ["a", "b", "c"])
    }

    func testListWithSpacesTrims() {
        XCTAssertEqual(sut.parseCommaList(" a , b , c "), ["a", "b", "c"])
    }

    func testEmptyStringReturnsEmptyArray() {
        XCTAssertEqual(sut.parseCommaList(""), [])
    }

    func testSingleItem() {
        XCTAssertEqual(sut.parseCommaList("hello"), ["hello"])
    }

    func testTrailingCommaFiltersEmpty() {
        XCTAssertEqual(sut.parseCommaList("a,b,"), ["a", "b"])
    }

    func testConsecutiveCommasFiltersEmpty() {
        XCTAssertEqual(sut.parseCommaList("a,,b"), ["a", "b"])
    }
}

// MARK: - RelayNodesView Formatting Logic

@MainActor
final class RelayNodesViewModeOrderTests: XCTestCase {
    let sut = RelayNodesView()

    func testMasterOrderZero() {
        XCTAssertEqual(ViewFormatting.modeOrder("master"), 0)
    }

    func testSlaveOrderOne() {
        XCTAssertEqual(ViewFormatting.modeOrder("slave"), 1)
    }

    func testNilOrderTwo() {
        XCTAssertEqual(ViewFormatting.modeOrder(nil), 2)
    }

    func testUnknownModeOrderTwo() {
        XCTAssertEqual(ViewFormatting.modeOrder("standalone"), 2)
    }
}

@MainActor
final class RelayNodesViewSortedNodesTests: XCTestCase {
    let sut = RelayNodesView()

    func testMasterFirstThenSlave() {
        let slave = RelayNode(instanceID: "slave-1", hostname: "b-host", mode: "slave",
                              version: nil, toolCount: nil, connectedAt: nil,
                              online: true, uptime: nil, provider: nil,
                              relayActive: nil, botActive: nil, healthy: nil)
        let master = RelayNode(instanceID: "master-1", hostname: "a-host", mode: "master",
                               version: nil, toolCount: nil, connectedAt: nil,
                               online: true, uptime: nil, provider: nil,
                               relayActive: nil, botActive: nil, healthy: nil)
        let sorted = ViewFormatting.sortedNodes([slave, master])
        XCTAssertEqual(sorted.first?.instanceID, "master-1")
        XCTAssertEqual(sorted.last?.instanceID, "slave-1")
    }

    func testSameModeSortedByHostname() {
        let alpha = RelayNode(instanceID: "node-2", hostname: "beta", mode: "slave",
                              version: nil, toolCount: nil, connectedAt: nil,
                              online: true, uptime: nil, provider: nil,
                              relayActive: nil, botActive: nil, healthy: nil)
        let beta = RelayNode(instanceID: "node-1", hostname: "alpha", mode: "slave",
                             version: nil, toolCount: nil, connectedAt: nil,
                             online: true, uptime: nil, provider: nil,
                             relayActive: nil, botActive: nil, healthy: nil)
        let sorted = ViewFormatting.sortedNodes([alpha, beta])
        XCTAssertEqual(sorted.first?.instanceID, "node-1")
        XCTAssertEqual(sorted.last?.instanceID, "node-2")
    }

    func testEmptyArrayReturnsEmpty() {
        XCTAssertTrue(ViewFormatting.sortedNodes([]).isEmpty)
    }

    func testSingleNodeReturnsSame() {
        let node = RelayNode(instanceID: "only", hostname: nil, mode: "master",
                             version: nil, toolCount: nil, connectedAt: nil,
                             online: true, uptime: nil, provider: nil,
                             relayActive: nil, botActive: nil, healthy: nil)
        let sorted = ViewFormatting.sortedNodes([node])
        XCTAssertEqual(sorted.count, 1)
        XCTAssertEqual(sorted.first?.instanceID, "only")
    }

    func testMasterSlaveAndOtherOrdered() {
        let other = RelayNode(instanceID: "other", hostname: "z-host", mode: nil,
                              version: nil, toolCount: nil, connectedAt: nil,
                              online: true, uptime: nil, provider: nil,
                              relayActive: nil, botActive: nil, healthy: nil)
        let slave = RelayNode(instanceID: "slave", hostname: "m-host", mode: "slave",
                              version: nil, toolCount: nil, connectedAt: nil,
                              online: true, uptime: nil, provider: nil,
                              relayActive: nil, botActive: nil, healthy: nil)
        let master = RelayNode(instanceID: "master", hostname: "a-host", mode: "master",
                               version: nil, toolCount: nil, connectedAt: nil,
                               online: true, uptime: nil, provider: nil,
                               relayActive: nil, botActive: nil, healthy: nil)
        let sorted = ViewFormatting.sortedNodes([other, slave, master])
        XCTAssertEqual(sorted[0].mode, "master")
        XCTAssertEqual(sorted[1].mode, "slave")
        XCTAssertNil(sorted[2].mode)
    }
}

// MARK: - NumberFormatting Dedup Verification

final class NumberFormattingSharedUsageTests: XCTestCase {
    func testFormatTokenCountViaModuleFunction() {
        // Verify the module-level function works (used by SessionsView after dedup)
        XCTAssertEqual(formatTokenCount(500), "500")
        XCTAssertEqual(formatTokenCount(1500), "1.5K")
        XCTAssertEqual(formatTokenCount(1_500_000), "1.5M")
    }

    func testFormatCostViaModuleFunction() {
        XCTAssertEqual(formatCost(0.5), "50.0¢")
        XCTAssertEqual(formatCost(1.0), "$1.000")
        XCTAssertEqual(formatCost(150.0), "$150.00")
    }

    func testFormatDurationViaModuleFunction() {
        XCTAssertEqual(formatDuration(500), "500ms")
        XCTAssertEqual(formatDuration(1500), "1.5s")
        XCTAssertEqual(formatDuration(90_000), "1.5m")
    }

    func testFormatCountViaModuleFunction() {
        XCTAssertEqual(formatCount(500), "500")
        XCTAssertEqual(formatCount(1500), "1.5K")
        XCTAssertEqual(formatCount(1_500_000), "1.5M")
    }
}
