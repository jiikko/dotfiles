# Forge 使用例: Electron/Node.js プロジェクト

## 実装モード（Standard）

```
/forge ファイルエクスポート時に進捗バーを表示する機能を追加

→ Phase 0: 要件確認
  「エクスポート処理中に renderer プロセスで進捗バーを表示し、
   完了時に通知を出す」で合意

→ Phase 1: 6エージェントが並行で調査
  - nodejs-expert: Stream ベースの進捗計算、IPC でのチャンク送信を提案
  - electron-expert: BrowserWindow の setProgressBar API と IPC チャネル設計を調査
  - research-assistant: Electron 公式ドキュメントの progress bar パターンを参照
  - Explore: 既存の importFile 実装を発見（src/main/fileHandler.ts:80-120）
  - architecture-reviewer: main/renderer の責務分離を推奨
  - css-expert: 進捗バーの CSS アニメーション設計を提案

→ Phase 1.1: クロスレビュー（並行実行）
  - architecture-reviewer が nodejs-expert の結果をレビュー
    → ✅ 同意: Stream ベースは適切
  - nodejs-expert が electron-expert の結果をレビュー
    → ⚠️ 要注意: IPC メッセージの頻度制限（throttle）が必要
  - security-auditor が research-assistant の結果をレビュー
    → 💡 追加: ファイルパスの sanitize が必要
  - css-expert が electron-expert の結果をレビュー
    → ✅ 同意: OS ネイティブの進捗バーとカスタム UI の併用は妥当

→ 統合エージェントが結果を統合
  - 合意事項: Stream ベース, main/renderer 分離, IPC 設計
  - 要注意: IPC throttle（100ms 間隔推奨）
  - 追加タスク: ファイルパス sanitize

→ Phase 1.5: 設計書作成（統合結果に基づく）
  - 参考: importFile の IPC パターン（src/main/fileHandler.ts:80-120）
  - 変更ファイル: src/main/exportService.ts, src/renderer/components/ProgressBar.tsx,
    src/shared/ipc-channels.ts, src/main/preload.ts
  - 注意: IPC throttle、ファイルパス sanitize
  → ユーザー承認取得

→ Phase 2: 実装 + ビルド確認
  - importFile のパターンに従って exportService を追加
  - npm run build → 成功

→ Phase 3: セルフレビュー x5
  [省略]

→ Phase 4: 専門家レビュー（6エージェント並行）
  - nodejs-expert: 「エラー時に Stream が close されていない」
  - security-auditor: 「preload.ts で contextBridge の exposeInMainWorld が不足」

→ Phase 4.1: クロスレビュー（並行実行）
  - electron-expert が security-auditor の指摘をレビュー
    → ✅ 同意: contextBridge は必須
  - architecture-reviewer が nodejs-expert の指摘をレビュー
    → ✅ 同意: Stream リーク防止は必須

→ Phase 4.2: 統合レビュー
  統合エージェントが結果を統合:
  ## 統合済みレビュー結果
  ### 🔴 High Priority
  | # | 指摘 | ファイル | 指摘元 | クロスレビュー |
  |---|-----|---------|--------|--------------|
  | 1 | Stream 未 close | exportService.ts | nodejs-expert | ✅ 同意 |
  | 2 | contextBridge 不足 | preload.ts | security-auditor | ✅ 同意 |

→ Phase 2 に戻る（サイクル2）
  - Stream の finally ブロックで close を追加
  - preload.ts に contextBridge.exposeInMainWorld を追加
  - npm run lint/build/test → 通過

→ Phase 3-4-4.1-4.2 再実行
  - 統合結果: 指摘なし

→ 完了（2サイクル）
```

## レビューモード（Standard）

