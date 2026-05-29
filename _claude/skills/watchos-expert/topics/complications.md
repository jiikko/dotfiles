# Complications

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
