# Performance Optimization

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
