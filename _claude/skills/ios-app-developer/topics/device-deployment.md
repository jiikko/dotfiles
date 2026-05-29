# Device Deployment

### First-time Setup

1. Connect device via USB
2. Trust computer on device
3. In Xcode: Settings → Accounts → Add Apple ID
4. Select device in scheme dropdown
5. Run (`Cmd + R`)
6. On device: Settings → General → VPN & Device Management → Trust

### Command-line Build (requires paid account)

```bash
xcodebuild \
  -project App.xcodeproj \
  -scheme App \
  -destination 'platform=iOS,name=DeviceName' \
  -allowProvisioningUpdates \
  build
```

### Common Issues

| Error | Solution |
|-------|----------|
| "Library not loaded: @rpath/Framework" | SPM dynamic framework not embedded. Build in Xcode GUI first, then CLI works |
| "No Account for Team" | Add Apple ID in Xcode Settings → Accounts |
| "Provisioning profile not found" | Free account limitation. Use Xcode GUI or get paid account |
| Device not listed | Reconnect USB, trust computer on device, restart Xcode |
| DerivedData won't delete | Close Xcode first: `pkill -9 Xcode && rm -rf ~/Library/Developer/Xcode/DerivedData/PROJECT-*` |

### Free vs Paid Developer Account

| Feature | Free Apple ID | Paid ($99/year) |
|---------|---------------|-----------------|
| Xcode GUI builds | ✅ | ✅ |
| Command-line builds | ❌ | ✅ |
| App validity | 7 days | 1 year |
| App Store | ❌ | ✅ |
| CI/CD | ❌ | ✅ |
