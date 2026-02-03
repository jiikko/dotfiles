---
name: watchos-expert
version: 1.0.0
description: Develops watchOS applications for Apple Watch with SwiftUI, WatchKit, and WatchConnectivity. Triggers on watchOS app development, Apple Watch complications, watch-iPhone communication, HealthKit integration, workout tracking, and Watch app optimization. Use when building Apple Watch apps, Watch extensions, or companion iOS apps.
---

# watchOS Development Expert

Build, configure, and optimize Apple Watch applications using SwiftUI, WatchKit, and watchOS-specific frameworks.

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

## SwiftUI for watchOS

### Basic Watch App Entry Point

```swift
import SwiftUI

@main
struct MyWatchApp: App {
    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
```

### Navigation Patterns

```swift
// watchOS 10+ TabView with vertical paging
struct ContentView: View {
    var body: some View {
        TabView {
            HomeView()
            ActivityView()
            SettingsView()
        }
        .tabViewStyle(.verticalPage)
    }
}

// watchOS 9 compatible
struct LegacyNavView: View {
    var body: some View {
        NavigationView {
            List {
                NavigationLink("Item 1") { DetailView() }
                NavigationLink("Item 2") { DetailView() }
            }
            .navigationTitle("My App")
        }
    }
}
```

### Watch-Specific Modifiers

```swift
// Digital Crown input
@State private var crownValue = 0.0

ScrollView {
    // Content
}
.focusable()
.digitalCrownRotation($crownValue)

// Container background (watchOS 10+)
.containerBackground(.blue.gradient, for: .navigation)

// Always-on display support
TimelineView(.everyMinute) { context in
    Text(context.date, style: .time)
}
.environment(\.isLuminanceReduced, true)
```

## Watch-iPhone Communication (WatchConnectivity)

### Session Setup

```swift
import WatchConnectivity

class WatchSessionManager: NSObject, ObservableObject, WCSessionDelegate {
    static let shared = WatchSessionManager()

    @Published var receivedMessage: [String: Any] = [:]

    override init() {
        super.init()
        if WCSession.isSupported() {
            WCSession.default.delegate = self
            WCSession.default.activate()
        }
    }

    // MARK: - WCSessionDelegate

    func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        if let error = error {
            print("WCSession activation failed: \(error)")
        }
    }

    #if os(iOS)
    func sessionDidBecomeInactive(_ session: WCSession) {}
    func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }
    #endif
}
```

### Sending Data

```swift
// Send message (requires reachability)
func sendMessage(_ message: [String: Any]) {
    guard WCSession.default.isReachable else { return }

    WCSession.default.sendMessage(message, replyHandler: { reply in
        print("Reply: \(reply)")
    }, errorHandler: { error in
        print("Error: \(error)")
    })
}

// Transfer user info (queued, delivered when possible)
func transferUserInfo(_ userInfo: [String: Any]) {
    WCSession.default.transferUserInfo(userInfo)
}

// Update application context (latest state only)
func updateContext(_ context: [String: Any]) {
    try? WCSession.default.updateApplicationContext(context)
}
```

### Receiving Data

```swift
// Receive messages
func session(_ session: WCSession, didReceiveMessage message: [String : Any]) {
    DispatchQueue.main.async {
        self.receivedMessage = message
    }
}

// Receive user info
func session(_ session: WCSession, didReceiveUserInfo userInfo: [String : Any] = [:]) {
    DispatchQueue.main.async {
        // Process userInfo
    }
}

// Receive application context
func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String : Any]) {
    DispatchQueue.main.async {
        // Update app state
    }
}
```

## Complications

### WidgetKit Complications (watchOS 9+)

```swift
import WidgetKit
import SwiftUI

struct MyComplication: Widget {
    let kind: String = "MyComplication"

    var body: some WidgetConfiguration {
        StaticConfiguration(kind: kind, provider: Provider()) { entry in
            ComplicationView(entry: entry)
        }
        .configurationDisplayName("My Complication")
        .description("Shows important info")
        .supportedFamilies([
            .accessoryCircular,
            .accessoryRectangular,
            .accessoryInline,
            .accessoryCorner
        ])
    }
}

struct ComplicationView: View {
    var entry: SimpleEntry

    @Environment(\.widgetFamily) var family

    var body: some View {
        switch family {
        case .accessoryCircular:
            Gauge(value: 0.7) {
                Image(systemName: "heart.fill")
            }
            .gaugeStyle(.accessoryCircularCapacity)

        case .accessoryRectangular:
            VStack(alignment: .leading) {
                Text("Steps")
                    .font(.headline)
                Text("8,500")
                    .font(.title2)
            }

        case .accessoryInline:
            Text("8,500 steps")

        default:
            Text("--")
        }
    }
}
```

### Timeline Provider

