# Claude Skills & Agents 改善案

調査日: 2026-02-19
調査モード: Forge Minimum+（architecture-reviewer, Explore）
最終リフレッシュ: 2026-07-16（実態と再照合。当時: 11スキル・agents 11200行 → 現在: 17スキル・31エージェント 9786行。electron-expert 縮小・swift-vlc-player / smoke-test 削除・forge の Workflow 化・全スキルへの version frontmatter 導入など大きく改修されており、多数の項目が解消/対象消滅）

---

## 🔴 High Priority

### ~~1. VALIDATION スキル（style-review）の実行条件が矛盾している~~ 対応済み 2026-02-19

### ~~2. `electron-expert.md` が 1644行の God File~~ 解消済み（2026-07-16 確認: 現在 181 行に再構成済み）

### ~~3. `swift-vlc-player/SKILL.md` が 1071行の God File~~ 対象消滅（2026-07-16 確認: スキル自体が削除済み）

### ~~4. Minimum モードのクロスレビュー・ペアリング定義が存在しない~~ 対応済み 2026-02-19

---

## 🟡 Medium Priority

### ~~5. モード定義が3ファイルに分散している（DRY 違反）~~ 解消済み（2026-07-16 確認: forge/SKILL.md は 160 行に縮小され「各モードの意味は `_common/modes.md`」の参照に一本化。audit も modes.md を読む構造）

### ~~6. 優先度ラベル（High/Medium/Low）の定義が4箇所に分散~~ 実質解消（2026-07-16 確認: `forge/_common/agents.md` に「優先度ラベル（統一基準）」テーブルが整備済み。cross-review はソート順の言及のみで定義を持たない。style-review の High/Medium 数による Pass/Fail は「判定基準」であってラベル定義の重複ではない）

### ~~7. `Task.detached` ハンドラパターンがエージェントに重複~~ 対応済み 2026-07-16（真の重複は handler deadlock パターンの 4 ファイルと精査し `_claude/_common/task-detached-deadlock-pattern.md` に集約。appstore-monetization は無関係な StoreKit サンプル、xcodebuild-runner は lint ルール表の 1 行言及のため対象外）

### 8. 一部エージェントがどのスキルトリガーにも登録されていない — 一部対応済み 2026-02-19（appstore-submission-expert を CLAUDE.md に追加。残りは TT 固有のため未対応）
- **内容**: `crash-analyzer`, `smoke-test-runner`, `statusline-setup`, `tt-api-expert`, `xcodebuild-runner` はトリガー未登録。名前を知らないと利用できない
- **推奨**: TT 固有のものは項目 13（プロジェクトローカル化）とセットで扱う。カタログは項目 21 参照

### 9. `_common/output-format-template.md` と `quality-checklist.md` が活用されていない — 現存（2026-07-16 確認: `_claude/_common/` に実在するが、`@../_common/` 参照を持つのは architecture-reviewer / swift-language-expert の 2 エージェントのみ）
- **推奨**: 全エージェントで参照させたいなら agents.md の共通指示に明記する。逆に 2 件しか使っていないならインライン化して `_common/` 側を削る選択もある（参照グラフを見て判断）

### 10. audit スキルの「直接実行」フローが薄い — 現存・緩和（2026-07-16 確認: forge 不在時に自動で直接実行へフォールバックする分岐は追加済みだが、直接実行時の調査指針の薄さ自体は変わらず）
- **推奨**: 各監査タイプに具体的な調査コマンド（Grep/Glob パターン）を追加するか、「直接実行は forge 不在環境向けの縮退運転」と位置づけを明記する

### 11. シンボリックリンク依存で断絶リスクがある — 一部対応（2026-07-16 確認: setup.sh に二重リンク掃除・リンク先破壊防止のガードは追加済み。健全性チェック（dangling link 検出）は未実装）
- **推奨**: setup.sh か make test に dangling symlink 検出を追加する

