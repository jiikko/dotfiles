# docs/ — 設計文書・仕様・調査記録の索引

**コードの What はソースが出典**。ここに置くのは実装から読み取れない判断 (なぜそうしたか / なぜそうしなかったか) と、
検証しないと壊れる前提、そして時点の調査結果。

読む順は「触る対象」で決める。下の表から 1 本選べば足りるように書いてあるので、全部開かない。

## ツールの使い方 (`tools/`)

dotfiles が入れる道具の使い方。root の README からはここへ案内している。

| 文書 | 内容 |
|---|---|
| [`tools/tmux.md`](tools/tmux.md) | tmux のキー (prefix `C-t`)・ペインの見え方・ウィンドウ名の自動反映・Claude Code の作業状態表示。設定の正本は `_tmux.conf` |
| [`tools/video-functions.md`](tools/video-functions.md) | 動画のシェル関数 `repair` / `av1ify` (`av1c`) / `concat` (`zshlib/`) |
| [`tools/macos.md`](tools/macos.md) | Karabiner-Elements の設定の扱い / `kernel-alloc-watch` / Finder Quick Actions |

## 触る前に読むもの (制約が書かれている)

これらは「知らずに触ると壊す」類。該当領域を変更する前に読む。

| 文書 | 何が書かれているか | 読む trigger |
|---|---|---|
| [`claude-mods.md`](claude-mods.md) | Claude Code の mods (関数 hook の plugin) の置き場所・読み込み (`CLAUDE_CODE_PLUGIN_DIRS`)・読まれる経路の実測・**このマシンで mod に届かないイベント**・pro-con の役に載せない理由・テストと CI の扱い | `_claude/mods/` に mod を足す / settings の hook を mod へ移す |
| [`claude-mods-guide.md`](claude-mods-guide.md) | mods の全体像 (どのファイルが何をするか)・各 mod の仕組み (desktop-statusline のデータの流れ、`/statusline-refresh` とは何か)・変更と試し方の手順・反映のタイミング・つまずき | mod を初めて触る / 直す / 足す / desktop に出ない |
| [`glogx-bubbletea-v2.md`](glogx-bubbletea-v2.md) | glogx が bubbletea v2 で動く前提、v2 の新機能を採らなかった判断、次に上げるとき測り直すもの。**v1 のまま repo の外へ出たモジュール (parallel-each) を上げない理由**と、charm 依存の版を module 間で揃える検査も | glogx の TUI を触る / bubbletea を上げる |
| [`nvim-ruby-lsp.md`](nvim-ruby-lsp.md) | nvim の Ruby LSP。定義ジャンプが索引をどう引くか、索引がいつ作られどこに在るか (ディスクには無い)、参照検索だけ 11 秒かかる理由、2026-09-08 の高速化で何を書き何を書かなかったか | Ruby のサーバ選択・`<C-k>`・ステータスラインの進捗を触る / 「遅い」と言われた |
| [`theme-colors.md`](theme-colors.md) | 色は「意味 (role) → 定数」で管理する。**使用箇所ではなく定数を触る**。色の意味マップ | tmux か nvim の色を変えたい |
| [`tmux-plugins.md`](tmux-plugins.md) | セッション永続化 (resurrect + continuum)。イベント駆動の debounce 保存と、全保存経路を直列化する単一 lock | tmux の保存・復元経路を触る |
| [`tmux-production-kill-guard.md`](tmux-production-kill-guard.md) | 本番 tmux サーバの誤殺を防ぐ二層ガード (`bin/tmux` shim + deny hook)。なぜ文字列 hook では止まらなかったか、保護対象の判定、kill-server/kill-session の別扱い、エスケープ、残存リスク | `bin/tmux` / `deny-bare-tmux-kill.sh` を触る / 本番 tmux が消えた |

## 仕様 (契約。実装より仕様が先)

glogx の画面のうち、**複数 repo をこの規約に寄せる** / **書き込みを行う**ために契約が要るもの。

| 文書 | 対象 | 性格 |
|---|---|---|
| [`issues-viewer-spec.md`](issues-viewer-spec.md) | glogx の issues viewer (`i` キー) が `issues/` をどう解釈するか | repo を寄せるための契約。読み方だけでなく、なぜその読み方かを実測つきで |
| [`glogx-ui-guide.md`](glogx-ui-guide.md) | tuikit を使う TUI (glogx 全画面と pro-con) に共通する操作感とキー語彙 (vim 層 / emacs 別名層 / 動作層、開閉・破壊的操作・案内の規律、`J`/`K` 項目送り、入力欄の編集キー、pro-con の例外、tuikit の部品の地図と通知の語彙) | 新しいキーを足す / 画面を足す / tuikit の部品を足す前に読む。個別キーの一覧は `src/glogx/README.md` |
| [`status-viewer-spec.md`](status-viewer-spec.md) | glogx の status viewer (`s` キー) の stage / unstage | **write する画面**なので「何を絶対にしないか」が本体 |
| [`macos-health-check.md`](macos-health-check.md) | glogx に足す予定のヘルスチェック画面 (`H` キー。**未実装**) が見る項目 | 画面の前段の情報源。項目・取得コマンド・判定と、コマンドごとの stdout / stderr / rc の実測 (macOS 27.0)。壊れていないかに加えて、推奨の設定 (CIS 由来のログイン・共有、Homebrew・Brewfile・CLT などの開発環境) になっているかも。点数を付ける方針 (健康と設定で分ける・判定不能は数えない) と `D` の doctor との境界も |

