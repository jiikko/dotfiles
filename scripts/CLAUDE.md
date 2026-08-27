# scripts/

tmux バインド (_tmux.conf) から display-popup / run-shell 経由で呼ばれるヘルパー群。
新しい .sh は登録不要で自動的に shellcheck 対象になる (discover_shell_scripts.sh が発見)。

## popup ヘルパーの罠

- popup 内の shell-command では `#{...}` format が展開されない (TMUX_PANE も無い。tmux 3.6a 実測)。
  対象 pane はスクリプト側で `tmux display-message -p` で解決し、冒頭で変数に固定してから
  confirm する (「確認した相手」と「操作する相手」を一致させる。tmux_kill_confirm.sh 参照)
- **対話 UI が要るものは Go の TUI (src/schedkeys) に出す選択肢がある**。gum は入力欄で
  IME の未確定文字が入力位置に出ない (bubbletea v1 が本物のカーソルを隠すため。pty 実測 2026-08-27)。
  日本語を打つ欄がある popup は bubbletea v2 の `tea.View.Cursor` を使う (src/schedkeys/README.md)。
  そのとき**破壊的な操作 (削除・取消) は UI 側で実行せず、シェルの gum confirm --default=false に残す**
  (tmux_schedule_keys.sh が実例)
- bind から引数を渡すときは `#{q:...}` でエスケープする (素の `#{...}` 埋めは値の `"` で
  sh 構文エラーになる穴があった。tmux_scratch_popup.sh 冒頭参照)

## 確認系 (tmux_*_confirm.sh) の fail-safe 規約

- `set -e` を使わない。fail-safe は `gum confirm && アクション` の && 短絡に依存する
  (gum 未導入なら exit 127 で何も起きない)。-e を足すと gum の非 0 終了でスクリプト自体が
  落ち、確認拒否と異常終了の挙動差が消える
- gum confirm は `--default=false` (Enter 素通しでは実行されない) に統一する。
  **強制手段**: `tests/tmux/test_confirm_default_gate.sh` が `scripts/*.sh` と `_tmux.conf` の
  `gum confirm` を発見式に列挙し、`--default=false` を伴わないものがあれば落とす (発見 0 件も
  失敗扱い)。個別の呼び出し検査は `tests/tmux/test_confirm_scripts.sh` 側

## hook (set-hook) から呼ばれるスクリプトの無音契約

- **hook の run-shell (-b 含む) 経由で呼ばれるスクリプトは、縮退時 (表示先クライアント不在・
  依存コマンド不在・保存失敗等) に「無音で exit 0」すること**。非 0 や stdout/stderr を返すと
  tmux がエラーをアクティブ pane の view-mode として積み、tmux 3.4 ではモードスタックが
  copy-mode スクロールを無反応にする実害まで連鎖する (bin/tmux-toast と debounce 保存で実測
  2026-07-29。経緯は docs/feedback-nvim-tmux-2026-07-29.md パターン1)
- 失敗の観測はログ (~/.cache/tt-*.log 等) が担う。pane はエラーの排出先ではない
- 対話 bind (C-s 等、ユーザーがキーを押した文脈) はこの契約の対象外 (エラーが見えるのが正)

## サーバ状態に触るスクリプトの不変条件

- tmux_reap_orphan_servers.sh: 生存 socket を持つプロセス (実サーバ・接続中 client) には
  絶対に触れない (zshlib/_tmux_session.zsh の bootstrap がこの前提で呼ぶ)
- resurrect の保存は全経路 tmux_resurrect_save.sh (単一 lock wrapper) を経由する。wrapper を
  迂回する保存経路を作らない (壊し合いの経緯は docs/tmux-plugins.md「保存経路の直列化」)
- resurrect の restore.sh パスはハードコードせず `@resurrect-restore-script-path` から解決する
  (vendor 移動で silent に壊れるため。tmux_restore_confirm.sh / _tt_wait_for_restore と同じ出典)
- 手動復元を display-popup -E 内で同期実行しない (popup close = 復元プロセスの kill になり
  silent な部分復元を生む。2026-07-30 実発 22/29 で停止)。confirm は popup、実行は
  tmux_restore_runner.sh (run-shell -b detach + 途中死の観測) に分離する
- tmux_server_watchdog.sh は `trap '' TERM` が生命線 (サーバは終了時に run-shell 子プロセスへ
  SIGTERM を送る。3.7b 実測 2026-07-30)。外すと肝心の死亡瞬間に watchdog ごと死ぬ
- 共有の観測ログ (~/.cache/tt-restore-trigger.log) に書くスクリプトは default socket ゲート
  (guards.sh の tt_on_default_server) を通す。-L テストサーバも conf を source して同じ hook を
  持つため、ゲート無しだとテストのイベントが本番の死因分類 (watchdog の verdict) を汚す。
  **書き手は guards.sh の `tt_trigger_log` だけ**にする (行書式の正本を 1 箇所に保つため)。
  直接 append する箇所は `make test-trigger-log-writers` (`scripts/check_trigger_log_writers.sh`)
  が落とす。`#!/bin/sh` で guards.sh を source できない等の例外は行内に
  `trigger-log-writer: allow` と理由を書く
- kill-server / kill-session の command-alias shim (tmux_log_kill_command.sh) は kill の実行
  経路上にある。どんな失敗でも kill 自体を止めない (無音 exit 0) こと。行の書式は
  docs/tmux-plugins.md「観測ログの読み方」の表が読者側の正本

## テスト

- このディレクトリのスクリプトの unit テストは tests/tmux/ に stub 方式 (PATH 先頭に偽
  tmux/gum/fzf を置き呼び出しを記録) で置く。実 tmux サーバには触れないため macOS でも
  スキップ不要。共有アサーションは tests/tmux/lib/stub_assert_helper.sh
- 例外的に実サーバが要るテスト (test_fork_scratch.sh) は Darwin skip + unset TMUX が必須
  (2026-07-07 の実サーバ誤 kill 事故。同ファイル冒頭参照)

## lib/

- lib/tmux_resurrect_guards.sh は zshlib からも source される共有実装。POSIX 互換関数のみで
  書く (bash / zsh 両対応。判定式・TTL をスクリプト側と zsh 側で二重定義しないための集約点)
