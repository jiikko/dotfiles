#!/usr/bin/env bash
# _tmux.conf の C-v bind (issue 138) を隔離サーバで検証する。
# 固定する不変条件:
#   - root テーブルに C-v が 1 つだけ登録され、-N の説明が付いている (キーガイドに出る)
#   - 判定式 #{==:#{pane_current_command},zsh} が zsh のペインでだけ真になる
#     (= 実際の分岐先を決める値。nvim / less / cat が前面なら偽 = 既存の C-v が残る)
#   - true 側のコマンド (scripts/tmux_paste_clipboard.sh) が実際にペインへ流し込み、
#     貼ったバイト数のトーストを出す (issue 248 の確認 1 を人が判定するための数字)
#
# 🚨 **キー押下そのものは自動テストで再現できない**。tmux send-keys は key table を通さず
# ペインへ直接キーを送るので、root テーブルの bind は発火しない (2026-09-04 に実測: zsh には
# 生の C-v が届き `^` が出るだけだった)。押下経路の確認は pty で attach したクライアントか
# 人の手が要る (verify-interactive-prompt-with-pty-driver.md)。ここでは bind の登録・判定式・
# true 側のコマンドの 3 つに分解して固定する。
#
# 🚨 socket 隔離: $TMUX は TMUX_TMPDIR より優先されるので、必ず unset してユニークな -L を使う
# (_claude/rules/tmux-probe-requires-socket-isolation.md)。
set -uo pipefail
unset CDPATH TMUX TMUX_PANE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONF="$ROOT_DIR/_tmux.conf"
# conf 内の kill-* alias が `${DOTFILES_DIR:-$HOME/dotfiles}/scripts/...` を呼ぶ。CI の checkout は
# ~/dotfiles ではないので、明示しないと run-shell が 127 を stderr に吐く
export DOTFILES_DIR="$ROOT_DIR"
SOCK="ctrlv-test-$$"
TMP_DIR="$(mktemp -d)"
BIN="$TMP_DIR/bin"
mkdir -p "$BIN"

