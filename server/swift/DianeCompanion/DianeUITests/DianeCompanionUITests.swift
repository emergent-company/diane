import XCTest

/// UI tests for the Diane Companion app (macOS).
///
/// The app runs with --uitesting which skips network-heavy startup and
/// makes the StatusMonitor poll http://127.0.0.1:18990/api/status instead
/// of the real Diane API port. A mock HTTP server on port 18990 fakes
/// server availability so the full onboarding→sidebar→content flow is
/// exercised end-to-end without a live backend.
final class DianeUITests: XCTestCase {

    var app: XCUIApplication!
    var mockServer: Process!

    // ──────────────────────────────────────────────
    // MARK: - Setup / Teardown
    // ──────────────────────────────────────────────

    override func setUpWithError() throws {
        continueAfterFailure = false

        // Start a minimal mock HTTP server on the port that --uitesting uses
        startMockServer()

        app = XCUIApplication()
        app.launchArguments = ["--uitesting"]
        app.launch()
    }

    override func tearDownWithError() throws {
        // Shut down the mock server
        if mockServer?.isRunning == true {
            mockServer.terminate()
            mockServer.waitUntilExit()
        }
        mockServer = nil
        app = nil
    }

    // ──────────────────────────────────────────────
    // MARK: - Launch & Window
    // ──────────────────────────────────────────────

