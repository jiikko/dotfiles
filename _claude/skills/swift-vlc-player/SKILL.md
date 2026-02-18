---
name: swift-vlc-player
version: 1.0.0
description: Builds media player features using VLCKit in Swift for iOS and macOS. Triggers on VLCKit/MobileVLCKit integration, video/audio streaming (RTSP, HLS, RTMP), SwiftUI media player UI, VLCMediaPlayer delegate handling, or network stream playback. Use when adding media playback, building a custom video player, or integrating VLCKit into Swift apps.
---

# Swift VLC Player Expert

Build media playback features using VLCKit/MobileVLCKit in Swift for iOS and macOS applications.

## Quick Reference

| Task | Detail |
|------|--------|
| SPM package URL | `https://github.com/tylerjonesio/vlckit-spm` (v3.6.0) |
| SPM import | `import VLCKitSPM` |
| CocoaPods (macOS) | `pod 'VLCKit', '~> 3.6.0'` |
| CocoaPods (iOS) | `pod 'MobileVLCKit', '~> 3.6.0'` |
| Stable version | 3.6.0 (June 2024) |
| Alpha version | 4.0.0a9 (unified, visionOS support) |
| License | LGPLv2.1 (dynamic linking required) |

## Installation

### Swift Package Manager

```swift
// Package.swift
dependencies: [
    .package(url: "https://github.com/tylerjonesio/vlckit-spm/", .upToNextMajor(from: "3.6.0"))
]
```

Or in Xcode: File > Add Package Dependencies > `https://github.com/tylerjonesio/vlckit-spm`

```swift
import VLCKitSPM
```

### CocoaPods

```ruby
# iOS
pod 'MobileVLCKit', '~> 3.6.0'

# macOS
pod 'VLCKit', '~> 3.6.0'
```

Bridging header (if needed):

```objc
// YourApp-Bridging-Header.h
#import <MobileVLCKit/MobileVLCKit.h>   // iOS
#import <VLCKit/VLCKit.h>               // macOS
```

## Core API: VLCMediaPlayer

### Initialization

```swift
let player = VLCMediaPlayer()

// With custom libvlc options
let player = VLCMediaPlayer(options: ["--network-caching=300"])
```

### Playback Controls

```swift
player.play()
player.pause()
player.stop()

// Seeking
player.position = 0.5                       // 0.0 - 1.0
player.time = VLCTime(int: 60000)           // milliseconds

// Jump (seconds)
player.jumpForward(30)
player.jumpBackward(10)

// Playback rate
player.rate = 1.5

// State queries
player.isPlaying    // Bool
player.isSeekable   // Bool
player.canPause     // Bool
player.state        // VLCMediaPlayerState
```

### Audio Controls

```swift
player.audio?.volume = 100     // 0-200 (100 = normal)
player.audio?.isMuted = true
player.audio?.volumeUp()
player.audio?.volumeDown()

// Track selection
player.currentAudioTrackIndex = 1
```

### Video Properties

```swift
player.videoSize                   // CGSize
player.hasVideoOut                 // Bool
player.currentVideoSubTitleIndex = 1
```

### Player State

```swift
switch player.state {
case .stopped:   break
case .opening:   break   // Connecting to media
case .buffering: break   // Loading data
case .playing:   break
case .paused:    break
case .ended:     break   // Playback finished
case .error:     break
@unknown default: break
}
```

## Delegate (VLCMediaPlayerDelegate)

```swift
class PlayerController: NSObject, VLCMediaPlayerDelegate {
    let player = VLCMediaPlayer()

    func setup() {
        player.delegate = self
    }

    func mediaPlayerStateChanged(_ aNotification: Notification) {
        guard let player = aNotification.object as? VLCMediaPlayer else { return }
        // Update UI based on player.state
    }

    func mediaPlayerTimeChanged(_ aNotification: Notification) {
        guard let player = aNotification.object as? VLCMediaPlayer else { return }
        let current = player.time?.stringValue ?? "--:--"
        let remaining = player.remainingTime?.stringValue ?? "--:--"
        let position = player.position  // 0.0 - 1.0
    }
}
```

## Media Loading

### Local File

```swift
// From bundle
guard let path = Bundle.main.path(forResource: "video", ofType: "mp4") else { return }
let media = VLCMedia(path: path)

// From Documents
guard let docsURL = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first else { return }
let fileURL = docsURL.appendingPathComponent("video.mkv")
let media = VLCMedia(url: fileURL)

player.media = media
player.play()
```