cleanup() {
  tmux -L "$SOCK" kill-server 2>/dev/null
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

fail=0
ok()   { printf '✓ %s\n' "$1"; }
bad()  { printf '✗ %s\n' "$1" >&2; fail=1; }

# 隔離の実証: このソケットに本番セッションが見えないこと
if tmux -L "$SOCK" ls >/dev/null 2>&1; then
  bad "隔離できていない (このソケットに既存サーバがある)"
  exit 1
fi

# pbpaste を差し替える (実クリップボードに触らない)
# 🚨 **マルチバイトを混ぜる**。ASCII だけだと bytes == chars なので、`wc -c` を `wc -m` に
# 変える変異が緑のまま通る (実測 2026-09-05)。トーストの主張は「貼ったバイト数」なので、
# バイト数と文字数が食い違う入力でだけ固定できる。
# 'PASTED-FROM-CLIPBOARD' (21) + 'あいう' (9 バイト / 3 文字) = 30 バイト / 24 文字
cat > "$BIN/pbpaste" <<'STUB'
#!/bin/sh
printf 'PASTED-FROM-CLIPBOARDあいう'
STUB
chmod +x "$BIN/pbpaste"
export PATH="$BIN:$PATH"

tmux -L "$SOCK" -f "$CONF" new-session -d -x 120 -y 30 "sh -c 'sleep 30'" 2>/dev/null

# --- 1. bind の登録と説明 ---
keys="$(tmux -L "$SOCK" list-keys -T root 2>/dev/null | grep -c ' C-v ')"
[ "$keys" = 1 ] && ok "root テーブルに C-v が 1 つ登録されている" || bad "C-v の登録数が 1 でない: $keys"
# 🚨 `cmd | grep -q` は使わない。pipefail 下では **一致していても非 0 になる**
# (grep -q が早期に閉じて cmd が SIGPIPE を受ける。issue 096)
if grep -q 'C-v.*クリップボード' <<< "$(tmux -L "$SOCK" list-keys -N -T root 2>/dev/null)"; then
  ok "-N の説明が付いている (キーガイドに出る)"
else
  bad "-N の説明が無い"
fi

# --- 2. 判定式がペインの前面プロセスで正しく分かれる ---
# 🚨 最初のセッションを消してから作り直さない。最後のセッションが消えるとサーバごと終了し、
# 次の new-session は **conf を読まない新サーバ** を立てる (-f はサーバ起動時にしか効かない)。
# 手元では ~/.tmux.conf -> _tmux.conf の link が同じ bind を読み直すので緑に見え、CI (link 無し)
# で「bind から判定式を取り出せない」として初めて出た (2026-09-04 run 33823346926)。
tmux -L "$SOCK" new-session -d -s zsh -x 120 -y 30 "zsh -f" 2>/dev/null
tmux -L "$SOCK" new-session -d -s other -x 120 -y 30 "sh -c 'sleep 30'" 2>/dev/null
tmux -L "$SOCK" kill-session -t 0 2>/dev/null
sleep 1
# 🚨 判定式は **conf に登録されている bind から取り出す**。テスト側に式をコピーすると、
# bind の条件を書き換えても (例: if-shell -F '1' で全ペインから C-v を奪う) テストが緑のまま
# 通る = 何も守らない (2026-09-04 に実際に踏んだ。mutation-verify-new-tests.md の
# 「同じ判定を 2 箇所で別実装していないか」)。
cond="$(tmux -L "$SOCK" list-keys -T root 2>/dev/null | grep ' C-v ' \
  | sed -e 's/.*if-shell -F "//' -e 's/" "run.*//')"
if [ -z "$cond" ]; then
  bad "判定不能: bind から判定式を取り出せない (conf の書式が変わった)"
else
  got_zsh="$(tmux -L "$SOCK" display -p -t zsh "$cond" 2>/dev/null)"
  got_other="$(tmux -L "$SOCK" display -p -t other "$cond" 2>/dev/null)"
  [ "$got_zsh" = 1 ] && ok "zsh のペインで判定式が真" || bad "zsh のペインで判定式が偽: [$got_zsh]"
  [ "$got_other" = 0 ] && ok "zsh 以外のペインで判定式が偽 (既存の C-v が残る)" \
    || bad "zsh 以外のペインで判定式が真になった: [$got_other] (既存の C-v を潰す)"
fi

# --- 3. true 側のコマンドが実際にペインへ流し込む ---
# bind と同じコマンド列を走らせる。🚨 コマンドを bind から引き写すのではなく、
# conf に書いてある文字列を取り出して使う (二重管理にすると conf を変えてもテストが気づかない)
cmd="$(tmux -L "$SOCK" list-keys -T root 2>/dev/null | grep ' C-v ' \
  | sed -e 's/.*run -b \\"//' -e 's/\\".*//')"
# list-keys は if-shell の入れ子ぶん quote を重ねて返す (`\\\$HOME` 等)。ここでは
# シェルへ渡し直すので、その escape だけ剥がす (conf 側の意味は変えない)
cmd="${cmd//\\/}"
if [ -z "$cmd" ]; then
  bad "判定不能: bind から true 側のコマンドを取り出せない (conf の書式が変わった)"
else
  # 🚨 -t を明示する。run-shell の対象ペインが paste-buffer の貼り先になるので、
  # 省くと「今アクティブなペイン」= 別セッションへ流れる (2026-09-04 に実測)。
  # 本番の bind ではキー押下したペインが対象になるので -t は要らない
  tmux -L "$SOCK" run -t zsh "$cmd" 2>/dev/null
  sleep 1
  if grep -q 'PASTED-FROM-CLIPBOARDあいう' <<< "$(tmux -L "$SOCK" capture-pane -p -t zsh 2>/dev/null)"; then
    ok "true 側のコマンドがクリップボードをペインへ流し込む"
  else
    bad "true 側のコマンドが流し込めていない: $(tmux -L "$SOCK" capture-pane -p -t zsh 2>/dev/null | tr -d '\n' | tail -c 60)"
  fi
  # トーストは display-message なので message-log に残る。**バイト数まで**見る:
  # 文言だけの一致だと、数え方が壊れて 0 バイトと出る変異を素通しする (issue 248 の確認 1 は
  # ⌘V との数字比較で判定するので、数字が本文の主張)。
  # 🚨 **client が 1 つも attach していないと display-message は `message:` 行を残さない**
  # (`command:` 行だけになる。実測 2026-09-05。ログ自体はサーバに 1 本)。
  # pty で attach したクライアントを 1 つ用意する
  # `</dev/null`: stdin が socket の環境 (エージェントのシェル) では script が
  # tcgetattr で落ちる (実測 2026-09-05)
  script -q /dev/null tmux -L "$SOCK" attach -t zsh >/dev/null 2>&1 </dev/null &
  client_pid=$!
  for _ in $(seq 60); do
    [ -n "$(tmux -L "$SOCK" list-clients -F '#{client_name}' 2>/dev/null)" ] && break
    sleep 0.05
  done
  tmux -L "$SOCK" run -t zsh "$cmd" 2>/dev/null
  for _ in $(seq 60); do
    grep -q 'message: 📋' <<< "$(tmux -L "$SOCK" show-messages -t zsh 2>/dev/null)" && break
    sleep 0.05
  done
  msg="$(tmux -L "$SOCK" show-messages -t zsh 2>/dev/null | grep 'message: 📋')"
  kill "$client_pid" 2>/dev/null
  # 🚨 区切りごと固定する。数字だけの部分一致は `130 バイト` のような桁違いも通す
  if grep -q ': 30 バイト' <<< "$msg"; then
    ok "貼ったバイト数のトーストが出る (21 + あいう 9 = 30 バイト / 24 文字)"
  else
    bad "バイト数のトーストが出ていない: $(tr -d '\n' <<< "$msg" | tail -c 80)"
  fi
fi

[ "$fail" = 0 ] && printf 'All ctrl-v paste tests passed successfully!\n'
exit "$fail"
