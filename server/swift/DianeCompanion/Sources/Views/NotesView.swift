import SwiftUI

/// Notes view — create Apple Notes via AppleScript.
struct NotesView: View {
    @StateObject private var manager = NotesManager()

    @State private var title = ""
    @State private var body_ = ""
    @State private var isCreating = false
    @State private var error: String? = nil
    @State private var lastCreated: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Image(systemName: "note.text")
                    .foregroundStyle(.green)
                Text("Create Notes via AppleScript")
                    .font(.caption)
                Spacer()
            }
            .padding(8)
            .background(Color.green.opacity(0.05))

            Divider()

            ScrollView {
                VStack(spacing: 16) {
                    VStack(spacing: 12) {
                        TextField("Note Title", text: $title)
                            .textFieldStyle(.roundedBorder)

                        TextEditor(text: $body_)
                            .font(.body)
                            .frame(minHeight: 250)
                            .border(Color.secondary.opacity(0.2))

                        if let err = error {
                            Text(err)
                                .font(.caption)
                                .foregroundStyle(.red)
                        }

                        if let created = lastCreated {
                            HStack(spacing: 4) {
                                Image(systemName: "checkmark.circle.fill")
                                    .foregroundStyle(.green)
                                    .font(.caption)
                                Text("Created: \(created)")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }

                        Button("Create Note") {
                            Task { await create() }
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(title.isEmpty || isCreating)
                    }
                    .padding()
                }
            }
        }
        .navigationTitle("Notes")
    }

    private func create() async {
        isCreating = true
        error = nil
        do {
            try await manager.createNote(title: title, body: body_)
            lastCreated = title
            title = ""
            body_ = ""
        } catch {
            self.error = error.localizedDescription
        }
        isCreating = false
    }
}
