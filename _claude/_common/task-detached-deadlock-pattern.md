# Task.detached による MainActor デッドロック回避パターン

複数エージェント（swift-concurrency-expert / swiftui-test-expert / test-runner / test-coverage-advisor）から
参照される共通パターン。エージェント側にはコピペせず、必要になったらこのファイルを Read すること。

## 問題の構造

MainActor 上のコード（またはテストランナーのメインスレッド）から、内部で MainActor への
ディスパッチを含む同期 API を直接呼ぶと、相互待ちでデッドロックする。

```swift
// ❌ DEADLOCK: semaphore がメインスレッドを塞ぎ、MainActor タスクが永遠に走れない
func handleAPIRequest() -> Response {
    let semaphore = DispatchSemaphore(value: 0)
    var result: Response?

    Task { @MainActor in
        result = await processOnMainActor()
        semaphore.signal()  // メインスレッドが塞がっていると到達しない
    }

    semaphore.wait()  // 呼び出し元がメインスレッドならここで詰む
    return result!
}

// ✅ SOLUTION: Task.detached で MainActor 依存を切る
func handleAPIRequestSafe() async -> Response {
    await Task.detached {
        await MainActor.run {
            processOnMainActor()
        }
    }.value
}
```

## テストでの必須パターン（ThumbnailThumb: issue done/096-api-handler-deadlock.md）

```swift
// ❌ FORBIDDEN: handler の直接呼び出しは MainActor デッドロック
// SwiftLint rule `handler_direct_call_in_tests` が ERROR で検出する
func test_badExample() {
    let result = StatusHandler.handleGetStatus()  // 🚫 BLOCKS
}

// ✅ REQUIRED: すべての API handler テストは Task.detached で包む
func test_goodExample() async {
    let result = await Task.detached {
        StatusHandler.handleGetStatus()
    }.value

    XCTAssertEqual(result.status, "ok")
}
```

## ハング時のスタックパターン早見表

| Stack Pattern | Cause | Fix |
|---------------|-------|-----|
| `dispatch_semaphore_wait` + `MainActor` | Semaphore deadlock | `Task.detached` で包む（issue #096） |
| `swift_task_switch` stuck | Async deadlock | MainActor への同期呼び出しを疑う |
| `_dispatch_lane_barrier_sync_invoke` | Main thread block | 重い処理を `Task.detached` へ逃がす |
| `XCTWaiter.wait` timeout | expectation.fulfill() 漏れ | timeout / fulfill ロジックを確認 |

## 検出と強制

- ThumbnailThumb では SwiftLint custom rule `handler_direct_call_in_tests` が直接呼び出しを ERROR で止める
  （テストファイル内の `*Handler.handle*` 直接呼び出し / `Task.detached` ラッパー欠落を検出）
- テストがハングしたら: `make test-debug TIMEOUT=60` → `./tmp/test-stacktrace-*.txt` を上の表と照合
