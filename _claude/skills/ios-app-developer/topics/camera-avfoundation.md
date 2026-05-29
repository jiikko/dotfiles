# Camera / AVFoundation

Camera preview requires real device (simulator has no camera).

### Quick Debugging Checklist

1. **Permission**: Added `NSCameraUsageDescription` to Info.plist?
2. **Device**: Running on real device, not simulator?
3. **Session running**: `session.startRunning()` called on background thread?
4. **View size**: UIViewRepresentable has non-zero bounds?
5. **Video mirroring**: Disabled `automaticallyAdjustsVideoMirroring` before setting `isVideoMirrored`?

### Video Mirroring (Front Camera)

**CRITICAL**: Must disable automatic adjustment before setting manual mirroring:

```swift
// WRONG - crashes with "Cannot be set when automaticallyAdjustsVideoMirroring is YES"
connection.isVideoMirrored = true

// CORRECT - disable automatic first
connection.automaticallyAdjustsVideoMirroring = false
connection.isVideoMirrored = true
```

### UIViewRepresentable Sizing Issue

UIViewRepresentable in ZStack may have zero bounds. Fix with explicit frame:

```swift
// BAD: UIViewRepresentable may get zero size in ZStack
ZStack {
    CameraPreviewView(session: session)  // May be invisible!
    OtherContent()
}

// GOOD: Explicit sizing
ZStack {
    GeometryReader { geo in
        CameraPreviewView(session: session)
            .frame(width: geo.size.width, height: geo.size.height)
    }
    .ignoresSafeArea()
    OtherContent()
}
```
