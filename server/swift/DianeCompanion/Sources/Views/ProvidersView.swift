import SwiftUI

// MARK: - ProvidersView

/// Providers view — project-level provider configs, policies,
/// embedding pipeline status, and per-object-type embedding policies.
///
/// API calls:
///   GET  /api/v1/projects/{projectId}/providers
///   PUT  /api/v1/projects/{projectId}/providers/{provider}
///   DELETE /api/v1/projects/{projectId}/providers/{provider}
///   POST /api/v1/providers/{provider}/test?projectId=...
///   GET  /api/v1/providers/{provider}/models?type=...
///   GET  /api/v1/projects/{projectId}/providers/policies
///   PUT  /api/v1/projects/{projectId}/providers/{provider}/policy
///   GET  /api/embeddings/status
///   GET  /api/graph/embedding-policies?project_id=<id>
struct ProvidersView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    // Provider configs
    @State private var orgProviderConfigs: [OrgProviderConfig] = []
    @State private var providerConfigsError: String? = nil
    @State private var showAddProviderConfig = false
    @State private var editingProviderConfig: OrgProviderConfig? = nil
    @State private var providerConfigToDelete: OrgProviderConfig? = nil
    @State private var showDeleteProviderConfig = false
    @State private var testingProvider: String? = nil
    @State private var testResults: [String: String] = [:]

    // Project policies
    @State private var projectPolicies: [ProjectPolicy] = []
    @State private var projectPoliciesError: String? = nil
    @State private var selectedPolicyForEdit: PolicyEditContext? = nil
    @State private var showSetPolicySheet = false

    // Embedding status
    @State private var embeddingStatus: EmbeddingStatus? = nil
    @State private var policies: [EmbeddingPolicy] = []
    @State private var isLoading = false
    @State private var statusError: String? = nil
    @State private var policiesError: String? = nil

    var body: some View {
        List {
            // Error banners
            if let err = providerConfigsError {
                Section {
                    ErrorBannerView(message: "Provider configs: \(err)") {
                        Task { await loadProjectProviderConfigs() }
                    }
                }
            }
            if let err = projectPoliciesError {
                Section {
                    ErrorBannerView(message: "Provider policies: \(err)") {
                        Task { await loadProjectPolicies() }
                    }
                }
            }
            if let err = statusError {
                Section {
                    ErrorBannerView(message: "Pipeline status: \(err)") {
                        Task { await loadStatus() }
                    }
                }
            }
            if let err = policiesError {
                Section {
                    ErrorBannerView(message: "Embedding policies: \(err)") {
                        Task { await loadPolicies() }
                    }
                }
            }

            // MARK: Provider Configs section
            Section {
                if appState.selectedProject == nil {
                    Text("Select a project to manage provider configs")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else if isLoading && orgProviderConfigs.isEmpty && providerConfigsError == nil {
                    HStack {
                        ProgressView().controlSize(.small)
                        Text("Loading…").font(.caption).foregroundStyle(.secondary)
                    }
                } else if orgProviderConfigs.isEmpty && providerConfigsError == nil {
                    Text("No provider configs configured")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(orgProviderConfigs) { config in
                        providerConfigRow(config)
                    }
                }
            } header: {
                HStack {
                    Text("Provider Configs")
                    Spacer()
                    if appState.selectedProject != nil {
                        Button {
                            editingProviderConfig = nil
                            showAddProviderConfig = true
                        } label: {
                            Image(systemName: "plus")
                                .font(.caption)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }

            // MARK: Project Provider Policies section
            Section("Provider Policies") {
                if appState.selectedProject == nil {
                    Text("Select a project to view provider policies")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else if isLoading && projectPolicies.isEmpty && projectPoliciesError == nil {
                    HStack {
                        ProgressView().controlSize(.small)
                        Text("Loading…").font(.caption).foregroundStyle(.secondary)
                    }
                } else {
                    ForEach(projectPolicies) { policy in
                        projectPolicyRow(policy)
                    }
                }
            }

            // MARK: Embedding Pipeline section
            Section("Embedding Pipeline") {
                if isLoading && embeddingStatus == nil {
                    HStack {
                        ProgressView().controlSize(.small)
                        Text("Loading…").font(.caption).foregroundStyle(.secondary)
                    }
                } else if let status = embeddingStatus {
                    pipelineWorkerRow(label: "Objects", state: status.objects)
                    pipelineWorkerRow(label: "Relationships", state: status.relationships)
                    pipelineWorkerRow(label: "Sweep", state: status.sweep)

                    if let config = status.config {
                        Divider()
                        if let batchSize = config.batchSize {
                            configRow(label: "Batch Size", value: "\(batchSize)")
                        }
                        if let concurrency = config.concurrency {
                            configRow(label: "Concurrency", value: "\(concurrency)")
                        }
                        if let current = config.currentConcurrency {
                            configRow(label: "Current Concurrency", value: "\(current)")
                        }
                        if let intervalMs = config.intervalMs {
                            configRow(label: "Interval", value: "\(intervalMs) ms")
                        }
                    }
                } else if statusError == nil {
                    Text("No pipeline data available")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            // MARK: Embedding Policies section
            Section("Embedding Policies") {
                if appState.selectedProject == nil {
                    Text("Select a project to view embedding policies")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else if isLoading && policies.isEmpty && policiesError == nil {
                    HStack {
                        ProgressView().controlSize(.small)
                        Text("Loading…").font(.caption).foregroundStyle(.secondary)
                    }
                } else if policies.isEmpty && policiesError == nil {
                    Text("No per-type policies configured")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(policies) { policy in
                        embeddingPolicyRow(policy)
                    }
                }
            }
        }
        .listStyle(.inset)
        .navigationTitle("Providers")
        .task(id: appState.selectedProject?.id) {
            await loadAll()
        }
        // Sheets
        .sheet(isPresented: $showAddProviderConfig) {
            if let projectID = appState.selectedProject?.id {
                AddOrgProviderConfigSheet(
                    isPresented: $showAddProviderConfig,
                    projectID: projectID,
                    editing: editingProviderConfig
                ) {
                    await loadProjectProviderConfigs()
                }
                .environmentObject(apiClient)
            }
        }
        .sheet(isPresented: $showSetPolicySheet) {
            if let ctx = selectedPolicyForEdit,
               let projectID = appState.selectedProject?.id,
               let orgID = appState.selectedProject?.orgId {
                SetProjectPolicySheet(
                    isPresented: $showSetPolicySheet,
                    projectID: projectID,
                    orgID: orgID,
                    provider: ctx.provider,
                    currentPolicy: ctx.policy
                ) {
                    await loadProjectPolicies()
                }
                .environmentObject(apiClient)
            }
        }
        .confirmationDialog(
            "Delete \(providerConfigToDelete?.provider ?? "provider config")?",
            isPresented: $showDeleteProviderConfig,
            titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive) {
                if let config = providerConfigToDelete, let projectID = appState.selectedProject?.id {
                    Task { await performDeleteProviderConfig(projectID: projectID, config: config) }
                }
            }
            Button("Cancel", role: .cancel) { providerConfigToDelete = nil }
        } message: {
            Text("This will permanently remove the provider configuration for \(providerConfigToDelete?.provider ?? "this provider").")
        }
    }

    // MARK: - Row builders

    private func providerConfigRow(_ config: OrgProviderConfig) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(providerDisplayName(config.provider))
                    .font(.subheadline)
                if let model = config.generativeModel {
                    Text("Model: \(model)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                if let url = config.baseURL, !url.isEmpty {
                    Text(url)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                if let gcp = config.gcpProject {
                    Text("GCP: \(gcp)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                if let loc = config.location {
                    Text("Location: \(loc)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                HStack(spacing: 8) {
                    if testingProvider == config.id {
                        ProgressView()
                            .controlSize(.small)
                            .scaleEffect(0.8)
                    } else {
                        Button("Test") {
                            Task { await performTestProvider(config: config) }
                        }
                        .font(.caption)
                        .buttonStyle(.plain)
                        .foregroundStyle(.blue)
                    }
                    Button {
                        editingProviderConfig = config
                        showAddProviderConfig = true
                    } label: {
                        Image(systemName: "pencil")
                            .font(.caption)
                    }
                    .buttonStyle(.plain)
                    Button(role: .destructive) {
                        providerConfigToDelete = config
                        showDeleteProviderConfig = true
                    } label: {
                        Image(systemName: "trash")
                            .font(.caption)
                            .foregroundStyle(.red)
                    }
                    .buttonStyle(.plain)
                }
                if let result = testResults[config.id] {
                    Text(result)
                        .font(.caption2)
                        .foregroundStyle(result.hasPrefix("✅") ? .green : .red)
                        .lineLimit(2)
                }
            }
        }
        .padding(.vertical, 2)
    }

    private func projectPolicyRow(_ policy: ProjectPolicy) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(providerDisplayName(policy.provider))
                    .font(.subheadline)
                Text(policyDisplayLabel(policy.policy))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button("Configure") {
                selectedPolicyForEdit = PolicyEditContext(provider: policy.provider, policy: policy)
                showSetPolicySheet = true
            }
            .font(.caption)
            .buttonStyle(.borderless)
        }
        .padding(.vertical, 2)
    }

    private func pipelineWorkerRow(label: String, state: EmbeddingWorkerState?) -> some View {
        HStack {
            Text(label).font(.subheadline)
            Spacer()
            if let state = state {
                if state.paused {
                    Label("Paused", systemImage: "pause.circle.fill").font(.caption).foregroundStyle(.orange)
                } else if state.running {
                    Label("Running", systemImage: "checkmark.circle.fill").font(.caption).foregroundStyle(.green)
                } else {
                    Label("Stopped", systemImage: "xmark.circle.fill").font(.caption).foregroundStyle(.red)
                }
            } else {
                Text("Unknown").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private func configRow(label: String, value: String) -> some View {
        HStack {
            Text(label).font(.caption).foregroundStyle(.secondary)
            Spacer()
            Text(value).font(.system(.caption, design: .monospaced)).foregroundStyle(.primary)
        }
    }

    private func embeddingPolicyRow(_ policy: EmbeddingPolicy) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(policy.name).font(.subheadline)
                if let types = policy.objectTypes, !types.isEmpty {
                    Text(types.joined(separator: ", "))
                        .font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                }
                if let model = policy.model {
                    Text(model).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            Spacer()
            if policy.active {
                Label("Active", systemImage: "checkmark.circle.fill")
                    .font(.caption).foregroundStyle(.green).labelStyle(.iconOnly)
            } else {
                Label("Inactive", systemImage: "circle")
                    .font(.caption).foregroundStyle(.secondary).labelStyle(.iconOnly)
            }
        }
        .padding(.vertical, 2)
    }

    // MARK: - Helpers

    func providerDisplayName(_ provider: String) -> String {
        switch provider {
        case "google-ai":        return "Google AI"
        case "vertex-ai":        return "Vertex AI"
        case "google":           return "Google"
        case "google-vertex":    return "Google Vertex"
        case "deepseek":         return "DeepSeek"
        case "openai-compatible": return "OpenAI Compatible"
        default:                 return provider
        }
    }

    func policyDisplayLabel(_ policy: String) -> String {
        switch policy {
        case "none":         return "None"
        case "organization": return "Organization"
        case "project":      return "Project-specific"
        default:             return policy
        }
    }

    // MARK: - Data loading

    @MainActor
    private func loadAll() async {
        // Auto-select first project if none selected
        if appState.selectedProject == nil {
            if let projects = try? await apiClient.fetchProjects(), let first = projects.first {
                appState.selectedProject = ProjectInfo(id: first.id, name: first.name, orgId: first.orgId)
            }
        }
        isLoading = true
        async let a: Void = loadProjectPolicies()
        async let b: Void = loadStatus()
        async let c: Void = loadPolicies()
        async let d: Void = loadProjectProviderConfigs()
        _ = await (a, b, c, d)
        isLoading = false
    }

    @MainActor
    private func loadProjectProviderConfigs() async {
        guard let projectID = appState.selectedProject?.id else {
            orgProviderConfigs = []
            providerConfigsError = nil
            return
        }
        do {
            orgProviderConfigs = try await apiClient.fetchProjectProviderConfigs(projectID: projectID)
            providerConfigsError = nil
        } catch {
            providerConfigsError = error.localizedDescription
        }
    }

    @MainActor
    private func loadProjectPolicies() async {
        guard let projectID = appState.selectedProject?.id,
              let orgID = appState.selectedProject?.orgId else {
            projectPolicies = []
            projectPoliciesError = nil
            return
        }
        do {
            projectPolicies = try await apiClient.fetchProjectPolicies(projectID: projectID, orgID: orgID)
            projectPoliciesError = nil
        } catch {
            projectPoliciesError = error.localizedDescription
        }
    }

    @MainActor
    private func loadStatus() async {
        do {
            embeddingStatus = try await apiClient.fetchEmbeddingStatus()
            statusError = nil
        } catch {
            statusError = error.localizedDescription
        }
    }

    @MainActor
    private func loadPolicies() async {
        guard let projectID = appState.selectedProject?.id else {
            policies = []
            policiesError = nil
            return
        }
        do {
            policies = try await apiClient.fetchEmbeddingPolicies(projectID: projectID)
            policiesError = nil
        } catch {
            policiesError = error.localizedDescription
        }
    }

    @MainActor
    private func performDeleteProviderConfig(projectID: String, config: OrgProviderConfig) async {
        do {
            try await apiClient.deleteProjectProviderConfig(projectID: projectID, provider: config.provider)
            providerConfigToDelete = nil
            await loadProjectProviderConfigs()
        } catch {
            providerConfigsError = error.localizedDescription
        }
    }

    @MainActor
    private func performTestProvider(config: OrgProviderConfig) async {
        guard let projectID = appState.selectedProject?.id else { return }
        testingProvider = config.id
        testResults[config.id] = nil
        do {
            let result = try await apiClient.testProjectProvider(
                projectID: projectID,
                provider: config.provider
            )
            testResults[config.id] = "✅ \(result.model) — \(result.latencyMs)ms"
        } catch {
            testResults[config.id] = "❌ \(error.localizedDescription)"
        }
        testingProvider = nil
    }
}

// MARK: - PolicyEditContext

/// Carries context for the SetProjectPolicySheet.
private struct PolicyEditContext {
    let provider: String
    let policy: ProjectPolicy?
}

// MARK: - AddOrgProviderConfigSheet

/// Sheet for adding or editing a project-level provider config.
/// API: PUT /api/v1/projects/{projectId}/providers/{provider}
struct AddOrgProviderConfigSheet: View {
    @Binding var isPresented: Bool
    let projectID: String
    let editing: OrgProviderConfig?
    let onSave: () async -> Void

    @EnvironmentObject var apiClient: EmergentAPIClient

    private let providerOptions: [(String, String)] = [
        ("deepseek",          "DeepSeek"),
        ("openai-compatible", "OpenAI Compatible"),
        ("google",            "Google"),
        ("google-vertex",     "Google Vertex"),
    ]

    @State private var selectedProvider = "deepseek"
    @State private var apiKey = ""
    @State private var baseURL = ""
    @State private var model = ""
    @State private var gcpProject = ""
    @State private var location = ""
    @State private var serviceAccountJSON = ""
    @State private var isSaving = false
    @State private var saveError: String? = nil

    private var isEditing: Bool { editing != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(isEditing ? "Edit Provider Config" : "Add Provider Config")
                .font(.headline)

            if !isEditing {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Provider")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Picker("Provider", selection: $selectedProvider) {
                        ForEach(providerOptions, id: \.0) { value, label in
                            Text(label).tag(value)
                        }
                    }
                    .pickerStyle(.segmented)
                }
            } else {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Provider")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(providerLabel(editing!.provider))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }

            VStack(alignment: .leading, spacing: 6) {
                Text(isEditing ? "API Key (leave blank to keep existing)" : "API Key")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                SecureField("Enter API key", text: $apiKey)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Generative Model")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                TextField("e.g. deepseek-chat, gpt-4o, gemini-1.5-pro", text: $model)
                    .textFieldStyle(.roundedBorder)
            }

            if selectedProvider == "openai-compatible" || selectedProvider == "deepseek" || (editing?.baseURL?.isEmpty == false) {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Base URL")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    TextField("e.g. https://api.deepseek.com", text: $baseURL)
                        .textFieldStyle(.roundedBorder)
                }
            }

            if selectedProvider == "google" || selectedProvider == "google-vertex" || (editing?.gcpProject != nil) {
                VStack(alignment: .leading, spacing: 6) {
                    Text("GCP Project")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    TextField("e.g. my-gcp-project", text: $gcpProject)
                        .textFieldStyle(.roundedBorder)
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text("Location")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    TextField("e.g. us-central1", text: $location)
                        .textFieldStyle(.roundedBorder)
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text("Service Account JSON")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    TextEditor(text: $serviceAccountJSON)
                        .font(.system(.caption, design: .monospaced))
                        .frame(minHeight: 120)
                        .overlay(RoundedRectangle(cornerRadius: 6).stroke(Color.secondary.opacity(0.3), lineWidth: 1))
                }
            }

            if let err = saveError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            HStack {
                Spacer()
                Button("Cancel") { isPresented = false }
                    .keyboardShortcut(.escape)
                Button("Save") {
                    Task { await save() }
                }
                .keyboardShortcut(.return)
                .disabled(isSaving)
            }
        }
        .padding(20)
        .frame(minWidth: 400)
        .onAppear { populateFields() }
    }

    private func populateFields() {
        guard let e = editing else { return }
        selectedProvider = e.provider
        baseURL = e.baseURL ?? ""
        model = e.generativeModel ?? ""
        gcpProject = e.gcpProject ?? ""
        location = e.location ?? ""
    }

    @MainActor
    private func save() async {
        isSaving = true
        saveError = nil
        let provider = isEditing ? editing!.provider : selectedProvider
        let actualBaseURL = (selectedProvider == "openai-compatible" || selectedProvider == "deepseek") ? baseURL.trimmingCharacters(in: .whitespaces) : ""
        let actualGCP = (selectedProvider == "google" || selectedProvider == "google-vertex") ? gcpProject.trimmingCharacters(in: .whitespaces) : ""
        let actualLoc = (selectedProvider == "google" || selectedProvider == "google-vertex") ? location.trimmingCharacters(in: .whitespaces) : ""
        let actualSA = (selectedProvider == "google" || selectedProvider == "google-vertex") ? serviceAccountJSON.trimmingCharacters(in: .whitespaces) : ""
        do {
            try await apiClient.saveProjectProviderConfig(
                projectID: projectID,
                provider: provider,
                apiKey: apiKey.trimmingCharacters(in: .whitespaces).isEmpty ? nil : apiKey.trimmingCharacters(in: .whitespaces),
                baseURL: actualBaseURL.isEmpty ? nil : actualBaseURL,
                generativeModel: model.trimmingCharacters(in: .whitespaces).isEmpty ? nil : model.trimmingCharacters(in: .whitespaces),
                serviceAccountJSON: actualSA.isEmpty ? nil : actualSA,
                gcpProject: actualGCP.isEmpty ? nil : actualGCP,
                location: actualLoc.isEmpty ? nil : actualLoc
            )
            isPresented = false
            await onSave()
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
    }

    private func providerLabel(_ provider: String) -> String {
        for (value, label) in providerOptions {
            if value == provider { return label }
        }
        return provider
    }
}

// MARK: - SetProjectPolicySheet

struct SetProjectPolicySheet: View {
    @Binding var isPresented: Bool
    let projectID: String
    let orgID: String
    let provider: String
    let currentPolicy: ProjectPolicy?
    let onSave: () async -> Void

    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var selectedPolicy: String = "none"
    @State private var embeddingModel = ""
    @State private var generativeModel = ""
    @State private var isSaving = false
    @State private var saveError: String? = nil

    private let policyOptions = [
        ("none",         "None"),
        ("organization", "Organization"),
        ("project",      "Project-specific"),
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Configure \(providerDisplayName(provider)) Policy")
                .font(.headline)

            VStack(alignment: .leading, spacing: 6) {
                Text("Policy")
                    .font(.caption).foregroundStyle(.secondary)
                Picker("Policy", selection: $selectedPolicy) {
                    ForEach(policyOptions, id: \.0) { value, label in
                        Text(label).tag(value)
                    }
                }
                .pickerStyle(.segmented)
            }

            if selectedPolicy == "project" {
                VStack(alignment: .leading, spacing: 10) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Embedding Model (optional)")
                            .font(.caption).foregroundStyle(.secondary)
                        TextField("e.g. text-embedding-004", text: $embeddingModel)
                            .textFieldStyle(.roundedBorder)
                    }
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Generative Model (optional)")
                            .font(.caption).foregroundStyle(.secondary)
                        TextField("e.g. gemini-1.5-pro", text: $generativeModel)
                            .textFieldStyle(.roundedBorder)
                    }
                }
            }

            if let err = saveError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            HStack {
                Spacer()
                Button("Cancel") { isPresented = false }
                    .keyboardShortcut(.escape)
                Button("Save") {
                    Task { await save() }
                }
                .keyboardShortcut(.return)
                .disabled(isSaving)
            }
        }
        .padding(20)
        .frame(minWidth: 380)
        .onAppear { applyCurrentPolicy() }
    }

    private func applyCurrentPolicy() {
        if let p = currentPolicy {
            selectedPolicy   = p.policy
            embeddingModel  = p.embeddingModel ?? ""
            generativeModel = p.generativeModel ?? ""
        }
    }

    @MainActor
    private func save() async {
        isSaving = true
        saveError = nil
        do {
            try await apiClient.setProjectPolicy(
                projectID: projectID,
                orgID: orgID,
                provider: provider,
                policy: selectedPolicy,
                embeddingModel: embeddingModel.trimmingCharacters(in: .whitespaces).isEmpty ? nil : embeddingModel.trimmingCharacters(in: .whitespaces),
                generativeModel: generativeModel.trimmingCharacters(in: .whitespaces).isEmpty ? nil : generativeModel.trimmingCharacters(in: .whitespaces)
            )
            isPresented = false
            await onSave()
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
    }

    private func providerDisplayName(_ provider: String) -> String {
        switch provider {
        case "google":           return "Google"
        case "google-vertex":    return "Google Vertex"
        case "deepseek":         return "DeepSeek"
        case "openai-compatible": return "OpenAI Compatible"
        default:                 return provider
        }
    }
}

// MARK: - Error banner component
// (Defined in StateFeedbackViews.swift)
