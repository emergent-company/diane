import SwiftUI

/// Sheet for editing override config for a built-in agent.
/// Only fields the user fills in are sent as overrides.
struct AgentOverrideEditorView: View {
    let agentName: String
    let agentDetail: AgentDetail?
    let existingOverride: AgentOverrideConfig?
    let onSave: (AgentOverrideConfig) async -> Void
    let onDelete: () async -> Void
    let onClose: () -> Void

    @State private var systemPrompt: String = ""
    @State private var skillsString: String = ""
    @State private var modelProvider: String = ""
    @State private var modelName: String = ""
    @State private var modelTemperature: String = ""
    @State private var modelMaxTokens: String = ""
    @State private var maxSteps: String = ""
    @State private var timeout: String = ""
    @State private var visibility: String = ""
    @State private var sandboxEnabled: Bool = false
    @State private var hasSandboxOverride: Bool = false
    @State private var disabled: Bool = false

    @State private var isSaving = false
    @State private var saved = false
    @State private var error: String?

    private var hasExistingOverride: Bool { existingOverride != nil }

    init(agentName: String, agentDetail: AgentDetail?, existingOverride: AgentOverrideConfig?,
         onSave: @escaping (AgentOverrideConfig) async -> Void,
         onDelete: @escaping () async -> Void,
         onClose: @escaping () -> Void) {
        self.agentName = agentName
        self.agentDetail = agentDetail
        self.existingOverride = existingOverride
        self.onSave = onSave
        self.onDelete = onDelete
        self.onClose = onClose

        let oc = existingOverride
        _systemPrompt = State(initialValue: oc?.systemPrompt ?? "")
        _skillsString = State(initialValue: oc?.skills?.joined(separator: ", ") ?? "")
        _modelProvider = State(initialValue: oc?.modelProvider ?? "")
        _modelName = State(initialValue: oc?.modelName ?? "")
        _modelTemperature = State(initialValue: oc.flatMap { $0.modelTemperature.map { String($0) } } ?? "")
        _modelMaxTokens = State(initialValue: oc.flatMap { $0.modelMaxTokens.map { String($0) } } ?? "")
        _maxSteps = State(initialValue: oc.flatMap { $0.maxSteps.map { String($0) } } ?? "")
        _timeout = State(initialValue: oc.flatMap { $0.timeout.map { String($0) } } ?? "")
        _visibility = State(initialValue: oc?.visibility ?? "")
        _sandboxEnabled = State(initialValue: oc?.sandboxEnabled ?? false)
        _hasSandboxOverride = State(initialValue: oc?.sandboxEnabled != nil)
        _disabled = State(initialValue: oc?.disabled ?? false)
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Override: \(agentName)")
                    .font(.headline)
                Spacer()
                if hasExistingOverride {
                    Button("Remove Override", role: .destructive) {
                        Task { await deleteOverride() }
                    }
                    .disabled(isSaving)
                }
                Button("Cancel", action: onClose)
                    .keyboardShortcut(.escape)
            }
            .padding()
            .background(Color(NSColor.windowBackgroundColor))

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: Design.Spacing.md) {
                    // System Prompt
                    VStack(alignment: .leading, spacing: 4) {
                        Text("System Prompt").font(.caption).foregroundStyle(.secondary)
                        TextEditor(text: $systemPrompt)
                            .font(.system(.caption, design: .monospaced))
                            .frame(minHeight: 120)
                            .border(Color.secondary.opacity(0.3))
                            .overlay(alignment: .topTrailing) {
                                Text("\(systemPrompt.count) chars")
                                    .font(.system(size: 9))
                                    .foregroundStyle(.tertiary)
                                    .padding(4)
                            }
                    }

                    // Skills (comma-separated)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Skills (comma-separated)").font(.caption).foregroundStyle(.secondary)
                        TextField("skill1, skill2", text: $skillsString)
                            .textFieldStyle(.roundedBorder)
                    }

                    // Model
                    GroupBox("Model Override") {
                        VStack(alignment: .leading, spacing: 8) {
                            HStack {
                                Text("Provider").font(.caption).frame(width: 80, alignment: .leading)
                                TextField("deepseek", text: $modelProvider)
                                    .textFieldStyle(.roundedBorder)
                            }
                            HStack {
                                Text("Name").font(.caption).frame(width: 80, alignment: .leading)
                                TextField("deepseek-v4-flash", text: $modelName)
                                    .textFieldStyle(.roundedBorder)
                            }
                            HStack {
                                Text("Temperature").font(.caption).frame(width: 80, alignment: .leading)
                                TextField("0.7", text: $modelTemperature)
                                    .textFieldStyle(.roundedBorder)
                            }
                            HStack {
                                Text("Max Tokens").font(.caption).frame(width: 80, alignment: .leading)
                                TextField("4096", text: $modelMaxTokens)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                        .padding(4)
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

                    // Visibility
                    HStack {
                        Text("Visibility").font(.caption).frame(width: 80, alignment: .leading)
                        Picker("", selection: $visibility) {
                            Text("(keep built-in)").tag("")
                            Text("project").tag("project")
                            Text("org").tag("org")
                            Text("private").tag("private")
                        }
                        .labelsHidden()
                        .frame(width: 160)
                        Spacer()
                    }

                    // Sandbox
                    Toggle(isOn: $hasSandboxOverride) {
                        Text("Override sandbox config").font(.caption)
                    }
                    if hasSandboxOverride {
                        Toggle("Sandbox Enabled", isOn: $sandboxEnabled)
                            .padding(.leading, 20)
                    }

                    // Disable
                    Toggle(isOn: $disabled) {
                        HStack {
                            Text("Disabled").font(.caption)
                            Text("(agent won't be seeded — effectively deleted)")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
                .padding()
            }

            Divider()

            // Footer
            HStack {
                if let err = error {
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
                Spacer()
                if saved {
                    Text("✅ Saved")
                        .font(.caption)
                        .foregroundStyle(.green)
                }
                Button("Save Override") {
                    Task { await save() }
                }
                .buttonStyle(.borderedProminent)
                .disabled(isSaving)
            }
            .padding()
        }
        .frame(width: 520, height: 620)
    }

    private func buildOverride() -> AgentOverrideConfig {
        var oc = AgentOverrideConfig(agentName: agentName)
        if !systemPrompt.isEmpty { oc.systemPrompt = systemPrompt }
        let skillList = skillsString.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        if !skillList.isEmpty { oc.skills = skillList }
        if !modelProvider.isEmpty { oc.modelProvider = modelProvider }
        if !modelName.isEmpty { oc.modelName = modelName }
        if let t = Double(modelTemperature), t != 0 { oc.modelTemperature = t }
        if let mt = Int(modelMaxTokens), mt != 0 { oc.modelMaxTokens = mt }
        if let ms = Int(maxSteps), ms > 0 { oc.maxSteps = ms }
        if let to = Int(timeout), to > 0 { oc.timeout = to }
        if !visibility.isEmpty { oc.visibility = visibility }
        if hasSandboxOverride { oc.sandboxEnabled = sandboxEnabled }
        if disabled { oc.disabled = true }
        return oc
    }

    private func save() async {
        isSaving = true
        error = nil
        await onSave(buildOverride())
        saved = true
        isSaving = false
    }

    private func deleteOverride() async {
        isSaving = true
        error = nil
        await onDelete()
        saved = true
        isSaving = false
    }
}
