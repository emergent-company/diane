import XCTest
import SwiftUI
@testable import Diane

// MARK: - SystemView

@MainActor
final class SystemViewDoctorStatusColorTests: XCTestCase {
    let sut = SystemView()

    func testOkGreen() { XCTAssertEqual(sut.doctorStatusColor("ok"), .green) }
    func testWarningOrange() { XCTAssertEqual(sut.doctorStatusColor("warning"), .orange) }
    func testErrorRed() { XCTAssertEqual(sut.doctorStatusColor("error"), .red) }
    func testUnknownSecondary() { XCTAssertEqual(sut.doctorStatusColor("unknown"), .secondary) }
}

@MainActor
final class SystemViewFriendlyDateTests: XCTestCase {
    let sut = SystemView()

    func testISODateWithFractionalSeconds() {
        let result = sut.friendlyDate("2026-05-07T23:30:00.000Z")
        // Should produce formatted date like "May 7, 2026  11:30 PM"
        XCTAssertTrue(result.contains("May"))
        XCTAssertTrue(result.contains("2026"))
    }

    func testISODateWithoutFractionalSeconds() {
        let result = sut.friendlyDate("2026-05-07T10:00:00Z")
        XCTAssertTrue(result.contains("May"))
    }

    func testInvalidDateReturnsRaw() {
        XCTAssertEqual(sut.friendlyDate("not-a-date"), "not-a-date")
    }

    func testEmptyStringReturnsEmpty() {
        XCTAssertEqual(sut.friendlyDate(""), "")
    }

    func testDateOverload() {
        let date = Date(timeIntervalSince1970: 0)  // Jan 1, 1970
        let result = sut.friendlyDate(date)
        XCTAssertTrue(result.contains("1970"))
    }
}

// MARK: - ProvidersView

@MainActor
final class ProvidersViewDisplayNameTests: XCTestCase {
    let sut = ProvidersView()

    func testGoogleAIName() { XCTAssertEqual(sut.providerDisplayName("google-ai"), "Google AI") }
    func testVertexAIName() { XCTAssertEqual(sut.providerDisplayName("vertex-ai"), "Vertex AI") }
    func testOtherNameReturnsRaw() { XCTAssertEqual(sut.providerDisplayName("openai"), "openai") }
    func testEmptyNameReturnsEmpty() { XCTAssertEqual(sut.providerDisplayName(""), "") }
}

@MainActor
final class ProvidersViewPolicyLabelTests: XCTestCase {
    let sut = ProvidersView()

    func testNonePolicy() { XCTAssertEqual(sut.policyDisplayLabel("none"), "None") }
    func testOrganizationPolicy() { XCTAssertEqual(sut.policyDisplayLabel("organization"), "Organization") }
    func testProjectPolicy() { XCTAssertEqual(sut.policyDisplayLabel("project"), "Project-specific") }
    func testUnknownPolicyReturnsRaw() { XCTAssertEqual(sut.policyDisplayLabel("custom"), "custom") }
}

// MARK: - SchemaTypeDetailView

@MainActor
final class SchemaTypeDetailViewShortNameTests: XCTestCase {
    let sut = SchemaTypeDetailView(
        type: SchemaNodeType(typeName: "Person", label: "Person", description: "", namespace: nil, properties: [], objectCount: 0, relationshipCount: 0),
        allRelationships: [],
        dianeAPI: DianeAPIClient()
    )

    func testCalendarPrefixStripped() { XCTAssertEqual(sut.shortTypeName("CalendarEvent"), "Event") }
    func testFinancialPrefixStripped() { XCTAssertEqual(sut.shortTypeName("FinancialTransaction"), "Transaction") }
    func testShoppingPrefixStripped() { XCTAssertEqual(sut.shortTypeName("ShoppingItem"), "Item") }
    func testNoPrefixReturnsFull() { XCTAssertEqual(sut.shortTypeName("Person"), "Person") }
    func testEmptyNameReturnsEmpty() { XCTAssertEqual(sut.shortTypeName(""), "") }
    func testOnlyPrefixReturnsEmpty() { XCTAssertEqual(sut.shortTypeName("Shopping"), "") }
}

@MainActor
final class SchemaTypeDetailViewNamespaceTests: XCTestCase {
    let sut = SchemaTypeDetailView(
        type: SchemaNodeType(typeName: "Person", label: "Person", description: "", namespace: nil, properties: [], objectCount: 0, relationshipCount: 0),
        allRelationships: [],
        dianeAPI: DianeAPIClient()
    )

    func testDianePrefixIsSystem() { XCTAssertEqual(sut.typeNamespace("DianeDefault"), "system") }
    func testSkillMonitorIsSystem() { XCTAssertEqual(sut.typeNamespace("SkillMonitorCheckpoint"), "system") }
    func testPersonIsPersonal() { XCTAssertEqual(sut.typeNamespace("Person"), "personal") }
    func testMemoryFactIsPersonal() { XCTAssertEqual(sut.typeNamespace("MemoryFact"), "personal") }
    func testUnknownIsPersonal() { XCTAssertEqual(sut.typeNamespace("RandomType"), "personal") }

    func testDianePrefixColorPurple() { XCTAssertEqual(sut.typeNamespaceColor("DianeDefault"), Color.purple) }
    func testPersonalColorBlue() { XCTAssertEqual(sut.typeNamespaceColor("Person"), Color.blue) }
}

@MainActor
final class SchemaTypeDetailViewStatusColorTests: XCTestCase {
    let sut = SchemaTypeDetailView(
        type: SchemaNodeType(typeName: "Person", label: "Person", description: "", namespace: nil, properties: [], objectCount: 0, relationshipCount: 0),
        allRelationships: [],
        dianeAPI: DianeAPIClient()
    )

