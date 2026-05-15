import SwiftUI

// MARK: - ProvidersView

/// Simple list of project-level LLM providers, fetched from the local Diane API.
/// Each row links to a detail view showing the provider configuration.
struct ProvidersView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var dianeAPI: DianeAPIClient

    @State private var providers: [ProjectProviderInfo] = []
    @State private var isLoading = false
    @State private var error: String? = nil

    var body: some View {
        Group {
            if isLoading && providers.isEmpty {
                VStack(spacing: 12) {
                    ProgressView()
                    Text("Loading providers…")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let err = error {
                ErrorBannerView(message: err) {
                    Task { await load() }
                }
                .padding()
            } else if providers.isEmpty {
                EmptyStateView(
                    title: "No Providers",
                    icon: "gearshape.2",
                    description: "No LLM providers configured for this project."
                )
            } else {
                List(providers) { provider in
                    NavigationLink {
                        ProviderDetailView(provider: provider)
                    } label: {
                        providerRow(provider)
                    }
                }
                .listStyle(.inset)
            }
        }
        .navigationTitle("Providers")
        .task {
            await load()
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        error = nil
        do {
            providers = try await dianeAPI.fetchProjectProviders()
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }

    // MARK: - Row

    private func providerRow(_ p: ProjectProviderInfo) -> some View {
        HStack(spacing: 12) {
            Image(systemName: providerIcon(p.provider))
                .font(.title3)
                .foregroundStyle(providerColor(p.provider))
                .frame(width: 24)

            VStack(alignment: .leading, spacing: 2) {
                Text(providerDisplayName(p.provider))
                    .font(.subheadline)
                    .fontWeight(.medium)
                if let model = p.generativeModel, !model.isEmpty {
                    Text(model)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()
        }
        .padding(.vertical, 4)
    }

    // MARK: - Provider Helpers

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
        case "google", "vertex":    return "Google Vertex"
        case "deepseek":            return "DeepSeek"
        case "mistral":             return "Mistral AI"
        default:                    return name
        }
    }
}

// MARK: - ProviderDetailView

/// Detail view for a single project-level provider.
struct ProviderDetailView: View {
    let provider: ProjectProviderInfo

    var body: some View {
        List {
            Section("Provider") {
                LabeledContent("Name", value: providerDisplayName(provider.provider))

                if let model = provider.generativeModel, !model.isEmpty {
                    LabeledContent("Generative Model", value: model)
                } else {
                    LabeledContent("Generative Model", value: "—")
                }

                if let embed = provider.embeddingModel, !embed.isEmpty {
                    LabeledContent("Embedding Model", value: embed)
                } else {
                    LabeledContent("Embedding Model", value: "—")
                }

                if let url = provider.baseUrl, !url.isEmpty {
                    LabeledContent("Base URL", value: url)
                }
            }
        }
        .listStyle(.inset)
        .navigationTitle(providerDisplayName(provider.provider))
    }

    private func providerDisplayName(_ name: String) -> String {
        switch name.lowercased() {
        case "openai":              return "OpenAI"
        case "anthropic":           return "Anthropic"
        case "google", "vertex":    return "Google Vertex"
        case "deepseek":            return "DeepSeek"
        case "mistral":             return "Mistral AI"
        default:                    return name
        }
    }
}

// MARK: - Previews

#Preview {
    NavigationStack {
        ProvidersView()
            .environmentObject(AppState())
            .environmentObject(DianeAPIClient())
    }
    .frame(width: 400, height: 500)
}

#Preview("Detail") {
    NavigationStack {
        ProviderDetailView(
            provider: ProjectProviderInfo(
                provider: "deepseek",
                baseUrl: "https://api.deepseek.com",
                generativeModel: "deepseek-chat",
                embeddingModel: "gemini-embedding-2-preview"
            )
        )
    }
    .frame(width: 400, height: 300)
}
