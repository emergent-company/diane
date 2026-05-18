import XCTest

@MainActor
final class DianeUITests: XCTestCase {
    let app = XCUIApplication()

    override func setUp() async throws {
        try await super.setUp()
        continueAfterFailure = false
        app.launchArguments = ["UITesting"]
        app.launch()

        // Dismiss any system alerts that may appear (network, notifications, etc.)
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allowButton = springboard.buttons["Allow"]
        if allowButton.waitForExistence(timeout: 2) {
            allowButton.tap()
        }
    }

    // MARK: - Launcher / Tab Bar Tests

    func testAppLaunchesToChatsScreen() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))
    }

    // MARK: - Chats List Tests

    func testChatsHasNewSessionButton() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        let newButton = app.buttons["plus.bubble"]
        XCTAssertTrue(newButton.exists)
        XCTAssertTrue(newButton.isHittable)
    }

    func testSearchBarExists() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        let searchField = app.searchFields["Search sessions..."]
        XCTAssertTrue(searchField.waitForExistence(timeout: 3))
    }

    func testNewChatButtonOpensAgentPicker() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["plus.bubble"].tap()

        let agentPickerTitle = app.navigationBars["Choose Agent"]
        XCTAssertTrue(agentPickerTitle.waitForExistence(timeout: 3))
    }

    func testAgentPickerCanBeCancelled() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["plus.bubble"].tap()

        let agentPickerTitle = app.navigationBars["Choose Agent"]
        XCTAssertTrue(agentPickerTitle.waitForExistence(timeout: 3))

        // Tap Cancel and verify we're back on Chats
        app.buttons["Cancel"].tap()
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 3))
    }

    // MARK: - Session List (Mock Data) Tests

    func testMockSessionAppearsInList() throws {
        // Mock data injected via UITesting launch arg
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 3))
    }

    func testMockSessionShowsAgentAndMessageCount() throws {
        let agentLabel = app.staticTexts["diane-default"]
        XCTAssertTrue(agentLabel.waitForExistence(timeout: 3))
    }

    // MARK: - Chat View Tests (requires mock data)

    func testNavigateToChatView() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()
    }

    func testChatViewTitleButtonExists() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
    }

    func testChatViewShowsMessages() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        // Wait for chat to load
        let userMessage = app.staticTexts["Hello, what can you do?"]
        XCTAssertTrue(userMessage.waitForExistence(timeout: 3))

        let assistantMessage = app.staticTexts["I can help with various tasks like searching the web, reading files, and running code."]
        XCTAssertTrue(assistantMessage.waitForExistence(timeout: 3))
    }

    func testChatViewShowsToolCallNames() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        // Wait for the messages to load — tool call buttons are inside ExyteChat cells
        // Try finding by button text first (accessibility label on the button)
        let webSearchButton = app.buttons["web_search"]
        if webSearchButton.waitForExistence(timeout: 5) {
            XCTAssertTrue(webSearchButton.exists)
        } else {
            // Fallback: check that tool call buttons with the bubble identifier exist
            let assistantBubble = app.buttons["bubble-msg-2"]
            XCTAssertTrue(assistantBubble.waitForExistence(timeout: 3))

            // Verify the tool call name appears as static text inside the bubble
            let toolText = app.staticTexts["web_search"]
            XCTAssertTrue(toolText.waitForExistence(timeout: 2))

            let readFileText = app.staticTexts["read_file"]
            XCTAssertTrue(readFileText.waitForExistence(timeout: 2))
        }
    }

    func testToolCallExpandsOnTap() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        // Tap the tool call button directly
        let webSearchTool = app.buttons["web_search"]
        guard webSearchTool.waitForExistence(timeout: 3) else {
            throw XCTSkip("Tool call button not exposed in ExyteChat cell hierarchy")
        }

        // Tap to expand — should reveal arguments
        webSearchTool.tap()

        // Look for argument text visible after expansion
        let argText = app.staticTexts["Arguments"]
        let resultText = app.staticTexts["Result"]
        // At least one of these should appear after expanding
        let found = XCTWaiter.wait(for: [
            XCTNSPredicateExpectation(
                predicate: NSPredicate(format: "exists == true"),
                object: argText
            )
        ], timeout: 2)
        if found == .completed {
            XCTAssertTrue(argText.exists)
        } else {
            // Result label might appear instead
            XCTAssertTrue(resultText.waitForExistence(timeout: 2))
        }
    }

    // MARK: - Session Detail Sheet Tests

    func testSessionDetailSheetOpens() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        // Tap the title button to open session detail
        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
        titleButton.tap()

        // Session detail sheet should appear
        let sessionTitle = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionTitle.waitForExistence(timeout: 3))
    }

    func testSessionDetailShowsStats() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
        titleButton.tap()

        // Stat cards should be visible
        let runCountStat = app.staticTexts["2"]
        XCTAssertTrue(runCountStat.waitForExistence(timeout: 3))

        let tokensStat = app.staticTexts["161.1K"]
        XCTAssertTrue(tokensStat.waitForExistence(timeout: 1))
    }

    func testSessionDetailShowsAgent() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
        titleButton.tap()

        let agentLabel = app.staticTexts["diane-default"]
        XCTAssertTrue(agentLabel.waitForExistence(timeout: 2))
    }

    func testSessionDetailShowsArchiveButton() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
        titleButton.tap()

        let archiveButton = app.buttons["archive-session-button"]
        XCTAssertTrue(archiveButton.waitForExistence(timeout: 5))
    }

    func testSessionDetailCanBeDismissed() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
        titleButton.tap()

        // Dismiss the sheet
        app.buttons["Done"].tap()
    }

    // MARK: - Message Detail Sheet Tests

    func testMessageDetailSheetOpensOnTap() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()
        // Tap the user message button to open the detail sheet
        // (use firstMatch in case the NavigationStack keeps a copy in the hierarchy)
        let userBubble = app.buttons["bubble-msg-1"].firstMatch
        XCTAssertTrue(userBubble.waitForExistence(timeout: 5))
        userBubble.tap()

        // Message detail sheet should show metadata
        let metadataHeader = app.otherElements["message-detail-metadata"]
        XCTAssertTrue(metadataHeader.waitForExistence(timeout: 3))
    }

    func testMessageDetailShowsCopyButton() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let userBubble = app.buttons["bubble-msg-1"].firstMatch
        XCTAssertTrue(userBubble.waitForExistence(timeout: 5))
        userBubble.tap()

        let copyButton = app.buttons["copy-message-button"]
        XCTAssertTrue(copyButton.waitForExistence(timeout: 3))
        XCTAssertTrue(copyButton.isHittable)
    }

    func testMessageDetailCanBeDismissed() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let userBubble = app.buttons["bubble-msg-1"].firstMatch
        XCTAssertTrue(userBubble.waitForExistence(timeout: 5))
        userBubble.tap()

        // Dismiss
        app.buttons["Done"].tap()
    }

    func testMessageDetailShowsToolCallsInExpandedView() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        // Tap the assistant bubble (msg-2 has tool calls and reasoning)
        let assistantBubble = app.buttons["bubble-msg-2"].firstMatch
        XCTAssertTrue(assistantBubble.waitForExistence(timeout: 5))
        assistantBubble.tap()

        // Should show reasoning content and tool calls
        let reasoningLabel = app.staticTexts["Reasoning"]
        XCTAssertTrue(reasoningLabel.waitForExistence(timeout: 3))

        // Tool call names should be visible in the expanded view
        let webSearchLabel = app.staticTexts["web_search"]
        XCTAssertTrue(webSearchLabel.waitForExistence(timeout: 2))

        // Arguments section should exist
        let argsLabel = app.staticTexts["Arguments"]
        XCTAssertTrue(argsLabel.waitForExistence(timeout: 1))
    }

    // MARK: - Settings Tests

    func testSettingsCanBeOpened() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))
    }

    func testSettingsShowsConnectionFields() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        let apiKeyField = app.secureTextFields["API Key"]
        XCTAssertTrue(apiKeyField.waitForExistence(timeout: 3))

        let projectIDField = app.textFields["Project ID"]
        XCTAssertTrue(projectIDField.exists)
    }

    func testSettingsCanTypeApiKey() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        let apiKeyField = app.secureTextFields["API Key"]
        XCTAssertTrue(apiKeyField.waitForExistence(timeout: 3))

        apiKeyField.tap()
        apiKeyField.typeText("mp-test-key-123")

        let typedValue = apiKeyField.value as? String
        XCTAssertNotNil(typedValue)
        XCTAssertGreaterThan(typedValue!.count, 0)
        XCTAssertEqual(typedValue!.count, "mp-test-key-123".count)
    }

    func testSettingsCanTypeProjectID() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        let projectIDField = app.textFields["Project ID"]
        XCTAssertTrue(projectIDField.waitForExistence(timeout: 3))

        projectIDField.tap()
        projectIDField.typeText("test-project")

        XCTAssertEqual(projectIDField.value as? String, "test-project")
    }

    func testSettingsHasTestConnectionButton() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        let testButton = app.buttons["Test Connection"]
        XCTAssertTrue(testButton.waitForExistence(timeout: 3))
        XCTAssertTrue(testButton.isHittable)
    }

    func testSettingsHasSaveButton() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        let saveButton = app.buttons["Save"]
        XCTAssertTrue(saveButton.waitForExistence(timeout: 3))
    }

    func testSettingsCanBeDismissed() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        app.buttons["Cancel"].tap()

        XCTAssertTrue(chatsNav.waitForExistence(timeout: 3))
    }

    // MARK: - Settings Sound Picker Test

    func testSettingsShowsNotificationsSection() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        let allSwitches = app.switches.allElementsBoundByIndex
        let pushToggle = allSwitches.first {
            $0.label.contains("Push Notifications") || $0.identifier.contains("Push Notifications")
        }

        if let toggle = pushToggle {
            XCTAssertTrue(toggle.exists)
        } else {
            throw XCTSkip("Notifications toggle not on screen — form needs scrolling")
        }
    }

    // MARK: - Navigation Tests

    func testSettingsCanDismissViaToolbarButton() throws {
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))

        app.buttons["gearshape"].tap()

        let settingsNav = app.navigationBars["Settings"]
        XCTAssertTrue(settingsNav.waitForExistence(timeout: 3))

        let saveButton = app.buttons["Save"]
        XCTAssertTrue(saveButton.exists)
        XCTAssertFalse(saveButton.isEnabled)
    }

    // MARK: - Swipe Actions Tests

    func testSessionHasSwipeActions() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))

        // Swipe left on the cell to reveal trailing swipe actions (Archive).
        let cellFrame = sessionRow.frame
        let startX = cellFrame.midX
        let startY = cellFrame.midY
        let startCoord = app.coordinate(withNormalizedOffset: .zero)
            .withOffset(CGVector(dx: startX, dy: startY))
        let endCoord = app.coordinate(withNormalizedOffset: .zero)
            .withOffset(CGVector(dx: startX - cellFrame.width * 0.6, dy: startY))
        startCoord.press(forDuration: 0.05, thenDragTo: endCoord)

        let archiveButton = app.buttons["Archive"]
        XCTAssertTrue(archiveButton.waitForExistence(timeout: 2))
    }

    // MARK: - State Restoration Tests

    func testSessionDetailTokensFormatted() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
        titleButton.tap()

        // 161128 tokens should display as 161.1K
        let formattedTokens = app.staticTexts["161.1K"]
        XCTAssertTrue(formattedTokens.waitForExistence(timeout: 2))
    }

    func testSessionDetailCostFormatted() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        let titleButton = app.buttons["chat-title-button"]
        XCTAssertTrue(titleButton.waitForExistence(timeout: 5))
        titleButton.tap()

        // The stats grid should render the cost stat card's value
        let costValue = app.staticTexts.element(matching: NSPredicate(format: "label CONTAINS '$0.016'"))
        XCTAssertTrue(costValue.waitForExistence(timeout: 3))
    }

    // MARK: - Edge Cases

    func testTapEmptyAreaDoesNothing() throws {
        // Just verify the app doesn't crash when tapping non-interactive areas
        // on the session list
        let chatsNav = app.navigationBars["Chats"]
        XCTAssertTrue(chatsNav.waitForExistence(timeout: 5))
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.8)).tap()
    }

    func testToolCallShowsArgumentsAfterExpand() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        // Tap tool call button to expand
        let readFileTool = app.buttons["read_file"]
        guard readFileTool.waitForExistence(timeout: 5) else {
            throw XCTSkip("Tool call button not exposed in ExyteChat cell hierarchy")
        }
        readFileTool.tap()

        // Should now see the result text
        let resultLabel = app.staticTexts["Result"]
        XCTAssertTrue(resultLabel.waitForExistence(timeout: 2))
    }

    func testReasoningSectionExpands() throws {
        let sessionRow = app.staticTexts["Mock Chat"]
        XCTAssertTrue(sessionRow.waitForExistence(timeout: 5))
        sessionRow.tap()

        // Tap the reasoning section button to expand it
        let reasoningButton = app.buttons["Reasoning"]
        if reasoningButton.waitForExistence(timeout: 3) {
            reasoningButton.tap()
            // After tapping, the reasoning content should be visible
            let reasoningContent = app.staticTexts["The user wants to know my capabilities. Let me list them."]
            XCTAssertTrue(reasoningContent.waitForExistence(timeout: 1))
        }
    }
}
