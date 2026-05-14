//
//  ActivityIndicator.swift
//
//
//  Created by Alisa Mylnikova on 01.09.2023.
//

import SwiftUI

struct ActivityIndicator: View {

    @Environment(\.chatTheme) var theme
    var size: CGFloat = 50
    var showBackground = true
    var color: Color? = nil

    var body: some View {
        ZStack {
            if showBackground {
#if os(iOS)
                Color(UIColor.secondarySystemBackground).opacity(0.8)
                    .frame(width: 100, height: 100)
                    .cornerRadius(8)
#else
                Color(.windowBackgroundColor).opacity(0.8)
                    .frame(width: 100, height: 100)
                    .cornerRadius(8)
#endif
            }

            ProgressView()
                .progressViewStyle(.circular)
                .scaleEffect(1.2)
                .frame(width: size, height: size)
        }
    }
}
