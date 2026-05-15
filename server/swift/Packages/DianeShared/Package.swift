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
    dependencies: [
        .package(url: "https://github.com/gonzalezreal/textual", from: "0.3.1"),
    ],
    targets: [
        .target(
            name: "DianeShared",
            dependencies: [
                .product(name: "Textual", package: "textual", condition: .when(platforms: [.macOS, .iOS])),
            ]
        ),
        .testTarget(
            name: "DianeSharedTests",
            dependencies: ["DianeShared"]
        ),
    ]
)
