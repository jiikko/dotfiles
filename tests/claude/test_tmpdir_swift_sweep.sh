#!/usr/bin/env bash
# _claude/hooks/tmpdir-swift-sweep.sh (SessionStart で 1 日 1 回、裏で tmpclean sweep を走らせる) のテスト。
# tmpclean は fake (呼ばれた回数を数え、rc と「止まっている」を外から決める)。排他は本物の bin/lockman で見る。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/tmpdir-swift-sweep.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
# shellcheck source=tests/lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh"

FAIL_COUNT=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }

[ -x "$HOOK" ] || { ng "フックが無いか実行権限が無い: $HOOK"; exit 1; }
if jq -e '[.hooks.SessionStart[]?.hooks[]?.command // ""] | map(test("tmpdir-swift-sweep")) | any' "$SETTINGS" >/dev/null; then
  ok "settings.json の SessionStart に配線されている"
else
  ng "SessionStart に配線されていない"
fi

# lockman を先にビルドしておく (初回のビルドが case 1 の待ちの上限に食い込むと、負荷の高い CI で偽の赤になる)
"$ROOT_DIR/bin/lockman" --help > /dev/null 2>&1

work=$(mktemp -d)
# 止められたときも、止めてある fake を放してから消す (放さないと fake が runtimeout の上限まで回り続ける)
trap 'touch "$work"/*/release 2>/dev/null; rm -rf "$work"' EXIT

# newcase は使い捨ての state と fake の tmpclean を作り、環境変数を設定する。fake は呼ばれるたびに calls へ 1 行足し、
# $work/<case>/hold があれば release ができるまで止まり、rc ファイルの値で終わる。
newcase() {
  C="$work/$1"
  mkdir -p "$C/state"
  cat > "$C/tmpclean" <<EOF
#!/bin/bash
echo "\$*" >> "$C/calls"
if [ -e "$C/hold" ]; then
  touch "$C/started"
  while [ ! -e "$C/release" ]; do sleep 0.05; done # sleep-ok: tick: fake が外から放されるまで待つ刻み
fi
printf '{\n  "removed": [\n    "/x/TemporaryDirectory.aaaaaa"\n  ]\n}\n'
exit \$(cat "$C/rc" 2>/dev/null || echo 0)
EOF
  chmod +x "$C/tmpclean"
  export TMPDIR_SWEEP_STATE="$C/state" TMPDIR_SWEEP_TMPCLEAN="$C/tmpclean" TMPDIR_SWEEP_RUNTIMEOUT="$RUNTIMEOUT"
  export TMPDIR_SWEEP_LOCKMAN="$ROOT_DIR/bin/lockman"
}
ncalls() { [ -e "$C/calls" ] && wc -l < "$C/calls" | tr -d ' ' || echo 0; }
has_stamp() { [ -e "$C/state/stamp" ]; }
has_started() { [ -e "$C/started" ]; }
log_has() { grep -q "$1" "$C/state/sweep.log" 2>/dev/null; }
hook() { echo '{}' | "$RUNTIMEOUT" 10 "$HOOK"; }

# 1. 初回: hook はすぐ返り、裏で sweep が 1 回走って印と記録が残る
newcase first
hook; rc=$?
[ "$rc" -eq 0 ] && ok "hook は rc=0 で返る" || ng "hook rc=$rc"
if tt_wait_until has_stamp && tt_wait_until log_has 'rc=0 removed=1'; then
  ok "裏で sweep が走り、印と記録 (rc=0 removed=1) が残る"
else
  ng "印か記録が無い: $(cat "$C/state/sweep.log" 2>/dev/null)"
fi
[ "$(ncalls)" = 1 ] && grep -qx 'sweep -json' "$C/calls" && ok "tmpclean sweep -json を 1 回呼んだ" || ng "calls=$(cat "$C/calls" 2>/dev/null)"

# 2. 同じ日の 2 回目: 何も起こさない。hook の外の判定は lockman も起こさない (毎セッション裏でプロセスを起こさない)。
#    --run を直接呼んでも、lock の中の判定で何もしない
printf '#!/bin/sh\ntouch "%s/lockman-called"\n' "$C" > "$C/fake-lockman"
chmod +x "$C/fake-lockman"
TMPDIR_SWEEP_LOCKMAN="$C/fake-lockman" hook
sleep 1 # sleep-ok: negative: 「起こさない」ことを見る。待つべき成立条件が無い (起こしていれば 1 秒で印のファイルができる)
[ ! -e "$C/lockman-called" ] && [ "$(ncalls)" = 1 ] && ok "印が新しければ hook は lockman を起こさない" || ng "calls=$(ncalls) lockman=$([ -e "$C/lockman-called" ] && echo called)"
"$HOOK" --run < /dev/null; rc=$?
[ "$rc" -eq 0 ] && [ "$(ncalls)" = 1 ] && ok "印が新しければ --run は何もしない" || ng "rc=$rc calls=$(ncalls)"

