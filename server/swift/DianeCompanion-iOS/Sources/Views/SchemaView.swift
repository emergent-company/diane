import SwiftUI
import DianeShared

struct SchemaView: View {
    @Environment(\.cloudClient) private var cloudClient

    @State private var schema: SchemaResponse?
    @State private var isLoading = true
    @State private var error: String?

    var body: some View {
        Group {
            if isLoading {
                ProgressView("Loading schema...")
                    .frame(maxHeight: .infinity)
            } else if let err = error {
                ContentUnavailableView(
                    "Could Not Load Schema",
                    systemImage: "point.3.connected.trianglepath.dotted",
                    description: Text(err)
                )
            } else if let schema = schema {
                List {
                    if let types = schema.types, !types.isEmpty {
                        Section("Types (\(types.count))") {
                            ForEach(types) { type in
                                NavigationLink(destination: SchemaTypeDetailView(type: type)) {
                                    SchemaTypeRow(type: type)
                                }
                            }
                        }
                    }

                    if let rels = schema.relationships, !rels.isEmpty {
                        Section("Relationships (\(rels.count))") {
                            ForEach(rels, id: \.name) { rel in
                                RelationshipRow(rel: rel)
                            }
                        }
                    }

                    if let enums = schema.enums, !enums.isEmpty {
                        Section("Enums (\(enums.count))") {
                            ForEach(enums) { enumType in
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(enumType.name)
                                        .font(.body.monospaced())
                                    if let desc = enumType.description, !desc.isEmpty {
                                        Text(desc)
                                            .font(.caption)
                                            .foregroundColor(.secondary)
                                    }
                                    Text("\(enumType.values.count) values")
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                            }
                        }
                    }
                }
                .listStyle(.insetGrouped)
            }
        }
        .navigationTitle("Schema")
        .task { await load() }
        .refreshable { await load() }
        .sentryView("SchemaView")
    }

    private func load() async {
        isLoading = true
        error = nil
        do {
            schema = try await cloudClient.fetchSchema()
        } catch {
            self.error = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - Schema Type Row

struct SchemaTypeRow: View {
    let type: SchemaType

    var body: some View {
        HStack(spacing: DesignTokens.spacingMD) {
            Image(systemName: "square.dashed")
                .font(.caption)
                .foregroundColor(.purple)

            VStack(alignment: .leading, spacing: 2) {
                Text(type.name)
                    .font(.body.monospaced())
                HStack(spacing: DesignTokens.spacingXS) {
                    if let fields = type.fields {
                        Text("\(fields.count) fields")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                    if let desc = type.description, !desc.isEmpty {
                        Text(desc)
                            .font(.caption)
                            .foregroundColor(.secondary)
                            .lineLimit(1)
                    }
                }
            }
        }
    }
}

// MARK: - Relationship Row

struct RelationshipRow: View {
    let rel: SchemaRelationship

    var body: some View {
        HStack(spacing: DesignTokens.spacingSM) {
            Image(systemName: "arrow.triangle.branch")
                .font(.caption)
                .foregroundColor(.teal)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Text(rel.fromType)
                        .font(.caption.monospaced())
                        .fontWeight(.semibold)
                    Image(systemName: "arrow.right")
                        .font(.caption2)
                        .foregroundColor(.secondary)
                    Text(rel.toType)
                        .font(.caption.monospaced())
                        .fontWeight(.semibold)
                }
                Text(relationshipTypeLabel(rel.type))
                    .font(.caption2)
                    .foregroundColor(.secondary)
            }
        }
    }

    private func relationshipTypeLabel(_ type: String) -> String {
        switch type {
        case "one_to_one": return "One to One"
        case "one_to_many": return "One to Many"
        case "many_to_many": return "Many to Many"
        default: return type
        }
    }
}

// MARK: - Schema Type Detail

struct SchemaTypeDetailView: View {
    let type: SchemaType

    var body: some View {
        List {
            if let desc = type.description, !desc.isEmpty {
                Section("Description") {
                    Text(desc)
                        .font(.subheadline)
                        .foregroundColor(.secondary)
                }
            }

            if let fields = type.fields, !fields.isEmpty {
                Section("Fields (\(fields.count))") {
                    ForEach(fields, id: \.name) { field in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: DesignTokens.spacingXS) {
                                Text(field.name)
                                    .font(.body.monospaced())
                                if field.required == true {
                                    Text("required")
                                        .font(.caption2)
                                        .foregroundColor(.orange)
                                        .padding(.horizontal, 4)
                                        .background(Color.orange.opacity(0.1))
                                        .cornerRadius(4)
                                }
                            }

                            HStack(spacing: 4) {
                                Text(typeLabel(field))
                                    .font(.caption.monospaced())
                                    .foregroundColor(.secondary)
                                if field.isList == true {
                                    Text("[]")
                                        .font(.caption.monospaced())
                                        .foregroundColor(.secondary)
                                }
                            }

                            if let desc = field.description, !desc.isEmpty {
                                Text(desc)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle(type.name)
        .navigationBarTitleDisplayMode(.inline)
    }

    private func typeLabel(_ field: SchemaField) -> String {
        var label = field.type
        if let defaultValue = field.defaultValue {
            label += " = \(defaultValue)"
        }
        return label
    }
}
