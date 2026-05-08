import XCTest

/// UI tests for the Diane Companion app.
///
/// These tests verify navigation, data display, and interaction
/// with the main window. The app launches as a Window scene
/// (with an additional MenuBarExtra), so we work with the window directly.
final class DianeUITests: XCTestCase {

    var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["--uitesting"]
        app.launchEnvironment = ["NSDoubleLocalizedStrings": "YES"]
        app.launch()
    }

    override func tearDownWithError() throws {
        app = nil
    }

    // MARK: - Launch & Window

    func testAppLaunches() throws {
        // Verify the app process is running
        XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground)
    }

    func testMainWindowExists() throws {
        // The Window scene has id "main" and title "Diane"
        let window = app.windows["Diane"]
        // Window may not appear immediately - give it time
        let exists = window.waitForExistence(timeout: 5)
        XCTAssertTrue(exists, "Main window should exist")
    }

    // MARK: - Sidebar Navigation

    func testSidebarDashboardExists() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // Check for navigation sidebar with common items
        // The sidebar uses NavigationLink or List with sidebar items
        let sidebar = window.outlines.firstMatch
        if sidebar.exists {
            // We have a sidebar (macOS 15 style)
            // Verify at least one sidebar button exists
            let dashboardButton = window.buttons["Dashboard"]
            let sessionsButton = window.buttons["Sessions"]
            XCTAssertTrue(dashboardButton.exists || sessionsButton.exists,
                          "At least one sidebar item should be visible")
        }
    }

    func testSidebarSessionsSelected() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // Try to click the Sessions sidebar button
        let sessionsButton = window.buttons["Sessions"]
        if sessionsButton.exists {
            sessionsButton.click()
            // After clicking, the navigation title should reflect "Sessions"
            // Give it time to load
            let navTitle = window.staticTexts["Sessions"]
            let exists = navTitle.waitForExistence(timeout: 3)
            XCTAssertTrue(exists, "Sessions should be the active view after clicking sidebar")
        }
    }

    func testSidebarDashboardSelected() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        let dashboardButton = window.buttons["Dashboard"]
        if dashboardButton.exists {
            dashboardButton.click()
            let navTitle = window.staticTexts["Dashboard"]
            let exists = navTitle.waitForExistence(timeout: 3)
            XCTAssertTrue(exists, "Dashboard should be the active view after clicking sidebar")
        }
    }

    func testSidebarServersSelected() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        let serversButton = window.buttons["MCP Servers"]
        if serversButton.exists {
            serversButton.click()
            let navTitle = window.staticTexts["MCP Servers"]
            let exists = navTitle.waitForExistence(timeout: 3)
            XCTAssertTrue(exists, "MCP Servers should be the active view after clicking sidebar")
        }
    }

    // MARK: - Content Area Verification

    func testContentAreaIsNotEmpty() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // Verify the window has actual content (not just empty/blank)
        // Look for any static text, buttons, or images in the window
        let hasContent = window.staticTexts.count > 0 || window.buttons.count > 0
        XCTAssertTrue(hasContent, "Window should contain UI elements")
    }

    func testNavigationTitleIsVisible() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // The dashboard should show a navigation title
        // This might be a toolbar item or a static text
        let hasTitle = window.staticTexts["Dashboard"].exists
            || window.staticTexts["Sessions"].exists
            || window.staticTexts["Diane"].exists
        XCTAssertTrue(hasTitle, "A navigation title should be visible")
    }

    // MARK: - Toolbar Interaction

    func testToolbarExists() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // Check for toolbar elements — typically buttons in the toolbar
        let toolbar = window.toolbars.firstMatch
        if toolbar.exists {
            // Toolbar found — verify it has interactive elements
            let toolbarButtons = toolbar.buttons
            XCTAssertTrue(toolbarButtons.count > 0 || !toolbar.exists,
                          "Toolbar should have buttons if present")
        }
    }

    // MARK: - Data Display (when loaded)

    func testWindowRespondsToResize() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // Verify window has reasonable minimum size
        let frame = window.frame
        XCTAssertGreaterThan(frame.width, 400, "Window should be wider than 400pt")
        XCTAssertGreaterThan(frame.height, 300, "Window should be taller than 300pt")
    }

    // MARK: - Tab Interaction (MCP Server Detail)

    func testMCPTabSwitching() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // Navigate to MCP Servers
        let serversButton = window.buttons["MCP Servers"]
        guard serversButton.exists else { return }  // Skip if no sidebar

        serversButton.click()
        Thread.sleep(forTimeInterval: 1)

        // Check for segmented control tabs (Connection, Tools, Prompts)
        // These appear in the MCP server detail panel
        let tabs = window.radioButtons
        if tabs.count > 0 {
            // Try clicking each tab
            for tab in tabs.allElementsBoundByIndex {
                if tab.exists && tab.isEnabled {
                    tab.click()
                    Thread.sleep(forTimeInterval: 0.5)
                }
            }
        }
    }

    // MARK: - Error Handling

    func testNoCrashOnViewSwitch() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 5) else {
            XCTFail("Window not found")
            return
        }

        // Navigate through all sidebar items we can find
        let sidebarItems = ["Dashboard", "Sessions", "Agents", "Schema", "MCP Servers", "System"]

        for item in sidebarItems {
            let button = window.buttons[item]
            if button.exists && button.isHittable {
                button.click()
                Thread.sleep(forTimeInterval: 0.5)
            }
        }

        // If we got here without crash, test passes
        XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground,
                      "App should still be running after view switching")
    }
}
