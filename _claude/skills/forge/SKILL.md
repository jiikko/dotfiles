---
name: forge
version: 2.0.0
description: 専門家エージェントの並行実行＋クロスレビューで実装・改善・レビューを行う高品質ワークフロー。「/forge」「forgeで」「専門家エージェントで実装して」、またはバグ修正の自前試行が1-2回失敗した時のエスカレーション先として発火。typo修正・数行の軽微変更には使わない。レビューのみ（修正・実装まで不要）なら cross-review、Codex単体レビューなら codex-review を使う。
---

# Forge

専門家エージェントによる高品質な実装・改善スキル。タスク実装とコードレビュー両方に対応。

## アーキテクチャ（v2: 対話シェル + 決定論 fan-out エンジン）

```
┌────────────────────────────────────────────────────────────────┐
│  メイン Claude（対話シェル）— skill 層                            │
│  人間ループを担当: モード選択 / 要件確認 / 設計承認 /            │
│  実装 / セルフレビュー / 修正方針 / Codex / 完了レポート          │
└────────────────────────────────────────────────────────────────┘
                              │ Workflow ツールで起動
                              ▼
┌────────────────────────────────────────────────────────────────┐
│  forge.js（決定論 fan-out エンジン）— Workflow 層                 │
│  scriptPath: $HOME/.claude/workflows/forge.js                    │
│  重い並行処理を担当:                                             │
│   - investigate: Phase 1（専門家並行調査）+ 1.1（クロスレビュー） │
│   - review:      Phase 4（専門家レビュー）+ 4.1 + 4.2（統合）     │
│   - ultra:       Phase 4.3（反復並列思考・収束まで）             │
└────────────────────────────────────────────────────────────────┘
```

**なぜ二層なのか**: fan-out / クロスレビュー / 収束ループは `pipeline`/`parallel`/loop で**決定論的**に書ける（`forge.js`）。一方モード選択・設計承認・修正方針は **mid-run でユーザーに聞く必要があり、background 実行の Workflow では表現できない**ため skill 層（main Claude）に残す。

> **重要**: `forge.js` は `_common/modes.md`（モード別動作）・`_common/agents.md`（ロスター）・`_common/cross-review.md`（ペアリング/統合）の仕様を実装したもの。これらの仕様を変更したら `forge.js` も同期更新すること（片方だけ直すと乖離する）。

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

| 入力パターン | タイプ | fan-out の kind |
|-------------|--------|----------------|
| ファイル/ディレクトリパス | レビュー | `review`（Ultra 選択時は `ultra`）|
| それ以外（タスク説明） | 実装 | `investigate` →（実装後）`review` |

## Phase -1: モード選択（必須・全タスク共通）

**すべてのタスク開始時に、AskUserQuestion でモードを選択させる。**

> **呼び出し元がモードを明示している場合は、この AskUserQuestion を省略してそのモードを使う**（例: `audit` / `escalate-to-forge` から「Maximum モードで」と指定された、または非対話で起動された場合）。モードを二重に尋ねない。明示が無いときのみ選択させる。

選択肢: Minimum / Minimum+ / Standard / Maximum / Ultra（各モードの意味は `_common/modes.md`）。
提示時に `_common/modes.md` のスコアリングで算出した推奨モードを 💡 付きで明示する（例: 「💡 推奨: Standard（スコア 4）」）。

### モード選択直後に Read する共通ファイル（skill 層で使うもの）

fan-out 自体は `forge.js` が実装するため、skill 層が読むのは **判断に必要な 3 ファイルのみ**:

```
Read: ~/.claude/skills/forge/_common/modes.md          # スコアリング・モード別動作（モード推奨・再評価に使う）
Read: ~/.claude/skills/forge/_common/agents.md         # 条件付き専門家の検出ルール（extraAgents の決定に使う）
Read: ~/.claude/skills/forge/_common/skill-triggers.md # VALIDATION/DIAGNOSTIC/TESTING スキルの起動判定
```

> `_common/cross-review.md` はペアリング/統合の仕様で、実装は `forge.js` 側にある。skill 層が読む必要はない（仕様確認時のみ参照）。

## forge.js 呼び出し契約（fan-out フェーズの実行方法）

Phase 1（事前調査）・Phase 4（レビュー）・Phase 4.3（Ultra）に到達したら、**自分でエージェントを並べず** Workflow ツールで `forge.js` を起動する:

```
Workflow({
  scriptPath: "<HOME>/.claude/workflows/forge.js",   // ~ は $HOME に展開して絶対パスで渡す
  args: {
    kind: "investigate" | "review" | "ultra",
    mode: "Minimum" | "Minimum+" | "Standard" | "Maximum" | "Ultra",   // Phase -1 で選択したモード
    target: "タスク説明（investigate）または ファイル/ディレクトリパス（review/ultra）",
    language: "swift" | "electron" | "node" | "go" | "rails" | "css" | "generic",  // 対象から判定
    extraAgents: [ ... ],   // 任意: agents.md の検出ルールで判定した条件付き専門家
    maxRounds: 3,           // ultra のみ（既定 3）
  }
})
```

### extraAgents の決め方（agents.md の検出ルール）