```swift
struct Provider: TimelineProvider {
    func placeholder(in context: Context) -> SimpleEntry {
        SimpleEntry(date: Date())
    }

    func getSnapshot(in context: Context, completion: @escaping (SimpleEntry) -> ()) {
        completion(SimpleEntry(date: Date()))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<SimpleEntry>) -> ()) {
        let entries = [SimpleEntry(date: Date())]
        let timeline = Timeline(entries: entries, policy: .after(Date().addingTimeInterval(3600)))
        completion(timeline)
    }
}

struct SimpleEntry: TimelineEntry {
    let date: Date
}
```

## HealthKit Integration

### Setup

1. Add HealthKit capability in Xcode
2. Add usage descriptions to Info.plist:
   - `NSHealthShareUsageDescription`
   - `NSHealthUpdateUsageDescription`

### Request Authorization

```swift
import HealthKit

class HealthManager: ObservableObject {
    let healthStore = HKHealthStore()

    func requestAuthorization() async throws {
        let typesToRead: Set<HKObjectType> = [
            HKObjectType.quantityType(forIdentifier: .heartRate)!,
            HKObjectType.quantityType(forIdentifier: .stepCount)!,
            HKObjectType.workoutType()
        ]

        let typesToWrite: Set<HKSampleType> = [
            HKObjectType.workoutType()
        ]

        try await healthStore.requestAuthorization(toShare: typesToWrite, read: typesToRead)
    }
}
```

### Query Data

```swift
func fetchHeartRate() async throws -> Double {
    let heartRateType = HKQuantityType.quantityType(forIdentifier: .heartRate)!
    let predicate = HKQuery.predicateForSamples(withStart: Date().addingTimeInterval(-3600), end: Date())
    let sortDescriptor = NSSortDescriptor(key: HKSampleSortIdentifierEndDate, ascending: false)

    return try await withCheckedThrowingContinuation { continuation in
        let query = HKSampleQuery(sampleType: heartRateType, predicate: predicate, limit: 1, sortDescriptors: [sortDescriptor]) { _, samples, error in
            if let error = error {
                continuation.resume(throwing: error)
                return
            }

            guard let sample = samples?.first as? HKQuantitySample else {
                continuation.resume(returning: 0)
                return
            }

            let heartRate = sample.quantity.doubleValue(for: HKUnit(from: "count/min"))
            continuation.resume(returning: heartRate)
        }

        healthStore.execute(query)
    }
}
```

## Workout Tracking

### Start a Workout Session

```swift
import HealthKit
import WorkoutKit

class WorkoutManager: NSObject, ObservableObject {
    let healthStore = HKHealthStore()
    var session: HKWorkoutSession?
    var builder: HKLiveWorkoutBuilder?

    func startWorkout(workoutType: HKWorkoutActivityType) async throws {
        let configuration = HKWorkoutConfiguration()
        configuration.activityType = workoutType
        configuration.locationType = .outdoor

        session = try HKWorkoutSession(healthStore: healthStore, configuration: configuration)
        builder = session?.associatedWorkoutBuilder()

        builder?.dataSource = HKLiveWorkoutDataSource(healthStore: healthStore, workoutConfiguration: configuration)

        session?.delegate = self
        builder?.delegate = self

        let startDate = Date()
        session?.startActivity(with: startDate)
        try await builder?.beginCollection(at: startDate)
    }

    func endWorkout() async throws {
        session?.end()
        try await builder?.endCollection(at: Date())
        try await builder?.finishWorkout()
    }
}

extension WorkoutManager: HKWorkoutSessionDelegate, HKLiveWorkoutBuilderDelegate {
    func workoutSession(_ workoutSession: HKWorkoutSession, didChangeTo toState: HKWorkoutSessionState, from fromState: HKWorkoutSessionState, date: Date) {
        // Handle state changes
    }

    func workoutSession(_ workoutSession: HKWorkoutSession, didFailWithError error: Error) {
        // Handle errors
    }

    func workoutBuilderDidCollectEvent(_ workoutBuilder: HKLiveWorkoutBuilder) {
        // Handle workout events
    }

    func workoutBuilder(_ workoutBuilder: HKLiveWorkoutBuilder, didCollectDataOf collectedTypes: Set<HKSampleType>) {
        // Process collected data
    }
}
```

## Performance Optimization

### Watch-Specific Considerations

1. **Memory constraints**: Watch has limited RAM (~512MB-1GB)
2. **Battery**: Minimize background activity
3. **Display**: Support always-on display efficiently
4. **Connectivity**: Handle offline scenarios gracefully

### Best Practices

```swift
// Use lightweight images
Image(systemName: "heart.fill")
    .resizable()
    .scaledToFit()
    .frame(width: 40, height: 40)

// Efficient list rendering
List {
    ForEach(items) { item in
        ItemRow(item: item)
    }
}
.listStyle(.carousel)  // watchOS native style

// Background tasks
WKApplication.shared().scheduleBackgroundRefresh(
    withPreferredDate: Date().addingTimeInterval(3600),
    userInfo: nil
) { error in
    if let error = error {
        print("Background refresh failed: \(error)")
    }
}
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
