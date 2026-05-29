---
name: watchos-expert
version: 1.1.0
description: Develops watchOS applications for Apple Watch with SwiftUI, WatchKit, and WatchConnectivity. Triggers on watchOS app development, Apple Watch complications, watch-iPhone communication, HealthKit integration, workout tracking, and Watch app optimization. Use when building Apple Watch apps, Watch extensions, or companion iOS apps.
---

# watchOS Development Expert

Build, configure, and optimize Apple Watch applications using SwiftUI, WatchKit, and watchOS-specific frameworks.

## トピック別リファレンス

作業内容に応じて該当ファイルを **Read してから着手する**（progressive disclosure: 必要なトピックだけ読む）。

| 作業内容 | 参照ファイル |
|---------|-------------|
| Watch アプリの基本 View / Navigation / Digital Crown / Always-on | [`topics/swiftui-watchos.md`](topics/swiftui-watchos.md) |
| iPhone ⇔ Watch 通信 (WatchConnectivity) | [`topics/watch-connectivity.md`](topics/watch-connectivity.md) |
| Complications (WidgetKit) / Timeline Provider | [`topics/complications.md`](topics/complications.md) |
| HealthKit 連携 (認可・クエリ) | [`topics/healthkit.md`](topics/healthkit.md) |
| Workout トラッキング (HKWorkoutSession) | [`topics/workout-tracking.md`](topics/workout-tracking.md) |
| パフォーマンス最適化 (メモリ・バッテリー・背景処理) | [`topics/performance.md`](topics/performance.md) |

以下の Quick Reference / Project Structure / Version Compatibility / Common Issues / Testing は常に有効な横断情報。

## Quick Reference

| Task | Command |
|------|---------|
| Build Watch simulator | `xcodebuild -destination 'platform=watchOS Simulator,name=Apple Watch Series 10 (46mm)' build` |
| List Watch simulators | `xcrun simctl list devices 'watchOS'` |
| Pair Watch to iPhone | Use Xcode's "Devices and Simulators" window |
| Clean DerivedData | `rm -rf ~/Library/Developer/Xcode/DerivedData/PROJECT-*` |

## Project Structure

### Multi-target Watch App (with iOS companion)

```
MyApp/
├── MyApp/                     # iOS app target
│   ├── MyAppApp.swift
│   └── ContentView.swift
├── MyWatch Watch App/         # watchOS app target
│   ├── MyWatchApp.swift
│   ├── ContentView.swift
│   └── Assets.xcassets
└── Shared/                    # Shared code between iOS and watchOS
    └── SharedModels.swift
```

### Standalone Watch App

```
MyWatchApp/
├── MyWatchApp Watch App/
│   ├── MyWatchAppApp.swift
│   ├── ContentView.swift
│   ├── ComplicationController.swift
│   └── Assets.xcassets
└── Package.swift              # Optional SPM dependencies
```

## watchOS Version Compatibility

### API Changes by Version

| watchOS 10+ Only | watchOS 9 Compatible |
|------------------|----------------------|
| `NavigationSplitView` | `NavigationView` |
| `TabView` (vertical paging) | `PageTabViewStyle()` |
| `.containerBackground()` | Custom backgrounds |
| New Workout APIs | WorkoutKit basics |
| Smart Stack widgets | Basic complications |

### Minimum Deployment Target

```yaml
# XcodeGen project.yml
options:
  deploymentTarget:
    watchOS: "9.0"
```

## Common Issues

| Error | Solution |
|-------|----------|
| Watch simulator not paired | Open Xcode → Window → Devices and Simulators → Pair with iPhone simulator |
| WCSession not reachable | Check both Watch and iPhone apps are running, use `transferUserInfo` for queued delivery |
| Complication not updating | Ensure `Timeline` returns proper refresh policy, call `WidgetCenter.shared.reloadAllTimelines()` |
| HealthKit authorization failed | Check entitlements and Info.plist descriptions are set |
| Background refresh not working | Verify WKApplication background modes capability enabled |

## Testing on Simulator

1. Open Xcode → Window → Devices and Simulators
2. Create iPhone and Watch simulator pair
3. Select Watch scheme in Xcode
4. Choose paired Watch simulator as destination
5. Run (`Cmd + R`)

**Tip**: Test WatchConnectivity by running both iPhone and Watch apps simultaneously in their respective simulators.

## Resources

- [watchOS Human Interface Guidelines](https://developer.apple.com/design/human-interface-guidelines/watchos)
- [WatchKit Documentation](https://developer.apple.com/documentation/watchkit)
- [WidgetKit for watchOS](https://developer.apple.com/documentation/widgetkit/creating-a-widget-extension)
- [HealthKit](https://developer.apple.com/documentation/healthkit)
- [WatchConnectivity](https://developer.apple.com/documentation/watchconnectivity)
- [WorkoutKit](https://developer.apple.com/documentation/workoutkit)

## Source

Custom skill created based on Apple's official watchOS documentation and best practices.