`forge.js` はモード別の標準ロスターを内蔵している。それに**上乗せする条件付き専門家**だけを skill 層が対象コードの内容から判定して渡す:

- `async`/`await`/`actor`/`Task` を含む → `swift-concurrency-expert`
- ファイル操作 / 外部入力 / API 通信 → `security-auditor`
- リファクタ/分割/抽出/移動/整理タスク → `refactoring-patterns`
- `NSViewRepresentable` 等 → `appkit-swiftui-integration-expert`、`Codable`/`SwiftData` → `data-persistence-expert` 等（agents.md「追加エージェント」表）

> ロスターを完全に自前制御したい特殊ケースでは `args.agents: [{name, reviewer, lens}]` を渡すと内蔵ロスターを上書きできる。通常は不要。

### 返り値の使い方

- `investigate`: `{ integrated, reviewed }`（Minimum は `integrated: null` で `raw` に各エージェントの生 findings）。`integrated` を Phase 1.5 の設計書作成の材料にする。Minimum では `raw` を main Claude が直接マージする。
- `review`: `{ integrated, reviewed }`（Minimum は `integrated: null` で `raw`）。`integrated.high/medium/low/excluded/conflicts` を Phase 5 の修正方針提示に使う。`conflicts` は**独自判断せずユーザーに委ねる**。
- `ultra`: `{ rounds, integrated }`。`integrated.rootCause` と `high` を修正方針に使う。

## 実装モードのフロー

```
Phase -1  モード選択（AskUserQuestion）                          [skill]
Phase 0   要件確認（AskUserQuestion）+ skill 候補特定            [skill]   → phases-0-1.md
Phase 1   事前調査 + 1.1 クロスレビュー + 統合                   [forge.js: kind=investigate]
Phase 1.5 設計書作成 → ユーザー承認（AskUserQuestion）          [skill]   → phases-0-1.md
Phase 2   実装 + ビルド確認（難所は Agent で専門家相談可）       [skill]   → phases-2-3.md
Phase 3   セルフレビュー ×5                                     [skill]   → phases-2-3.md
Phase 3.5 VALIDATION スキル自動検証（Standard 以上 / 条件付き）  [skill]   → skill-triggers.md
Phase 4   専門家レビュー + 4.1 + 4.2 統合                        [forge.js: kind=review、Ultra なら kind=ultra]
Phase 4.5 デバッグ支援（ランタイムエラー時のみ）                 [skill]   → phases-5-completion.md
Phase 5   修正方針（AskUserQuestion）→ 修正 → 再レビュー収束      [skill + forge.js を収束まで再起動]
Phase 5.3 Codex Review（全モード必須）                          [skill: /codex-review]
Phase 5.5 TESTING スキル（Maximum 以上 / 現状未登録のためスキップ）[skill]
→ 完了レポート                                                  [skill]   → phases-5-completion.md
```

## レビューモードのフロー

```
Phase -1  モード選択（AskUserQuestion）                          [skill]
Phase 4   専門家レビュー + 4.1 + 4.2 統合                        [forge.js: kind=review、Ultra なら kind=ultra]
Phase 4.5 デバッグ支援（必要時）                                 [skill]
Phase 5   修正方針 → 修正 → 再レビュー収束                       [skill + forge.js]
Phase 5.3 Codex Review（全モード必須）                          [skill]
→ 完了レポート                                                  [skill]
```

## Phase 5 の収束ループ（skill + forge.js）

1. `forge.js` の `integrated` を受け取り、AskUserQuestion で対応方針を確認（全部修正 / 個別確認 / レポートのみ / Issue 化）。
2. 承認された修正を main Claude が実施 → `make lint && make build && make test`（無ければ同等コマンド）。
3. 再レビューが必要なら `forge.js`（kind=review）を再起動。「指摘なし」または残りが全てスキップ/Issue 化されたら収束。
4. 上限 **5 サイクル**。超過時は AskUserQuestion で継続可否を確認（詳細は phases-5-completion.md）。

## 詳細ドキュメント

各フェーズの詳細は `~/.claude/skills/forge/` 配下を該当フェーズ開始前に Read すること:

- `phases-0-1.md` - Phase 0（要件確認・interactive）/ Phase 1.5（設計書・承認・interactive）。Phase 1/1.1 の fan-out は `forge.js` が実行
- `phases-2-3.md` - Phase 2（実装）/ Phase 3（セルフレビュー ×5）/ Phase 3.5（VALIDATION）— すべて skill 層
- `phases-4-review.md` - Phase 4/4.1/4.2 の**仕様**（実行は `forge.js: kind=review`）
- `phase-4.3-ultra.md` - Phase 4.3 の**仕様**（実行は `forge.js: kind=ultra`）
- `phases-5-completion.md` - Phase 4.5/5/5.3/5.5/完了レポート — skill 層
- `_common/modes.md` / `_common/agents.md` / `_common/cross-review.md` / `_common/skill-triggers.md` - `forge.js` が実装する仕様の出典
- `examples.md` - 使用例
- `forge.js`（= `_claude/workflows/forge.js`） - fan-out エンジン本体
```
