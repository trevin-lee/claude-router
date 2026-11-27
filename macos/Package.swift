// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "ClaudeRouterBar",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "ClaudeRouterBar", targets: ["ClaudeRouterBar"]),
    ],
    targets: [
        // Reading claude-router's status and formatting it. No UI, so it stays
        // nonisolated and testable on its own.
        .target(name: "RouterKit"),
        .executableTarget(
            name: "ClaudeRouterBar",
            dependencies: ["RouterKit"],
            swiftSettings: [.defaultIsolation(MainActor.self)]
        ),
        .testTarget(name: "RouterKitTests", dependencies: ["RouterKit"]),
    ]
)
