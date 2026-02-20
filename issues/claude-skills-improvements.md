# Claude Skills & Agents 改善案

調査日: 2026-02-19
調査モード: Forge Minimum+（architecture-reviewer, Explore）
調査対象: ~/.claude/skills/ (11スキル・23ファイル・5424行), ~/.claude/agents/ (32エージェント・11200行)

---

## 🔴 High Priority

### 1. VALIDATION スキル（style-review）の実行条件が矛盾している
- **ファイル**: `_claude/skills/forge/_common/skill-triggers.md`
- **内容**: `skill-triggers.md` では Minimum/Minimum+ モードで VALIDATION スキルを「省略」と定義しているが、CSS 変更を含む実装をこれらのモードで行った場合、WCAG 検証がスキップされる。audit/SKILL.md は「forge 経由で実行」を推奨しているため、Minimum+ で audit → forge → CSS 変更という経路では style-review が実行されない
- **推奨**: VALIDATION を全モード標準実行にするか、CSS/スタイル変更検出時は強制的に実行するロジックを追加する

### 2. `electron-expert.md` が 1644行の God File
- **ファイル**: `_claude/agents/electron-expert.md`
- **内容**: 単一エージェント定義が 1644行。IPC, セキュリティ, パッケージング, auto-update, native modules など複数の責務が混在している
- **推奨**: 責務ごとにエージェントを分割（例: `electron-ipc-expert`, `electron-packaging-expert`）、または主要セクションをサブドキュメント化して参照する形に変更

### 3. `swift-vlc-player/SKILL.md` が 1071行の God File
- **ファイル**: `_claude/skills/swift-vlc-player/SKILL.md`
- **内容**: Section 7 (398-749行) と Section 8 (751-959行) に「BAD → GOOD パターン」が合計9回繰り返されている。パターンの共通部分が抽出されていない
- **推奨**: `_common/` 相当のサブファイルに分割（例: `race-condition-patterns.md`, `error-recovery-patterns.md`）。SKILL.md 自体は参照のみにする

### 4. Minimum モードのクロスレビュー・ペアリング定義が存在しない
- **ファイル**: `_claude/skills/forge/_common/cross-review.md`
- **内容**: `cross-review.md` には「Minimum+ モード用ペアリング」テーブルはあるが、Minimum モードのペアリングが未定義。modes.md では Minimum の Phase 1.1 は「省略」と記載されているが、実際に Minimum モードでクロスレビューを省略するロジックがどのエージェントが担当するか明記されていない
- **推奨**: `cross-review.md` に「Minimum モード: クロスレビューなし（理由: 速度優先）」と明記する

---

## 🟡 Medium Priority

### 5. モード定義が3ファイルに分散している（DRY 違反）
- **ファイル**: `_claude/skills/forge/SKILL.md:64-99`, `_claude/skills/forge/_common/modes.md`, `_claude/skills/audit/SKILL.md:62-73`
- **内容**: Minimum/Minimum+/Standard/Maximum/Ultra のモード説明が約70%重複した内容で3ファイルに書かれている。modes.md を更新しても他ファイルへの反映が漏れる可能性がある
- **推奨**: `modes.md` を Single Source of Truth として、他ファイルは「詳細は `_common/modes.md` を参照」のみに変更する

### 6. 優先度ラベル（High/Medium/Low）の定義が4箇所に分散
- **ファイル**: `_claude/skills/forge/_common/agents.md:39-45`, `_claude/skills/forge/_common/cross-review.md:61-67`, `_claude/skills/style-review/SKILL.md:159-165`, `_claude/skills/audit/SKILL.md:24-42`
- **内容**: 同じ概念の定義が4箇所に存在し、style-review や audit のものは定義が若干異なる
- **推奨**: `_common/priority-labels.md` を新設して一元管理する

