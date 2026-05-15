import XCTest

/// UI tests for the Diane Companion app (macOS).
/// The app runs with --uitesting to skip network-heavy startup.
final class DianeUITests: XCTestCase {

    var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["--uitesting"]
        app.launch()
    }

    override func tearDownWithError() throws {
        app = nil
    }

    // MARK: - Launch & Window

    func testAppLaunches() throws {
        XCTAssertTrue(app.state == .runningForeground || app.state == .runningBackground)
    }

    func testMainWindowExists() throws {
        let window = app.windows["Diane"]
        XCTAssertTrue(window.waitForExistence(timeout: 10))
    }

    // MARK: - Onboarding (unconfigured state)

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

    func testOnboardingCanTypeServerURL() throws {
        let window = app.windows["Diane"]
        guard window.waitForExistence(timeout: 10) else { return }

        let urlField = window.textFields["https://your-server:8080"]
        XCTAssertTrue(urlField.waitForExistence(timeout: 5))
        urlField.click()
        urlField.typeText("http://localhost:8890")
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
}
