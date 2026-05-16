import XCTest

@MainActor
final class DianeUITests: XCTestCase {
    let app = XCUIApplication()

    override func setUp() async throws {
        try await super.setUp()
        continueAfterFailure = false
        app.launchArguments = ["UITesting"]
        app.launch()
    }

    // MARK: - Tab Bar Tests

    func testTabBarExistsWithAllTabs() throws {
        let tabBar = app.tabBars.element
        XCTAssertTrue(tabBar.waitForExistence(timeout: 10))
        XCTAssertTrue(tabBar.buttons["Chats"].exists)
        XCTAssertTrue(tabBar.buttons["Agents"].exists)
        XCTAssertTrue(tabBar.buttons["Status"].exists)
        XCTAssertTrue(tabBar.buttons["Settings"].exists)
    }

    func testTabNavigationSwitchesViews() throws {
        let tabBar = app.tabBars.element
        XCTAssertTrue(tabBar.waitForExistence(timeout: 10))

        // Chats is default
        XCTAssertTrue(tabBar.buttons["Chats"].isSelected)

        for tab in ["Agents", "Status", "Settings"] {
            tabBar.buttons[tab].tap()
            XCTAssertTrue(tabBar.buttons[tab].isSelected)
        }
    }

    // MARK: - Settings Tests

    func testSettingsShowsConnectionFields() throws {
        app.tabBars.buttons["Settings"].tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 3))

        // Verify connection section text fields
        let serverURLField = app.textFields["Server URL"]
        XCTAssertTrue(serverURLField.waitForExistence(timeout: 3))

        let apiKeyField = app.secureTextFields["API Key"]
        XCTAssertTrue(apiKeyField.exists)

        let saveButton = app.buttons["Save"]
        XCTAssertTrue(saveButton.exists)
    }

    func testSettingsCanTypeServerURL() throws {
        app.tabBars.buttons["Settings"].tap()

        let serverURLField = app.textFields["Server URL"]
        XCTAssertTrue(serverURLField.waitForExistence(timeout: 3))

        serverURLField.tap()
        serverURLField.typeText("http://localhost:8890")

        // Verify typed value
        XCTAssertEqual(serverURLField.value as? String, "http://localhost:8890")
    }

    func testSettingsHasTestConnectionButton() throws {
        app.tabBars.buttons["Settings"].tap()

        let testButton = app.buttons["Test Connection"]
        XCTAssertTrue(testButton.waitForExistence(timeout: 3))
        XCTAssertTrue(testButton.isHittable)
    }

    // MARK: - Chats List Tests

    func testChatsShowsEmptyState() throws {
        // Without a configured API key, the chat list should show empty state
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))
    }

    func testChatsHasNewSessionButton() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        // Toolbar has + button for new session
        let newButton = app.buttons["plus.bubble"]
        XCTAssertTrue(newButton.exists)
    }
}
