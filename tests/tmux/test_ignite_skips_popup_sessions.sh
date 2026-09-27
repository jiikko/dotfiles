#!/usr/bin/env bash
# scripts/tmux_ignite_current.sh が popup の session (scratch / claude-fork) では点火しないことのテスト (issue 567)。
#
# 固定する不変条件:
#   - 切替先が popup の session ならフレームを 1 つも出さない (popup は 1 フレームごとに全体を描き直すため)
#   - 普通の session と、session 名を渡さない旧来の呼び方では従来どおり点火する
#   - _tmux.conf の 2 つの hook が session 名を渡している (渡し忘れると黙って全 session で点火に戻る)
#
# TT_IGNITE_DRYRUN=1 でフレーム色を print させるだけで、tmux サーバは立てない。
# スクリプトが読む `tmux show` が本番サーバへ届かないよう、TMUX を外し TMUX_TMPDIR を空の一時 dir にする。
set -uo pipefail
unset CDPATH
unset TMUX TMUX_PANE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_ignite_current.sh"
fails=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

TMUX_TMPDIR="$(mktemp -d)"
export TMUX_TMPDIR
trap 'rm -rf "$TMUX_TMPDIR"' EXIT

frames() { TT_IGNITE_DRYRUN=1 sh "$SCRIPT" "$@" 2>/dev/null | grep -c '^colour'; }

for s in scratch claude-fork; do
  n=$(frames colour201 "$s")
  [ "$n" -eq 0 ] && ok "$s ではフレームを出さない" || ng "$s でフレームが $n 個出た"
done
for s in main scratch2 myscratch; do
  n=$(frames colour201 "$s")
  [ "$n" -gt 0 ] && ok "$s では点火する ($n フレーム)" || ng "$s で点火しなかった"
done
n=$(frames colour201)
[ "$n" -gt 0 ] && ok "session 名なしの呼び方でも点火する ($n フレーム)" || ng "session 名なしで点火しなかった"

# コメント行を除いた呼び出し行の全部が session 名を渡すこと (本数は固定しない)
calls=$(grep -v '^[[:space:]]*#' "$ROOT_DIR/_tmux.conf" | grep -c 'tmux_ignite_current\.sh')
wired=$(grep -v '^[[:space:]]*#' "$ROOT_DIR/_tmux.conf" | grep -c 'tmux_ignite_current\.sh .* #{q:session_name}'"'")
[ "$calls" -gt 0 ] && [ "$wired" -eq "$calls" ] && ok "_tmux.conf の呼び出し $calls 行すべてが session 名を渡す" \
  || ng "_tmux.conf: 呼び出し $calls 行中 session 名を渡すのは $wired 行"

[ "$fails" -eq 0 ] || exit 1
