#!/usr/bin/env bash
# _claude/hooks/deny-bare-tmux-kill.sh (PreToolUse(Bash) の破壊的 tmux コマンド deny) の
# unit テスト。合成した hook JSON を stdin で流し、deny / allow の判定を pin する。
#
# なぜ: このフックは 2026-07-30 の本番サーバ誤殺 (bare `tmux kill-server` が $TMUX 優先で
# 本番直撃) の再発を harness 側で止める最後の砦。判定式の退行 = 事故の再発経路が開く。
# 規範: _claude/rules/tmux-probe-requires-socket-isolation.md
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/deny-bare-tmux-kill.sh"

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq が無い環境 (フック自体も jq 不在時は fail-open で無効)"
  exit 0
fi

decision() {  # $1=コマンド文字列 → "deny" or "allow"
  jq -n --arg c "$1" '{tool_name:"Bash",tool_input:{command:$c}}' \
    | "$HOOK" | jq -r '.hookSpecificOutput.permissionDecision // empty' | grep . || echo allow
}

expect() {  # $1=期待(deny/allow) $2=説明 $3=コマンド
  local got
  got="$(decision "$3")"
  if [ "$got" != "$1" ]; then
    printf '✗ %s\n  期待=%s 実際=%s\n  cmd: %s\n' "$2" "$1" "$got" "$3"
    exit 1
  fi
  printf '✓ %s (%s)\n' "$2" "$1"
}

# --- deny すべきもの (本番直撃の形) -----------------------------------------------------
expect deny  "bare kill-server" 'tmux kill-server'
expect deny  "bare kill-session" 'tmux kill-session -t foo'
expect deny  "2026-07-30 の事故コマンドそのもの" \
  'export TMUX_TMPDIR=$(mktemp -d); tmux -f /dev/null new-session -d -s probe; tmux kill-server 2>/dev/null; rm -rf $TMUX_TMPDIR'
expect deny  "TMUX_TMPDIR だけの隔離 (不十分)" 'env TMUX_TMPDIR=$d tmux kill-server'
expect deny  "複合コマンドの後半だけ bare" 'tmux -L lab new-session -d && tmux kill-server'
expect deny  "tmux 内コマンド連結 \\; 経由の bare" 'tmux new-session -d \; kill-server'
expect deny  "pkill -f tmux (全サーバ無差別)" 'pkill -f tmux'
expect deny  "pkill -x tmux" 'pkill -x tmux'
expect deny  "killall tmux" 'killall tmux'

# --- 2026-07-30 の敵対的レビューで実証されたバイパス (全て allow だった) --------------
expect deny  "フルパス起動" '/opt/homebrew/bin/tmux kill-server'
expect deny  "フルパスの事故コマンド列" \
  'export TMUX_TMPDIR=$(mktemp -d); /opt/homebrew/bin/tmux -f /dev/null new-session -d -s probe; /opt/homebrew/bin/tmux kill-server'
expect deny  "bash -c 経由 (引用符が直前)" "bash -c 'tmux kill-server'"
expect deny  "sh -lc 経由 (二重引用符が直前)" 'sh -lc "tmux kill-session -t probe"'
expect deny  "subcommand 略記 kill-serve (tmux は前方一致を受理)" 'tmux kill-serve'
expect deny  "subcommand 略記 kill-ser" 'tmux kill-ser'
expect deny  "subcommand 略記 kill-sessio" 'tmux kill-sessio -t x'
expect deny  "行継続で分断された形" 'tmux \
  kill-server'
expect deny  "コメント内の -L で免除されない" 'tmux kill-server  # -L probe の後片付け'
expect deny  "kill トークンより後ろの -L では免除しない" 'tmux kill-session -t x; sort -S 1G /dev/null'

# --- allow すべきもの (ソケット明示 / 無関係) -------------------------------------------
expect allow "-L 明示の kill-server" 'tmux -L lab kill-server'
expect allow "-S 明示の kill-server" 'tmux -S /path/sock kill-server'
expect allow "-L 明示の複合" 'tmux -L lab new-session -d && tmux -L lab kill-server'
expect allow "-L 明示の \\; 連結" 'tmux -L lab new-session -d \; kill-server'
expect allow "読み取り系 tmux" 'tmux ls'
expect allow "kill-window は対象外" 'tmux kill-window -t 1'
expect allow "pid 指定の kill" 'kill -TERM 1234'
expect allow "無関係な pkill" 'pkill -f myapp'
expect allow "文字列に tmux を含む git" 'git commit -m "fix tmux config"'
expect allow "make test" 'make test'

printf '\nAll deny-bare-tmux-kill tests passed successfully!\n'
