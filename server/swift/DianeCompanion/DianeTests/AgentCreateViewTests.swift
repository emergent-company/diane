import XCTest
@testable import Diane

// MARK: - AgentCreateView Request Building

final class AgentCreateViewBuildRequestTests: XCTestCase {

    // MARK: - Validation

    func testEmptyNameReturnsNil() {
        XCTAssertNil(AgentCreateView.buildRequest(
            name: "", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        ))
    }

    func testWhitespaceNameReturnsNil() {
        XCTAssertNil(AgentCreateView.buildRequest(
            name: "   ", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        ))
    }

    func testValidNameReturnsRequest() {
        let req = AgentCreateView.buildRequest(
            name: "my-agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertNotNil(req)
        XCTAssertEqual(req?.name, "my-agent")
    }

    // MARK: - Field Mapping

    func testDescriptionField() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "A test agent", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.description, "A test agent")
    }

    func testEmptyDescriptionIsNil() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertNil(req?.description)
    }

    func testSystemPromptField() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "Be helpful",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.systemPrompt, "Be helpful")
    }

    func testToolsCommaSeparated() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "web_search,calculator", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.tools, ["web_search", "calculator"])
    }

    func testEmptyToolsIsNil() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertNil(req?.tools)
    }

    func testSkillsCommaSeparated() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "coding,research", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.skills, ["coding", "research"])
    }

    func testMaxStepsParsed() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "10",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.maxSteps, 10)
    }

    func testInvalidMaxStepsIsNil() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "not-a-number",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertNil(req?.maxSteps)
    }

    func testZeroMaxStepsIsNil() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "0",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertNil(req?.maxSteps)
    }

    func testTimeoutParsed() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "300", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.defaultTimeout, 300)
    }

    func testZeroTimeoutIsNil() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "0", visibility: "project", flowType: "standard"
        )
        XCTAssertNil(req?.defaultTimeout)
    }

    func testVisibilityDefaultsToProject() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.visibility, "project")
    }

    func testVisibilityCanBeOrg() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "org", flowType: "standard"
        )
        XCTAssertEqual(req?.visibility, "org")
    }

    func testFlowTypeDefaultsToStandard() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.flowType, "standard")
    }

    func testFlowTypeCanBeChain() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "chain"
        )
        XCTAssertEqual(req?.flowType, "chain")
    }

    // MARK: - Complex Request

    func testAllFieldsInComplexRequest() {
        let req = AgentCreateView.buildRequest(
            name: "complex-agent",
            description: "Does everything",
            systemPrompt: "Be smart",
            toolsString: "a,b,c",
            skillsString: "x,y",
            maxSteps: "25",
            timeout: "60",
            visibility: "org",
            flowType: "chain"
        )
        XCTAssertNotNil(req)
        XCTAssertEqual(req?.name, "complex-agent")
        XCTAssertEqual(req?.description, "Does everything")
        XCTAssertEqual(req?.systemPrompt, "Be smart")
        XCTAssertEqual(req?.tools, ["a", "b", "c"])
        XCTAssertEqual(req?.skills, ["x", "y"])
        XCTAssertEqual(req?.maxSteps, 25)
        XCTAssertEqual(req?.defaultTimeout, 60)
        XCTAssertEqual(req?.visibility, "org")
        XCTAssertEqual(req?.flowType, "chain")
    }

    // MARK: - Whitespace Trimming

    func testNameIsTrimmed() {
        let req = AgentCreateView.buildRequest(
            name: "  my-agent  ", description: "", systemPrompt: "",
            toolsString: "", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.name, "my-agent")
    }

    func testToolsAreTrimmed() {
        let req = AgentCreateView.buildRequest(
            name: "agent", description: "", systemPrompt: "",
            toolsString: " web_search , calculator ", skillsString: "", maxSteps: "",
            timeout: "", visibility: "project", flowType: "standard"
        )
        XCTAssertEqual(req?.tools, ["web_search", "calculator"])
    }
}
