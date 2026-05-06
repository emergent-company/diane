import Foundation
import Contacts

/// Manages macOS Contacts access via Contacts framework.
@MainActor
final class ContactsManager: ObservableObject {
    private nonisolated(unsafe) let store = CNContactStore()
    
    @Published private(set) var isAuthorized = false
    
    var authorizationStatus: CNAuthorizationStatus {
        CNContactStore.authorizationStatus(for: .contacts)
    }
    
    func requestPermission() async -> Bool {
        do {
            let granted = try await store.requestAccess(for: .contacts)
            isAuthorized = granted
            return granted
        } catch {
            logError("Contacts permission request failed: \(error.localizedDescription)", category: "Contacts")
            return false
        }
    }
    
    func searchContacts(query: String) throws -> [CNContact] {
        let keys = [CNContactGivenNameKey, CNContactFamilyNameKey, CNContactEmailAddressesKey, CNContactPhoneNumbersKey, CNContactIdentifierKey]
        let predicate = CNContact.predicateForContacts(matchingName: query)
        return try store.unifiedContacts(matching: predicate, keysToFetch: keys as [CNKeyDescriptor])
    }
    
    func listAllContacts() throws -> [CNContact] {
        let keys = [CNContactGivenNameKey, CNContactFamilyNameKey, CNContactEmailAddressesKey, CNContactIdentifierKey]
        var all: [CNContact] = []
        let request = CNContactFetchRequest(keysToFetch: keys as [CNKeyDescriptor])
        try store.enumerateContacts(with: request) { contact, _ in
            all.append(contact)
        }
        return all
    }
}
