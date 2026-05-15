import SwiftUI

// MARK: - ProvidersView

/// List + detail panel for managing project-level LLM providers.
/// Fetches the provider list from the local Diane API, edits via proxy to Memory Platform.
struct ProvidersView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient
    @EnvironmentObject var serverConfig: ServerConfiguration

    @State private var providers: [ProjectProviderInfo] = []
    @State private var selectedProvider: ProjectProviderInfo? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var showAddSheet = false
    @State private var needsReload = false

    private var projectID: String { serverConfig.projectID }

    var body: some View {
        HSplitView {
            // MARK: Left — Provider List
            VStack(spacing: 0) {
                if isLoading && providers.isEmpty {
                    VStack(spacing: 8) {
                        ProgressView()
                            .scaleEffect(0.8)
                        Text("Loading…")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if let err = error {
                    ErrorBannerView(message: err) {
                        Task { await load() }
                    }
                    .padding()
                } else if providers.isEmpty {
                    Text("No providers configured")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    List(selection: $selectedProvider) {
                        ForEach(providers) { provider in
                            providerRow(provider)
                                .tag(provider)
                        }
                    }
                    .listStyle(.sidebar)
                }
            }
            .frame(minWidth: 200, idealWidth: 260)

            // MARK: Right — Detail / Edit Panel
            if let provider = selectedProvider {
                ProviderEditPanel(
                    provider: provider,
                    projectID: projectID,
                    dianeAPI: dianeAPI
                ) {
                    Task { await load() }
                }
                .id(provider.id)
                .frame(minWidth: 340, idealWidth: 400)
            } else {
                Text("Select a provider to edit")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("Providers")
        .toolbar {
            ToolbarItem {
                Button {
                    showAddSheet = true
                } label: {
                    Image(systemName: "plus")
                }
                .disabled(projectID.isEmpty)
                .help("Add Provider")
            }
            ToolbarItem {
                Button {
                    Task { await load() }
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .help("Reload")
            }
        }
        .task {
            await load()
        }
        .sheet(isPresented: $showAddSheet) {
            AddProviderSheet(projectID: projectID, dianeAPI: dianeAPI) {
                Task { await load() }
            }
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        // Resolve project ID from config if empty
        if projectID.isEmpty, let projects = try? await dianeAPI.fetchProjects(), let first = projects.first {
            serverConfig.projectID = first.id
        }
        isLoading = true
        error = nil
        do {
            providers = try await dianeAPI.fetchProjectProviders()
            // Maintain selection across reloads
            if let sel = selectedProvider, !providers.contains(where: { $0.id == sel.id }) {
                selectedProvider = providers.first
            }
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    // MARK: - Row

    func providerRow(_ p: ProjectProviderInfo) -> some View {
        HStack(spacing: 10) {
            Image(systemName: providerIcon(p.provider))
                .font(.title3)
                .foregroundStyle(providerColor(p.provider))
                .frame(width: 20)

            VStack(alignment: .leading, spacing: 1) {
                Text(providerDisplayName(p.provider))
                    .font(.subheadline)
                    .fontWeight(.medium)
                if let model = p.generativeModel, !model.isEmpty {
                    Text(model)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
        }
        .padding(.vertical, 3)
    }

    func providerIcon(_ name: String) -> String {
        switch name.lowercased() {
        case _ where name.contains("openai"):      return "sparkles.square"
        case _ where name.contains("anthropic"):    return "brain"
        case _ where name.contains("google"):       return "leaf"
        case _ where name.contains("deepseek"):     return "magnifyingglass"
        case _ where name.contains("mistral"):      return "wind"
        default:                                    return "globe"
        }
    }

    func providerColor(_ name: String) -> Color {
        switch name.lowercased() {
        case _ where name.contains("openai"):      return .green
        case _ where name.contains("anthropic"):    return .purple
        case _ where name.contains("google"):       return .blue
        case _ where name.contains("deepseek"):     return .red
        case _ where name.contains("mistral"):      return .orange
        default:                                    return .secondary
        }
    }

    func providerDisplayName(_ name: String) -> String {
        switch name.lowercased() {
        case "openai":              return "OpenAI"
        case "anthropic":           return "Anthropic"
        case "google":              return "Google Vertex"
        case "deepseek":            return "DeepSeek"
        case "mistral":             return "Mistral AI"
        default:                    return name
        }
    }
}

// MARK: - ProviderEditPanel

/// Right-side edit panel for a single provider.
struct ProviderEditPanel: View {
    let provider: ProjectProviderInfo
    let projectID: String
    let dianeAPI: DianeAPIClient
    let onSaved: () async -> Void

    @State private var baseURL: String = ""
    @State private var generativeModel: String = ""
    @State private var embeddingModel: String = ""
    @State private var apiKey: String = ""
    @State private var isSaving = false
    @State private var isDeleting = false
    @State private var saveError: String? = nil
    @State private var showDeleteConfirm = false
    @State private var showTestResult: String? = nil
    @State private var isTesting = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                // Header
                HStack(spacing: 10) {
                    Image(systemName: providerIcon(provider.provider))
                        .font(.title2)
                        .foregroundStyle(providerColor(provider.provider))
                    Text(providerDisplayName(provider.provider))
                        .font(.title3)
                        .fontWeight(.semibold)
                    Spacer()
                }
                .padding(.bottom, 4)

                Divider()

                // Editable fields
                Group {
                    labeledField("Provider", value: providerDisplayName(provider.provider), readOnly: true)

                    labeledField("Base URL", value: baseURL.isEmpty ? "(not set)" : baseURL, readOnly: true)

                    VStack(alignment: .leading, spacing: 4) {
                        Text("Default Generative Model")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        TextField("e.g. deepseek-chat", text: $generativeModel)
                            .textFieldStyle(.roundedBorder)
                        if let models = provider.availableGenerativeModels, !models.isEmpty {
                            modelTagList(models, current: generativeModel)
                        }
                    }

                    VStack(alignment: .leading, spacing: 4) {
                        Text("Default Embedding Model")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        TextField("e.g. text-embedding-004", text: $embeddingModel)
                            .textFieldStyle(.roundedBorder)
                        if let models = provider.availableEmbeddingModels, !models.isEmpty {
                            modelTagList(models, current: embeddingModel)
                        }
                    }

                    VStack(alignment: .leading, spacing: 4) {
                        Text("API Key (leave blank to keep existing)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        SecureField("Enter new API key", text: $apiKey)
                            .textFieldStyle(.roundedBorder)
                    }
                }

                Divider()

                // Actions
                VStack(spacing: 10) {
                    if let err = saveError {
                        Text(err)
                            .font(.caption)
                            .foregroundStyle(.red)
                    }

                    HStack(spacing: 12) {
                        Button("Save") {
                            Task { await save() }
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(isSaving || isDeleting)

                        if isSaving {
                            ProgressView()
                                .scaleEffect(0.7)
                        }

                        Spacer()

                        Button(role: .destructive) {
                            showDeleteConfirm = true
                        } label: {
                            Label("Delete Provider", systemImage: "trash")
                        }
                        .disabled(isDeleting || isSaving)
                        .confirmationDialog(
                            "Delete \(providerDisplayName(provider.provider))?",
                            isPresented: $showDeleteConfirm,
                            titleVisibility: .visible
                        ) {
                            Button("Delete", role: .destructive) {
                                Task { await delete() }
                            }
                            Button("Cancel", role: .cancel) {}
                        } message: {
                            Text("This will permanently remove the provider configuration.")
                        }
                    }

                    // Test button
                    HStack(spacing: 8) {
                        Button("Test Connection") {
                            Task { await test() }
                        }
                        .buttonStyle(.bordered)
                        .disabled(isTesting || isSaving)

                        if isTesting {
                            ProgressView()
                                .scaleEffect(0.7)
                        }

                        if let result = showTestResult {
                            Text(result)
                                .font(.caption)
                                .foregroundStyle(result.hasPrefix("✅") ? .green : .red)
                        }
                    }
                }
            }
            .padding(20)
        }
        .onAppear {
            populateFields()
        }
    }

    // MARK: - Helpers

    private func populateFields() {
        baseURL = provider.baseUrl ?? ""
        generativeModel = provider.generativeModel ?? ""
        embeddingModel = provider.embeddingModel ?? ""
    }

    @MainActor
    private func save() async {
        isSaving = true
        saveError = nil
        do {
            try await dianeAPI.saveProviderConfig(
                projectID: projectID,
                provider: provider.provider,
                apiKey: apiKey.trimmingCharacters(in: .whitespaces).isEmpty ? nil : apiKey.trimmingCharacters(in: .whitespaces),
                baseURL: baseURL.trimmingCharacters(in: .whitespaces).isEmpty ? nil : baseURL.trimmingCharacters(in: .whitespaces),
                generativeModel: generativeModel.trimmingCharacters(in: .whitespaces).isEmpty ? nil : generativeModel.trimmingCharacters(in: .whitespaces),
                embeddingModel: embeddingModel.trimmingCharacters(in: .whitespaces).isEmpty ? nil : embeddingModel.trimmingCharacters(in: .whitespaces)
            )
            await onSaved()
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
    }

    @MainActor
    private func delete() async {
        isDeleting = true
        saveError = nil
        do {
            try await dianeAPI.deleteProviderConfig(projectID: projectID, provider: provider.provider)
            await onSaved()
        } catch {
            saveError = error.localizedDescription
        }
        isDeleting = false
    }

    @MainActor
    private func test() async {
        isTesting = true
        showTestResult = nil
        do {
            struct TestResponse: Decodable {
                let provider: String?
                let model: String?
                let reply: String?
            }
            let projPath = projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? projectID
            let provPath = provider.provider.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? provider.provider
            let data = try await dianeAPI.post(
                "/api/v1/providers/\(provPath)/test?projectId=\(projPath)",
                body: Data()
            )
            if let resp = try? JSONDecoder().decode(TestResponse.self, from: data) {
                showTestResult = "✅ \(resp.model ?? "OK")"
            } else {
                showTestResult = "✅ OK"
            }
        } catch {
            showTestResult = "❌ \(error.localizedDescription)"
        }
        isTesting = false
    }

    private func providerIcon(_ name: String) -> String {
        switch name.lowercased() {
        case _ where name.contains("openai"):      return "sparkles.square"
        case _ where name.contains("anthropic"):    return "brain"
        case _ where name.contains("google"):       return "leaf"
        case _ where name.contains("deepseek"):     return "magnifyingglass"
        default:                                    return "globe"
        }
    }

    private func providerColor(_ name: String) -> Color {
        switch name.lowercased() {
        case _ where name.contains("openai"):      return .green
        case _ where name.contains("anthropic"):    return .purple
        case _ where name.contains("google"):       return .blue
        case _ where name.contains("deepseek"):     return .red
        default:                                    return .secondary
        }
    }

    private func providerDisplayName(_ name: String) -> String {
        switch name.lowercased() {
        case "openai":              return "OpenAI"
        case "anthropic":           return "Anthropic"
        case "google":              return "Google Vertex"
        case "deepseek":            return "DeepSeek"
        case "mistral":             return "Mistral AI"
        default:                    return name
        }
    }

    private func labeledField(_ label: String, value: String, readOnly: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.subheadline)
                .foregroundStyle(readOnly ? .secondary : .primary)
        }
    }

    /// Compact reference list of available model names shown below the TextField.
    @ViewBuilder
    private func modelTagList(_ models: [String], current: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text("Available")
                .font(.caption2)
                .foregroundStyle(.tertiary)
            ForEach(models, id: \.self) { model in
                HStack(spacing: 4) {
                    Circle()
                        .fill(model == current ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 4, height: 4)
                    Text(model)
                        .font(.caption)
                        .foregroundStyle(model == current ? Color.accentColor : .secondary)
                }
                .padding(.vertical, 1)
            }
        }
    }
}

// MARK: - AddProviderSheet

struct AddProviderSheet: View {
    let projectID: String
    let dianeAPI: DianeAPIClient
    let onDone: () async -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var selectedType = "deepseek"
    @State private var apiKey = ""
    @State private var baseURL = ""
    @State private var generativeModel = ""
    @State private var embeddingModel = ""
    @State private var isSaving = false
    @State private var saveError: String? = nil

    private let providerOptions: [(String, String)] = [
        ("deepseek",          "DeepSeek"),
        ("openai-compatible", "OpenAI Compatible"),
        ("google",            "Google Vertex"),
        ("anthropic",         "Anthropic"),
        ("openai",            "OpenAI"),
        ("mistral",           "Mistral AI"),
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Add Provider")
                .font(.headline)

            VStack(alignment: .leading, spacing: 6) {
                Text("Provider").font(.caption).foregroundStyle(.secondary)
                Picker("Provider", selection: $selectedType) {
                    ForEach(providerOptions, id: \.0) { value, label in
                        Text(label).tag(value)
                    }
                }
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("API Key").font(.caption).foregroundStyle(.secondary)
                SecureField("Required", text: $apiKey)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Base URL (optional)").font(.caption).foregroundStyle(.secondary)
                TextField("e.g. https://api.deepseek.com", text: $baseURL)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Generative Model (optional)").font(.caption).foregroundStyle(.secondary)
                TextField("e.g. deepseek-chat", text: $generativeModel)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Embedding Model (optional)").font(.caption).foregroundStyle(.secondary)
                TextField("e.g. text-embedding-004", text: $embeddingModel)
                    .textFieldStyle(.roundedBorder)
            }

            if let err = saveError {
                Text(err).font(.caption).foregroundStyle(.red)
            }

            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.escape)
                Button("Save") {
                    Task { await save() }
                }
                .keyboardShortcut(.return)
                .disabled(isSaving || apiKey.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        }
        .padding(20)
        .frame(width: 400)
    }

    @MainActor
    private func save() async {
        isSaving = true
        saveError = nil
        do {
            try await dianeAPI.saveProviderConfig(
                projectID: projectID,
                provider: selectedType,
                apiKey: apiKey.trimmingCharacters(in: .whitespaces),
                baseURL: baseURL.trimmingCharacters(in: .whitespaces).isEmpty ? nil : baseURL.trimmingCharacters(in: .whitespaces),
                generativeModel: generativeModel.trimmingCharacters(in: .whitespaces).isEmpty ? nil : generativeModel.trimmingCharacters(in: .whitespaces),
                embeddingModel: embeddingModel.trimmingCharacters(in: .whitespaces).isEmpty ? nil : embeddingModel.trimmingCharacters(in: .whitespaces)
            )
            dismiss()
            await onDone()
        } catch {
            saveError = error.localizedDescription
        }
        isSaving = false
    }
}

// MARK: - Previews

#Preview("List + Detail") {
    ProvidersView()
        .environmentObject(AppState())
        .environmentObject(DianeAPIClient())
        .environmentObject(ServerConfiguration())
        .frame(width: 700, height: 500)
}
