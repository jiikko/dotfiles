---
name: forge
version: 1.0.0
description: 専門家エージェントの並行実行＋クロスレビューで実装・改善・レビューを行う高品質ワークフロー。「/forge」「forgeで」「専門家エージェントで実装して」、またはバグ修正の自前試行が1-2回失敗した時のエスカレーション先として発火。typo修正・数行の軽微変更には使わない。レビューのみ（修正・実装まで不要）なら cross-review、Codex単体レビューなら codex-review を使う。
---

# Forge

専門家エージェントによる高品質な実装・改善スキル。タスク実装とコードレビュー両方に対応。

## アーキテクチャ

```
┌────────────────────────────────────────────────────────────────┐
│                    メイン Claude（オーケストレーター）            │
│  役割: 指示出し、進行管理、最終判断のみ                          │
│  禁止: 直接的なコード分析、レビュー結果の独自解釈                  │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│                    Phase N: 専門家エージェント並行実行            │
│  swift-language-expert, swiftui-macos-designer, etc.           │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│                    Phase N.x: クロスレビュー                     │
│  各エージェントの出力を別の観点を持つエージェントが検証            │
│  メインClaudeは検証済み結果のみを受け取る                        │
└────────────────────────────────────────────────────────────────┘
```

### メイン Claude の責務

| やること | やらないこと |
|---------|-------------|
| Phase の進行管理 | コードの直接分析 |
| sub agent への指示出し | レビュー結果の独自解釈 |
| ユーザーとの対話 | 専門判断（sub agent に委譲）|
| 最終結果の報告 | 重複排除（統合エージェントに委譲）|

## 使い方

```
/forge [タスク説明 または 対象ファイル/ディレクトリ]
```

例:
- `/forge TextElement に letterSpacing プロパティを追加` → 実装モード
- `/forge バグ #123 を修正` → 実装モード
- `/forge Sources/ViewModels/CanvasViewModel.swift` → レビューモード
- `/forge Sources/Services/` → レビューモード

## タスクタイプ判定

`$ARGUMENTS` の内容で自動判定:

| 入力パターン | タイプ | フロー |
|-------------|--------|--------|
| ファイル/ディレクトリパス | レビュー | Phase -1 → 4 → 4.1 → 4.2 → 4.5 → 5 → 5.3 → 5.5 → 完了 |
| それ以外（タスク説明） | 実装 | Phase -1 → 0 → 1 → 1.1 → 1.5 → 2 → 3 → 3.5 → 4 → 4.1 → 4.2 → 4.5 → 5 → 5.3 → 5.5 → 完了 |

## Phase -1: モード選択（必須）

**すべてのタスク開始時に、AskUserQuestion でモードを選択させる。**

### AskUserQuestion の内容

```
質問: 「実行モードを選択してください」
ヘッダー: "Mode"

選択肢:
1. Minimum   — 3エージェント並行、クロスレビュー省略、最速
2. Minimum+  — 3エージェント並行、クロスレビューあり、軽量品質検証
3. Standard  — 6エージェント並行、クロスレビュー・統合あり、バランス型
4. Maximum   — 全エージェント並行、全カテゴリスキル起動、最高品質
5. Ultra     — 全エージェント反復並行、収束まで複数ラウンド、複雑なデバッグ向け
```

選択肢の提示時には、`_common/modes.md` のスコアリングで算出した推奨モードを 💡 付きで明示する（例: 「💡 推奨: Standard（スコア 4）」）。

> **詳細**: `_common/modes.md` を参照（モード一覧、スコアリングシステム、各フェーズ動作テーブル）

## 共通ファイルの読み込み（重要）

**共通ファイルは自動では読み込まれません。** Phase -1（モード選択）の直後に、以下の4ファイルを明示的に Read してください。一度読み込めば全フェーズで有効です（フェーズごとの再 Read は不要）。

```
Read: ~/.claude/skills/forge/_common/modes.md          # モード別動作定義
Read: ~/.claude/skills/forge/_common/agents.md         # エージェント定義
Read: ~/.claude/skills/forge/_common/cross-review.md   # クロスレビュー仕様
Read: ~/.claude/skills/forge/_common/skill-triggers.md # スキル自動起動定義
```

### エージェント定義ファイル参照時

エージェント定義（`~/.claude/agents/*.md`）内の `@../_common/` 参照も自動解決されません。必要に応じて以下を Read（実体は dotfiles リポジトリ内。`~/.claude/_common/` には存在しない）:

```
Read: ~/dotfiles/_claude/_common/language-adaptation.md      # 言語適応ルール
Read: ~/dotfiles/_claude/_common/tool-selection-strategy.md  # ツール選択戦略
Read: ~/dotfiles/_claude/_common/output-format-template.md   # 出力フォーマット
Read: ~/dotfiles/_claude/_common/quality-checklist.md        # 品質チェックリスト
```

## 詳細ドキュメント

各フェーズの詳細は `~/.claude/skills/forge/` 配下の以下を、該当フェーズ開始前に Read すること:

- phases-0-1.md - Phase 0〜1.5: 要件確認・事前調査・設計
- phases-2-3.md - Phase 2〜3.5: 実装・セルフレビュー・スキル検証
- phases-4-review.md - Phase 4〜4.2: 専門家レビュー・クロスレビュー・統合
- phase-4.3-ultra.md - Phase 4.3: 反復並列思考（Ultra モードのみ）
- phases-5-completion.md - Phase 4.5〜5.5: デバッグ・収束・Codex Review・完了レポート
- examples.md - 使用例
