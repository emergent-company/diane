//
//  Created by Alex.M on 02.10.2023.
//

import Foundation
import Combine
#if os(iOS)
import UIKit
#endif

@MainActor
public final class KeyboardState: ObservableObject {
    @Published private(set) public var isShown: Bool = false
    @Published private(set) public var keyboardFrame: CGRect = .zero
    
    private var subscriptions = Set<AnyCancellable>()

    init() {
#if os(iOS)
        subscribeKeyboardNotifications()
#endif
    }

    /// Requests the dismissal of the current / active keyboard
    public func resignFirstResponder() {
#if os(iOS)
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
#endif
    }
}

#if os(iOS)
private extension KeyboardState {
    func subscribeKeyboardNotifications() {
        let pub = Publishers.Merge(
            NotificationCenter.default
                .publisher(for: UIResponder.keyboardWillShowNotification)
                .compactMap { $0.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? NSValue }
                .map { $0.cgRectValue },

            NotificationCenter.default
                .publisher(for: UIResponder.keyboardWillHideNotification)
                .map { _ in .zero }
        )
        .receive(on: RunLoop.main)
        
        pub.assign(to: \.keyboardFrame, on: self).store(in: &subscriptions)
        pub.map { $0 != .zero }.assign(to: \.isShown, on: self).store(in: &subscriptions)
    }
}
#endif
