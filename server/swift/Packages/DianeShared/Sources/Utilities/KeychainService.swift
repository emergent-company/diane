import Foundation
import Security

/// Secure storage for sensitive credentials using the system Keychain.
/// Works on both iOS and macOS. Provides migration from UserDefaults.
public enum KeychainService {

    private static let serviceName = "com.diane.keychain"

    // MARK: - Keys

    public enum Key: String, CaseIterable {
        case apiKey = "com.diane.apiKey"
        case projectID = "com.diane.projectID"
        case serverURL = "com.diane.serverURL"
    }

    // MARK: - CRUD

    /// Write a string value to the keychain.
    @discardableResult
    public static func set(_ value: String, for key: Key) -> Bool {
        guard let data = value.data(using: .utf8) else { return false }
        delete(key)

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceName,
            kSecAttrAccount as String: key.rawValue,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            kSecValueData as String: data,
        ]

        return SecItemAdd(query as CFDictionary, nil) == errSecSuccess
    }

    /// Read a string value from the keychain.
    public static func get(_ key: Key) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceName,
            kSecAttrAccount as String: key.rawValue,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]

        var result: AnyObject?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let data = result as? Data else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }

    /// Delete a value from the keychain.
    @discardableResult
    public static func delete(_ key: Key) -> Bool {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceName,
            kSecAttrAccount as String: key.rawValue,
        ]

        let status = SecItemDelete(query as CFDictionary)
        return status == errSecSuccess || status == errSecItemNotFound
    }

    /// Clear all Diane keychain entries.
    public static func clearAll() {
        for key in Key.allCases { delete(key) }
    }

    // MARK: - Migration

    /// Migrate values from UserDefaults to Keychain, then clear UserDefaults.
    public static func migrateFromUserDefaults(userDefaults: UserDefaults = .standard) {
        let mappings: [(Key, String)] = [
            (.apiKey, "apiKey"),
            (.projectID, "projectID"),
            (.serverURL, "serverURL"),
        ]
        var didMigrate = false

        for (kcKey, defaultsKey) in mappings {
            guard get(kcKey) == nil else { continue }
            if let value = userDefaults.string(forKey: defaultsKey), !value.isEmpty {
                set(value, for: kcKey)
                userDefaults.removeObject(forKey: defaultsKey)
                didMigrate = true
            }
        }

        if didMigrate {
            #if os(iOS)
            userDefaults.synchronize()
            #endif
        }
    }
}
