# SPM Dependencies

### SPM Dynamic Framework Not Embedded

**Root Cause**: XcodeGen doesn't generate the "Embed Frameworks" build phase for SPM dynamic frameworks (like RealmSwift, Realm). The app builds successfully but crashes on launch with:

```
dyld: Library not loaded: @rpath/RealmSwift.framework/RealmSwift
  Referenced from: /var/containers/Bundle/Application/.../App.app/App
  Reason: image not found
```

**Why This Happens**:
- Static frameworks (most SPM packages) are linked into the binary - no embedding needed
- Dynamic frameworks (RealmSwift, etc.) must be copied into the app bundle
- XcodeGen generates link phase but NOT embed phase for SPM packages
- `embed: true` in project.yml causes build errors (XcodeGen limitation)

**The Fix** (Manual, one-time per project):
1. Open project in Xcode GUI
2. Select target → General → Frameworks, Libraries
3. Find the dynamic framework (RealmSwift)
4. Change "Do Not Embed" → "Embed & Sign"
5. Build and run from Xcode GUI first

**After Manual Fix**: Command-line builds (`xcodebuild`) will work because Xcode persists the embed setting in project.pbxproj.

**Identifying Dynamic Frameworks**:
```bash
# Check if a framework is dynamic
file ~/Library/Developer/Xcode/DerivedData/PROJECT-*/Build/Products/Debug-iphoneos/FRAMEWORK.framework/FRAMEWORK
# Dynamic: "Mach-O 64-bit dynamically linked shared library"
# Static: "current ar archive"
```

### Adding Packages

```yaml
packages:
  AudioKit:
    url: https://github.com/AudioKit/AudioKit
    from: "5.6.5"
  RealmSwift:
    url: https://github.com/realm/realm-swift
    from: "10.54.6"

targets:
  App:
    dependencies:
      - package: AudioKit
      - package: RealmSwift
        product: RealmSwift  # Explicit product name when package has multiple
```

### Resolving Dependencies

```bash
xcodebuild -scmProvider system -resolvePackageDependencies
```

**Never clear global SPM cache** (`~/Library/Caches/org.swift.swiftpm`). Re-downloading is slow.
