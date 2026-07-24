// swift-tools-version:5.9
import PackageDescription

// Лёгкий исполняемый таргет для меню-бара. Собирается Command Line Tools
// (swift build), полноценный Xcode не нужен. AppKit тянется системный.
let package = Package(
    name: "tinvest-menubar",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(name: "tinvest-menubar")
    ]
)