## 時点の調査記録 (実測。コードが動けば古くなる)

| 文書 | 何を測ったか |
|---|---|
| [`../nvim/ruby-refs-index/README.md`](../nvim/ruby-refs-index/README.md) | ruby-lsp の参照検索がなぜ遅いか (索引に呼び出し側が無い) と、呼び出し側索引の実測 (構築 1 秒 / 37 MB / クエリ数 µs)。ripgrep との精度差も。issue 334 |

## 仕組みの説明 (作ったものの設計)

| 文書 | 何の仕組みか |
|---|---|
| [`tmux-as-platform.md`](tmux-as-platform.md) | tmux を「小さなツールの土台」として使う。popup / menu / prompt / formats / hooks を UI と自動化の primitive として捉える見方。**この repo の tmux 系スクリプトの設計思想**と、キー・実装ファイルの対応表（本番サーバの kill 防護 shim `bin/tmux` を含む） |
| [`tmux-window-fade.md`](tmux-window-fade.md) | window list の放置フェード (最近作業した window ほど派手に光る) |
| [`tmux-toast.md`](tmux-toast.md) | `bin/tmux-toast`。フォーカスを奪わない通知 (display-popup との違い) |
| [`claude-fork-popup.md`](claude-fork-popup.md) | Claude の会話を `--fork-session` で枝分かれさせ、`C-t b` の popup で覗く。**現在は休眠中**（bind はコメントアウト。使わないと決めた理由と復活手順つき） |
| [`nvim-plugin-load-tracker.md`](nvim-plugin-load-tracker.md) | 使っていないプラグインを勘でなく数値で棚卸しする仕組み |

## 調査・棚卸し (時点の記録。鮮度に注意)

**書かれた時点のスナップショット**なので、日付を見て古ければ測り直す。コードが動けば嘘になる類。

| 文書 | 時点 | 内容 |
|---|---|---|
| [`nvim-plugins.md`](nvim-plugins.md) | 2026-08 | プラグイン単位の棚卸し。カテゴリごとのデファクト候補と乗り換え可否 |
| [`nvim-trends-2026-08.md`](nvim-trends-2026-08.md) | 2026-08 | Neovim 生態系の流れ (0.12 / vim.pack / treesitter main / ACP) と、この設定の立ち位置 |
| [`pro-con-vs-parallel-sessions-2026-09-26.md`](pro-con-vs-parallel-sessions-2026-09-26.md) | 2026-09-26 | pro-con 経由で作業させるか、Claude を何本も立ち上げてチャットで指示するかのトレードオフ (2 日の dogfooding の実測つき) |
| [`pro-con-vs-chat-cli-2026-09-27.md`](pro-con-vs-chat-cli-2026-09-27.md) | 2026-09-27 | pro-con は便利か。普通の Claude Code CLI のチャットと比べた Claude の観点 (チャット側の調査と pro-con のカード 10 枚を並べた 1 日の実例つき) |
| [`feedback-nvim-tmux-2026-07-29.md`](feedback-nvim-tmux-2026-07-29.md) | 2026-07-29 | nvim 約 2,000 行 + tmux 約 2,600 行の全読レビュー (実測つき) |

## アイデア (未着手)

まだ着手していない案。実装する前に、それぞれの「確かめること」を実測する。

| 文書 | 何の案か |
|---|---|
| [`ideas/anomaly-notification.md`](ideas/anomaly-notification.md) | 暴走しているプロセスや「いつもと違う」状態を、楽に知らせる仕組み。プロセスごとの「いつもの値」との比較と、「使っていないのに熱い」の判定。既製品 (Netdata など) の調べ |

## ここに置かないもの

| 種類 | 置き場 |
|---|---|
| 全プロジェクト共通の作業規範 (毎セッション読まれる) | [`_claude/rules/`](../_claude/rules/) — 本文は規範だけ。根拠は `_claude/rules-rationale/` |
| dotfiles 固有で、必要なときだけ読む規範 | [`rules/`](../rules/README.md) — bench の見方 (索引つき。zsh の hook / trap は `.claude/rules/` へ移した) |
| ディレクトリ固有の規約 | そのディレクトリの `CLAUDE.md` (`scripts/` / `tests/` / `_claude/` / `src/<name>/` の各 module など) |
| Go で書いた各ツールの使い方 | `src/<name>/README.md` と `<tool> --help` (tmux・シェル関数・macOS 連携は上の `tools/`) |
| 作業の記録・残課題・振り返り | [`issues/`](../issues/) (共通規約は [`_claude/issue-rules.md`](../_claude/issue-rules.md)、dotfiles 固有は `issues/README.md`) |
| 検証レポートの中間生成物 | `./tmp` (gitignore。**結論は issue かコードへ移す**。掃除は `make clean-tmp`) |

## 書くときの規律

- **What を書かない**。ソースが出典。ここには「なぜ」と「検証しないと壊れる前提」を書く
- **調査は日付を入れる**。ファイル名か本文冒頭に時点を書き、古くなったら測り直す前提にする
- **新しく足したらこの索引に 1 行足す** ([`new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md))。
  索引に載っていない文書は、存在を知っている人にしか届かない
