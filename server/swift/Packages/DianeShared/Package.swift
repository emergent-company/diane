// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "DianeShared",
    platforms: [
        .macOS(.v15),
        .iOS(.v18)
    ],
    products: [
        .library(name: "DianeShared", targets: ["DianeShared"]),
    ],
    targets: [
        .target(
            name: "DianeShared",
            dependencies: []
        ),
        .testTarget(
            name: "DianeSharedTests",
            dependencies: ["DianeShared"]
        ),
    ]
)
