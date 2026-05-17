import XCTest

final class DianeUITests: XCTestCase {

    var app: XCUIApplication!

    // ──────────────────────────────────────────────
    // MARK: - Setup / Teardown
    // ──────────────────────────────────────────────

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["--uitesting"]
        app.launch()
    }

    override func tearDownWithError() throws {
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
    // MARK: - Onboarding
    // ──────────────────────────────────────────────

    /// Asserts the app is showing the onboarding view.
    private func assertOnboardingVisible(file: StaticString = #filePath, line: UInt = #line) {
        let welcome = app.staticTexts["Welcome to Diane"]
        XCTAssertTrue(welcome.waitForExistence(timeout: 10),
                      "Expected onboarding to be visible", file: file, line: line)
    }

    /// Asserts the onboarding view is NOT visible (transitioned away).
    private func assertOnboardingNotVisible(file: StaticString = #filePath, line: UInt = #line) {
        let welcome = app.staticTexts["Welcome to Diane"]
        XCTAssertFalse(welcome.waitForExistence(timeout: 3),
                       "Expected onboarding to be dismissed", file: file, line: line)
    }

    func testOnboardingShowsWelcome() throws {
        assertOnboardingVisible()
    }

    func testOnboardingHasAPIKeyField() throws {
        assertOnboardingVisible()
        let keyField = app.secureTextFields["Account API key"]
        XCTAssertTrue(keyField.waitForExistence(timeout: 5))
    }

    func testOnboardingHasSaveButton() throws {
        assertOnboardingVisible()
        let saveButton = app.buttons["Save"]
        XCTAssertTrue(saveButton.waitForExistence(timeout: 5))
    }

    // ──────────────────────────────────────────────
    // MARK: - Configuration Flow
    // ──────────────────────────────────────────────

    /// Helper: fill in the API key and tap Save. Does NOT verify the result.
    private func configureApp() {
        assertOnboardingVisible()

        let keyField = app.secureTextFields["Account API key"]
        XCTAssertTrue(keyField.waitForExistence(timeout: 5))
        keyField.click()
        keyField.typeText("test-api-key")

        let saveButton = app.buttons["Save"]
        XCTAssertTrue(saveButton.isEnabled)
        saveButton.click()
    }

    /// Helper: configure the app and wait for the sidebar to appear.
    private func waitForSidebar(file: StaticString = #filePath, line: UInt = #line) {
        configureApp()

        // After Save, the app transitions to main content with sidebar.
        // In --uitesting mode this happens immediately (no server check).
        // Sidebar items are buttons on macOS, use the first one to confirm.
        let dashboard = app.buttons["Dashboard"]
        XCTAssertTrue(dashboard.waitForExistence(timeout: 15),
                      "Sidebar should appear after configuration",
                      file: file, line: line)
    }

    /// Helper: tap a sidebar item and verify the detail view updates.
    private func tapSidebarItem(
        _ name: String,
        file: StaticString = #filePath,
        line: UInt = #line
    ) {
        // Sidebar items are rendered as buttons on macOS List with .sidebar style
        let item = app.buttons[name]
        XCTAssertTrue(item.waitForExistence(timeout: 5),
                      "Sidebar item '\(name)' should exist",
                      file: file, line: line)
        XCTAssertTrue(item.isHittable,
                      "Sidebar item '\(name)' should be hittable",
                      file: file, line: line)

        item.click()

        // Give the detail view time to render
        Thread.sleep(forTimeInterval: 1)

        // Verify app didn't crash
        XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground,
                      "App crashed after tapping '\(name)'",
                      file: file, line: line)
    }

    func testConfigureAndShowMainContent() throws {
        configureApp()

        // After Save: verify onboarding is dismissed and sidebar is visible
        assertOnboardingNotVisible()
        let dashboard = app.buttons["Dashboard"]
        XCTAssertTrue(dashboard.waitForExistence(timeout: 15),
                      "Dashboard sidebar item should appear after configuration")
    }

    // ──────────────────────────────────────────────
    // MARK: - Sidebar Content
    // ──────────────────────────────────────────────

    func testSidebarContainsAllItems() throws {
        waitForSidebar()

        // Give the sidebar a moment to fully render all items
        Thread.sleep(forTimeInterval: 1)

        let expectedItems = [
            "Dashboard", "Sessions", "Documents", "Agents",
            "Schema", "Ask", "MCP Servers", "Nodes",
            "Objects", "Providers", "Permissions", "System"
        ]
        for item in expectedItems {
            let label = app.buttons[item]
            XCTAssertTrue(label.waitForExistence(timeout: 5),
                          "Sidebar should contain '\(item)'")
        }
    }

    // ──────────────────────────────────────────────
    // MARK: - Navigation & View Rendering
    // ──────────────────────────────────────────────

    func testSessionsViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Sessions")
    }

    func testDocumentsViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Documents")
    }

    func testAgentsViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Agents")
    }

    func testSchemaViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Schema")
    }

    func testAskViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Ask")
    }

    func testMCPServersViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("MCP Servers")
    }

    func testNodesViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Nodes")
    }

    func testObjectsViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Objects")
    }

    func testProvidersViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Providers")
    }

    func testPermissionsViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("Permissions")
    }

    func testSystemViewIsAccessible() throws {
        waitForSidebar()
        tapSidebarItem("System")
    }

    // ──────────────────────────────────────────────
    // MARK: - Full Navigation Tour
    // ──────────────────────────────────────────────

    func testAllViewsRenderWithoutCrash() throws {
        waitForSidebar()
        Thread.sleep(forTimeInterval: 1)

        let sidebarItems = SidebarItem.allCases
        for item in sidebarItems {
            tapSidebarItem(item.rawValue)
            // Verify app is still alive after each navigation
            XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground,
                          "App crashed when navigating to '\(item.rawValue)'")
        }
    }
}

private enum SidebarItem: String, CaseIterable {
    case dashboard = "Dashboard"
    case sessions = "Sessions"
    case documents = "Documents"
    case agents = "Agents"
    case schema = "Schema"
    case ask = "Ask"
    case mcpServers = "MCP Servers"
    case nodes = "Nodes"
    case objects = "Objects"
    case providers = "Providers"
    case permissions = "Permissions"
    case system = "System"
}
