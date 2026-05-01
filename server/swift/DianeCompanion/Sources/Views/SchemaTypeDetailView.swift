import SwiftUI

/// Detail view for a single schema type showing its properties,
/// relationships, and recent objects from the project's memory graph.
struct SchemaTypeDetailView: View {
    let type: SchemaNodeType
    let allRelationships: [SchemaRelationship]
    let dianeAPI: DianeAPIClient

    @State private var objectsResponse: SchemaObjectsResponse? = nil
    @State private var isLoadingObjects = false
    @State private var objectsError: String? = nil

    private var relatedRelationships: [SchemaRelationship] {
        allRelationships.filter { $0.sourceType == type.typeName || $0.targetType == type.typeName }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Design.Spacing.lg) {
                // Header
                headerSection

                // Stats row
                statsRow

                // Properties
                if !type.properties.isEmpty {
                    propertiesSection
                }

                // Relationships
                if !relatedRelationships.isEmpty {
                    relationshipsSection
                }

                // Recent objects
                recentObjectsSection
            }
            .padding()
        }
        .navigationTitle(type.label)
        .task { await loadObjects() }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.xs) {
            HStack(spacing: Design.Spacing.sm) {
                Text(type.label)
                    .font(.title2)
                    .fontWeight(.bold)

                namespaceBadge(type.namespace)
            }

            Text(type.typeName)
                .font(.subheadline)
                .monospaced()
                .foregroundStyle(.tertiary)

            if !type.description.isEmpty {
                Text(type.description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.top, Design.Spacing.xxs)
            }
        }
    }

    // MARK: - Stats Row

    private var statsRow: some View {
        HStack(spacing: Design.Spacing.lg) {
            statItem(icon: "square.stack.3d.up", value: "\(type.objectCount)", label: "Objects", color: .cyan)
            statItem(icon: "arrow.triangle.branch", value: "\(type.relationshipCount)", label: "Relationships", color: .purple)
            statItem(icon: "list.bullet", value: "\(type.properties.count)", label: "Properties", color: .secondary)
        }
    }

    private func statItem(icon: String, value: String, label: String, color: Color) -> some View {
        HStack(spacing: Design.Spacing.xs) {
            Image(systemName: icon)
                .font(.caption)
                .foregroundStyle(color)
            VStack(alignment: .leading, spacing: 0) {
                Text(value)
                    .font(.subheadline)
                    .fontWeight(.semibold)
                    .monospacedDigit()
                Text(label)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    // MARK: - Properties Section

    private var propertiesSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            SectionHeaderView(icon: "list.bullet.rectangle", title: "Properties", count: type.properties.count)

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 280, maximum: 400), spacing: Design.Spacing.sm)],
                spacing: Design.Spacing.sm
            ) {
                ForEach(type.properties) { prop in
                    propertyCard(prop)
                }
            }
        }
    }

    private func propertyCard(_ prop: SchemaProperty) -> some View {
        HStack(spacing: Design.Spacing.sm) {
            typeIcon(prop.type)
                .font(.callout)
                .foregroundStyle(.tertiary)
                .frame(width: 18)

            VStack(alignment: .leading, spacing: 1) {
                Text(prop.name)
                    .font(.subheadline)
                    .fontWeight(.medium)

                if !prop.description.isEmpty {
                    Text(prop.description)
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .lineLimit(2)
                }
            }

            Spacer()

            if let enums = prop.enumValues, !enums.isEmpty {
                Text(enums.count <= 3
                    ? enums.joined(separator: ", ")
                    : "\(enums.count) values")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .monospaced()
            } else {
                typeTag(prop.type)
            }
        }
        .padding(Design.Padding.card)
        .background(Design.Surface.cardBackground)
        .cornerRadius(Design.CornerRadius.medium)
        .overlay(
            RoundedRectangle(cornerRadius: Design.CornerRadius.medium)
                .stroke(Design.Surface.border, lineWidth: 1)
        )
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

    private func typeTag(_ type: String) -> some View {
        Text(type)
            .font(.caption2)
            .foregroundStyle(.secondary)
            .padding(.horizontal, Design.Padding.badgeH)
            .padding(.vertical, Design.Padding.badgeV)
            .background(Color.secondary.opacity(0.08))
            .cornerRadius(Design.CornerRadius.small)
            .monospaced()
    }

    // MARK: - Relationships Section

    private var relationshipsSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            SectionHeaderView(icon: "arrow.triangle.branch", title: "Relationships", count: relatedRelationships.count)

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 280, maximum: 400), spacing: Design.Spacing.sm)],
                spacing: Design.Spacing.sm
            ) {
                ForEach(relatedRelationships) { rel in
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
        let prefixes = ["Calendar", "Financial", "Shopping"]
        for prefix in prefixes {
            if name.hasPrefix(prefix) {
                return String(name.dropFirst(prefix.count))
            }
        }
        return name
    }

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
        typeNamespace(typeName) == "system" ? Color.purple : Color.blue
    }

    // MARK: - Recent Objects

    private var recentObjectsSection: some View {
        VStack(alignment: .leading, spacing: Design.Spacing.sm) {
            SectionHeaderView(
                icon: "clock.arrow.circlepath",
                title: "Recent Objects",
                count: objectsResponse?.total
            )

            if isLoadingObjects && objectsResponse == nil {
                HStack {
                    Spacer()
                    ProgressView()
                        .controlSize(.small)
                    Text("Loading objects…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                }
                .padding(.vertical, Design.Spacing.md)
            } else if let err = objectsError {
                HStack {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, Design.Spacing.sm)
            } else if let resp = objectsResponse, !resp.objects.isEmpty {
                LazyVGrid(
                    columns: [GridItem(.adaptive(minimum: 280, maximum: 400), spacing: Design.Spacing.sm)],
                    spacing: Design.Spacing.sm
                ) {
                    ForEach(resp.objects) { obj in
                        objectCard(obj)
                    }
                }
            } else if let resp = objectsResponse, resp.objects.isEmpty {
                Text("No objects of this type in the project yet.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, Design.Spacing.sm)
            }
        }
    }

    private func objectCard(_ obj: SchemaObjectSummary) -> some View {
        VStack(alignment: .leading, spacing: Design.Spacing.xs) {
            // Title or entity ID
            HStack(spacing: Design.Spacing.xs) {
                Image(systemName: "cube")
                    .font(.caption2)
                    .foregroundStyle(.cyan)
                Text(obj.title ?? obj.entityID.prefix(12) + "…")
                    .font(.subheadline)
                    .fontWeight(.medium)
                    .lineLimit(1)
                Spacer()
                if obj.relationshipCount > 0 {
                    HStack(spacing: 1) {
                        Image(systemName: "arrow.triangle.branch")
                            .font(.caption2)
                        Text("\(obj.relationshipCount)")
                            .font(.caption2)
                            .monospacedDigit()
                    }
                    .foregroundStyle(.purple)
                    .help("Relationships")
                }
            }

            if let key = obj.key, !key.isEmpty {
                Text(key)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .monospaced()
            }

            if let status = obj.status, !status.isEmpty {
                Text(status)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .badgeStyle(color: statusColor(status))
            }

            // Created at
            Text(formatDate(obj.createdAt))
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .padding(Design.Padding.card)
        .background(Design.Surface.cardBackground)
        .cornerRadius(Design.CornerRadius.medium)
        .overlay(
            RoundedRectangle(cornerRadius: Design.CornerRadius.medium)
                .stroke(Design.Surface.border, lineWidth: 1)
        )
    }

    private func statusColor(_ status: String) -> Color {
        switch status.lowercased() {
        case "active", "open":   return .green
        case "inactive", "closed": return .gray
        case "error", "failed":  return .red
        default:                  return .secondary
        }
    }

    private func formatDate(_ iso: String) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = formatter.date(from: iso) ?? ISO8601DateFormatter().date(from: iso) else {
            return iso
        }
        let df = DateFormatter()
        df.dateFormat = "MMM d, yyyy HH:mm"
        return df.string(from: date)
    }

    // MARK: - Namespace Badge

    private func namespaceBadge(_ namespace: String?) -> some View {
        let ns = namespace ?? "other"
        let color: Color = ns == "system" ? .purple : .blue
        return Text(ns)
            .font(.caption2)
            .foregroundStyle(color)
            .padding(.horizontal, Design.Padding.badgeH)
            .padding(.vertical, 2)
            .background(color.opacity(0.1))
            .cornerRadius(Design.CornerRadius.small)
    }

    // MARK: - Data Loading

    @MainActor
    private func loadObjects() async {
        isLoadingObjects = true
        objectsError = nil
        do {
            objectsResponse = try await dianeAPI.fetchSchemaObjects(typeName: type.typeName, limit: 20)
        } catch {
            objectsError = error.localizedDescription
        }
        isLoadingObjects = false
    }
}

// MARK: - Section Header

private struct SectionHeaderView: View {
    let icon: String
    let title: String
    let count: Int?

    init(icon: String, title: String, count: Int? = nil) {
        self.icon = icon
        self.title = title
        self.count = count
    }

    var body: some View {
        HStack(spacing: Design.Spacing.xs) {
            Image(systemName: icon)
                .foregroundStyle(.secondary)
                .font(.caption)
            Text(title)
                .font(.headline)
            if let c = count {
                Spacer()
                Text("\(c)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.top, Design.Spacing.sm)
    }
}

// MARK: - Previews

#Preview {
    SchemaTypeDetailView(
        type: SchemaNodeType(
            typeName: "MemoryFact",
            label: "Memory Fact",
            description: "A discrete piece of knowledge or information stored in Diane's memory graph.",
            namespace: "personal",
            properties: [
                SchemaProperty(name: "content", type: "string", description: "The factual content", enumValues: nil),
                SchemaProperty(name: "confidence", type: "number", description: "Confidence score 0.0–1.0", enumValues: nil),
                SchemaProperty(name: "source", type: "string", description: "Origin of this fact", enumValues: ["user", "system", "inferred"]),
            ],
            objectCount: 42,
            relationshipCount: 3
        ),
        allRelationships: [
            SchemaRelationship(name: "refers_to", label: "Refers To", inverseLabel: "Referenced By", description: "", sourceType: "MemoryFact", targetType: "Session")
        ],
        dianeAPI: DianeAPIClient()
    )
    .frame(width: 700, height: 600)
}