### 7. `Task.detached` ハンドラパターンが4エージェントに重複
- **ファイル**: 複数の `_claude/agents/*.md`
- **内容**: デッドロック防止のための `Task.detached` パターン（ほぼ同一のコード例）が少なくとも4エージェントにコピーされている
- **推奨**: `_common/swift-concurrency-patterns.md` として抽出し、各エージェントから参照する

### 8. 6エージェントがどのスキルトリガーにも登録されていない
- **ファイル**: `_claude/skills/forge/_common/skill-triggers.md`, `~/.claude/CLAUDE.md`
- **内容**: `appstore-submission-expert`, `crash-analyzer`, `smoke-test-runner`, `statusline-setup`, `tt-api-expert`, `xcodebuild-runner` など複数のエージェントがトリガーなしで定義されているため、ユーザーが名前を知らないと利用できない
- **推奨**: CLAUDE.md のスキルトリガーテーブルにこれらのエージェントを追加するか、`agents/README.md` として全エージェントのカタログを作成する

### 9. `_common/output-format-template.md` と `quality-checklist.md` が活用されていない
- **ファイル**: `_claude/agents/` 配下の `_common/` 参照部分
- **内容**: これらの共通ファイルが定義されているが、各エージェントで実際に Read 指示が行われているかが不明確。効果が発揮されていない可能性がある
- **推奨**: `agents.md` の「共通ファイルの読み込み」セクションに両ファイルを明記し、全エージェントで参照するよう強制する

### 10. audit スキルの「直接実行」フローが薄い
- **ファイル**: `_claude/skills/audit/SKILL.md:103-121`
- **内容**: forge を使わない「直接実行」の調査指針が高レベルな要点のみ。ユーザーが自力で実装するには情報が不足している
- **推奨**: 各監査タイプに具体的な調査コマンド（Grep パターン、Glob パターン等）を追加する。または「直接実行は非推奨、常に forge 経由を推奨」と明記して、指針を削除する

### 11. シンボリックリンク依存で断絶リスクがある
- **ファイル**: `~/.claude/skills/` → `/Users/koji/dotfiles/_claude/skills/`
- **内容**: すべてのスキル・エージェントが dotfiles リポジトリへのシンボリックリンク。リンク再構築時や環境移行時にパスが無効化される可能性がある
- **推奨**: `setup.sh` にシンボリックリンクの健全性チェックを追加するか、リンク断絶時のエラーメッセージを明確にする

### 12. CLAUDE.md のスキルトリガーテーブルが手動メンテナンス
- **ファイル**: `~/.claude/CLAUDE.md:19-29`
- **内容**: スキルを追加・削除するたびに CLAUDE.md のテーブルを手動で更新する必要がある。テーブルとスキルディレクトリの自動同期の仕組みがない
- **推奨**: `setup.sh` にスキル一覧を自動生成するスクリプトを追加するか、CLAUDE.md に「スキルを追加した際はこのテーブルも更新すること」という明記とチェックリストを加える

### 13. プロジェクト固有スキル（perf-analysis, smoke-test）がグローバルスコープに存在
- **ファイル**: `_claude/skills/perf-analysis/`, `_claude/skills/smoke-test/`
- **内容**: `./tmp/perf.log` や `bin/tt-client` への参照がハードコードされており、特定プロジェクト（ThumbnailThumb）専用にも関わらずグローバルスキルとして配置されている
- **推奨**: プロジェクト固有スキルは各プロジェクトの `_claude/skills/` に移動する。グローバルには汎用パラメータ化バージョンのみ置く

### 14. forge スキルが audit スキルの内部ファイルパスを直接参照
- **ファイル**: `_claude/skills/audit/SKILL.md`
- **内容**: audit が `~/.claude/skills/forge/_common/modes.md` を直接 Read するよう指示している。forge の内部構造変更時に audit が壊れる密結合
- **推奨**: forge SKILL.md にモード一覧を返す「パブリックAPI」的なインターフェースを定義し、audit はそちらを参照するように変更する