### Network Streams

```swift
// RTSP
guard let url = URL(string: "rtsp://192.168.1.100:554/stream") else { return }
let media = VLCMedia(url: url)
media.addOptions([
    "network-caching": 300,
    "rtsp-tcp": ""          // Force TCP (more reliable)
])

// HLS
guard let url = URL(string: "https://example.com/stream/playlist.m3u8") else { return }
let media = VLCMedia(url: url)
media.addOptions(["network-caching": 1000])

// RTMP
guard let url = URL(string: "rtmp://live.example.com/app/stream_key") else { return }
let media = VLCMedia(url: url)
media.addOptions(["network-caching": 500])

player.media = media
player.play()
```

### Streaming Options

| Option | Description | Low Latency | Stable |
|--------|-------------|-------------|--------|
| `network-caching` | Buffer size (ms) | 150 | 1500 |
| `rtsp-tcp` | Force TCP transport | `""` | `""` |
| `clock-jitter` | Clock jitter tolerance | 0 | (default) |
| `clock-synchro` | Clock sync | 0 | (default) |
| `live-caching` | Live stream buffer (ms) | 150 | (default) |

### Audio-Only Playback

Omit setting `drawable`:

```swift
let player = VLCMediaPlayer()
// No drawable set = audio only
player.media = VLCMedia(url: audioURL)
player.play()
```

## SwiftUI Integration

### Platform-Adaptive Video View

```swift
#if os(macOS)
struct VLCVideoView: NSViewRepresentable {
    let player: VLCMediaPlayer

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        view.wantsLayer = true
        view.layer?.backgroundColor = NSColor.black.cgColor
        player.drawable = view
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {}
}
#else
struct VLCVideoView: UIViewRepresentable {
    let player: VLCMediaPlayer

    func makeUIView(context: Context) -> UIView {
        let view = UIView()
        view.backgroundColor = .black
        player.drawable = view
        return view
    }

    func updateUIView(_ uiView: UIView, context: Context) {}
}
#endif
```

### ViewModel

```swift
class VLCPlayerViewModel: NSObject, ObservableObject, VLCMediaPlayerDelegate {
    let player = VLCMediaPlayer()

    @Published var isPlaying = false
    @Published var currentTime: String = "--:--"
    @Published var remainingTime: String = "--:--"
    @Published var position: Float = 0.0
    @Published var state: VLCMediaPlayerState = .stopped

    override init() {
        super.init()
        player.delegate = self
    }

    func load(url: URL, options: [String: Any]? = nil) {
        let media = VLCMedia(url: url)
        if let options { media.addOptions(options) }
        player.media = media
    }

    func togglePlayPause() {
        if player.isPlaying { player.pause() } else { player.play() }
    }

    func stop() { player.stop() }

    func seek(to position: Float) { player.position = position }

    // MARK: - VLCMediaPlayerDelegate

    func mediaPlayerStateChanged(_ aNotification: Notification) {
        DispatchQueue.main.async { [weak self] in
            guard let self, let p = aNotification.object as? VLCMediaPlayer else { return }
            self.state = p.state
            self.isPlaying = p.isPlaying
        }
    }

    func mediaPlayerTimeChanged(_ aNotification: Notification) {
        DispatchQueue.main.async { [weak self] in
            guard let self, let p = aNotification.object as? VLCMediaPlayer else { return }
            self.currentTime = p.time?.stringValue ?? "--:--"
            self.remainingTime = p.remainingTime?.stringValue ?? "--:--"
            self.position = p.position
        }
    }

    deinit { player.stop() }
}
```

### Player Screen

```swift
struct PlayerScreen: View {
    @StateObject private var viewModel = VLCPlayerViewModel()
    let mediaURL: URL

    var body: some View {
        VStack(spacing: 0) {
            VLCVideoView(player: viewModel.player)
                .aspectRatio(16/9, contentMode: .fit)
                .onAppear {
                    viewModel.load(url: mediaURL)
                    viewModel.player.play()
                }
                .onDisappear {
                    viewModel.stop()
                }

            VStack(spacing: 8) {
                Slider(
                    value: Binding(
                        get: { viewModel.position },
                        set: { viewModel.seek(to: $0) }
                    ),
                    in: 0...1
                )

                HStack {
                    Text(viewModel.currentTime).font(.caption)
                    Spacer()
                    Text(viewModel.remainingTime).font(.caption)
                }

                HStack(spacing: 24) {
                    Button { viewModel.player.jumpBackward(10) } label: {
                        Image(systemName: "gobackward.10")
                    }
                    Button { viewModel.togglePlayPause() } label: {
                        Image(systemName: viewModel.isPlaying ? "pause.fill" : "play.fill")
                            .font(.title)
                    }
                    Button { viewModel.player.jumpForward(10) } label: {
                        Image(systemName: "goforward.10")
                    }
                }
            }
            .padding()
        }
    }
}
```

