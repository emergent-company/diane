import SwiftUI

/// Messages view — send iMessages via the Messages app.
struct MessagesView: View {
    @StateObject private var manager = MessagesManager()

    @State private var recipient = ""
    @State private var message = ""
    @State private var recentConversations: [String] = []
    @State private var isSending = false
    @State private var error: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            permissionBanner
            Divider()

            if !manager.isAuthorized {
                EmptyStateView(
                    title: "Messages Access Required",
                    icon: "message",
                    description: "Grant automation permission to send iMessages."
                )
            } else {
                composeArea
            }
        }
        .navigationTitle("Messages")
        .task {
            manager.checkAuthorization()
            if let convos = try? await manager.fetchRecentConversations() {
                recentConversations = convos
            }
        }
    }

    @ViewBuilder
    private var permissionBanner: some View {
        HStack {
            Image(systemName: "message")
                .foregroundStyle(manager.isAuthorized ? .green : .secondary)
            Text(manager.isAuthorized ? "Messages Access Ready" : "Messages Access Required")
                .font(.caption)
            Spacer()
            if !manager.isAuthorized {
                Button("Check Access") {
                    manager.checkAuthorization()
                }
                .font(.caption)
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
        }
        .padding(8)
        .background(manager.isAuthorized ? Color.green.opacity(0.05) : Color.orange.opacity(0.05))
    }

    @ViewBuilder
    private var composeArea: some View {
        ScrollView {
            VStack(spacing: 16) {
                if !recentConversations.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Recent Conversations")
                            .font(.caption)
                            .fontWeight(.semibold)
                            .foregroundStyle(.secondary)

                        ForEach(recentConversations.prefix(5), id: \.self) { name in
                            Button(name) {
                                recipient = name
                            }
                            .font(.caption)
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding()
                }

                VStack(spacing: 12) {
                    TextField("Recipient (email or phone)", text: $recipient)
                        .textFieldStyle(.roundedBorder)

                    TextEditor(text: $message)
                        .font(.body)
                        .frame(minHeight: 150)
                        .border(Color.secondary.opacity(0.2))

                    if let err = error {
                        Text(err)
                            .font(.caption)
                            .foregroundStyle(.red)
                    }

                    Button("Send iMessage") {
                        Task { await send() }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(recipient.isEmpty || message.isEmpty || isSending)
                }
                .padding()
            }
        }
    }

    private func send() async {
        isSending = true
        error = nil
        do {
            try await manager.sendMessage(text: message, to: recipient)
            message = ""
        } catch {
            self.error = error.localizedDescription
        }
        isSending = false
    }
}