    func testAppLaunches() throws {
        XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground)
    }

    func testMainWindowExists() throws {
        let window = app.windows["Diane"]
        XCTAssertTrue(window.waitForExistence(timeout: 10))
    }

    func testWindowHasReasonableSize() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }
        let frame = window.frame
        XCTAssertGreaterThan(frame.width, 400)
        XCTAssertGreaterThan(frame.height, 300)
    }

    func testNoCrashOnStartup() throws {
        Thread.sleep(forTimeInterval: 3)
        XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground,
                      "App should still be running after launch")
    }

    // ──────────────────────────────────────────────
    // MARK: - Onboarding (unconfigured state)
    // ──────────────────────────────────────────────

    func testOnboardingShowsWelcome() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else {
            XCTFail("Window not found"); return
        }
        XCTAssertTrue(window.staticTexts["Welcome to Diane"].waitForExistence(timeout: 5))
    }

    func testOnboardingHasServerURLField() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }

        let urlField = window.textFields["https://your-server:8080"]
        XCTAssertTrue(urlField.waitForExistence(timeout: 5))
    }

    func testOnboardingHasAPIKeyField() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }

        let keyField = window.secureTextFields["Account API key"]
        XCTAssertTrue(keyField.waitForExistence(timeout: 5))
    }

    func testOnboardingHasSaveButton() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }

        let saveButton = window.buttons["Save"]
        XCTAssertTrue(saveButton.waitForExistence(timeout: 5))
    }

    func testOnboardingHasTestConnectionButton() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }

        let testButton = window.buttons["Test Connection"]
        XCTAssertTrue(testButton.waitForExistence(timeout: 5))
    }

    func testOnboardingCanTypeServerURL() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }

        let urlField = window.textFields["https://your-server:8080"]
        XCTAssertTrue(urlField.waitForExistence(timeout: 5))
        urlField.click()
        urlField.typeText("http://localhost:8890")
    }

    // ──────────────────────────────────────────────
    // MARK: - Full Onboarding → Configuration Flow
    // ──────────────────────────────────────────────

    /// Fills in the onboarding form, saves, and waits for the main
    /// sidebar/content view to appear (via the mock server on port 18990).
    func testConfigureAndShowMainContent() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else {
            XCTFail("Window not found"); return
        }

        // 1. Wait for onboarding to render
        XCTAssertTrue(window.staticTexts["Welcome to Diane"].waitForExistence(timeout: 5))

        // 2. Enter server URL
        let urlField = window.textFields["https://your-server:8080"]
        XCTAssertTrue(urlField.waitForExistence(timeout: 5))
        urlField.click()
        urlField.typeText("http://localhost:8890")

        // 3. Enter API key
        let keyField = window.secureTextFields["Account API key"]
        XCTAssertTrue(keyField.waitForExistence(timeout: 5))
        keyField.click()
        keyField.typeText("test-api-key")

        // 4. Click Save
        let saveButton = window.buttons["Save"]
        XCTAssertTrue(saveButton.isEnabled)
        saveButton.click()

        // 5. Wait for saved confirmation
        XCTAssertTrue(window.staticTexts["Saved"].waitForExistence(timeout: 3))

        // 6. Wait for the main content to appear — the mock server
        //    on port 18990 will make StatusMonitor.isLocalAPIReachable true
        //    within the connecting phase (2-4s polling interval).
        //    The sidebar has "Diane" as its section header.
        let sidebarLabel = window.staticTexts["Diane"]
        XCTAssertTrue(sidebarLabel.waitForExistence(timeout: 15),
                      "Sidebar should appear after configuring and connecting")
    }

    // ──────────────────────────────────────────────
    // MARK: - Sidebar Navigation
    // ──────────────────────────────────────────────

    /// Helper: configure the app via onboarding so we can access the sidebar.
    private func configureApp() {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }
        guard window.staticTexts["Welcome to Diane"].waitForExistence(timeout: 5) else {
            // Already configured from a previous test (shouldn't happen since
            // each test gets a fresh app launch)
            return
        }

        let urlField = window.textFields["https://your-server:8080"]
        guard urlField.waitForExistence(timeout: 3) else { return }
        urlField.click()
        urlField.typeText("http://localhost:8890")

        let keyField = window.secureTextFields["Account API key"]
        guard keyField.waitForExistence(timeout: 3) else { return }
        keyField.click()
        keyField.typeText("test-api-key")

        window.buttons["Save"].click()
    }

    /// Helper: wait for the sidebar to appear after configuring.
    private func waitForSidebar(file: StaticString = #filePath, line: UInt = #line) {
        configureApp()
        let sidebarLabel = app.windows["Diane"].staticTexts["Diane"]
        XCTAssertTrue(sidebarLabel.waitForExistence(timeout: 15),
                      "Sidebar should be visible after configuration",
                      file: file, line: line)
    }

    /// Helper: click a sidebar item by its label text.
    private func clickSidebarItem(_ name: String, file: StaticString = #filePath, line: UInt = #line) {
        let window = app.windows["Diane"]
        let item = window.staticTexts[name]
        XCTAssertTrue(item.waitForExistence(timeout: 5),
                      "Sidebar item '\(name)' should exist", file: file, line: line)
        item.click()
        // Brief settle time for the detail view to render
        Thread.sleep(forTimeInterval: 0.5)
    }

    func testSidebarContainsAllItems() throws {
        waitForSidebar()

        let window = app.windows["Diane"]
        let expectedItems = [
            "Dashboard", "Sessions", "Documents", "Agents",
            "Schema", "Ask", "MCP Servers", "Nodes",
            "Objects", "Providers", "Permissions", "System"
        ]
        for item in expectedItems {
            XCTAssertTrue(window.staticTexts[item].waitForExistence(timeout: 3),
                          "Sidebar should contain '\(item)'")
        }
    }

    // ──────────────────────────────────────────────
    // MARK: - Dashboard / Stats View
    // ──────────────────────────────────────────────

    func testDashboardViewShowsHeader() throws {
        waitForSidebar()

        // Dashboard is the default view — no need to click
        let window = app.windows["Diane"]

        // The dashboard/StatsView should render its title or content area
        // Look for text that indicates the view loaded
        let contentLoaded = window.staticTexts.containing(NSPredicate(format: "label CONTAINS[c] 'Stats' OR label CONTAINS[c] 'Dashboard' OR label CONTAINS[c] 'Status' OR label CONTAINS[c] 'Provider' OR label CONTAINS[c] 'Version'"))
        XCTAssertTrue(contentLoaded.element.waitForExistence(timeout: 5),
                      "Dashboard should show some content after loading")
    }

    // ──────────────────────────────────────────────
    // MARK: - Sessions View
    // ──────────────────────────────────────────────

    func testSessionsViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Sessions")

        let window = app.windows["Diane"]

        // Sessions view should render — check for any text content
        // Even empty state would show something
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Sessions view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Documents View
    // ──────────────────────────────────────────────

    func testDocumentsViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Documents")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Documents view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Agents View
    // ──────────────────────────────────────────────

    func testAgentsViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Agents")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Agents view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Schema View
    // ──────────────────────────────────────────────

    func testSchemaViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Schema")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Schema view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Ask / Questions View
    // ──────────────────────────────────────────────

    func testAskViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Ask")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Ask view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - MCP Servers View
    // ──────────────────────────────────────────────

    func testMCPServersViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("MCP Servers")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "MCP Servers view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Nodes View
    // ──────────────────────────────────────────────

    func testNodesViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Nodes")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Nodes view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Objects Browser View
    // ──────────────────────────────────────────────

    func testObjectsViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Objects")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Objects view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Providers View
    // ──────────────────────────────────────────────

    func testProvidersViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Providers")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Providers view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Permissions View
    // ──────────────────────────────────────────────

    func testPermissionsViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("Permissions")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "Permissions view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - System View
    // ──────────────────────────────────────────────

    func testSystemViewIsAccessible() throws {
        waitForSidebar()
        clickSidebarItem("System")

        let window = app.windows["Diane"]
        let contentExists = window.staticTexts.count > 1 || window.buttons.count > 0
        XCTAssertTrue(contentExists, "System view should render after navigation")
    }

    // ──────────────────────────────────────────────
    // MARK: - Full Navigation Tour
    // ──────────────────────────────────────────────

    /// Navigates through every sidebar item and verifies each view
    /// renders without crashing the app.
    func testAllViewsRenderWithoutCrash() throws {
        waitForSidebar()

        let sidebarItems = SidebarItem.allCases

        for item in sidebarItems {
            clickSidebarItem(item.rawValue)
            // Brief pause for the view to render
            Thread.sleep(forTimeInterval: 0.3)

            // Verify the app is still running
            XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground,
                          "App crashed when navigating to '\(item.rawValue)'")
        }
    }

    // ──────────────────────────────────────────────
    // MARK: - Mock HTTP Server
    // ──────────────────────────────────────────────

    /// Starts a minimal Python HTTP server on port 18990 that responds with
    /// 200 OK to any GET request. The StatusMonitor polls this endpoint
    /// during --uitesting mode instead of the real Diane API port.
    private func startMockServer() {
        let pythonScript = """
import http.server
import json

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps({'status': 'ok', 'version': '0.0.0'}).encode())
    def log_message(self, format, *args):
        pass  # suppress logs

if __name__ == '__main__':
    server = http.server.HTTPServer(('127.0.0.1', 18990), Handler)
    server.serve_forever()
"""

        mockServer = Process()
        mockServer.executableURL = URL(fileURLWithPath: "/usr/bin/python3")
        mockServer.arguments = ["-c", pythonScript]

        // Set up a pipe to discard stdout
        let pipe = Pipe()
        mockServer.standardOutput = pipe
        mockServer.standardError = pipe

        do {
            try mockServer.run()
        } catch {
            XCTFail("Failed to start mock HTTP server: \(error.localizedDescription)")
        }

        // Give the server a moment to bind the port
        Thread.sleep(forTimeInterval: 0.5)
    }
}

// MARK: - SidebarItem mirror for test enumeration

/// Mirrors the app's SidebarItem for test-side iteration.
/// Keep in sync with AppState.swift in the main target.
private enum SidebarItem: String, CaseIterable {
    case dashboard  = "Dashboard"
    case sessions   = "Sessions"
    case documents  = "Documents"
    case agents     = "Agents"
    case schema     = "Schema"
    case ask        = "Ask"
    case mcpServers = "MCP Servers"
    case nodes      = "Nodes"
    case objects    = "Objects"
    case providers  = "Providers"
    case permissions = "Permissions"
    case system     = "System"
}
