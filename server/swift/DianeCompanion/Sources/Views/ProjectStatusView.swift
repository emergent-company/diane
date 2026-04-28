import SwiftUI

/// Project Status view — fetches stats for the selected project from:
///   - GET /api/type-registry/projects/{id}/stats  (object & type counts)
///   - GET /api/documents?limit=1                  (document total)
struct ProjectStatusView: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var apiClient: EmergentAPIClient

    @State private var stats: ProjectStats? = nil
    @State private var isLoading = false
    @State private var errorMessage: String? = nil

    var body: some View {
        VStack(spacing: 0) {
            if let project = appState.selectedProject {
                content(project: project)
                    .task(id: project.id) {
                        await loadStats(for: project)
                    }
            } else {
                EmptyStateView(
                    title: "No Project Selected",
                    icon: "chart.line.uptrend.xyaxis",
                    description: "Select a project from the toolbar to view its status."
                )
            }
        }
        .navigationTitle("Project Status")
    }

    // MARK: - Content

    @ViewBuilder
    private func content(project: Project) -> some View {
        if isLoading {
            LoadingStateView(message: "Loading stats…")
        } else if let err = errorMessage {
            ErrorBannerView(message: err, retryAction: {
                Task { await loadStats(for: project) }
            })
            .padding()
        } else if let s = stats {
            ScrollView {
                LazyVGrid(columns: [
                    GridItem(.flexible()),
                    GridItem(.flexible()),
                    GridItem(.flexible())
                ], spacing: 12) {
                    StatCardView(
                        title: "Objects",
                        value: s.totalObjects.formatted(),
                        icon: "cube",
                        tint: .blue
                    )
                    StatCardView(
                        title: "Object Types",
                        value: "\(s.typesWithObjects) / \(s.totalTypes)",
                        icon: "square.stack.3d.up",
                        tint: .purple
                    )
                    StatCardView(
                        title: "Documents",
                        value: s.totalDocuments.formatted(),
                        icon: "doc.text",
                        tint: .teal
                    )
                    StatCardView(
                        title: "Active Types",
                        value: s.enabledTypes.formatted(),
                        icon: "checkmark.seal",
                        tint: .green
                    )
                }
                .padding()
            }
        } else {
            EmptyStateView(
                title: "No Stats Available",
                icon: "chart.line.uptrend.xyaxis",
                description: "Could not load stats for this project."
            )
        }
    }

    // MARK: - Data Loading

    @MainActor
    private func loadStats(for project: Project) async {
        isLoading = true
        errorMessage = nil
        stats = nil
        defer { isLoading = false }
        do {
            stats = try await apiClient.fetchProjectStats(projectID: project.id)
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
