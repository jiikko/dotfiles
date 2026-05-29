# SwiftUI Best Practices for iOS

### State Management

```swift
// View-local state
@State private var isShowing = false

// Parent-owned state
@Binding var selectedItem: Item?

// Observable objects
@StateObject private var viewModel = ViewModel()  // Create
@ObservedObject var viewModel: ViewModel          // Passed in

// iOS 17+ Observable
@Observable class Store { var items: [Item] = [] }
```

### Navigation (iOS 16+)

```swift
// NavigationStack with programmatic navigation
@State private var path = NavigationPath()

NavigationStack(path: $path) {
    List(items) { item in
        NavigationLink(value: item) {
            Text(item.name)
        }
    }
    .navigationDestination(for: Item.self) { item in
        DetailView(item: item)
    }
}
```

### Async/Await

```swift
.task {
    await loadData()
}

.refreshable {
    await refreshData()
}

func loadData() async {
    do {
        items = try await api.fetchItems()
    } catch {
        self.error = error
    }
}
```