## macOS vs iOS

| Aspect | macOS (VLCKit) | iOS (MobileVLCKit) |
|--------|---------------|-------------------|
| CocoaPods | `pod 'VLCKit'` | `pod 'MobileVLCKit'` |
| Import (CocoaPods) | `import VLCKit` | `import MobileVLCKit` |
| Import (SPM) | `import VLCKitSPM` | `import VLCKitSPM` |
| Video drawable | `NSView` (requires `wantsLayer = true`) | `UIView` |
| Transcoding | Supported | Not available |

### macOS Sandbox Entitlements

```xml
<!-- For network streams -->
<key>com.apple.security.network.client</key>
<true/>

<!-- For user-selected local files (read only) -->
<key>com.apple.security.files.user-selected.read-only</key>
<true/>

<!-- Recording/Snapshot 機能を使う場合のみ追加 (NSSavePanel 経由で使用) -->
<!-- <key>com.apple.security.files.user-selected.read-write</key> -->
<!-- <true/> -->
```

## Common Issues

| Issue | Solution |
|-------|----------|
| SwiftUI `@Published` not updating UI | Dispatch delegate callbacks to `DispatchQueue.main.async` |
| RTSP slow loading on iOS 16+ | Add `"rtsp-tcp": ""` and lower `network-caching` to 300 |
| Audio muted after resume from pause | Known issue (#615); upgrade to latest version |
| NSView drawable shows nothing (macOS) | Set `view.wantsLayer = true` before assigning as drawable |
| Player crash on dealloc | Always call `player.stop()` in `deinit` / `.onDisappear` |
| UIViewRepresentable zero size in ZStack | Wrap in `GeometryReader` with explicit `.frame()` |
| Bitcode error during build | Set "Enable Bitcode" to No (deprecated since Xcode 14) |
| Multiple simultaneous RTSP streams lag | Limit concurrent streams; reduce resolution/bitrate |

## Race Condition Handling

VLCMediaPlayer の内部は libvlc のバックグラウンドスレッドで動作する。delegate コールバック、状態遷移、メディア切り替えで以下のレースコンディションが発生する。

> **重要**: VLC delegate メソッドは VLC 内部スレッドから呼ばれる。共有プロパティの読み書きにはアトミック操作またはロックが必須。

### 1. Delegate コールバックのスレッド安全性

delegate メソッドは VLC の内部スレッドから呼ばれる。`@Published` プロパティへの書き込みはメインスレッドで行う必要がある。

```swift
// BAD: delegate コールバックは VLC 内部スレッドから呼ばれる
func mediaPlayerStateChanged(_ aNotification: Notification) {
    self.isPlaying = player.isPlaying  // @Published への直接書き込み → data race
}

// GOOD: メインスレッドへディスパッチ + weak self でライフサイクル保護
func mediaPlayerStateChanged(_ aNotification: Notification) {
    DispatchQueue.main.async { [weak self] in
        guard let self else { return }
        self.isPlaying = self.player.isPlaying
    }
}
```

### 2. 世代カウンタのデータ競合

世代カウンタはメインスレッドで書き込み、VLC 内部スレッドで読み取られる。`Int` の読み書きは Swift では非アトミックであり、ロックが必要。

```swift
import os

// BAD: mediaGeneration を直接読み書き → VLC スレッドとの data race
private var mediaGeneration = 0

func mediaPlayerStateChanged(_ aNotification: Notification) {
    let gen = self.mediaGeneration  // VLC 内部スレッドから読み取り → data race!
    DispatchQueue.main.async { ... }
}

// GOOD: OSAllocatedUnfairLock でアトミック化 (macOS 13+ / iOS 16+)
private let _generationLock = OSAllocatedUnfairLock(initialState: 0)

func incrementGeneration() -> Int {
    _generationLock.withLock { state -> Int in
        state += 1
        return state
    }
}

func readGeneration() -> Int {
    _generationLock.withLock { $0 }
}

// macOS 12 / iOS 15 以前は os_unfair_lock_s を直接使うか、
// swift-atomics パッケージの ManagedAtomic<Int> を使う
```

### 3. メディア切り替えレース

再生中にメディアを切り替えると、旧メディアの delegate コールバックが新メディア設定後に到着する。固定遅延 (0.1秒等) ではなく、state callback で `stopped` を検知してから切り替える。

```swift
// BAD: stop() は内部的に非同期。固定遅延で待つのは脆弱
func switchMedia(to url: URL) {
    player.stop()
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) { // マジックナンバー!
        self.player.media = VLCMedia(url: url)
        self.player.play()
    }
}

// GOOD: pendingAction + state callback で stopped を検知してから切り替え
private var pendingMediaURL: URL?

func switchMedia(to url: URL) {
    _ = incrementGeneration()
    pendingMediaURL = url

    if player.isPlaying || player.state == .buffering || player.state == .opening {
        player.stop()
        // → mediaPlayerStateChanged で .stopped を検知後に再生
    } else {
        pendingMediaURL = nil
        player.media = VLCMedia(url: url)
        player.play()
    }
}

func mediaPlayerStateChanged(_ aNotification: Notification) {
    let gen = readGeneration()
    DispatchQueue.main.async { [weak self] in
        guard let self else { return }
        guard self.readGeneration() == gen else { return }

        self.state = self.player.state
        self.isPlaying = self.player.isPlaying

        // stopped 検知後に pending media を再生
        if self.player.state == .stopped, let url = self.pendingMediaURL {
            self.pendingMediaURL = nil
            self.player.media = VLCMedia(url: url)
            self.player.play()
        }
    }
}
```

### 4. Stop/Play の非同期レース

`stop()` は libvlc 内部で非同期的に処理される。直後の `play()` と競合する。

```swift
// BAD: stop と play が内部で競合
player.stop()
player.play()

// GOOD: 状態遷移コールバックで stopped 確認してから次のアクションを実行
func stopThenPlay(url: URL) {
    pendingAction = .playAfterStop(url)
    player.stop()
}

// mediaPlayerStateChanged 内で:
if p.state == .stopped, case .playAfterStop(let url) = self.pendingAction {
    self.pendingAction = nil
    self.player.media = VLCMedia(url: url)
    self.player.play()
}

enum PendingAction {
    case playAfterStop(URL)
}
```

### 5. Dealloc とライフサイクル

`[weak self]` が `deinit` 保護を行うため、`isInvalidated` フラグは `deinit` では不要。明示的な無効化が必要な場合はアトミックフラグを使う。`deinit` 内で `stop()` がブロックするリスクがあるため、事前に `cleanup()` を呼ぶ設計が望ましい。

```swift
// 明示的な無効化（ビューの .onDisappear 等から呼ぶ）
func cleanup() {
    player.delegate = nil
    let playerRef = player
    DispatchQueue.global(qos: .userInitiated).async {
        playerRef.stop()
        playerRef.media = nil
    }
}

// deinit は最小限。stop() のブロックを避ける。
deinit {
    player.delegate = nil
    // stop() は cleanup() で事前に行う。
    // cleanup() が呼ばれなかった場合のフォールバック:
    let playerRef = player
    DispatchQueue.global(qos: .background).async {
        playerRef.stop()
        playerRef.media = nil
    }
}
```

### 6. スレッドセーフな ViewModel (推奨パターン)

上記の問題をすべて解決した ViewModel の実装:

```swift
import os

class VLCPlayerViewModel: NSObject, ObservableObject, VLCMediaPlayerDelegate {
    let player = VLCMediaPlayer()

    @Published var isPlaying = false
    @Published var currentTime: String = "--:--"
    @Published var remainingTime: String = "--:--"
    @Published var position: Float = 0.0
    @Published var state: VLCMediaPlayerState = .stopped
    @Published var isLoading = false

    /// メディア切り替え時のレースを防ぐ世代カウンタ (アトミック)
    private let _generationLock = OSAllocatedUnfairLock(initialState: 0)
    /// 連続操作の競合を防ぐペンディングアクション (メインスレッドからのみアクセス)
    private var pendingAction: PendingAction?

    enum PendingAction {
        case playAfterStop(URL, [String: Any]?)
    }

    override init() {
        super.init()
        player.delegate = self
    }

    private func incrementGeneration() -> Int {
        _generationLock.withLock { state -> Int in
            state += 1
            return state
        }
    }

    private func readGeneration() -> Int {
        _generationLock.withLock { $0 }
    }

    func load(url: URL, options: [String: Any]? = nil) {
        _ = incrementGeneration()
        isLoading = true

        let media = VLCMedia(url: url)
        if let options { media.addOptions(options) }
        player.media = media
    }

    func switchMedia(to url: URL, options: [String: Any]? = nil) {
        _ = incrementGeneration()

        if player.isPlaying || player.state == .buffering || player.state == .opening {
            pendingAction = .playAfterStop(url, options)
            player.stop()
        } else {
            load(url: url, options: options)
            player.play()
        }
    }

    func togglePlayPause() {
        if player.isPlaying { player.pause() } else { player.play() }
    }

    func stop() {
        pendingAction = nil
        player.stop()
    }

    func seek(to position: Float) {
        guard player.isSeekable else { return }
        player.position = position
    }

    /// ビューの .onDisappear から呼ぶ。deinit のブロックを防ぐ。
    func cleanup() {
        player.delegate = nil
        let playerRef = player
        DispatchQueue.global(qos: .userInitiated).async {
            playerRef.stop()
            playerRef.media = nil
        }
    }

    // MARK: - VLCMediaPlayerDelegate

    func mediaPlayerStateChanged(_ aNotification: Notification) {
        let gen = readGeneration()  // アトミック読み取り (VLC 内部スレッド)
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            guard self.readGeneration() == gen else { return }
            guard let p = aNotification.object as? VLCMediaPlayer else { return }

            self.state = p.state
            self.isPlaying = p.isPlaying

            switch p.state {
            case .playing, .paused, .ended, .error:
                self.isLoading = false
            case .opening, .buffering:
                self.isLoading = true
            default:
                break
            }

            // 停止後のペンディングアクションを処理
            if p.state == .stopped, case .playAfterStop(let url, let opts) = self.pendingAction {
                self.pendingAction = nil
                self.load(url: url, options: opts)
                self.player.play()
            }
        }
    }

    func mediaPlayerTimeChanged(_ aNotification: Notification) {
        let gen = readGeneration()  // アトミック読み取り (VLC 内部スレッド)
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            guard self.readGeneration() == gen else { return }
            guard let p = aNotification.object as? VLCMediaPlayer else { return }
            self.currentTime = p.time?.stringValue ?? "--:--"
            self.remainingTime = p.remainingTime?.stringValue ?? "--:--"
            self.position = p.position
        }
    }

    deinit {
        player.delegate = nil
        let playerRef = player
        DispatchQueue.global(qos: .background).async {
            playerRef.stop()
            playerRef.media = nil
        }
    }
}
```

### 7. 複数プレイヤーインスタンスの排他制御

同時に複数の VLCMediaPlayer を使う場合（プレイリストのプリロード等）、リソース競合に注意。`stop()` のブロックを避けるため、dictionary 操作と停止処理を分離する。

```swift
class MultiPlayerController {
    private var activePlayers: [UUID: VLCMediaPlayer] = [:]
    private let queue = DispatchQueue(label: "com.app.vlc-players")

    func createPlayer(id: UUID) -> VLCMediaPlayer {
        let player = VLCMediaPlayer()
        queue.sync { activePlayers[id] = player }
        return player
    }

    func removePlayer(id: UUID) {
        // dictionary の操作だけを同期的に行う
        let player: VLCMediaPlayer? = queue.sync {
            activePlayers.removeValue(forKey: id)
        }

        // stop 処理は queue の外で非同期に行う (queue のブロックを防ぐ)
        guard let player else { return }
        player.delegate = nil
        DispatchQueue.global(qos: .userInitiated).async {
            player.stop()
            player.media = nil
        }
    }

    /// 同時再生数を制限（iOS では 4 ストリーム程度が上限）
    func canCreatePlayer() -> Bool {
        queue.sync { activePlayers.count < 4 }
    }
}
```

### レースコンディション チェックリスト

| チェック項目 | 対策 |
|------------|------|
| delegate コールバックでの `@Published` 更新 | `DispatchQueue.main.async` + `[weak self]` |
| 世代カウンタの読み書き | `OSAllocatedUnfairLock` でアトミック化 |
| メディア切り替え時の旧コールバック | アトミック世代カウンタ + state callback で stopped 検知 |
| `stop()` 直後の `play()` / `media` 設定 | `pendingAction` パターンで状態遷移を待つ (固定遅延は使わない) |
| deinit 時の delegate コールバック | `[weak self]` が保護。`delegate = nil` は早期に行う |
| deinit 内の `stop()` ブロック | `cleanup()` で事前に非同期 stop。deinit は background dispatch |
| 複数プレイヤーのリソース競合 | dictionary 操作と stop 処理を分離、同時数を制限 |
| SwiftUI ビューの再生成 | `@StateObject` で ViewModel のライフサイクルを保護 |

## Hang Prevention and Error Recovery

VLCMediaPlayer の `stop()` や ネットワークストリーム接続はプロセス全体をブロックする可能性がある。以下のパターンでハングアップの影響を局所化する。

### 1. バッファリング/接続タイムアウト

ネットワークストリームが応答しない場合、`opening` / `buffering` 状態が無限に続く。ウォッチドッグタイマーで検知する。

```swift
class VLCPlayerViewModel: NSObject, ObservableObject, VLCMediaPlayerDelegate {
    // ... 既存プロパティ ...

    private var stateWatchdog: DispatchWorkItem?

    /// タイムアウト付きで再生を開始
    func playWithTimeout(url: URL, options: [String: Any]? = nil, timeout: TimeInterval = 15) {
        load(url: url, options: options)
        player.play()
        startWatchdog(timeout: timeout)
    }

    private func startWatchdog(timeout: TimeInterval) {
        stateWatchdog?.cancel()
        let work = DispatchWorkItem { [weak self] in
            guard let self else { return }
            let currentState = self.player.state
            if currentState == .opening || currentState == .buffering {
                self.handleTimeout()
            }
        }
        stateWatchdog = work
        DispatchQueue.main.asyncAfter(deadline: .now() + timeout, execute: work)
    }

    private func cancelWatchdog() {
        stateWatchdog?.cancel()
        stateWatchdog = nil
    }

    private func handleTimeout() {
        player.stop()
        DispatchQueue.main.async { [weak self] in
            self?.state = .error
            self?.isLoading = false
            self?.lastError = .timeout
        }
    }

    // delegate でウォッチドッグを解除
    func mediaPlayerStateChanged(_ aNotification: Notification) {
        DispatchQueue.main.async { [weak self] in
            guard let self, let p = aNotification.object as? VLCMediaPlayer else { return }
            if p.state == .playing || p.state == .error || p.state == .ended {
                self.cancelWatchdog()
            }
            // ... 通常の状態更新処理 ...
        }
    }

    enum PlayerError {
        case timeout
        case networkUnreachable
        case unsupportedFormat
        case unknown
    }

    @Published var lastError: PlayerError?
}
```

### 2. stop() のハングアップ防止

`player.stop()` は libvlc 内部でスレッドの join を待つため、ネットワーク不良時にメインスレッドをブロックする。バックグラウンドスレッドで実行する。

```swift
/// メインスレッドをブロックしない安全な stop
func safeStop(timeout: TimeInterval = 5) {
    player.delegate = nil  // コールバックを即座に停止

    DispatchQueue.global(qos: .userInitiated).async { [weak self] in
        self?.player.stop()
        self?.player.media = nil

        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.isPlaying = false
            self.state = .stopped
            self.isLoading = false
        }
    }

    // タイムアウト後にまだ停止していなければ、新しいプレイヤーに差し替え
    DispatchQueue.main.asyncAfter(deadline: .now() + timeout) { [weak self] in
        guard let self else { return }
        if self.player.state != .stopped {
            // 旧プレイヤーは放棄し新規作成で回復
            self.replacePlayer()
        }
    }
}

/// ハングしたプレイヤーを破棄して新規作成
private func replacePlayer() {
    let oldPlayer = player
    // バックグラウンドで旧プレイヤーの解放を試みる
    DispatchQueue.global(qos: .background).async {
        oldPlayer.delegate = nil
        oldPlayer.stop()
        oldPlayer.media = nil
        // oldPlayer は ARC で解放される
    }

    // 注意: player が let の場合はこのパターンは使えない。
    // その場合は ViewModel 自体を再生成する。
}
```

### 3. エラー状態からの自動リカバリ

ストリーム再生では接続切断が頻繁に発生する。自動リトライで回復する。

```swift
private var retryCount = 0
private let maxRetries = 3
private var currentURL: URL?

func mediaPlayerStateChanged(_ aNotification: Notification) {
    DispatchQueue.main.async { [weak self] in
        guard let self, let p = aNotification.object as? VLCMediaPlayer else { return }

        switch p.state {
        case .error:
            self.handlePlaybackError()
        case .playing:
            self.retryCount = 0  // 再生成功でリセット
            self.cancelWatchdog()
        case .ended:
            self.retryCount = 0
        default:
            break
        }
    }
}

private func handlePlaybackError() {
    guard retryCount < maxRetries, let url = currentURL else {
        lastError = .unknown
        isLoading = false
        return
    }

    retryCount += 1
    let delay = Double(retryCount) * 2.0  // exponential backoff: 2s, 4s, 6s

    DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
        guard let self else { return }
        self.player.stop()
        self.load(url: url)
        self.player.play()
        self.startWatchdog(timeout: 15)
    }
}
```

### 4. VLCMedia のエラーイベント (VLCMediaDelegate)

プレイヤーレベルだけでなく、メディアレベルでもエラーを捕捉する。パース失敗やフォーマット非対応はここで検知する。

```swift
extension VLCPlayerViewModel: VLCMediaDelegate {
    func mediaDidFinishParsing(_ aMedia: VLCMedia) {
        DispatchQueue.main.async { [weak self] in
            if aMedia.parsedStatus == .failed {
                self?.lastError = .unsupportedFormat
                self?.isLoading = false
            }
        }
    }

    func mediaMetaDataDidChange(_ aMedia: VLCMedia) {
        // メタデータ更新時の処理（タイトル、アートワーク等）
    }
}

// メディア設定時に delegate も設定
func load(url: URL, options: [String: Any]? = nil) {
    mediaGeneration += 1
    isLoading = true
    lastError = nil
    currentURL = url

    let media = VLCMedia(url: url)
    media.delegate = self
    if let options { media.addOptions(options) }
    media.parse(withOptions: VLCMediaParsingOptions(rawValue: 0x01 | 0x02))  // local + network
    player.media = media
}
```

### ハングアップ防止チェックリスト

| リスク | 対策 | 影響範囲 |
|-------|------|---------|
| `opening`/`buffering` が無限に続く | ウォッチドッグタイマー (15秒) | UI の応答性 |
| `stop()` がメインスレッドをブロック | バックグラウンドスレッドで実行 | アプリ全体のフリーズ |
| ネットワーク切断で再生が停止 | exponential backoff 付き自動リトライ (最大3回) | ストリーム再生の安定性 |
| メディアパース失敗 | `VLCMediaDelegate` でパース結果を検知 | エラー表示の即時性 |
| プレイヤーが回復不能な状態 | プレイヤーインスタンスを破棄・再生成 | 最終手段としてのリカバリ |

## Recording and Snapshots

```swift
// NSSavePanel でユーザーにパスを選択させるのが最も安全
let panel = NSSavePanel()
panel.allowedContentTypes = [.mpeg2TransportStream]
if panel.runModal() == .OK, let url = panel.url {
    player.startRecording(atPath: url.path)
}
player.stopRecording()

// Save video snapshot
player.saveVideoSnapshot(at: outputPath, withWidth: 1920, andHeight: 1080)
```

> **注意**: `startRecording(atPath:)` / `saveVideoSnapshot(at:)` はファイル書き込みを行う。ユーザー入力からパスを構築する場合はパストラバーサルに注意。Sandbox 有効時は `files.user-selected.read-write` entitlement が必要。

## Third-Party SwiftUI Wrapper

[VLCUI](https://github.com/LePips/VLCUI) (MIT, v0.7.4) provides a ready-made SwiftUI component:

```swift
import VLCUI

guard let url = URL(string: "https://example.com/video.mp4") else { return }
VLCVideoPlayer(url: url)
```

## Security Considerations

### URL バリデーション (SSRF 防止)

ユーザー入力から URL を構築する場合、プロトコルのホワイトリスト検証が必須。VLCKit は `file://`, `smb://`, `ftp://` 等も処理できるため、意図しないプロトコルでの内部リソースアクセスを防ぐ。

```swift
let allowedSchemes: Set<String> = ["rtsp", "rtsps", "rtmp", "http", "https"]

func validateStreamURL(_ urlString: String) -> URL? {
    guard let url = URL(string: urlString),
          let scheme = url.scheme?.lowercased(),
          allowedSchemes.contains(scheme),
          url.host != nil else {
        return nil
    }
    return url
}
```

### パストラバーサル防止

`startRecording(atPath:)` や `saveVideoSnapshot(at:)` にユーザー入力を含める場合:

```swift
func safeOutputPath(baseDir: URL, filename: String) -> URL? {
    let sanitized = filename
        .replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "..", with: "_")
    let fullPath = baseDir.appendingPathComponent(sanitized)
    let resolved = fullPath.standardizedFileURL
    guard resolved.path.hasPrefix(baseDir.standardizedFileURL.path) else {
        return nil  // ベースディレクトリ外への書き込みを拒否
    }
    return resolved
}
```

### libvlc オプションの安全な設定

`media.addOptions()` にユーザー入力を含めないこと。特に `--sout` (ストリーム出力) や `--input-record` はファイルシステム書き込みにつながる。

```swift
// 安全: 許可リストに基づく定数オプション
struct SafeMediaOptions {
    static let networkCachingRange = 100...5000

    static func streaming(networkCaching: Int = 1000) -> [String: Any] {
        let clamped = networkCaching.clamped(to: networkCachingRange)
        return ["network-caching": clamped, "rtsp-tcp": ""]
    }
}
```

## Resources

### Primary Documentation (一次ドキュメント)

- [VLCKit Official Repository (VideoLAN GitLab)](https://code.videolan.org/videolan/VLCKit) -- 公式リポジトリ (GitLab)
- [VLCKit GitHub Mirror](https://github.com/videolan/vlckit) -- GitHub ミラー
- [VLCMediaPlayer Class Reference](https://videolan.videolan.me/VLCKit/interface_v_l_c_media_player.html) -- API リファレンス
- [VLCMedia Class Reference](https://videolan.videolan.me/VLCKit/interface_v_l_c_media.html) -- メディアオブジェクト API
- [VLCAudio Class Reference](https://videolan.videolan.me/VLCKit/interface_v_l_c_audio.html) -- オーディオ制御 API
- [VLCMediaPlayerDelegate Protocol](https://videolan.videolan.me/VLCKit/protocol_v_l_c_media_player_delegate_01-p.html) -- delegate プロトコル
- [VLCMediaDelegate Protocol](https://videolan.videolan.me/VLCKit/protocol_v_l_c_media_delegate_01-p.html) -- メディア delegate プロトコル
- [VLCKit Wiki](https://wiki.videolan.org/VLCKit/) -- 公式 Wiki (セットアップガイド)
- [VLCKit NEWS/Changelog](https://code.videolan.org/videolan/VLCKit/-/blob/master/NEWS) -- バージョン別変更履歴
- [VLCMediaPlayer.h Header](https://code.videolan.org/videolan/VLCKit/-/blob/master/Headers/Public/VLCMediaPlayer.h) -- Obj-C ヘッダ (Swift 型マッピングの確認用)

### Issue Tracker (既知の不具合)

- [#463 SwiftUI Binding Interference](https://code.videolan.org/videolan/VLCKit/-/issues/463) -- SwiftUI `@Published` が更新されない
- [#638 RTSP Loading Regression (iOS 16)](https://code.videolan.org/videolan/VLCKit/-/issues/638) -- RTSP 接続が遅い
- [#615 Audio Muted After Resume](https://code.videolan.org/videolan/VLCKit/-/issues/615) -- pause 後の音声途切れ
- [#399 Player Release Crash](https://code.videolan.org/videolan/VLCKit/-/issues/399) -- 停止前の解放でクラッシュ
- [#302 SPM Support Discussion](https://code.videolan.org/videolan/VLCKit/-/issues/302) -- 公式 SPM サポートの議論
- [#416 Obj-C Modernization for Swift](https://code.videolan.org/videolan/VLCKit/-/issues/416) -- Swift interop 改善

### Packages and Community

- [vlckit-spm (SPM Package)](https://github.com/tylerjonesio/vlckit-spm) -- コミュニティ SPM ラッパー
- [MobileVLCKit on CocoaPods](https://cocoapods.org/pods/MobileVLCKit) -- iOS 用 CocoaPod
- [VLCKit on CocoaPods](https://cocoapods.org/pods/VLCKit) -- macOS 用 CocoaPod
- [VLCUI SwiftUI Wrapper](https://github.com/LePips/VLCUI) -- サードパーティ SwiftUI コンポーネント