### 15. エージェント出力 JSON スキーマが agents.md と style-review で非互換
- **ファイル**: `_claude/skills/forge/_common/agents.md:86-104`, `_claude/skills/style-review/SKILL.md:111-132`
- **内容**: 基本フィールドは同じだが style-review が `wcag` フィールドを独自に追加しており、統合エージェントがマージする際にスキーマ差異を考慮できていない可能性がある
- **推奨**: `agents.md` のスキーマに `extensions` フィールドを追加し、拡張フィールドはそこに格納するよう標準化する

---

## 🟢 Low Priority

### 16. forge の Ultra モードへの到達が事実上困難
- **ファイル**: `_claude/skills/forge/_common/modes.md:85-91`
- **内容**: Ultra モードは「原因不明」「クラッシュ」「デッドロック」等のキーワードで即推奨されるが、日常的なユーザーはこのキーワードを意識せずに使う。Ultra モードの存在がドキュメントされているが発見性が低い
- **推奨**: forge SKILL.md の Phase -1 の選択肢説明に「いつ Ultra を選ぶか」の具体例を追加する

### 17. Language Adaptation の参照方法が統一されていない
- **ファイル**: `_claude/agents/` 配下の複数ファイル
- **内容**: 一部エージェントは `_common/language-adaptation.md` を参照しているが、3つのエージェントは同様の内容をインラインで記述している（約60%重複）
- **推奨**: インライン記述のエージェントを `_common/language-adaptation.md` 参照に統一する

### 18. audit スキルの AskUserQuestion が4選択肢制限を意識した迂回設計になっている
- **ファイル**: `_claude/skills/audit/SKILL.md:55-73`
- **内容**: 監査タイプが16種類あるため、4選択肢制限により2段階の質問が必要になっている。カテゴリ選択→タイプ選択の2段階は UX として冗長
- **推奨**: カテゴリとタイプを統合し、最もよく使われる監査タイプ上位4つを直接提示する（その他はフリーテキスト入力で対応）

### 19. スキルに version/changelog の概念がない
- **ファイル**: 全スキルの SKILL.md
- **内容**: スキルが更新されても変更履歴がない。どのバージョンのスキルを使っているか追跡できない
- **推奨**: 各 SKILL.md の先頭に `## Version` セクションを追加する（例: `v1.2 - 2026-02-19: クロスレビュー改善`）

### 20. swift-vlc-player の「段階的学習」意図が未明記
- **ファイル**: `_claude/skills/swift-vlc-player/SKILL.md`
- **内容**: Section 7・8 の繰り返しパターンが意図的な段階的解説なのか、コピー忘れなのかが読み手に不明
- **推奨**: セクション冒頭に「このセクションは前のパターンを発展させた高度な実装を示します」等の意図説明を追加する

### 21. agents/ にカタログ・インデックスがない
- **ファイル**: `_claude/agents/`
- **内容**: 32エージェントが並列に配置されており、どのエージェントがいつ使われるべきか新規ユーザーには把握困難
- **推奨**: `_claude/agents/README.md` を作成し、エージェントを用途別にグルーピングしたカタログを提供する

### 22. forge examples.md が Electron と SwiftUI のみで偏り
- **ファイル**: `_claude/skills/forge/examples-electron.md`, `_claude/skills/forge/examples-swiftui.md`
- **内容**: サンプルが2技術スタックのみ。Go、Rails、Node.js などバックエンド向けのサンプルがない
- **推奨**: `examples-go.md`, `examples-rails.md` 等を追加するか、または `examples.md` に各技術スタックのミニサンプルを含める

---

## ⚪ 対応不要（検討済み・意図的な設計）

| 項目 | 理由 |
|------|------|
| `@_common/` 参照が自動解決されない | SKILL.md に「明示的に Read すること」と記載あり。Claude の動作仕様による制約 |
| forge の複雑なフェーズ構造（14フェーズ） | 各モードで実行フェーズが異なるため必要な複雑度。現状で機能している |
| audit/SKILL.md の issues-done タイプ | 一見デッドコードに見えるが完了issueの管理用途として有効 |
