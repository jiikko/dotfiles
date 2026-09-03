#!/usr/bin/env bash
# _tmux.conf の C-v bind (issue 138) を隔離サーバで検証する。
# 固定する不変条件:
#   - root テーブルに C-v が 1 つだけ登録され、-N の説明が付いている (キーガイドに出る)
#   - 判定式 #{==:#{pane_current_command},zsh} が zsh のペインでだけ真になる
#     (= 実際の分岐先を決める値。nvim / less / cat が前面なら偽 = 既存の C-v が残る)
#   - true 側のコマンド (pbpaste | load-buffer && paste-buffer) が実際にペインへ流し込む
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
cat > "$BIN/pbpaste" <<'STUB'
#!/bin/sh
printf 'PASTED-FROM-CLIPBOARD'
STUB
chmod +x "$BIN/pbpaste"
export PATH="$BIN:$PATH"

tmux -L "$SOCK" -f "$CONF" new-session -d -x 120 -y 30 "sh -c 'sleep 30'" 2>/dev/null

# --- 1. bind の登録と説明 ---
keys="$(tmux -L "$SOCK" list-keys -T root 2>/dev/null | grep -c ' C-v ')"
[ "$keys" = 1 ] && ok "root テーブルに C-v が 1 つ登録されている" || bad "C-v の登録数が 1 でない: $keys"
tmux -L "$SOCK" list-keys -N -T root 2>/dev/null | grep -q 'C-v.*クリップボード' \
  && ok "-N の説明が付いている (キーガイドに出る)" || bad "-N の説明が無い"

# --- 2. 判定式がペインの前面プロセスで正しく分かれる ---
tmux -L "$SOCK" kill-session -t 0 2>/dev/null
tmux -L "$SOCK" new-session -d -s zsh -x 120 -y 30 "zsh -f" 2>/dev/null
tmux -L "$SOCK" new-session -d -s other -x 120 -y 30 "sh -c 'sleep 30'" 2>/dev/null
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
if [ -z "$cmd" ]; then
  bad "判定不能: bind から true 側のコマンドを取り出せない (conf の書式が変わった)"
else
  # 🚨 -t を明示する。run-shell の対象ペインが paste-buffer の貼り先になるので、
  # 省くと「今アクティブなペイン」= 別セッションへ流れる (2026-09-04 に実測)。
  # 本番の bind ではキー押下したペインが対象になるので -t は要らない
  tmux -L "$SOCK" run -t zsh "$cmd" 2>/dev/null
  sleep 1
  if tmux -L "$SOCK" capture-pane -p -t zsh 2>/dev/null | grep -q 'PASTED-FROM-CLIPBOARD'; then
    ok "true 側のコマンドがクリップボードをペインへ流し込む"
  else
    bad "true 側のコマンドが流し込めていない: $(tmux -L "$SOCK" capture-pane -p -t zsh 2>/dev/null | tr -d '\n' | tail -c 60)"
  fi
fi

[ "$fail" = 0 ] && printf 'All ctrl-v paste tests passed successfully!\n'
exit "$fail"