# 3. 印が古ければもう一度走る
touch -t 202001010000 "$C/state/stamp"
hook
# sweep の記録の行だけを数える (sweep.log には lockman の出力 (初回のビルドの行など) も入る)
log_lines() { local n; n=$(grep -c ' rc=' "$C/state/sweep.log" 2>/dev/null); [ "${n:-0}" -ge 2 ]; }
tt_wait_until log_lines && [ "$(ncalls)" = 2 ] && ok "印が古ければもう一度走る" || ng "calls=$(ncalls) log=$(cat "$C/state/sweep.log" 2>/dev/null)"

# 3b. 記録を切り詰めても、既に開いている書き手の行は残る (同じファイルのまま切り詰める)
for i in $(seq 4000); do echo "old line $i ................................................................"; done >> "$C/state/sweep.log"
touch -t 202001010000 "$C/state/stamp"
exec 7>> "$C/state/sweep.log"
"$HOOK" --run < /dev/null
echo "written by an open writer" >&7
exec 7>&-
small() { [ "$(wc -c < "$C/state/sweep.log")" -lt 262144 ]; }
if small && grep -q 'written by an open writer' "$C/state/sweep.log" && [ ! -e "$C/state/sweep.log.tmp" ]; then
  ok "大きくなった記録を切り詰め、開いていた書き手の行も残る"
else
  ng "切り詰め: size=$(wc -c < "$C/state/sweep.log") tail=$(tail -1 "$C/state/sweep.log")"
fi

# 4. rc=3 (実行環境の失敗) は印を付けない (次のセッションで再試行)。rc=2 (判定不能で止めた) は付ける
for want in 3:no 2:yes 1:yes; do
  newcase "rc${want%%:*}"
  echo "${want%%:*}" > "$C/rc"
  "$HOOK" --run < /dev/null
  if has_stamp; then got=yes; else got=no; fi
  [ "$got" = "${want##*:}" ] && ok "rc=${want%%:*} で印=${want##*:}" || ng "rc=${want%%:*} で印=$got"
done

# 5. 排他: 1 本目が走っている間に起こした 2 本目は lock を取れず、tmpclean を呼ばない。1 本目が終わった後に印が付く
#    あわせて、裏の sweep が止まっている間に hook の stdout が閉じ、中身が空であること (Claude Code は hook の stdout の終わりを待ち、
#    中身をコンテキストへ入れる。裏の子が stdout を握ると、起動が sweep の終わりまで待たされる)
newcase lock
touch "$C/hold"
( out=$(echo '{}' | "$HOOK" 2>&1); printf '%s' "$out" > "$C/hook.out"; touch "$C/hook.done" ) & # stderr を握る退行もここで見る
hook_done() { [ -e "$C/hook.done" ]; }
tt_wait_until has_started || ng "1 本目が始まらない: $(cat "$C/state/sweep.log" 2>/dev/null)"
if tt_wait_until hook_done && [ ! -e "$C/release" ]; then
  ok "裏の sweep が止まっている間に hook の stdout が閉じる"
else
  ng "hook の stdout が裏の sweep の終わりまで閉じない"
fi
[ -e "$C/hook.out" ] && [ ! -s "$C/hook.out" ] && ok "hook は stdout にも stderr にも何も出さない" || ng "hook の出力: $(cat "$C/hook.out" 2>/dev/null)"
hook
two_tries() { local n; n=$(grep -c 'tmpdir-swift-sweep\|取得\|保持' "$C/state/sweep.log" 2>/dev/null); [ "${n:-0}" -ge 1 ]; }
if tt_wait_until two_tries; then
  ok "2 本目は lock を取れなかった (lockman の出力が記録に残る)"
else
  ng "2 本目の痕跡が無い: $(cat "$C/state/sweep.log" 2>/dev/null)"
fi
[ "$(ncalls)" = 1 ] && ok "走っている間に起こした 2 本目は tmpclean を呼ばない" || ng "calls=$(ncalls)"
touch "$C/release"
tt_wait_until has_stamp && [ "$(ncalls)" = 1 ] && ok "1 本目が終わって印が付き、呼ばれたのは 1 回だけ" || ng "calls=$(ncalls) stamp=$(has_stamp && echo y)"

if [ "$FAIL_COUNT" -gt 0 ]; then
  printf '%d 件失敗\n' "$FAIL_COUNT"
  exit 1
fi
echo "all passed"
