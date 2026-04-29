import SwiftUI
import Contacts

/// Contacts view — search and browse contacts via Contacts framework.
struct ContactsView: View {
    @StateObject private var manager = ContactsManager()

    @State private var searchText = ""
    @State private var contacts: [CNContact] = []
    @State private var isLoading = false
    @State private var selectedContact: CNContact? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            permissionBanner
            Divider()

            if !manager.isAuthorized {
                EmptyStateView(
                    title: "Contacts Access Required",
                    icon: "person.crop.circle",
                    description: "Grant contacts permission to search and browse contacts.",
                    action: { Task { await manager.requestPermission() } },
                    actionLabel: "Grant Access"
                )
            } else {
                contactsContent
            }
        }
        .navigationTitle("Contacts")
    }

    @ViewBuilder
    private var permissionBanner: some View {
        HStack {
            Image(systemName: "person.crop.circle")
                .foregroundStyle(manager.isAuthorized ? .green : .secondary)
            Text(manager.isAuthorized ? "Contacts Access Granted" : "Contacts Access Required")
                .font(.caption)
            Spacer()
            if !manager.isAuthorized {
                Button("Authorize") {
                    Task { await manager.requestPermission() }
                }
                .font(.caption)
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
            }
        }
        .padding(8)
        .background(manager.isAuthorized ? Color.green.opacity(0.05) : Color.orange.opacity(0.05))
    }

    @ViewBuilder
    private var contactsContent: some View {
        HSplitView {
            // Search + list
            VStack(spacing: 0) {
                // Search bar
                HStack {
                    Image(systemName: "magnifyingglass")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    TextField("Search contacts…", text: $searchText)
                        .textFieldStyle(.plain)
                        .font(.caption)
                        .onSubmit { Task { await search() } }
                    if !searchText.isEmpty {
                        Button {
                            searchText = ""
                            contacts = []
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .font(.caption)
                        }
                        .buttonStyle(.borderless)
                    }
                }
                .padding(8)
                .background(Color.primary.opacity(0.03))

                Divider()

                if isLoading {
                    LoadingStateView(message: "Searching…")
                } else if contacts.isEmpty {
                    EmptyStateView(
                        title: searchText.isEmpty ? "Search Contacts" : "No Results",
                        icon: "person.crop.circle",
                        description: searchText.isEmpty
                            ? "Type a name to search your contacts."
                            : "No contacts match \"\(searchText)\"."
                    )
                } else {
                    List(contacts, id: \.identifier, selection: $selectedContact) { contact in
                        contactRow(contact)
                            .tag(contact)
                    }
                    .listStyle(.plain)
                }
            }
            .frame(minWidth: 250)

            // Detail panel
            if let contact = selectedContact {
                contactDetail(contact)
                    .frame(minWidth: 250)
            } else {
                EmptyStateView(
                    title: "Select a Contact",
                    icon: "person.crop.circle",
                    description: "Select a contact to view details."
                )
                .frame(minWidth: 250)
            }
        }
    }

    private func contactRow(_ contact: CNContact) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "person.circle.fill")
                .font(.title3)
                .foregroundStyle(.blue)
            VStack(alignment: .leading, spacing: 2) {
                Text("\(contact.givenName) \(contact.familyName)")
                    .font(.subheadline)
                    .lineLimit(1)
                if let email = contact.emailAddresses.first?.value as String? {
                    Text(email)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, 2)
    }

    private func contactDetail(_ contact: CNContact) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                Image(systemName: "person.circle.fill")
                    .font(.largeTitle)
                    .foregroundStyle(.blue)
                VStack(alignment: .leading, spacing: 4) {
                    Text("\(contact.givenName) \(contact.familyName)")
                        .font(.subheadline)
                        .fontWeight(.semibold)
                }
                Spacer()
            }
            .padding(12)
            .background(Color.primary.opacity(0.04))

            Divider()

            List {
                if !contact.emailAddresses.isEmpty {
                    Section("Email") {
                        ForEach(contact.emailAddresses, id: \.identifier) { email in
                            Text(email.value as String)
                                .font(.caption)
                        }
                    }
                }

                if !contact.phoneNumbers.isEmpty {
                    Section("Phone") {
                        ForEach(contact.phoneNumbers, id: \.identifier) { phone in
                            Text(phone.value.stringValue)
                                .font(.caption)
                        }
                    }
                }
            }
            .listStyle(.plain)
        }
    }

    @MainActor
    private func search() async {
        guard !searchText.isEmpty else {
            contacts = []
            return
        }
        isLoading = true
        do {
            contacts = try manager.searchContacts(query: searchText)
        } catch {
            contacts = []
        }
        isLoading = false
    }
}
