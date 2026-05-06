import SwiftUI

/// Mail view — send emails and browse inbox via AppleScript.
struct MailView: View {
    @StateObject private var manager = MailManager()

    @State private var inbox: [MailMessage] = []
    @State private var isLoading = false
    @State private var showCompose = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Image(systemName: "envelope")
                    .foregroundStyle(manager.isAuthorized ? .green : .secondary)
                Text(manager.isAuthorized ? "Mail Access Granted" : "Mail Access Required")
                    .font(.caption)
                Spacer()
                if !manager.isAuthorized {
                    Button("Authorize") {
                        manager.checkAuthorization()
                    }
                    .font(.caption)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                }
            }
            .padding(8)
            .background(manager.isAuthorized ? Color.green.opacity(0.05) : Color.orange.opacity(0.05))

            Divider()

            HStack {
                Button("Compose") { showCompose = true }
                    .font(.caption)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .disabled(!manager.isAuthorized)
                Spacer()
                Button("Refresh") { Task { await loadInbox() } }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
            .padding(8)

            Divider()

            if isLoading {
                LoadingStateView(message: "Loading inbox…")
            } else if inbox.isEmpty {
                EmptyStateView(
                    title: "Inbox",
                    icon: "envelope",
                    description: "Your recent messages will appear here."
                )
            } else {
                List(inbox) { msg in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(msg.subject ?? "(No Subject)")
                            .font(.subheadline)
                            .fontWeight(.medium)
                            .lineLimit(1)
                        HStack(spacing: 6) {
                            if let sender = msg.sender {
                                Text(sender)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }
                            if let date = msg.dateString {
                                Text(date)
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            }
                        }
                    }
                    .padding(.vertical, 2)
                }
                .listStyle(.plain)
            }
        }
        .navigationTitle("Mail")
        .sheet(isPresented: $showCompose) {
            ComposeMailView(manager: manager)
        }
        .task { await loadInbox() }
    }

    @MainActor
    private func loadInbox() async {
        isLoading = true
        do {
            inbox = try await manager.fetchInbox(count: 20)
        } catch {
            inbox = []
        }
        isLoading = false
    }
}

/// Simple compose sheet for sending email.
struct ComposeMailView: View {
    @Environment(\.dismiss) private var dismiss
    let manager: MailManager

    @State private var to = ""
    @State private var subject = ""
    @State private var body_ = ""
    @State private var isSending = false
    @State private var error: String? = nil

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Compose Mail")
                    .font(.headline)
                Spacer()
                Button("Cancel") { dismiss() }
                    .buttonStyle(.borderless)
            }
            .padding()

            Divider()

            VStack(spacing: 12) {
                TextField("To:", text: $to)
                    .textFieldStyle(.roundedBorder)
                TextField("Subject:", text: $subject)
                    .textFieldStyle(.roundedBorder)
                TextEditor(text: $body_)
                    .font(.body)
                    .border(Color.secondary.opacity(0.2))
                    .frame(minHeight: 200)
            }
            .padding()

            if let err = error {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding(.horizontal)
            }

            Divider()

            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .buttonStyle(.bordered)
                Button("Send") { Task { await send() } }
                    .buttonStyle(.borderedProminent)
                    .disabled(to.isEmpty || isSending)
            }
            .padding()
        }
        .frame(width: 500, height: 450)
    }

    private func send() async {
        isSending = true
        error = nil
        do {
            try await manager.sendEmail(to: to, subject: subject, body: body_)
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
        isSending = false
    }
}
