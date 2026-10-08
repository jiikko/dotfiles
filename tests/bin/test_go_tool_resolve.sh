#!/usr/bin/env bash
# bin/lib/go_tool.sh の go_tool_resolve (と、それを使う runtimeout_resolve / codex_events_resolve) が、bash と zsh の両方から
# この checkout の Go の道具を解決できることを固定する (issue 670)。
#
# なぜ zsh も見るか: zsh では path が PATH と結び付いた配列で、関数の中で local path と書くと PATH が空になる。
# bash では同じコードが動くので、bash だけで確かめると見落とす (実際に 1 度書いて、zsh だけ「ビルドできない」で落ちた)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/go-tool.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

want_rt="$ROOT_DIR/src/runtimeout/runtimeout"
want_ce="$ROOT_DIR/src/codexevents/codex-events"
for sh in bash zsh; do
  # 🚨 stdout だけを照合する。バイナリがまだ無いと (CI の新品の checkout)、最初の解決でビルドが走り、ラッパーが stderr に
  #    「building...」を出す。2>&1 で混ぜると、解決は正しいのに照合が落ちる (手元はビルド済みなので再現しない。2026-10-09 CI で発覚)
  got="$(env -u RUNTIMEOUT -u CODEX_EVENTS "$sh" -c '. "$1/bin/lib/runtimeout.sh" && . "$1/bin/lib/codex_events.sh" && runtimeout_resolve "$1" && codex_events_resolve "$1" && printf "%s\n%s" "$RUNTIMEOUT" "$CODEX_EVENTS"' _ "$ROOT_DIR" 2>"$WORK/$sh.err")" || true
  if [ "$got" = "$want_rt"$'\n'"$want_ce" ]; then
    ok "$sh: runtimeout と codex-events をこの checkout のバイナリに解決する"
  else
    fail "$sh: 解決できない: $got (stderr: $(cat "$WORK/$sh.err"))"
  fi
done

# 既にある値は、この checkout のバイナリを指すときだけ採る (別の checkout の版・偽物で、変更したコードを素通りしない)
got="$(CODEX_EVENTS=/usr/bin/true bash -c '. "$1/bin/lib/codex_events.sh" && codex_events_resolve "$1" && printf %s "$CODEX_EVENTS"' _ "$ROOT_DIR")"
if [ "$got" = "$want_ce" ]; then ok "別のバイナリを指す CODEX_EVENTS を採らない"; else fail "CODEX_EVENTS=/usr/bin/true をそのまま採った: $got"; fi
# 解決した値は子へ export される
got="$(env -u CODEX_EVENTS bash -c '. "$1/bin/lib/codex_events.sh" && codex_events_resolve "$1" && bash -c "printf %s \"\$CODEX_EVENTS\""' _ "$ROOT_DIR")"
if [ "$got" = "$want_ce" ]; then ok "解決した値を export する"; else fail "子に CODEX_EVENTS が渡らない: $got"; fi

# ラッパーが無い root では rc=1 で止まり、何が無いかを言う (黙って空の値で進まない)
mkdir -p "$WORK/noroot/bin/lib"
cp "$ROOT_DIR/bin/lib/go_tool.sh" "$ROOT_DIR/bin/lib/codex_events.sh" "$WORK/noroot/bin/lib/"
rc=0
out="$(env -u CODEX_EVENTS bash -c '. "$1/bin/lib/codex_events.sh" && codex_events_resolve "$1"' _ "$WORK/noroot" 2>&1)" || rc=$?
if [ "$rc" -eq 1 ] && grep -q 'codex-events をビルド・起動できない' <<<"$out"; then
  ok "ラッパーが無いと rc=1 で止まる"
else
  fail "ラッパーが無いのに rc=$rc: $out"
fi

if (( fails > 0 )); then
  exit 1
fi
