---
name: crash-log-analyzer
version: 1.0.0
description: macOS クラッシュログ (.ips) を解析し、根本原因を特定するスキル。subagent で解析を代行する。
---

# macOS Crash Log Analyzer

macOS アプリのクラッシュログ (.ips) を解析し、根本原因を特定して修正案を提示する。

## 使い方

```
/crash-log-analyzer
```

## 実行フロー

1. **クラッシュログの検出**: `~/Library/Logs/DiagnosticReports/` から最新のクラッシュログを特定
2. **アプリの特定**: ユーザーに対象アプリを確認（または自動検出）
3. **subagent 起動**: 解析を専門 subagent に委譲
4. **結果報告**: 根本原因・修正案・再発防止策を報告

## Step 1: クラッシュログの検出

```bash
# 直近24時間以内のクラッシュログを一覧（全アプリ）
find ~/Library/Logs/DiagnosticReports/ -name "*.ips" -mtime -1 -exec ls -lt {} + 2>/dev/null | head -20

# 特定アプリのクラッシュログ（APP_NAME は実際のアプリ名に置換）
ls -lt ~/Library/Logs/DiagnosticReports/ | grep -i "APP_NAME" | head -5
```

### プロジェクト固有のクラッシュログコマンド

プロジェクトに専用コマンドがある場合はそちらを優先する:

- `bin/tt-crash-log` (ThumbnailThumb)
- `bin/*-crash-log` パターンで検索

```bash
# プロジェクト固有コマンドの検出
ls bin/*crash* 2>/dev/null
```

## Step 2: ユーザー確認

AskUserQuestion で以下を確認:

1. **対象アプリ**: 自動検出した最新のクラッシュがあれば提示し確認
2. **クラッシュの状況**: 何をしていたときにクラッシュしたか（任意）

## Step 3: subagent で解析を実行

Agent ツールで解析 subagent を起動する。以下のプロンプトテンプレートを使用:

```
subagent_type: crash-log-analyzer
prompt: |
  以下のクラッシュログを解析してください。

  ## クラッシュログのパス
  {crash_log_path}

  ## プロジェクトのソースコード
  カレントディレクトリ内の Sources/ または src/ を検索対象にしてください。

  ## 解析手順

  ### 1. クラッシュログの読み込みとパース

  クラッシュログ (.ips) は JSON 形式です。以下のセクションを抽出してください:

  **必須抽出項目:**
  - `bug_type`: クラッシュの種類
  - `exception.type`: 例外タイプ (EXC_BAD_ACCESS, EXC_BREAKPOINT 等)
  - `exception.signal`: シグナル (SIGSEGV, SIGABRT, SIGTRAP 等)
  - `exception.codes`: 例外コード
  - `exception.subtype` / `termination.reason`: 終了理由
  - `faultingThread`: クラッシュしたスレッド番号
  - `threads[faultingThread].frames`: クラッシュスレッドのスタックトレース
  - `usedImages`: バイナリイメージ一覧（シンボル解決に使用）

  ### 2. スタックトレース解析

  **faulting thread のフレームを上から順に確認:**

  1. アプリのフレーム（`usedImages` でアプリバイナリを特定）を最優先で確認
  2. システムフレーム（libswiftCore, SwiftUI, AppKit 等）は補助情報として確認
  3. シンボル名からソースファイル・関数を特定

  **フレーム構造:**
  ```json
  {
    "imageOffset": 123456,
    "symbol": "ClassName.methodName() -> ReturnType",
    "symbolLocation": 42,
    "imageIndex": 5
  }
  ```

  - `symbol`: 関数名（デマングル済み）
  - `imageIndex`: `usedImages` 配列のインデックス
  - `imageOffset`: バイナリ内のオフセット

  ### 3. ソースコードとの照合

  スタックトレースから特定した関数名を、プロジェクトのソースコード内で検索:

  ```
  Grep: pattern="func methodName" path="." glob="*.swift"
  ```

  該当ファイルを読み、クラッシュの原因となりうるコードを特定する:
  - Force unwrap (`!`)
  - 配列の直接インデックスアクセス (`array[index]`)
  - nil pointer dereference
  - Actor 境界違反
  - メインスレッド制約違反

  ### 4. 根本原因の特定

  以下の分類で報告:

  | Exception Type | Signal | よくある原因 |
  |----------------|--------|-------------|
  | EXC_BAD_ACCESS | SIGSEGV | nil ポインタ参照、解放済みメモリアクセス |
  | EXC_BAD_ACCESS | SIGBUS | アライメント違反、マップされていないメモリ |
  | EXC_BREAKPOINT | SIGTRAP | Force unwrap of nil、precondition failure、Swift runtime error |
  | EXC_BAD_INSTRUCTION | SIGILL | 不正命令、enum の未処理ケース |
  | EXC_CRASH | SIGABRT | fatalError()、assertion failure、unhandled exception |
  | EXC_CRASH | SIGKILL | Watchdog timeout、メモリ超過によるシステム kill |
  | EXC_RESOURCE | - | CPU/メモリ/ディスクのリソース制限超過 |

  **Swift 固有のパターン:**
  - `swift_unexpectedError`: 未処理の throw
  - `swift_checkCast`: as! のキャスト失敗
  - `swift_arrayBoundsCheck`: 配列の範囲外アクセス
  - `_dispatch_assert_queue_fail`: 間違ったキューからのアクセス
  - `__swift_instantiateConcreteTypeFromMangledName`: 型のインスタンス化失敗

  **SwiftUI 固有のパターン:**
  - `AttributeGraph` cycle: View の循環依存
  - `precondition failure: attribute not found`: 属性グラフの破損
  - `Accessing StateObject's object without being installed on a View`: ライフサイクル違反

  **AppKit/UIKit 固有のパターン:**
  - `NSInternalInconsistencyException`: 内部状態の不整合
  - `CALayerInvalidGeometry`: 無効なジオメトリ (NaN, Inf)
  - `This application is modifying the autolayout engine from a background thread`: スレッド違反

  ### 5. レポート出力

  以下のフォーマットで報告:

  ```markdown
  ## クラッシュ解析レポート

  ### サマリー
  - **アプリ**: {app_name}
  - **クラッシュ日時**: {crash_date}
  - **例外**: {exception_type} ({signal})
  - **スレッド**: {faulting_thread} ({main/background})

  ### 根本原因
  {1-3文で原因を説明}

  ### クラッシュ箇所
  - **ファイル**: {source_file}:{line}（推定）
  - **関数**: {function_name}
  - **スタックトレース**:
    {relevant frames}

  ### 修正案
  {具体的なコード修正}

  ### 再発防止
  {lintルール追加、テスト追加、設計改善の提案}
  ```

  ### 6. 全スレッドの確認（必要な場合）

  faulting thread だけでは原因が不明な場合:
  - デッドロック: 他のスレッドが何をブロックしているか確認
  - レースコンディション: 同じリソースにアクセスしているスレッドを探す
  - Watchdog kill: メインスレッドが長時間ブロックされていないか確認
