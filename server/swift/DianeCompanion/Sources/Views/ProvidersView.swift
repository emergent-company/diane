import SwiftUI

// MARK: - ProvidersView

/// Providers view — org credential management, project provider policies,
/// embedding pipeline status, and per-object-type embedding policies.
///
/// API calls:
///   GET  /api/v1/organizations/{orgId}/providers/credentials
///   POST /api/v1/organizations/{orgId}/providers/google-ai/credentials
///   POST /api/v1/organizations/{orgId}/providers/vertex-ai/credentials
///   DELETE /api/v1/organizations/{orgId}/providers/{provider}/credentials
///   GET  /api/v1/projects/{projectId}/providers/policies
///   PUT  /api/v1/projects/{projectId}/providers/{provider}/policy
///   GET  /api/embeddings/status
///   GET  /api/graph/embedding-policies?project_id=<id>
struct ProvidersView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    // Org credentials
    @State private var orgCredentials: [OrgCredential] = []
    @State private var credentialsError: String? = nil
    @State private var showAddGoogleAI = false
    @State private var showAddVertexAI = false
    @State private var credentialToDelete: OrgCredential? = nil
    @State private var showDeleteConfirmation = false

    // Project policies
    @State private var projectPolicies: [ProjectPolicy] = []
    @State private var projectPoliciesError: String? = nil
    @State private var selectedPolicyForEdit: PolicyEditContext? = nil
    @State private var showSetPolicySheet = false

    // Embedding status (existing)
    @State private var embeddingStatus: EmbeddingStatus? = nil
    @State private var policies: [EmbeddingPolicy] = []
    @State private var isLoading = false
    @State private var statusError: String? = nil
    @State private var policiesError: String? = nil

    // Known providers
    private let knownProviders = ["google-ai", "vertex-ai"]

    var body: some View {
        List {
            // Error banners
            if let err = credentialsError {
                Section {
                    ErrorBannerView(message: "Credentials: \(err)") {
                        Task { await loadCredentials() }
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

            // MARK: Org Credentials section
            Section {
                if appState.selectedProject?.orgId == nil {
                    Text("Select a project to manage organization credentials")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else if isLoading && orgCredentials.isEmpty && credentialsError == nil {
                    HStack {
                        ProgressView().controlSize(.small)
                        Text("Loading…").font(.caption).foregroundStyle(.secondary)
                    }
                } else if orgCredentials.isEmpty && credentialsError == nil {
                    Text("No credentials configured")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(orgCredentials) { cred in
                        credentialRow(cred)
                    }
                }
            } header: {
                HStack {
                    Text("Organization Credentials")
                    Spacer()
                    if appState.selectedProject?.orgId != nil {
                        Menu {
                            Button("Google AI") { showAddGoogleAI = true }
                            Button("Vertex AI") { showAddVertexAI = true }
                        } label: {
                            Image(systemName: "plus")
                                .font(.caption)
                        }
                        .menuStyle(.borderlessButton)
                        .fixedSize()
                    }
                }
            }

            // MARK: Project Provider Policies section
            Section("Project Provider Policies") {
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
                    ForEach(knownProviders, id: \.self) { providerName in
                        projectPolicyRow(provider: providerName)
                    }
                }
            }

            // MARK: Embedding Pipeline section (existing)
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

            // MARK: Embedding Policies section (existing)
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
        .sheet(isPresented: $showAddGoogleAI) {
            if let orgID = appState.selectedProject?.orgId {
                AddGoogleAICredentialSheet(isPresented: $showAddGoogleAI, orgID: orgID) {
                    await loadCredentials()
                }
                .environmentObject(apiClient)
            }
        }
        .sheet(isPresented: $showAddVertexAI) {
            if let orgID = appState.selectedProject?.orgId {
                AddVertexAICredentialSheet(isPresented: $showAddVertexAI, orgID: orgID) {
                    await loadCredentials()
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
            "Delete \(credentialToDelete?.provider ?? "credential")?",
            isPresented: $showDeleteConfirmation,
            titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive) {
                if let cred = credentialToDelete, let orgID = appState.selectedProject?.orgId {
                    Task { await performDeleteCredential(orgID: orgID, cred: cred) }
                }
            }
            Button("Cancel", role: .cancel) { credentialToDelete = nil }
        } message: {
            Text("This will permanently remove the credential for \(credentialToDelete?.provider ?? "this provider").")
        }
    }

    // MARK: - Row builders

    private func credentialRow(_ cred: OrgCredential) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(providerDisplayName(cred.provider))
                    .font(.subheadline)
                if let gcp = cred.gcpProject {
                    Text("GCP: \(gcp)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                if let loc = cred.location {
                    Text("Location: \(loc)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            Button(role: .destructive) {
                credentialToDelete = cred
                showDeleteConfirmation = true
            } label: {
                Image(systemName: "trash")
                    .font(.caption)
                    .foregroundStyle(.red)
            }
            .buttonStyle(.plain)
        }
        .padding(.vertical, 2)
    }

    private func projectPolicyRow(provider: String) -> some View {
        let policy = projectPolicies.first(where: { $0.provider == provider })
        return HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(providerDisplayName(provider))
                    .font(.subheadline)
                if let p = policy {
                    Text(policyDisplayLabel(p.policy))
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                } else {
                    Text("Not configured")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            Button("Configure") {
                selectedPolicyForEdit = PolicyEditContext(provider: provider, policy: policy)
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

    private func providerDisplayName(_ provider: String) -> String {
        switch provider {
        case "google-ai":  return "Google AI"
        case "vertex-ai":  return "Vertex AI"
        default:           return provider
        }
    }

    private func policyDisplayLabel(_ policy: String) -> String {
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
        isLoading = true
        async let a: Void = loadCredentials()
        async let b: Void = loadProjectPolicies()
        async let c: Void = loadStatus()
        async let d: Void = loadPolicies()
        _ = await (a, b, c, d)
        isLoading = false
    }

    @MainActor
    private func loadCredentials() async {
        guard let orgID = appState.selectedProject?.orgId else {
            orgCredentials = []
            credentialsError = nil
            return
        }
        do {
            orgCredentials = try await apiClient.fetchOrgCredentials(orgID: orgID)
            credentialsError = nil
        } catch {
            credentialsError = error.localizedDescription
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
    private func performDeleteCredential(orgID: String, cred: OrgCredential) async {
        do {
            try await apiClient.deleteOrgCredential(orgID: orgID, provider: cred.provider)
            credentialToDelete = nil
            await loadCredentials()
        } catch {
            credentialsError = error.localizedDescription
        }
    }
}

// MARK: - PolicyEditContext

/// Carries context for the SetProjectPolicySheet.
private struct PolicyEditContext {
    let provider: String
    let policy: ProjectPolicy?
}

// MARK: - AddGoogleAICredentialSheet

struct AddGoogleAICredentialSheet: View {
    @Binding var isPresented: Bool
    let orgID: String
    let onSave: () async -> Void

    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var apiKey = ""
    @State private var isSaving = false
    @State private var saveError: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Add Google AI Credential")
                .font(.headline)

            VStack(alignment: .leading, spacing: 6) {
                Text("API Key")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                SecureField("Enter your Google AI API key", text: $apiKey)
                    .textFieldStyle(.roundedBorder)
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
                .disabled(apiKey.trimmingCharacters(in: .whitespaces).isEmpty || isSaving)
            }
        }
        .padding(20)
        .frame(minWidth: 360)
    }

    @MainActor
    private func save() async {
        isSaving = true
        saveError = nil
        do {
            try await apiClient.saveGoogleAICredential(orgID: orgID, apiKey: apiKey.trimmingCharacters(in: .whitespaces))
            isPresented = false
            await onSave()
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
    }
}

// MARK: - AddVertexAICredentialSheet

struct AddVertexAICredentialSheet: View {
    @Binding var isPresented: Bool
    let orgID: String
    let onSave: () async -> Void

    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var gcpProject = ""
    @State private var location = ""
    @State private var serviceAccountJSON = ""
    @State private var isSaving = false
    @State private var saveError: String? = nil

    private var canSave: Bool {
        !gcpProject.trimmingCharacters(in: .whitespaces).isEmpty &&
        !location.trimmingCharacters(in: .whitespaces).isEmpty &&
        !serviceAccountJSON.trimmingCharacters(in: .whitespaces).isEmpty
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Add Vertex AI Credential")
                .font(.headline)

            VStack(alignment: .leading, spacing: 6) {
                Text("GCP Project")
                    .font(.caption).foregroundStyle(.secondary)
                TextField("e.g. my-gcp-project", text: $gcpProject)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Location")
                    .font(.caption).foregroundStyle(.secondary)
                TextField("e.g. us-central1", text: $location)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Service Account JSON")
                    .font(.caption).foregroundStyle(.secondary)
                TextEditor(text: $serviceAccountJSON)
                    .font(.system(.caption, design: .monospaced))
                    .frame(minHeight: 120)
                    .overlay(RoundedRectangle(cornerRadius: 6).stroke(Color.secondary.opacity(0.3), lineWidth: 1))
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
                .disabled(!canSave || isSaving)
            }
        }
        .padding(20)
        .frame(minWidth: 420)
    }

    @MainActor
    private func save() async {
        isSaving = true
        saveError = nil
        do {
            try await apiClient.saveVertexAICredential(
                orgID: orgID,
                serviceAccountJSON: serviceAccountJSON.trimmingCharacters(in: .whitespaces),
                gcpProject: gcpProject.trimmingCharacters(in: .whitespaces),
                location: location.trimmingCharacters(in: .whitespaces)
            )
            isPresented = false
            await onSave()
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
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
            embeddingModel   = p.embeddingModel ?? ""
            generativeModel  = p.generativeModel ?? ""
        }
    }

    private func providerDisplayName(_ p: String) -> String {
        switch p {
        case "google-ai": return "Google AI"
        case "vertex-ai": return "Vertex AI"
        default:          return p
        }
    }

    @MainActor
    private func save() async {
        isSaving = true
        saveError = nil
        let emb  = embeddingModel.trimmingCharacters(in: .whitespaces).isEmpty  ? nil : embeddingModel.trimmingCharacters(in: .whitespaces)
        let gen  = generativeModel.trimmingCharacters(in: .whitespaces).isEmpty ? nil : generativeModel.trimmingCharacters(in: .whitespaces)
        do {
            try await apiClient.setProjectPolicy(
                projectID: projectID,
                orgID: orgID,
                provider: provider,
                policy: selectedPolicy,
                embeddingModel: selectedPolicy == "project" ? emb : nil,
                generativeModel: selectedPolicy == "project" ? gen : nil
            )
            isPresented = false
            await onSave()
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
    }
}
