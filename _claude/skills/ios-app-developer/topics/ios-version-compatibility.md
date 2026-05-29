# iOS Version Compatibility

### API Changes by Version

| iOS 17+ Only | iOS 16 Compatible |
|--------------|-------------------|
| `.onChange { old, new in }` | `.onChange { new in }` |
| `ContentUnavailableView` | Custom VStack |
| `AVAudioApplication` | `AVAudioSession` |
| `@Observable` macro | `@ObservableObject` |
| SwiftData | CoreData/Realm |

### Lowering Deployment Target

1. Update `project.yml`:
```yaml
deploymentTarget:
  iOS: "16.0"
```

2. Fix incompatible APIs:
```swift
// iOS 17
.onChange(of: value) { oldValue, newValue in }
// iOS 16
.onChange(of: value) { newValue in }

// iOS 17
ContentUnavailableView("Title", systemImage: "icon")
// iOS 16
VStack {
    Image(systemName: "icon").font(.system(size: 48))
    Text("Title").font(.title2.bold())
}

// iOS 17
AVAudioApplication.shared.recordPermission
// iOS 16
AVAudioSession.sharedInstance().recordPermission
```

3. Regenerate: `xcodegen generate`
