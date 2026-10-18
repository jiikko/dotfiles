#!/usr/bin/env bash
# scripts/check_unused_excluding_tests.sh が staticcheck のキャッシュを checkout ごとに分けることを固定する。
# 守るもの: STATICCHECK_CACHE を渡さなければ、その checkout の git dir の下 (worktree なら .git/worktrees/<名前>/) の、checkout の場所ごとの dir を使う / 継承した GIT_DIR に引きずられない /
# 渡したらそれを使う。staticcheck は PATH の先頭に置いた偽物 (受け取った STATICCHECK_CACHE を書き出して何も報告しない) にする。
# 偽物は何も報告しないので、検査そのものは allowlist の stale で赤になる。ここでは rc を見ず、渡った値だけを見る。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_unused_excluding_tests.sh"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-unused-cache.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"
cat > "$WORK/bin/staticcheck" <<'STUB'
#!/bin/sh
[ "${1:-}" = --version ] && { echo 'staticcheck 2026.1 (v0.7.0)'; exit 0; }
printf '%s\n' "${STATICCHECK_CACHE:-<unset>}" >> "$STUB_LOG"
exit 0
STUB
chmod +x "$WORK/bin/staticcheck"

# 期待する置き場は実装と別の手段で出す: .git がファイル (worktree) なら gitdir: の行、ディレクトリ (本体) ならそれ。
# 実装と同じ rev-parse で出すと、両方が同じ形で誤っていても一致する
if [ -f "$ROOT_DIR/.git" ]; then
  gd=$(sed -n 's/^gitdir: //p' "$ROOT_DIR/.git"); case "$gd" in /*) ;; *) gd="$ROOT_DIR/$gd" ;; esac
  gd=$(cd "$gd" && pwd -P)
else
  gd=$(cd "$ROOT_DIR/.git" && pwd -P)
fi
want_parent="$gd/staticcheck-cache"

: > "$WORK/log1"
( unset STATICCHECK_CACHE; STUB_LOG="$WORK/log1" PATH="$WORK/bin:$PATH" "$CHECK" >/dev/null 2>&1 )
n=$(wc -l < "$WORK/log1" | tr -d ' ')
got=$(sort -u "$WORK/log1")
got_parent=$(dirname "$got"); got_parent=$(cd "$got_parent" 2>/dev/null && pwd -P || printf '%s' "$got_parent")
if [ "$n" -gt 0 ] && [ "$(printf '%s\n' "$got" | wc -l | tr -d ' ')" -eq 1 ] && [ "$got_parent" = "$want_parent" ] && [[ "$(basename "$got")" =~ ^[0-9a-f]{12}$ ]]; then
  ok "渡さなければ git dir の下の、checkout の場所ごとの置き場を使う ($n モジュール)"
else
  bad "既定の置き場が git dir の下の場所ごとの dir でない (呼ばれた回数 $n / 期待の親 $want_parent): $(printf '%s' "$got" | head -3)"
fi

# 継承した GIT_DIR が別の git dir を指していても、この checkout の置き場を使う
: > "$WORK/log3"
( unset STATICCHECK_CACHE; GIT_DIR="$WORK/elsewhere.git" STUB_LOG="$WORK/log3" PATH="$WORK/bin:$PATH" "$CHECK" >/dev/null 2>&1 )
if [ -s "$WORK/log3" ] && [ "$(sort -u "$WORK/log3")" = "$got" ]; then
  ok '継承した GIT_DIR に引きずられない'
else
  bad "継承した GIT_DIR で置き場が変わる: $(sort -u "$WORK/log3" | head -3)"
fi

: > "$WORK/log2"
( STATICCHECK_CACHE="$WORK/given" STUB_LOG="$WORK/log2" PATH="$WORK/bin:$PATH" "$CHECK" >/dev/null 2>&1 )
if [ -s "$WORK/log2" ] && [ "$(sort -u "$WORK/log2")" = "$WORK/given" ]; then
  ok '渡した STATICCHECK_CACHE を使う'
else
  bad "渡した置き場を使わない: $(sort -u "$WORK/log2" | head -3)"
fi

[ "$fails" -eq 0 ] || { printf '✗ %s 件失敗\n' "$fails"; exit 1; }
printf '✓ 到達不能の検査は staticcheck のキャッシュを checkout ごとに分ける\n'
