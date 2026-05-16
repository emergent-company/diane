// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "DianeExyteChat",
    defaultLocalization: "en",
    platforms: [
        .iOS(.v17),
        .macOS(.v14),
    ],
    products: [
        .library(
            name: "ExyteChat",
            targets: ["ExyteChat"]
        ),
    ],
    targets: [
        .target(
            name: "ExyteChat",
            resources: [.process("Resources")]
        ),
    ]
)
