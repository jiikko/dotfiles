# Forge 使用例

## 実装モード

```
/forge TextElement に letterSpacing プロパティを追加

→ Phase 0: 要件確認
  「letterSpacing を追加し、UI からも設定可能にする」で合意

→ Phase 1: 6エージェントが並行で調査
  - swift-language-expert: CGFloat プロパティ、Codable 対応を提案
  - swiftui-macos-designer: Text modifier との連携方法を調査
  - research-assistant: Apple の Typography ガイドを参照
  - Explore: 既存の fontSize 実装を発見（TextElement.swift:45-60）
  - architecture-reviewer: Model層への配置を推奨
  - swiftui-test-expert: ラウンドトリップテストの更新が必要と指摘

→ Phase 1.1: クロスレビュー（並行実行）
  - architecture-reviewer が swift-language-expert の結果をレビュー
    → ✅ 同意: CGFloat は適切
  - swiftui-performance-expert が swiftui-macos-designer の結果をレビュー
    → ⚠️ 要注意: kerning modifier は大量テキストで重くなる可能性
  - swift-language-expert が architecture-reviewer の結果をレビュー
    → ✅ 同意: Model層配置は適切
  - Explore が swiftui-test-expert の結果をレビュー
    → ✅ 同意: ラウンドトリップテスト更新必要
  - swiftui-test-expert が Explore の結果をレビュー
    → 💡 追加: ExportSnapshotTest も更新推奨
  - security-auditor が research-assistant の結果をレビュー
    → ✅ 問題なし

→ 統合エージェントが結果を統合
  - 合意事項: CGFloat, Model層配置, テスト更新
  - 要注意: パフォーマンス（大量テキスト時）
  - 追加タスク: ExportSnapshotTest 更新

→ Phase 1.5: 設計書作成（統合結果に基づく）
  - 参考: fontSize の実装パターン（TextElement.swift:45-60）
  - 変更ファイル: TextElement.swift, TextElementView.swift, TextPropertyPanel.swift
  - 注意: 大量テキスト時のパフォーマンス考慮
  → ユーザー承認取得

→ Phase 2: 実装 + ビルド確認
  - fontSize のパターンに従って letterSpacing を追加
  - make build → 成功

→ Phase 3: セルフレビュー x5
  [省略]

→ Phase 4: 専門家レビュー（6エージェント並行）
  - swift-language-expert: 「Codable の CodingKeys 漏れ」
  - Explore: 「docs/elements.md 未更新」

→ Phase 4.1: クロスレビュー（並行実行）
  - architecture-reviewer が swift-language-expert の指摘をレビュー
    → ✅ 同意: CodingKeys は必須
  - swiftui-test-expert が Explore の指摘をレビュー
    → ✅ 同意: ドキュメント更新必須

→ Phase 4.2: 統合レビュー
  統合エージェントが結果を統合:
  ## 統合済みレビュー結果
  ### 🔴 High Priority
  | # | 指摘 | ファイル | 指摘元 | クロスレビュー |
  |---|-----|---------|--------|--------------|
  | 1 | CodingKeys 漏れ | TextElement.swift | swift-language-expert | ✅ 同意 |
  | 2 | docs 未更新 | docs/elements.md | Explore | ✅ 同意 |

→ Phase 2 に戻る（サイクル2）
  - CodingKeys を追加
  - docs/elements.md を更新
  - make lint/build/test → 通過

→ Phase 3-4-4.1-4.2 再実行
  - 統合結果: 指摘なし

→ 完了（2サイクル）
```

## レビューモード

