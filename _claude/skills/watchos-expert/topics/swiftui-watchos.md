# SwiftUI for watchOS

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