```
/forge src/main/windowManager.ts

→ Phase 4: 専門家レビュー（6エージェント並行）
  - electron-expert: 「BrowserWindow の webPreferences に nodeIntegration: true が残っている」
  - nodejs-expert: 「EventEmitter リスナーが removeListener されていない（メモリリーク）」
  - security-auditor: 「IPC ハンドラで sender の検証がない」

→ Phase 4.1: クロスレビュー（並行実行）
  - security-auditor が electron-expert の指摘をレビュー
    → ✅ 同意: nodeIntegration: true は重大なセキュリティリスク
  - electron-expert が nodejs-expert の指摘をレビュー
    → ✅ 同意: ウィンドウ close 時にリスナー解除必須
  - nodejs-expert が security-auditor の指摘をレビュー
    → ✅ 同意: sender 検証は必須

→ Phase 4.2: 統合レビュー
  統合エージェントが結果を統合:
  ### 🔴 High Priority
  | # | 指摘 | クロスレビュー |
  |---|-----|--------------|
  | 1 | nodeIntegration: true | ✅ 同意 |
  | 2 | IPC sender 未検証 | ✅ 同意 |
  ### 🟡 Medium Priority
  | # | 指摘 | クロスレビュー |
  |---|-----|--------------|
  | 1 | EventEmitter リスナーリーク | ✅ 同意 |

→ ユーザーに確認（統合結果を提示）
  「High Priority をすべて修正、Medium も対応」

→ Phase 5: 修正
  - nodeIntegration: false に変更、contextIsolation: true を設定
  - IPC ハンドラに event.senderFrame.url の検証を追加
  - ウィンドウ close イベントで全リスナーを解除
  - npm run lint/build/test → 通過

→ Phase 4-4.1-4.2: 再レビュー
  - 統合結果: 指摘なし

→ 完了（2サイクル）
```

## Ultra モード（デバッグ）

```
/forge ウィンドウを閉じた後もプロセスが残り続けてメモリが増加する

→ Phase -1: タスク分析
  - 影響範囲: 複数ファイル（main プロセス全体）
  - 複雑度: 高（プロセスライフサイクル、IPC、EventEmitter）
  - リスク: 中
  💡 推奨: Ultra
  → ユーザーが Ultra を選択

→ Phase 4: Round 1 - 全エージェント並行分析
  - electron-expert: 「BrowserWindow の closed イベント後に参照が残っている可能性」
  - nodejs-expert: 「EventEmitter のリスナーリークの可能性、--max-old-space-size 確認」
  - security-auditor: 「子プロセス spawn 後の kill 漏れの可能性」
  - architecture-reviewer: 「ウィンドウ管理が単一クラスに集中しすぎている」
  - debugger: 「process.memoryUsage() のログから heapUsed が単調増加」

→ Phase 4.3: Round 2 - 再分析（全員の Round 1 結果を入力）
  - electron-expert:
    🆕 「debugger のヒープ分析を受けて調査 →
        BrowserWindow.getAllWindows() が閉じたウィンドウを含んでいる」
  - nodejs-expert:
    🔄 「electron-expert の発見と合わせると、
        windowManager の Map に closed ウィンドウの参照が残っている」
  - security-auditor:
    🔍 「nodejs-expert の指摘を深掘り →
        IPC ハンドラのクロージャがウィンドウ参照をキャプチャしている」
  - architecture-reviewer:
    ✅ 「全員がウィンドウ参照リークで合意しつつある」
  - debugger:
    🎯 「src/main/windowManager.ts:67 で closed イベント後に
        this.windows.delete(id) が呼ばれていない」

→ Phase 4.3: Round 3 - 最終確認
  - 全エージェント: ✅ 「debugger の指摘で合意」
  - 収束判定: 新しい発見なし → 収束

→ 統合エージェント: Ultra モード統合結果
  ## 確定した根本原因
  ウィンドウ close 時の処理:
  1. BrowserWindow の closed イベント発火
  2. windowManager の Map から削除されていない
  3. IPC ハンドラのクロージャがウィンドウ参照を保持
  ↓ 問題
  GC がウィンドウオブジェクトを回収できず、関連するリスナー・バッファも残留

  ## 修正案
  closed イベントで Map から削除し、IPC ハンドラも解除する

→ Phase 2: 修正
  - windowManager.ts の closed ハンドラに cleanup 処理を追加
  - npm run build → 成功
  - 動作確認 → ウィンドウ close 後にメモリが安定

→ 完了（1サイクル、3ラウンド）
```
