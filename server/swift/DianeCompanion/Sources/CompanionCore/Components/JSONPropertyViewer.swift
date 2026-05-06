import SwiftUI

/// Reusable component for safely rendering complex / large JSON objects
/// in a scrollable, human-readable format.
///
/// Uses a `ScrollView` with progressive rendering to prevent UI thread
/// freezing on deeply nested or massive JSON trees.
///
/// Usage:
/// ```swift
/// JSONPropertyViewer(data: rawJSONData)
/// // or with a dictionary:
/// JSONPropertyViewer(properties: object.properties)
/// ```
struct JSONPropertyViewer: View {
    let properties: [String: Any]?
    private let sortedKeys: [String]

    init(data: Data?) {
        if let data,
           let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            self.properties = json
        } else {
            self.properties = nil
        }
        self.sortedKeys = (properties?.keys.sorted()) ?? []
    }

    init(properties: [String: AnyCodable]?) {
        if let props = properties {
            var dict: [String: Any] = [:]
            for (k, v) in props { dict[k] = v.value }
            self.properties = dict
        } else {
            self.properties = nil
        }
        self.sortedKeys = (properties?.keys.sorted()) ?? []
    }

    var body: some View {
        ScrollView {
            if let props = properties, !props.isEmpty {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(sortedKeys, id: \.self) { key in
                        PropertyRow(key: key, value: props[key])
                    }
                }
                .padding(8)
            } else {
                Text("No properties")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(8)
            }
        }
    }
}

// MARK: - PropertyRow

private struct PropertyRow: View {
    let key: String
    let value: Any?

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(key)
                .font(.system(.caption, design: .monospaced))
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .frame(width: 120, alignment: .trailing)

            Text(valueString)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.primary)
                .lineLimit(5)
                .truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 2)
        Divider()
    }

    private var valueString: String {
        guard let v = value else { return "null" }
        switch v {
        case let str as String:   return str
        case let num as NSNumber: return num.stringValue
        case let bool as Bool:    return bool ? "true" : "false"
        case let arr as [Any]:
            if let data = try? JSONSerialization.data(withJSONObject: arr),
               let str = String(data: data, encoding: .utf8) { return str }
            return "[\(arr.count) items]"
        case let dict as [String: Any]:
            if let data = try? JSONSerialization.data(withJSONObject: dict),
               let str = String(data: data, encoding: .utf8) { return str }
            return "{\(dict.count) keys}"
        default:
            return String(describing: v)
        }
    }
}
