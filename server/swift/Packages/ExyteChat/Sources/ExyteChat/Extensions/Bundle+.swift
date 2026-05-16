//
//  Bundle+.swift
//  
//
//  Created by Alex.M on 07.07.2022.
//

import Foundation

public extension Bundle {
    static var current: Bundle {
        Bundle.module
    }
}