```

## .ips ファイル構造リファレンス

macOS のクラッシュレポート (.ips) は JSON 形式で以下の構造を持つ:

```json
{
  "uptime": 123456,
  "procRole": "Foreground",
  "version": 2,
  "userID": 501,
  "deployVersion": 210,
  "modelCode": "Mac14,2",
  "coalitionID": 12345,
  "osVersion": {
    "train": "macOS 15.2",
    "build": "24C101",
    "releaseType": "User"
  },
  "captureTime": "2026-01-24 10:38:03.1234 +0900",
  "incident": "UUID-HERE",
  "pid": 12345,
  "cpuType": "ARM-64",
  "procName": "AppName",
  "procPath": "/Applications/AppName.app/Contents/MacOS/AppName",
  "bundleInfo": {
    "CFBundleShortVersionString": "1.2.0",
    "CFBundleVersion": "17",
    "CFBundleIdentifier": "com.example.app"
  },
  "storeInfo": {
    "deviceIdentifierForVendor": "...",
    "thirdParty": true
  },
  "exception": {
    "codes": "0x0000000000000001, 0x...",
    "rawCodes": [1, 0],
    "type": "EXC_BREAKPOINT",
    "signal": "SIGTRAP",
    "subtype": "..."
  },
  "termination": {
    "flags": 0,
    "code": 5,
    "namespace": "UNC",
    "indicator": "...",
    "byProc": "exc handler",
    "byPid": 12345,
    "reason": "Namespace UNC, Code 5"
  },
  "vmSummary": "...",
  "faultingThread": 0,
  "threads": [
    {
      "triggered": true,
      "id": 12345,
      "threadState": { "flavor": "ARM_THREAD_STATE64", "x": [...] },
      "queue": "com.apple.main-thread",
      "frames": [
        {
          "imageOffset": 123456,
          "symbol": "specialized ClassName.methodName(param:) -> ReturnType",
          "symbolLocation": 42,
          "imageIndex": 5
        }
      ]
    }
  ],
  "usedImages": [
    {
      "source": "P",
      "arch": "arm64",
      "base": 4294967296,
      "size": 1234567,
      "uuid": "UUID-HERE",
      "path": "/Applications/AppName.app/Contents/MacOS/AppName",
      "name": "AppName"
    }
  ],
  "sharedCache": { ... },
  "vmRegionInfo": "...",
  "legacyInfo": {
    "threadTriggered": { "queue": "com.apple.main-thread" }
  }
}
```

### 重要なフィールド解説

| フィールド | 説明 |
|-----------|------|
| `faultingThread` | クラッシュしたスレッドのインデックス |
| `threads[n].triggered` | `true` のスレッドが faulting thread |
| `threads[n].queue` | GCD キュー名（`com.apple.main-thread` ならメインスレッド） |
| `threads[n].frames[0]` | スタックトップ（クラッシュ地点に最も近い） |
| `usedImages[n].source` | `"P"` = アプリバイナリ、`"S"` = 共有キャッシュ |
| `usedImages[n].name` | バイナリ名（アプリ名と一致するものがアプリのコード） |
| `exception.codes` | `0x0000000000000000` = null pointer、`0x0000000000000001` = breakpoint |

## 注意事項

- subagent は **sonnet** モデルで十分（スタックトレースのパースは定型作業）
- ソースコードの深い理解が必要な場合のみ **opus** にエスカレーション
- クラッシュログが 24 時間以上前の場合はユーザーに確認
- symbolicate されていない（シンボル名がない）フレームは `atos` コマンドで解決を試みる:
  ```bash
  # dSYM がある場合
  atos -arch arm64 -o /path/to/AppName.app.dSYM/Contents/Resources/DWARF/AppName -l 0x{base} 0x{base+imageOffset}

  # dSYM がない場合（デバッグビルド）
  atos -arch arm64 -o /path/to/AppName.app/Contents/MacOS/AppName -l 0x{base} 0x{base+imageOffset}
  ```
