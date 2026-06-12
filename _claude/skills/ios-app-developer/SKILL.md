---
name: ios-app-developer
version: 1.1.0
description: Develops iOS/iPhone applications with XcodeGen, SwiftUI, and SPM. Triggers on XcodeGen project.yml configuration, SPM dependency issues, device deployment problems, code signing errors, camera/AVFoundation capture debugging, iOS version compatibility, or "Library not loaded @rpath" framework errors. Use when building iOS apps, fixing Xcode build failures, or deploying to real devices. Not for AVPlayer playback issues (seek/scrub/frame stepping → avfoundation-reference skill).
---

# iOS App Development

Build, configure, and deploy iOS/iPhone applications using XcodeGen and Swift Package Manager.

## Critical Warnings

| Issue | Cause | Solution |
|-------|-------|----------|
| "Library not loaded: @rpath/Framework" | XcodeGen doesn't auto-embed SPM dynamic frameworks | **Build in Xcode GUI first** (not xcodebuild). See [`topics/spm-dependencies.md`](topics/spm-dependencies.md#spm-dynamic-framework-not-embedded) |
| `xcodegen generate` loses signing | Overwrites project settings | Configure in `project.yml` target settings, not global |
| Command-line signing fails | Free Apple ID limitation | Use Xcode GUI or paid developer account ($99/yr) |
| "Cannot be set when automaticallyAdjustsVideoMirroring is YES" | Setting `isVideoMirrored` without disabling automatic | Set `automaticallyAdjustsVideoMirroring = false` first. See [`topics/camera-avfoundation.md`](topics/camera-avfoundation.md) |

## Quick Reference

| Task | Command |
|------|---------|
| Generate project | `xcodegen generate` |
| Build simulator | `xcodebuild -destination 'platform=iOS Simulator,name=iPhone 17' build` |
| Build device (paid account) | `xcodebuild -destination 'platform=iOS,name=DEVICE' -allowProvisioningUpdates build` |
| Clean DerivedData | `rm -rf ~/Library/Developer/Xcode/DerivedData/PROJECT-*` |
| Find device name | `xcrun xctrace list devices` |

## トピック別リファレンス

作業内容に応じて該当ファイルを **Read してから着手する**（progressive disclosure: 必要なトピックだけ読む）。

| 作業内容 | 参照ファイル |
|---------|-------------|
| XcodeGen project.yml の構成 / コード署名設定 | [`topics/xcodegen-config.md`](topics/xcodegen-config.md) |
| iOS バージョン互換 (iOS 16/17 API 差分・deployment target) | [`topics/ios-version-compatibility.md`](topics/ios-version-compatibility.md) |
| 実機デプロイ / provisioning / Free vs Paid アカウント | [`topics/device-deployment.md`](topics/device-deployment.md) |
| SPM 依存 / `@rpath` dynamic framework embed 問題 | [`topics/spm-dependencies.md`](topics/spm-dependencies.md) |
| カメラ / AVFoundation デバッグ (mirroring・preview sizing) | [`topics/camera-avfoundation.md`](topics/camera-avfoundation.md) |
| SwiftUI 実装パターン (State / Navigation / async) | [`topics/swiftui-ios.md`](topics/swiftui-ios.md) |

## Resources

- [Apple Developer Documentation](https://developer.apple.com/documentation/)
- [SwiftUI Tutorials](https://developer.apple.com/tutorials/swiftui)
- [Human Interface Guidelines](https://developer.apple.com/design/human-interface-guidelines/ios)
- [XcodeGen Documentation](https://github.com/yonaskolb/XcodeGen)

## Source

Skill derived from [daymade/claude-code-skills](https://github.com/daymade/claude-code-skills) marketplace.