### ~~12. CLAUDE.md のスキルトリガーテーブルが手動メンテナンス~~ 対応済み 2026-07-16（`tests/claude/test_skill_trigger_table.sh` を新設。削除残り参照・登録漏れを両方向で検出し make test で自動実行。意図的にテーブルへ載せないスキルは EXEMPT_SKILLS へ）

### 13. プロジェクト固有スキルがグローバルスコープに存在 — 半解消（2026-07-16 確認: smoke-test は削除済み。perf-analysis は残存し description に「ThumbnailThumb 専用」を明記して緩和済み）
- **推奨**: perf-analysis を ThumbnailThumb リポジトリの `.claude/skills/` へ移動する（ThumbnailThumb 側の作業時にまとめて）。関連: `issues/pending/skills-blog-lessons-improvements.md` 項目 8

### 14. forge スキルが audit スキルの内部ファイルパスを直接参照 — 現存・緩和（2026-07-16 確認: audit が `forge/_common/modes.md` を直接 Read する構造は継続。存在しない場合のフォールバック手順は追記済み）
- **推奨**: 実害が出るのは forge の内部構造変更時のみ。forge を触る改修が来た時に合わせて見直す（先回り改修はしない）

### 15. エージェント出力 JSON スキーマが agents.md と style-review で非互換 — 現存（2026-07-16 確認: style-review の `wcag` 独自フィールドは残存）
- **推奨**: 統合時に実害が観測されてから対処（`extensions` フィールド標準化）。現状は理論上の懸念

---

## 🟢 Low Priority

### ~~16. forge の Ultra モードへの到達が事実上困難~~ 実質解消（2026-07-16 確認: modes.md に「Ultra 直接トリガー（即座に Ultra 推奨）」の具体キーワード表が整備済みで、SKILL.md Phase -1 から modes.md 参照に一本化されている）

### 17. Language Adaptation の参照方法が統一されていない — 要再調査（2026-07-16 確認: `@../_common/language-adaptation.md` 参照は 2 エージェントのみ。他エージェントは settings.json の `language: 日本語` で足りている可能性があり、参照追加より「_common ファイル自体の要否」を先に判断すべき。項目 9 とセットで扱う）

### 18. audit スキルの AskUserQuestion が2段階質問 — 現存・構造変化（2026-07-16 確認: 現在は「実行方式（forge/直接）→ モード選択」の2段階。当時の「監査タイプ16種の2段階」とは別物になったが、2段階である点は同じ）
- **推奨**: 実運用で冗長と感じたら統合。使用頻度が低ければ現状維持

### ~~19. スキルに version/changelog の概念がない~~ 解消済み（2026-07-16 確認: 全 17 スキルの SKILL.md frontmatter に `version:` が導入済み）

### ~~20. swift-vlc-player の「段階的学習」意図が未明記~~ 対象消滅（スキル削除済み）

### 21. agents/ にカタログ・インデックスがない — 現存（31 エージェント）
- **推奨**: `_claude/agents/README.md` を作成し用途別グルーピングのカタログを提供する。ただし項目 12 と同じ「手動テーブルの陳腐化」問題を新たに抱えるため、作るなら自動生成を検討

### 22. forge examples.md が Electron と SwiftUI のみで偏り — 現存・緩和（2026-07-16 確認: examples.md がハブ化され構造は改善。Go/Rails/Node.js のサンプルは依然なし）
- **推奨**: 実際に Go/Rails プロジェクトで forge を使って不便を感じた時に追加（先回りで書かない）

---

## ⚪ 対応不要（検討済み・意図的な設計）

| 項目 | 理由 |
|------|------|
| `@_common/` 参照が自動解決されない | SKILL.md に「明示的に Read すること」と記載あり。Claude の動作仕様による制約 |
| forge の複雑なフェーズ構造 | forge は Workflow（forge.js）+ skill 層の二層構成に再設計済み（2026-07-16 時点）。決定論的部分は Workflow が担い、複雑度は意図的 |
| audit/SKILL.md の issues-done タイプ | 一見デッドコードに見えるが完了issueの管理用途として有効 |
