import SwiftUI

/// A friendly visual overview of the Diane knowledge graph schema.
/// Shows all object types with object counts, filterable by search and namespace.
/// Each type card links to a detail view with properties, relationships, and recent objects.
struct SchemaView: View {
    @EnvironmentObject var dianeAPI: DianeAPIClient

    @State private var schema: SchemaResponse? = nil
    @State private var isLoading = false
    @State private var error: String? = nil
    @State private var searchText = ""
    @State private var selectedNamespace: String? = nil

    private let namespaceColors: [String: Color] = [
        "personal": Color.blue,
        "system": Color.purple,
    ]

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                // Search + filter bar
                searchFilterBar
                    .padding(.horizontal)
                    .padding(.top, Design.Spacing.sm)

                ScrollView {
                    VStack(alignment: .leading, spacing: Design.Spacing.lg) {
                        if let err = error {
                            ErrorBannerView(message: err) {
                                Task { await load() }
                            }
                        }

                        if isLoading && schema == nil {
                            VStack(spacing: Design.Spacing.md) {
                                ProgressView()
                                Text("Loading schema…")
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                            }
                            .frame(maxWidth: .infinity)
                            .padding(.top, 60)
                        } else if let s = schema {
                            // Overview header
                            overviewHeader(nodeCount: s.nodeTypes.count, relCount: s.relationships.count)

                            // Object types grouped by namespace
                            nodeTypesSection(schema: s)
                        } else {
                            EmptyStateView(
                                title: "No Schema Loaded",
                                icon: "square.grid.3x3",
                                description: "Schema definitions are embedded in the diane binary. Ensure the server is running."
                            )
                            .padding(.top, 60)
                        }
                    }
                    .padding()
                }
            }
            .navigationTitle("Schema")
            .task { await load() }
        }
    }

    // MARK: - Search & Filter Bar

    private var searchFilterBar: some View {
        HStack(spacing: Design.Spacing.sm) {
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                    .font(.caption)
                TextField("Search types…", text: $searchText)
                    .textFieldStyle(.plain)
                    .font(.subheadline)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .background(Design.Surface.cardBackground)
            .cornerRadius(Design.CornerRadius.medium)

            namespaceFilterChip(nil, label: "All")
            namespaceFilterChip("personal", label: "Personal")
            namespaceFilterChip("system", label: "System")
        }
    }

    private func namespaceFilterChip(_ namespace: String?, label: String) -> some View {
        Button(action: {
            withAnimation(.easeInOut(duration: 0.2)) {
                selectedNamespace = selectedNamespace == namespace ? nil : namespace
            }
        }) {
            Text(label)
                .font(.caption)
                .fontWeight(selectedNamespace == namespace ? .semibold : .regular)
                .foregroundStyle(selectedNamespace == namespace ? .white : .secondary)
                .padding(.horizontal, Design.Padding.badgeH)
                .padding(.vertical, Design.Padding.badgeV)
                .background(
                    selectedNamespace == namespace
                        ? (namespaceColors[namespace ?? ""] ?? .accentColor)
                        : Color.secondary.opacity(0.1)
                )
                .cornerRadius(Design.CornerRadius.small)
        }
        .buttonStyle(.plain)
    }

    // MARK: - Overview Header

    private func overviewHeader(nodeCount: Int, relCount: Int) -> some View {
        HStack(spacing: Design.Spacing.lg) {
            statCard(icon: "square.grid.3x3", value: "\(nodeCount)", label: "Object Types", color: .blue)
            statCard(icon: "arrow.triangle.branch", value: "\(relCount)", label: "Relationships", color: .purple)
        }
    }

    private func statCard(icon: String, value: String, label: String, color: Color) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            Image(systemName: icon)
                .font(.title3)
                .foregroundStyle(color)
            VStack(alignment: .leading, spacing: 1) {
                Text(value)
                    .font(.title2)
                    .fontWeight(.bold)
                    .monospacedDigit()
                Text(label)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, Design.Spacing.md)
        .padding(.vertical, Design.Spacing.sm)
        .background(Design.Surface.cardBackground)
        .cornerRadius(Design.CornerRadius.medium)
    }

    // MARK: - Node Types Section

    private func nodeTypesSection(schema: SchemaResponse) -> some View {
        let grouped = Dictionary(grouping: filteredTypes(schema.nodeTypes)) { $0.namespace ?? "other" }
        let namespaceOrder = ["personal", "system", "other"]

        return ForEach(namespaceOrder, id: \.self) { ns in
            if let types = grouped[ns], !types.isEmpty {
                nodeTypesGroup(namespace: ns, types: types, allRelationships: schema.relationships)
            }
        }
    }

    private func nodeTypesGroup(namespace: String, types: [SchemaNodeType], allRelationships: [SchemaRelationship]) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            namespaceHeader(namespace)

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 280, maximum: 380), spacing: Design.Spacing.md)],
                spacing: Design.Spacing.md
            ) {
                ForEach(types) { type in
                    NavigationLink(destination: SchemaTypeDetailView(
                        type: type,
                        allRelationships: allRelationships,
                        dianeAPI: dianeAPI
                    )) {
                        nodeTypeCard(type)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    private func namespaceHeader(_ namespace: String) -> some View {
        HStack(spacing: Design.Spacing.xs) {
            Circle()
                .fill(namespaceColors[namespace] ?? .secondary)
                .frame(width: 8, height: 8)
            Text(namespace == "personal" ? "Personal Data" : namespace == "system" ? "System Types" : "Other")
                .font(.headline)
                .textCase(nil)
            Spacer()
        }
        .padding(.top, Design.Spacing.sm)
    }

    // MARK: - Node Type Card

    private func nodeTypeCard(_ type: SchemaNodeType) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            // Header: type name + namespace badge
            HStack(spacing: Design.Spacing.xs) {
                Text(type.label)
                    .font(.subheadline)
                    .fontWeight(.semibold)
                Spacer()
                namespaceBadge(type.namespace)
            }

            // Type name monospace subtitle
            Text(type.typeName)
                .font(.caption2)
                .foregroundStyle(.tertiary)
                .monospaced()

            // Description
            if !type.description.isEmpty {
                Text(type.description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            Spacer(minLength: Design.Spacing.xs)

            // Stats row: object count + relationship count + property count
            HStack(spacing: Design.Spacing.sm) {
                // Object count
                if type.objectCount > 0 {
                    HStack(spacing: 2) {
                        Image(systemName: "square.stack.3d.up")
                            .font(.caption2)
                        Text("\(type.objectCount)")
                            .font(.caption2)
                            .monospacedDigit()
                    }
                    .foregroundStyle(.cyan)
                }

                // Relationship count
                if type.relationshipCount > 0 {
                    HStack(spacing: 2) {
                        Image(systemName: "arrow.triangle.branch")
                            .font(.caption2)
                        Text("\(type.relationshipCount)")
                            .font(.caption2)
                            .monospacedDigit()
                    }
                    .foregroundStyle(.purple)
                }

                // Property count
                HStack(spacing: 2) {
                    Image(systemName: "list.bullet")
                        .font(.caption2)
                    Text("\(type.properties.count) prop\(type.properties.count == 1 ? "" : "s")")
                        .font(.caption2)
                        .monospacedDigit()
                }
                .foregroundStyle(.secondary)
            }
        }
        .cardStyle(cornerRadius: Design.CornerRadius.medium)
        .frame(minHeight: 120)
    }

    private func namespaceBadge(_ namespace: String?) -> some View {
        let ns = namespace ?? "other"
        let color = namespaceColors[ns] ?? .secondary
        return Text(ns)
            .font(.caption2)
            .foregroundStyle(color)
            .padding(.horizontal, Design.Padding.badgeH)
            .padding(.vertical, 2)
            .background(color.opacity(0.1))
            .cornerRadius(Design.CornerRadius.small)
    }

    // MARK: - Type Namespace Resolution

    private let personalTypes: Set<String> = [
        "MemoryFact", "Person", "Contact", "Task", "Project", "CalendarEvent",
        "FinancialTransaction", "Place", "Note", "Habit", "ShoppingItem",
        "Company", "Item", "Device", "Service", "Subscription", "Document",
        "Trip", "Car", "Insurance", "Invoice"
    ]

    private func typeNamespace(_ typeName: String) -> String {
        if typeName.hasPrefix("Diane") || typeName == "SkillMonitorCheckpoint" {
            return "system"
        }
        return "personal"
    }

    // MARK: - Filtering

    private func filteredTypes(_ types: [SchemaNodeType]) -> [SchemaNodeType] {
        var result = types
        if !searchText.isEmpty {
            result = result.filter { type in
                type.typeName.localizedCaseInsensitiveContains(searchText)
                || type.label.localizedCaseInsensitiveContains(searchText)
                || type.description.localizedCaseInsensitiveContains(searchText)
                || type.properties.contains(where: { $0.name.localizedCaseInsensitiveContains(searchText) })
            }
        }
        if let ns = selectedNamespace {
            result = result.filter { ($0.namespace ?? "other") == ns }
        }
        return result
    }

    // MARK: - Data Loading

    @MainActor
    private func load() async {
        isLoading = true
        error = nil
        do {
            schema = try await dianeAPI.fetchGraphSchema()
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - Previews

#Preview {
    SchemaView()
        .environmentObject(DianeAPIClient())
        .frame(width: 900, height: 700)
}
