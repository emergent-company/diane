import SwiftUI

/// A friendly visual overview of the Diane knowledge graph schema.
/// Shows all object types and their relationships in a card-based layout.
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

                        // Relationships section
                        relationshipsSection(rels: s.relationships)
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
                nodeTypesGroup(namespace: ns, types: types)
            }
        }
    }

    private func nodeTypesGroup(namespace: String, types: [SchemaNodeType]) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            namespaceHeader(namespace)

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 320, maximum: 480), spacing: Design.Spacing.md)],
                spacing: Design.Spacing.md
            ) {
                ForEach(types) { type in
                    nodeTypeCard(type)
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
                    .lineLimit(3)
            }

            // Properties count
            if !type.properties.isEmpty {
                Divider()
                    .opacity(0.3)

                Text("\(type.properties.count) propert\(type.properties.count == 1 ? "y" : "ies")")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                // First 5 properties shown inline
                ForEach(type.properties.prefix(5)) { prop in
                    propertyRow(prop)
                }

                if type.properties.count > 5 {
                    Text("+ \(type.properties.count - 5) more…")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
        }
        .cardStyle(cornerRadius: Design.CornerRadius.medium)
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

    // MARK: - Property Row

    private func propertyRow(_ prop: SchemaProperty) -> some View {
        HStack(spacing: Design.Spacing.xs) {
            typeIcon(prop.type)
                .font(.caption2)
                .foregroundStyle(.tertiary)
                .frame(width: 14)

            Text(prop.name)
                .font(.caption2)
                .foregroundStyle(.primary)

            Spacer()

            if let enums = prop.enumValues, !enums.isEmpty {
                let display = enums.count <= 3
                    ? enums.joined(separator: ", ")
                    : "\(enums.count) values"
                Text(display)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .monospaced()
            } else {
                Text(prop.type)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .monospaced()
            }
        }
        .help(prop.description)
    }

    private func typeIcon(_ type: String) -> some View {
        switch type {
        case "string":  return Image(systemName: "text.quote")
        case "number":  return Image(systemName: "number")
        case "integer": return Image(systemName: "number")
        case "boolean": return Image(systemName: "checkmark.circle")
        default:        return Image(systemName: "circle.dashed")
        }
    }

    // MARK: - Relationships Section

    private func relationshipsSection(rels: [SchemaRelationship]) -> some View {
        let filtered = searchText.isEmpty ? rels : rels.filter { rel in
            rel.name.localizedCaseInsensitiveContains(searchText)
            || rel.label.localizedCaseInsensitiveContains(searchText)
            || rel.sourceType.localizedCaseInsensitiveContains(searchText)
            || rel.targetType.localizedCaseInsensitiveContains(searchText)
        }
        .filter { rel in
            guard let ns = selectedNamespace else { return true }
            let sourceGroup = typeNamespace(rel.sourceType)
            let targetGroup = typeNamespace(rel.targetType)
            return sourceGroup == ns || targetGroup == ns
        }

        return VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: "arrow.triangle.branch")
                    .foregroundStyle(.purple)
                    .font(.caption)
                Text("Relationships")
                    .font(.headline)
                Spacer()
                Text("\(filtered.count) of \(rels.count)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(.top, Design.Spacing.sm)

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 280, maximum: 400), spacing: Design.Spacing.md)],
                spacing: Design.Spacing.sm
            ) {
                ForEach(filtered) { rel in
                    relationshipCard(rel)
                }
            }
        }
    }

    private func relationshipCard(_ rel: SchemaRelationship) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            // Source
            typeBadge(rel.sourceType)

            // Arrow and label
            VStack(spacing: 2) {
                Image(systemName: "arrow.right")
                    .font(.caption2)
                    .foregroundStyle(.purple)
                Text(rel.label)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }

            // Target
            typeBadge(rel.targetType)
        }
        .padding(.horizontal, Design.Spacing.md)
        .padding(.vertical, Design.Spacing.sm)
        .background(Design.Surface.cardBackground)
        .cornerRadius(Design.CornerRadius.medium)
        .overlay(
            RoundedRectangle(cornerRadius: Design.CornerRadius.medium)
                .stroke(Design.Surface.border, lineWidth: 1)
        )
        .help(rel.description)
    }

    private func typeBadge(_ typeName: String) -> some View {
        let color = typeNamespaceColor(typeName)
        return Text(shortTypeName(typeName))
            .font(.caption2)
            .fontWeight(.medium)
            .foregroundStyle(color)
            .padding(.horizontal, Design.Padding.badgeH)
            .padding(.vertical, Design.Padding.badgeV)
            .background(color.opacity(0.1))
            .cornerRadius(Design.CornerRadius.small)
    }

    private func shortTypeName(_ name: String) -> String {
        // Shorten common prefixes for display
        let prefixes = ["Calendar", "Financial", "Shopping"]
        for prefix in prefixes {
            if name.hasPrefix(prefix) {
                return String(name.dropFirst(prefix.count))
            }
        }
        return name
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

    private func typeNamespaceColor(_ typeName: String) -> Color {
        let ns = typeNamespace(typeName)
        return namespaceColors[ns] ?? .secondary
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
