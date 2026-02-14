# Forge 使用例

フレームワーク/言語別の具体例は以下を参照:

- @forge/examples-swiftui.md - Swift/SwiftUI プロジェクト
- @forge/examples-electron.md - Electron/Node.js プロジェクト

## フロー概要

### 実装モード

```
/forge [タスク説明]

Phase -1 → モード選択
Phase 0  → 要件確認
Phase 1  → エージェント並行調査（類似コード特定）
Phase 1.1 → クロスレビュー（エージェント間の相互検証）
Phase 1.5 → 設計書作成 → ユーザー承認
Phase 2  → 実装 + ビルド確認
Phase 3  → セルフレビュー x5
Phase 3.5 → スキル自動検証（VALIDATION）
Phase 4  → 専門家レビュー
Phase 4.1 → クロスレビュー
Phase 4.2 → 統合レビュー
Phase 4.5 → デバッグ支援（エラー発生時）
Phase 5  → 修正と収束
Phase 5.5 → スキルテスト（TESTING）
→ 完了レポート
```

### レビューモード

```
/forge [ファイル/ディレクトリパス]

Phase -1 → モード選択
Phase 4  → 専門家レビュー
Phase 4.1 → クロスレビュー
Phase 4.2 → 統合レビュー
Phase 4.5 → デバッグ支援（エラー発生時）
Phase 5  → 修正と収束
Phase 5.5 → スキルテスト（TESTING）
→ 完了レポート
```

### Ultra モード（デバッグ）

```
/forge [問題の説明]

Phase -1 → Ultra 選択
Phase 4  → Round 1: 全エージェント並行分析
Phase 4.3 → Round 2+: 他エージェントの発見を踏まえて再分析
           → 収束するまで繰り返し（最大3ラウンド）
→ 統合 → 修正 → 完了
```
