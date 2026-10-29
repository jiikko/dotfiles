# エージェント索引 (26 件)

`_claude/agents/*.md` の一覧。**名前を知らないと呼べない**状態を避けるための入口
(issue 001 の項目 21)。`~/.claude/CLAUDE.md` の「スキルファイル参照」表は skill が主役で、
agent は一部しか載っていないので、agent はここが正本。

- **モデル**は frontmatter の `model:`。opus は判断が要るもの、sonnet / haiku は定型作業
  (方針は [`subagent-model-tiering.md`](../rules/subagent-model-tiering.md))
- 説明は各ファイルの `description:` から。**発火条件はそちらが正本**なので、
  迷ったら当該ファイルを読む

## 言語・フレームワーク別 (実装の主役)

| エージェント | model | 守備範囲 |
|---|---|---|
| `swift-language-expert` | opus | Swift の言語機能 (async/await・actor・protocol・generics・メモリ管理) |
| `swiftui-macos-designer` | opus | macOS の SwiftUI 全般 (View・状態管理・AppKit 統合・HIG) |
| `swift-concurrency-expert` | opus | Swift Concurrency の設計とデバッグ (actor 設計・データ競合・MainActor) |
| `go-architecture-designer` | opus | Go の**すべて** (新機能・package 分割・interface 設計・並行) |
| `rails-domain-designer` | opus | Rails / Ruby の**すべて** (model・service・query object・責務の置き場所) |
| `nodejs-expert` | opus | Node.js / TypeScript のサーバサイド (async・stream・event loop) |
| `css-expert` | opus | CSS / SCSS / CSS-in-JS (セレクタ・レイアウト・アニメーション) |
| `electron-expert` | opus | Electron (main/renderer・IPC・ネイティブモジュール・配布) |

## 設計・レビュー

| エージェント | model | 守備範囲 |
|---|---|---|
| `architecture-reviewer` | opus | **既定のアーキレビュー**。層の分離・依存方向・保守性 |
| `swift-architecture-designer` | opus | **大きい**構造変更のみ (module 設計・God Object 分解)。日常は上を先に |
| `code-reviewer` | opus | 汎用のコードレビュー。言語特化のエージェントが在る言語はそちらが先 |
| `security-auditor` | opus | 認証・認可・入力処理・ファイル操作・外部通信が動いたとき |
| `refactoring-patterns` | opus | リファクタの手順 (Extract・Strangler Fig・段階移行) |
| `dependency-analyzer` | sonnet | 変更の影響範囲・循環依存。**リファクタ前に**使う |
| `test-coverage-advisor` | opus | テスト戦略。どこから書くか・回帰の守り方 |

## 実行・調査

| エージェント | model | 守備範囲 |
|---|---|---|
| `test-runner` | opus | 言語非依存のテスト実行 (pytest / jest / rspec / go test / cargo) |
| `swiftui-test-expert` | opus | XCTest / XCUITest / ViewInspector の**書き方**と flaky の潰し方 |
| `debugger` | opus | 実行時エラー・スタックトレース・想定外の挙動 (言語横断) |
| `research-assistant` | opus | 実装前の技術調査 (公式ドキュメント・ベストプラクティス・比較) |

## macOS / iOS の周辺領域

| エージェント | model | 守備範囲 |
|---|---|---|
| `macos-system-integration-expert` | opus | サンドボックス・Keychain・NSStatusItem・権限・NSWorkspace |
| `appkit-swiftui-integration-expert` | opus | AppKit と SwiftUI の境界 (NSViewRepresentable・responder chain・AttributeGraph) |
| `data-persistence-expert` | opus | SwiftData / Core Data / CloudKit / マイグレーション |
| `swiftui-performance-expert` | opus | View の再描画コスト・Instruments |
| `image-editing-expert` | opus | 画像編集 UI (Canvas・レイヤ・ツールパレット) |
| `appstore-submission-expert` | sonnet | App Store Connect の操作 (提出・TestFlight・リジェクト対応) |
| `crash-analyzer` | sonnet | クラッシュレポートの取得と解析 |

## 置いていないもの

- ThumbnailThumb 専用の `tt-api-expert` / `smoke-test-runner` と、`xcodebuild-runner` / `appstore-monetization-expert` / `swift-test-runner` は、
  2026-10-29 に global から消した (issue 914)。どれも使われておらず (このマシンの 08-22 以降で 0 回)、dotfiles からの参照も無かった。
  ThumbnailThumb と baby-note は自分の `.claude/agents/` に写しを持つ (project の agent が同じ名前の global より優先される)

## ハーネス組み込み

| エージェント | 備考 |
|---|---|
| `statusline-setup` | Claude Code 標準。ステータスラインの設定用でトリガー登録は不要 |

## 保守

- **agent を足したら / 消したらこの表も直す** ([`new-tool-requires-entrypoint-docs.md`](../rules/new-tool-requires-entrypoint-docs.md))。
  件数はファイル数と一致させる (`ls _claude/agents/*.md | wc -l`)
- ⚠️ **`@../_common/...` 形式で共通文書を参照しない**。展開されずリテラルのまま届く
  (実測 2026-09-02)。共通の作法は各 agent の本文へインラインで書く
