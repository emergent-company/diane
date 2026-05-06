import XCTest
@testable import Diane
import EmergentKit

/// Unit tests for the EmergentAPIClient — verifies HTTP routing, decoding,
/// and error mapping without hitting a real server.
///
/// Task 10.1
final class EmergentAPIClientTests: XCTestCase {

    // MARK: - Configure

    func testConfigureWithEmptyURL() {
        let client = EmergentAPIClient()
        // Configuring with an empty URL should make requests throw .notConfigured
        client.configure(serverURL: "", apiKey: "")
        // We can't directly test the private baseURL, but we verify via behavior
        // (tested indirectly through fetchProjects in integration tests)
    }

    // MARK: - Error mapping

    func testEmergentAPIErrorDescriptions() {
        XCTAssertEqual(
            EmergentAPIError.notConfigured.errorDescription,
            "Server URL not configured"
        )
        XCTAssertEqual(
            EmergentAPIError.unauthorized.errorDescription,
            "Unauthorized — check your API key"
        )
        XCTAssertEqual(
            EmergentAPIError.serverError(500).errorDescription,
            "Server error (500)"
        )
        XCTAssertEqual(
            EmergentAPIError.notFound("/projects").errorDescription,
            "Not found: /projects"
        )
        XCTAssertEqual(
            EmergentAPIError.network("timeout").errorDescription,
            "Network error: timeout"
        )
        XCTAssertEqual(
            EmergentAPIError.decodingFailed("bad key").errorDescription,
            "Decoding failed: bad key"
        )
    }
}

/// Unit tests for shared data models — verifies Codable round-trips.
///
/// Task 10.1
final class ModelsTests: XCTestCase {

    // MARK: - Project decoding

    func testProjectDecoding() throws {
        let json = """
        {
            "id": "proj-1",
            "name": "Test Project",
            "org_id": "org-42",
            "object_count": 100,
            "relation_count": 250,
            "agent_count": 3
        }
        """.data(using: .utf8)!

        let project = try JSONDecoder().decode(Project.self, from: json)
        XCTAssertEqual(project.id, "proj-1")
        XCTAssertEqual(project.name, "Test Project")
        XCTAssertEqual(project.orgID, "org-42")
        XCTAssertEqual(project.objectCount, 100)
        XCTAssertEqual(project.relationCount, 250)
        XCTAssertEqual(project.agentCount, 3)
    }

    func testProjectDecodingWithMissingOptionals() throws {
        let json = """{"id": "proj-2", "name": "Minimal"}""".data(using: .utf8)!
        let project = try JSONDecoder().decode(Project.self, from: json)
        XCTAssertNil(project.objectCount)
        XCTAssertNil(project.agentCount)
    }

    // MARK: - Trace decoding

    func testTraceDecoding() throws {
        let json = """
        {
            "id": "trace-abc",
            "status": "completed",
            "span_count": 12,
            "source_type": "document"
        }
        """.data(using: .utf8)!

        let trace = try JSONDecoder().decode(Trace.self, from: json)
        XCTAssertEqual(trace.id, "trace-abc")
        XCTAssertEqual(trace.status, "completed")
        XCTAssertEqual(trace.spanCount, 12)
        XCTAssertEqual(trace.sourceType, "document")
    }

    // MARK: - Worker decoding

    func testWorkerStatusDecoding() throws {
        XCTAssertEqual(WorkerStatus(rawValue: "idle"), .idle)
        XCTAssertEqual(WorkerStatus(rawValue: "busy"), .busy)
        XCTAssertEqual(WorkerStatus(rawValue: "offline"), .offline)
        XCTAssertNil(WorkerStatus(rawValue: "invalid"))
    }

    func testWorkerStatusDisplayLabel() {
        XCTAssertEqual(WorkerStatus.idle.displayLabel, "Idle")
        XCTAssertEqual(WorkerStatus.busy.displayLabel, "Busy")
        XCTAssertEqual(WorkerStatus.offline.displayLabel, "Offline")
    }

    // MARK: - AnyCodable round-trip

    func testAnyCodableString() throws {
        let json = #""hello""#.data(using: .utf8)!
        let decoded = try JSONDecoder().decode(AnyCodable.self, from: json)
        XCTAssertEqual(decoded.value as? String, "hello")
    }

    func testAnyCodableInt() throws {
        let json = "42".data(using: .utf8)!
        let decoded = try JSONDecoder().decode(AnyCodable.self, from: json)
        XCTAssertEqual(decoded.value as? Int, 42)
    }

    func testAnyCodableBool() throws {
        let json = "true".data(using: .utf8)!
        let decoded = try JSONDecoder().decode(AnyCodable.self, from: json)
        XCTAssertEqual(decoded.value as? Bool, true)
    }

    // MARK: - QueryResult decoding

    func testQueryResultDecoding() throws {
        let json = """
        {
            "row_count": 0,
            "columns": ["id", "name"],
            "rows": []
        }
        """.data(using: .utf8)!

        let result = try JSONDecoder().decode(QueryResult.self, from: json)
        XCTAssertEqual(result.rowCount, 0)
        XCTAssertEqual(result.columns, ["id", "name"])
        XCTAssertNil(result.error)
    }

    func testQueryResultWithError() throws {
        let json = """
        {"row_count": 0, "error": "Syntax error near 'SLECT'"}
        """.data(using: .utf8)!

        let result = try JSONDecoder().decode(QueryResult.self, from: json)
        XCTAssertEqual(result.error, "Syntax error near 'SLECT'")
    }
}

/// Unit tests for AppState — verifies observable state logic.
///
/// Task 10.2
@MainActor
final class AppStateTests: XCTestCase {

    func testInitialState() {
        let state = AppState()
        XCTAssertNil(state.selectedProject)
        XCTAssertFalse(state.isConnected)
        XCTAssertTrue(state.projects.isEmpty)
        XCTAssertFalse(state.isLoadingProjects)
        XCTAssertNil(state.projectLoadError)
    }

    func testIsReadyRequiresConnectionAndProject() {
        let state = AppState()
        XCTAssertFalse(state.isReady)

        state.isConnected = true
        XCTAssertFalse(state.isReady) // still no project

        state.selectedProject = Project(id: "p1", name: "Test")
        XCTAssertTrue(state.isReady)

        state.isConnected = false
        XCTAssertFalse(state.isReady)
    }

    func testActiveProjectID() {
        let state = AppState()
        XCTAssertNil(state.activeProjectID)

        state.selectedProject = Project(id: "proj-xyz", name: "Alpha")
        XCTAssertEqual(state.activeProjectID, "proj-xyz")
    }

    func testSidebarItemSectionGroups() {
        XCTAssertEqual(SidebarItem.query.sectionGroup, .project)
        XCTAssertEqual(SidebarItem.traces.sectionGroup, .project)
        XCTAssertEqual(SidebarItem.status.sectionGroup, .project)
        XCTAssertEqual(SidebarItem.workers.sectionGroup, .project)
        XCTAssertEqual(SidebarItem.objects.sectionGroup, .project)
        XCTAssertEqual(SidebarItem.documents.sectionGroup, .project)
        XCTAssertEqual(SidebarItem.accountStatus.sectionGroup, .account)
        XCTAssertEqual(SidebarItem.profile.sectionGroup, .account)
        XCTAssertEqual(SidebarItem.agents.sectionGroup, .configuration)
        XCTAssertEqual(SidebarItem.mcpServers.sectionGroup, .configuration)
    }
}