```
/forge Sources/ViewModels/CanvasViewModel.swift

→ Phase 4: 専門家レビュー（6エージェント並行）
  - swift-language-expert: 「42行目: retain cycle の可能性」
  - architecture-reviewer: 「責務が多すぎる、分割推奨」
  - swiftui-performance-expert: 「大量の要素で再描画が重い」

→ Phase 4.1: クロスレビュー（並行実行）
  - architecture-reviewer が swift-language-expert の指摘をレビュー
    → ✅ 同意: retain cycle は修正必須
  - swift-language-expert が architecture-reviewer の指摘をレビュー
    → ⚠️ 要検討: 分割は大きな変更、段階的に行うべき
  - swiftui-macos-designer が swiftui-performance-expert の指摘をレビュー
    → ✅ 同意: 再描画最適化は必要

→ Phase 4.2: 統合レビュー
  統合エージェントが結果を統合:
  ### 🔴 High Priority
  | # | 指摘 | クロスレビュー |
  |---|-----|--------------|
  | 1 | retain cycle | ✅ 同意 |
  | 2 | 再描画が重い | ✅ 同意 |
  ### ⚠️ 要検討
  | # | 指摘 | 論点 |
  |---|-----|------|
  | 1 | 責務分割 | 大きな変更のため段階的実施を推奨 |

→ ユーザーに確認（統合結果を提示）
  「High Priority をすべて修正、責務分割は Issue 化」

→ Phase 5: 修正
  - retain cycle を修正
  - 再描画を最適化
  - 責務分割は Issue #XXX として記録
  - make lint/build/test → 通過

→ Phase 4-4.1-4.2: 再レビュー
  - 統合結果: 指摘なし

→ 完了（2サイクル）
```

## Ultra モード（デバッグ）

```
/forge テキストエレメントをダブルクリックしても編集モードに入れない

→ Phase -1: タスク分析
  - 影響範囲: 複数ファイル
  - 複雑度: 高（状態遷移、AppKit-SwiftUI統合）
  - リスク: 中
  💡 推奨: Ultra Sonnet
  → ユーザーが Ultra Opus を選択

→ Phase 4: Round 1 - 全エージェント並行分析
  - swift-language-expert: 「非同期処理のタイミング問題の可能性」
  - swiftui-macos-designer: 「NSViewRepresentable のライフサイクル問題の可能性」
  - appkit-swiftui-integration-expert: 「firstResponder の競合の可能性」
  - architecture-reviewer: 「状態管理が複雑すぎる」
  - swift-concurrency-expert: 「DispatchQueue.main.async のタイミング」
  - debugger: 「ログから pendingElementID が設定されていない」

→ Phase 4.3: Round 2 - 再分析（全員の Round 1 結果を入力）
  - swift-language-expert:
    🆕 「debugger の指摘を受けて調査 → requestFocus が呼ばれていない」
  - swiftui-macos-designer:
    🔄 「appkit-swiftui-integration-expert の指摘と合わせると、
        ダブルクリックハンドラから focusManager への伝達が問題」
  - appkit-swiftui-integration-expert:
    🔍 「swift-language-expert の発見を深掘り →
        ダブルクリック時に editingElementID が設定される前に
        View が再描画されている」
  - architecture-reviewer:
    ✅ 「全員が状態遷移の問題で合意しつつある」
  - swift-concurrency-expert:
    💡 「View 更新と focusManager 更新の順序が逆になっている可能性」
  - debugger:
    🎯 「CanvasContainerView.swift:XXX 行で editingElementID を
        設定する前に requestFocus を呼ぶべき」

→ Phase 4.3: Round 3 - 最終確認
  - 全エージェント: ✅ 「debugger の指摘で合意」
  - 収束判定: 新しい発見なし → 収束

→ 統合エージェント: Ultra モード統合結果
  ## 確定した根本原因
  ダブルクリック時の処理順序:
  1. editingElementID を設定 (View 更新トリガー)
  2. requestFocus を呼び出し
  ↓ 問題
  View 更新が先に走り、focusManager がまだ準備できていない

  ## 修正案
  requestFocus を先に呼び、View 更新は focusManager の状態変化で行う

→ Phase 2: 修正
  - CanvasContainerView.swift を修正
  - make build → 成功
  - 動作確認 → 編集モードに入れるようになった

→ 完了（1サイクル、3ラウンド）
```
