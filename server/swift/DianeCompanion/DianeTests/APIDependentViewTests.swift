import XCTest
@testable import Diane

// MARK: - Mock API Client

@MainActor
final class MockEmergentAPIClient: EmergentAPIClient {
    var accountStatsResult: Result<AccountStats, Error>?
    var diagnosticsResult: Result<ServerDiagnostics, Error>?
    var searchObjectsResult: Result<[GraphObject], Error>?

    override func fetchAccountStats() async throws -> AccountStats {
        try accountStatsResult!.get()
    }

    override func fetchDiagnostics() async throws -> ServerDiagnostics {
        try diagnosticsResult!.get()
    }

    override func searchObjects(projectID: String, query: String, limit: Int) async throws -> [GraphObject] {
        try searchObjectsResult!.get()
    }
}

// MARK: - AccountStatusView

@MainActor
final class AccountStatusViewFetchStatsTests: XCTestCase {

    func testSuccessReturnsStatsAndNoError() async {
        let mock = MockEmergentAPIClient()
        mock.accountStatsResult = .success(AccountStats(
            serverURL: "http://localhost:8890", serverVersion: "1.0.0",
            latencyMs: 5.0, totalProjects: 3, totalObjects: 150,
            totalRelations: 420, totalApiRequests: 1000, avgLatencyMs: 2.5
        ))

        let result = await AccountStatusView.fetchAccountStats(client: mock)

        XCTAssertNotNil(result.stats)
        XCTAssertEqual(result.stats?.serverURL, "http://localhost:8890")
        XCTAssertEqual(result.stats?.serverVersion, "1.0.0")
        XCTAssertEqual(result.stats?.totalProjects, 3)
        XCTAssertEqual(result.stats?.totalObjects, 150)
        XCTAssertEqual(result.stats?.totalRelations, 420)
        XCTAssertEqual(result.stats?.totalApiRequests, 1000)
        XCTAssertNil(result.error)
    }

    func testFailureReturnsErrorAndNoStats() async {
        let mock = MockEmergentAPIClient()
        mock.accountStatsResult = .failure(NSError(domain: "", code: -1,
            userInfo: [NSLocalizedDescriptionKey: "Network error"]))

        let result = await AccountStatusView.fetchAccountStats(client: mock)

        XCTAssertNil(result.stats)
        XCTAssertEqual(result.error, "Network error")
    }

    func testMinimalStats() async {
        let mock = MockEmergentAPIClient()
        mock.accountStatsResult = .success(AccountStats(
            serverURL: "", serverVersion: nil, latencyMs: nil,
            totalProjects: 0, totalObjects: 0, totalRelations: 0,
            totalApiRequests: 0, avgLatencyMs: nil
        ))

        let result = await AccountStatusView.fetchAccountStats(client: mock)

        XCTAssertNotNil(result.stats)
        XCTAssertNil(result.stats?.serverVersion)
        XCTAssertEqual(result.stats?.totalProjects, 0)
        XCTAssertNil(result.error)
    }
}

// MARK: - WorkersView

@MainActor
final class WorkersViewFetchDiagnosticsTests: XCTestCase {

    func testSuccessReturnsDiagnostics() async {
        let mock = MockEmergentAPIClient()
        mock.diagnosticsResult = .success(ServerDiagnostics(
            timestamp: "2026-05-07T23:00:00Z", uptime: "5d",
            server: ServerDiagnostics.ServerInfo(version: "1.0.0", environment: "production"),
            database: ServerDiagnostics.DatabaseInfo(
                pool: ServerDiagnostics.DatabaseInfo.DBPool(totalConns: 10, idleConns: 3, maxConns: 20)
            )
        ))

        let result = await WorkersView.fetchDiagnostics(client: mock)

        XCTAssertNotNil(result.diagnostics)
        XCTAssertEqual(result.diagnostics?.server?.version, "1.0.0")
        XCTAssertEqual(result.diagnostics?.uptime, "5d")
        XCTAssertEqual(result.diagnostics?.database?.pool?.totalConns, 10)
        XCTAssertNil(result.error)
    }

    func testFailureReturnsError() async {
        let mock = MockEmergentAPIClient()
        mock.diagnosticsResult = .failure(NSError(domain: "", code: -1,
            userInfo: [NSLocalizedDescriptionKey: "Connection failed"]))

        let result = await WorkersView.fetchDiagnostics(client: mock)

        XCTAssertNil(result.diagnostics)
        XCTAssertEqual(result.error, "Connection failed")
    }

    func testNillableFields() async {
        let mock = MockEmergentAPIClient()
        mock.diagnosticsResult = .success(ServerDiagnostics(
            timestamp: nil, uptime: nil, server: nil, database: nil
        ))

        let result = await WorkersView.fetchDiagnostics(client: mock)

        XCTAssertNotNil(result.diagnostics)
        XCTAssertNil(result.diagnostics?.server)
        XCTAssertNil(result.diagnostics?.database)
        XCTAssertNil(result.error)
    }
}

// MARK: - ObjectsBrowserView

@MainActor
final class ObjectsBrowserViewSearchTests: XCTestCase {

    func testSuccessReturnsObjects() async {
        let mock = MockEmergentAPIClient()
        mock.searchObjectsResult = .success([
            GraphObject(id: "obj-1", type: "Person", score: 0.95, properties: nil, createdAt: nil),
            GraphObject(id: "obj-2", type: "Note", score: 0.80, properties: nil, createdAt: nil)
        ])

        let result = await ObjectsBrowserView.searchObjects(client: mock, projectID: "proj-1", query: "test")

        XCTAssertEqual(result.objects.count, 2)
        XCTAssertEqual(result.objects[0].id, "obj-1")
        XCTAssertEqual(result.objects[1].type, "Note")
        XCTAssertNil(result.error)
    }

    func testEmptyResults() async {
        let mock = MockEmergentAPIClient()
        mock.searchObjectsResult = .success([])

        let result = await ObjectsBrowserView.searchObjects(client: mock, projectID: "proj-1", query: "nothing")

        XCTAssertTrue(result.objects.isEmpty)
        XCTAssertNil(result.error)
    }

    func testFailureReturnsError() async {
        let mock = MockEmergentAPIClient()
        mock.searchObjectsResult = .failure(NSError(domain: "", code: -1,
            userInfo: [NSLocalizedDescriptionKey: "Search failed"]))

        let result = await ObjectsBrowserView.searchObjects(client: mock, projectID: "proj-1", query: "test")

        XCTAssertTrue(result.objects.isEmpty)
        XCTAssertEqual(result.error, "Search failed")
    }

    func testSearchWithPropertiesPopulatesDisplayName() async {
        let mock = MockEmergentAPIClient()
        let obj = GraphObject(
            id: "obj-1", type: "Person", score: nil,
            properties: ["name": AnyCodable("John Doe")],
            createdAt: "2026-05-01T00:00:00Z"
        )
        mock.searchObjectsResult = .success([obj])

        let result = await ObjectsBrowserView.searchObjects(client: mock, projectID: "proj-1", query: "john")

        XCTAssertEqual(result.objects.first?.displayName, "John Doe")
    }

    func testObjectWithoutNameUsesFallback() async {
        let mock = MockEmergentAPIClient()
        mock.searchObjectsResult = .success([
            GraphObject(id: "abc12345", type: "Note", score: nil, properties: nil, createdAt: nil)
        ])

        let result = await ObjectsBrowserView.searchObjects(client: mock, projectID: "proj-1", query: "test")

        XCTAssertTrue(result.objects.first?.displayName.hasPrefix("Note: abc1") ?? false)
    }
}
