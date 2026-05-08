import SwiftUI

/// Sheet for creating a new user-defined agent.
struct AgentCreateView: View {
    let onCreate: (CreateAgentRequest) async throws -> Void
    let onClose: () -> Void

    @State var name: String = ""
    @State var description: String = ""
    @State var systemPrompt: String = ""
    @State var toolsString: String = ""
    @State var skillsString: String = ""
    @State var maxSteps: String = ""
    @State var timeout: String = ""
    @State var visibility: String = "project"
    @State var flowType: String = "standard"

    @State var isCreating = false
    @State var error: String?

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("New Agent")
                    .font(.headline)
                Spacer()
                Button("Cancel", action: onClose)
                    .keyboardShortcut(.escape)
            }
            .padding()
            .background(Color(NSColor.windowBackgroundColor))

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: Design.Spacing.md) {
                    // Name (required)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Name *").font(.caption).foregroundStyle(.secondary)
                        TextField("my-custom-agent", text: $name)
                            .textFieldStyle(.roundedBorder)
                    }

                    // Description
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Description").font(.caption).foregroundStyle(.secondary)
                        TextField("What this agent does", text: $description)
                            .textFieldStyle(.roundedBorder)
                    }

                    // Flow Type
                    HStack {
                        Text("Flow Type").font(.caption).frame(width: 80, alignment: .leading)
                        Picker("", selection: $flowType) {
                            Text("standard").tag("standard")
                            Text("chat").tag("chat")
                            Text("agent").tag("agent")
                            Text("chain").tag("chain")
                        }
                        .labelsHidden()
                        .frame(width: 140)
                        Spacer()
                    }

                    // Visibility
                    HStack {
                        Text("Visibility").font(.caption).frame(width: 80, alignment: .leading)
                        Picker("", selection: $visibility) {
                            Text("project").tag("project")
                            Text("org").tag("org")
                            Text("private").tag("private")
                        }
                        .labelsHidden()
                        .frame(width: 140)
                        Spacer()
                    }

                    // System Prompt
                    VStack(alignment: .leading, spacing: 4) {
                        Text("System Prompt").font(.caption).foregroundStyle(.secondary)
                        TextEditor(text: $systemPrompt)
                            .font(.system(.caption, design: .monospaced))
                            .frame(minHeight: 120)
                            .border(Color.secondary.opacity(0.3))
                    }

                    // Tools (comma-separated)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Tools (comma-separated, e.g. search-hybrid, web-search-brave)")
                            .font(.caption).foregroundStyle(.secondary)
                        TextField("tool1, tool2", text: $toolsString)
                            .textFieldStyle(.roundedBorder)
                    }

                    // Skills (comma-separated)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Skills (comma-separated)").font(.caption).foregroundStyle(.secondary)
                        TextField("skill1, skill2", text: $skillsString)
                            .textFieldStyle(.roundedBorder)
                    }

                    // Runtime Limits
                    GroupBox("Runtime Limits") {
                        HStack {
                            Text("Max Steps").font(.caption).frame(width: 80, alignment: .leading)
                            TextField("50", text: $maxSteps)
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 80)
                            Spacer()
                            Text("Timeout (s)").font(.caption).frame(width: 80, alignment: .leading)
                            TextField("300", text: $timeout)
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 80)
                        }
                        .padding(4)
                    }

                    // Info
                    Text("User-defined agents are stored on Memory Platform and survive upgrades. They can be fully edited and deleted.")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .padding(.top, 8)
                }
                .padding()
            }

            Divider()

            // Footer
            HStack {
                if let err = error {
                    Text(err).font(.caption).foregroundStyle(.red)
                }
                Spacer()
                Button("Create Agent") {
                    Task { await create() }
                }
                .buttonStyle(.borderedProminent)
                .disabled(isCreating || name.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            .padding()
        }
        .frame(width: 520, height: 620)
    }

    func create() async {
        isCreating = true
        error = nil

        guard let req = Self.buildRequest(
            name: name,
            description: description,
            systemPrompt: systemPrompt,
            toolsString: toolsString,
            skillsString: skillsString,
            maxSteps: maxSteps,
            timeout: timeout,
            visibility: visibility,
            flowType: flowType
        ) else {
            error = "Name is required"
            isCreating = false
            return
        }

        do {
            try await onCreate(req)
        } catch {
            self.error = error.localizedDescription
        }
        isCreating = false
    }

    /// Build a CreateAgentRequest from form fields, returning nil if validation fails.
    static func buildRequest(
        name: String,
        description: String,
        systemPrompt: String,
        toolsString: String,
        skillsString: String,
        maxSteps: String,
        timeout: String,
        visibility: String,
        flowType: String
    ) -> CreateAgentRequest? {
        let trimmedName = name.trimmingCharacters(in: .whitespaces)
        guard !trimmedName.isEmpty else { return nil }

        var req = CreateAgentRequest(name: trimmedName)
        req.description = description.isEmpty ? nil : description
        req.systemPrompt = systemPrompt.isEmpty ? nil : systemPrompt
        req.flowType = flowType
        req.visibility = visibility

        let toolList = toolsString.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        if !toolList.isEmpty { req.tools = toolList }

        let skillList = skillsString.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        if !skillList.isEmpty { req.skills = skillList }

        if let ms = Int(maxSteps), ms > 0 { req.maxSteps = ms }
        if let to = Int(timeout), to > 0 { req.defaultTimeout = to }

        return req
    }
}