    func testActiveGreen() { XCTAssertEqual(sut.statusColor("active"), .green) }
    func testOpenGreen() { XCTAssertEqual(sut.statusColor("open"), .green) }
    func testInactiveGray() { XCTAssertEqual(sut.statusColor("inactive"), .gray) }
    func testClosedGray() { XCTAssertEqual(sut.statusColor("closed"), .gray) }
    func testErrorRed() { XCTAssertEqual(sut.statusColor("error"), .red) }
    func testFailedRed() { XCTAssertEqual(sut.statusColor("failed"), .red) }
    func testUnknownSecondary() { XCTAssertEqual(sut.statusColor("unknown"), .secondary) }
    func testCaseInsensitive() { XCTAssertEqual(sut.statusColor("ACTIVE"), .green) }
}

@MainActor
final class SchemaTypeDetailViewFormatDateTests: XCTestCase {
    let sut = SchemaTypeDetailView(
        type: SchemaNodeType(typeName: "Person", label: "Person", description: "", namespace: nil, properties: [], objectCount: 0, relationshipCount: 0),
        allRelationships: [],
        dianeAPI: DianeAPIClient()
    )

    func testISOWithFractionalSeconds() {
        let result = sut.formatDate("2026-05-07T23:30:00.000Z")
        XCTAssertTrue(result.contains("2026"))
        XCTAssertTrue(result.contains("May"))
    }

    func testISOWithoutFractional() {
        let result = sut.formatDate("2026-05-07T10:00:00Z")
        XCTAssertTrue(result.contains("2026"))
    }

    func testInvalidDateReturnsRaw() {
        XCTAssertEqual(sut.formatDate("bad-date"), "bad-date")
    }

    func testEmptyStringReturnsEmpty() {
        XCTAssertEqual(sut.formatDate(""), "")
    }
}

// MARK: - TracesView

@MainActor
final class TracesViewStatusColorTests: XCTestCase {
    let sut = TracesView()

    func testCompletedGreen() { XCTAssertEqual(sut.statusColor(for: "completed"), .green) }
    func testSuccessGreen() { XCTAssertEqual(sut.statusColor(for: "success"), .green) }
    func testRunningBlue() { XCTAssertEqual(sut.statusColor(for: "running"), .blue) }
    func testProcessingBlue() { XCTAssertEqual(sut.statusColor(for: "processing"), .blue) }
    func testFailedRed() { XCTAssertEqual(sut.statusColor(for: "failed"), .red) }
    func testErrorRed() { XCTAssertEqual(sut.statusColor(for: "error"), .red) }
    func testPendingOrange() { XCTAssertEqual(sut.statusColor(for: "pending"), .orange) }
    func testQueuedOrange() { XCTAssertEqual(sut.statusColor(for: "queued"), .orange) }
    func testUnknownSecondary() { XCTAssertEqual(sut.statusColor(for: "unknown"), .secondary) }
    func testCaseInsensitive() { XCTAssertEqual(sut.statusColor(for: "COMPLETED"), .green) }
}

@MainActor
final class TracesViewStatusBackgroundTests: XCTestCase {
    let sut = TracesView()

    func testCompletedBackground() {
        let bg = sut.statusBackground(for: "completed")
        XCTAssertEqual(bg, Color.green.opacity(0.15))
    }
    func testFailedBackground() {
        let bg = sut.statusBackground(for: "error")
        XCTAssertEqual(bg, Color.red.opacity(0.15))
    }
    func testUnknownBackground() {
        let bg = sut.statusBackground(for: "unknown")
        XCTAssertEqual(bg, Color.secondary.opacity(0.15))
    }
}

// MARK: - QueryView

@MainActor
final class QueryViewPropStringTests: XCTestCase {
    let sut = QueryView()

    func testNilReturnsDash() { XCTAssertEqual(sut.propString(nil), "—") }
    func testStringValue() { XCTAssertEqual(sut.propString(AnyCodable("hello")), "hello") }
    func testIntValue() { XCTAssertEqual(sut.propString(AnyCodable(42)), "42") }
    func testDoubleValue() { XCTAssertEqual(sut.propString(AnyCodable(3.14)), "3.14") }
    func testBoolTrue() { XCTAssertEqual(sut.propString(AnyCodable(true)), "true") }
    func testBoolFalse() { XCTAssertEqual(sut.propString(AnyCodable(false)), "false") }
    func testEmptyStringReturnsEmpty() { XCTAssertEqual(sut.propString(AnyCodable("")), "") }
}

// MARK: - ProfileView

@MainActor
final class ProfileViewInitialsTests: XCTestCase {
    let sut = ProfileView()

    func testFullName() { XCTAssertEqual(sut.initials(for: "John Doe"), "JD") }
    func testSingleName() { XCTAssertEqual(sut.initials(for: "John"), "JO") }
    func testNilReturnsQuestion() { XCTAssertEqual(sut.initials(for: nil), "?") }
    func testEmptyReturnsQuestion() { XCTAssertEqual(sut.initials(for: ""), "?") }
    func testTripleName() { XCTAssertEqual(sut.initials(for: "John Michael Doe"), "JM") }
    func testLowercaseInput() { XCTAssertEqual(sut.initials(for: "john doe"), "JD") }
}

@MainActor
final class ProfileViewMaskedKeyTests: XCTestCase {
    let sut = ProfileView()

    func testAPIKeyMasked() { XCTAssertEqual(sut.maskedKey("sk-abc123def456"), "****f456") }
    func testShortKey() { XCTAssertEqual(sut.maskedKey("ab"), "****ab") }
    func testEmptyKey() { XCTAssertEqual(sut.maskedKey(""), "****") }
    func testExact4Chars() { XCTAssertEqual(sut.maskedKey("abcd"), "****abcd") }
}
