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
        .package(url: "https://github.com/johnxnguyen/Down", from: "0.11.0"),
    ],
    targets: [
        .target(
            name: "DianeShared",
            dependencies: [
                .product(name: "Down", package: "Down"),
            ]
        ),
        .testTarget(
            name: "DianeSharedTests",
            dependencies: ["DianeShared"]
        ),
    ]
)
