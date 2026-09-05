# docs/ — 設計文書・仕様・調査記録の索引

**コードの What はソースが出典**。ここに置くのは実装から読み取れない判断 (なぜそうしたか / なぜそうしなかったか) と、
検証しないと壊れる前提、そして時点の調査結果。

読む順は「触る対象」で決める。下の表から 1 本選べば足りるように書いてあるので、全部開かない。

## 触る前に読むもの (制約が書かれている)

これらは「知らずに触ると壊す」類。該当領域を変更する前に読む。

| 文書 | 何が書かれているか | 読む trigger |
|---|---|---|
| [`glogx-bubbletea-v2.md`](glogx-bubbletea-v2.md) | glogx が bubbletea v2 で動く前提、v2 の新機能を採らなかった判断、次に上げるとき測り直すもの。**他モジュールが v1 のままである理由**も | glogx の TUI を触る / bubbletea を上げる |
| [`theme-colors.md`](theme-colors.md) | 色は「意味 (role) → 定数」で管理する。**使用箇所ではなく定数を触る**。色の意味マップ | tmux か nvim の色を変えたい |
| [`tmux-plugins.md`](tmux-plugins.md) | セッション永続化 (resurrect + continuum)。イベント駆動の debounce 保存と、全保存経路を直列化する単一 lock | tmux の保存・復元経路を触る |

## 仕様 (契約。実装より仕様が先)

glogx の画面のうち、**複数 repo をこの規約に寄せる** / **書き込みを行う**ために契約が要るもの。

| 文書 | 対象 | 性格 |
|---|---|---|
| [`issues-viewer-spec.md`](issues-viewer-spec.md) | glogx の issues viewer (`i` キー) が `issues/` をどう解釈するか | repo を寄せるための契約。読み方だけでなく、なぜその読み方かを実測つきで |
| [`glogx-ui-guide.md`](glogx-ui-guide.md) | glogx 全画面に共通する操作感とキー語彙 (vim 層 / emacs 別名層 / 動作層、開閉・破壊的操作・案内の規律、`J`/`K` 項目送り) | 新しいキーを足す / 画面を足す前に読む。個別キーの一覧は `src/glogx/README.md` |
| [`status-viewer-spec.md`](status-viewer-spec.md) | glogx の status viewer (`s` キー) の stage / unstage | **write する画面**なので「何を絶対にしないか」が本体 |

## 仕組みの説明 (作ったものの設計)

| 文書 | 何の仕組みか |
|---|---|
| [`tmux-as-platform.md`](tmux-as-platform.md) | tmux を「小さなツールの土台」として使う。popup / menu / prompt / formats / hooks を UI と自動化の primitive として捉える見方。**この repo の tmux 系スクリプトの設計思想** |
| [`tmux-window-fade.md`](tmux-window-fade.md) | window list の放置フェード (最近作業した window ほど派手に光る) |
| [`tmux-toast.md`](tmux-toast.md) | `bin/tmux-toast`。フォーカスを奪わない通知 (display-popup との違い) |
| [`claude-fork-popup.md`](claude-fork-popup.md) | Claude の会話を `--fork-session` で枝分かれさせ、`C-t b` の popup で覗く |
| [`nvim-plugin-load-tracker.md`](nvim-plugin-load-tracker.md) | 使っていないプラグインを勘でなく数値で棚卸しする仕組み |

## 調査・棚卸し (時点の記録。鮮度に注意)

**書かれた時点のスナップショット**なので、日付を見て古ければ測り直す。コードが動けば嘘になる類。

| 文書 | 時点 | 内容 |
|---|---|---|
| [`nvim-plugins.md`](nvim-plugins.md) | 2026-08 | プラグイン単位の棚卸し。カテゴリごとのデファクト候補と乗り換え可否 |
| [`nvim-trends-2026-08.md`](nvim-trends-2026-08.md) | 2026-08 | Neovim 生態系の流れ (0.12 / vim.pack / treesitter main / ACP) と、この設定の立ち位置 |
| [`feedback-nvim-tmux-2026-07-29.md`](feedback-nvim-tmux-2026-07-29.md) | 2026-07-29 | nvim 約 2,000 行 + tmux 約 2,600 行の全読レビュー (実測つき) |

## ここに置かないもの

| 種類 | 置き場 |
|---|---|
| 全プロジェクト共通の作業規範 (毎セッション読まれる) | [`_claude/rules/`](../_claude/rules/) — 本文は規範だけ。根拠は `_claude/rules-rationale/` |
| dotfiles 固有で、必要なときだけ読む規範 | [`rules/`](../rules/README.md) — zsh の hook / trap、bench の見方 (索引つき) |
| ディレクトリ固有の規約 | そのディレクトリの `CLAUDE.md` (`scripts/` / `tests/` / `src/glogx/` / `_claude/`) |
| 各ツールの使い方 | `src/<name>/README.md` と `<tool> --help` |
| 作業の記録・残課題・振り返り | [`issues/`](../issues/) (書式は `issues/README.md`) |
| 検証レポートの中間生成物 | `./tmp` (gitignore。**結論は issue かコードへ移す**。掃除は `make clean-tmp`) |

## 書くときの規律

- **What を書かない**。ソースが出典。ここには「なぜ」と「検証しないと壊れる前提」を書く
- **調査は日付を入れる**。ファイル名か本文冒頭に時点を書き、古くなったら測り直す前提にする
- **新しく足したらこの索引に 1 行足す** ([`new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md))。
  索引に載っていない文書は、存在を知っている人にしか届かない
