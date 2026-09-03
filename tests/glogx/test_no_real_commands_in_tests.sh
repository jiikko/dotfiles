#!/bin/sh
# test_no_real_commands_in_tests.sh — glogx の Go テストが**実マシンのコマンドを叩かない**ことの
# 回帰テスト (issue 216)。
#
# なぜ (実測 2026-09-03):
#   glogx のテストは doctor の走査口に seam を差していない経路があり、キーを 1 つ押しただけで
#   実 `xcrun simctl` / `brew doctor` / `pgrep` が走っていた (全体で 29 回)。しかも後始末が
#   cancel だけで join しないため、走査 goroutine が次のテストへ跨いで package 変数を踏み、
#   issue 214 のデータ競合として現れた。**「手元では緑」になる**のも同じ経路で、開発機に
#   snapshot があると走査が早期 return するため (CI の新品 cache では必ず漏れる)。
#
# 検出する形: PATH の先頭に記録つき shim を置いて go test を回し、**1 度も呼ばれない**ことを見る。
#   ⚠️ shim は実体を exec **しない** (記録して即 0 で返す)。期待値が 0 回なので実体は本来
#      呼ばれず、もし退行したときに `brew cleanup` / `xcrun simctl runtime delete` を
#      **検出する前に実マシンで走らせてしまう**のを避ける。
#   ⚠️ `go` は shim にしない (このスクリプト自身が go test を起こすので自分を止める)。
#      削除経路の `cli:go clean -modcache` は `disk/delete.go` の allowDestructive が
#      テスト中は fail-closed で止めるので、そちらが正本の防波堤。
#
# スコープ: 既定は browseModel を作る経路に絞る (make test から回るので速さが要る)。
#   `GLOGX_SHIM_SCOPE=all` で全テスト。**CI (src_glogx.yml の no-real-commands job) は all**。
#   ⚠️ 絞り込みは「テスト名が変わったら空振りする」ので、**PASS の件数に下限**を置いて
#      「そもそも走っていない」を緑にしない (verify-execution-not-just-exit-code.md)。
set -eu
unset CDPATH

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

# Tests workflow の runner には Go が無い (glogx の Go テストは src_glogx.yml が回す)。
# ⚠️ skip は exit 77 (runner は 0 を [ok] と数えるので、丸ごと skip で 0 を返さない)
if ! command -v go >/dev/null 2>&1; then
  echo "[test_no_real_commands_in_tests] skipped: go が無い" >&2
  exit 77
fi

SHIM="$TMP/shim"
mkdir -p "$SHIM"
LOG="$TMP/calls.log"
: > "$LOG"
# doctor が触りうる外部コマンド。**実体があるかに関わらず** shim を置く
# (実体が無いマシンでも「呼ぼうとした」ことは検出したい)
for cmd in brew xcrun pgrep launchctl du; do
  cat > "$SHIM/$cmd" <<SHIMEOF
#!/bin/sh
echo "\$(basename "\$0") \$*" >> "$LOG"
exit 0
SHIMEOF
  chmod +x "$SHIM/$cmd"
done

cd "$ROOT/src/glogx" || exit 1
scope="${GLOGX_SHIM_SCOPE:-subset}"
if [ "$scope" = all ]; then
  min_pass=300
  set -- -count=1 -v .
else
  min_pass=40
  set -- -count=1 -v -run 'TestUpdateKeys|TestDoctor|TestRestartPrompt|TestBrowse|TestDelete' .
fi
if ! PATH="$SHIM:$PATH" go test "$@" > "$TMP/test.out" 2>&1; then
  echo "✗ go test が落ちた (このテストの前提が崩れている):" >&2
  tail -20 "$TMP/test.out" >&2
  exit 1
fi

# 🚨 「走ったこと」を数える。-run のパターンに 1 つも当たらないと go test は
#    `ok ... [no tests to run]` で rc=0 を返し、呼び出し 0 回と区別できない
passed=$(grep -c '^--- PASS:\|^    --- PASS:' "$TMP/test.out" || true)
if [ "${passed:-0}" -lt "$min_pass" ]; then
  echo "✗ PASS が ${passed} 件しかない (下限 ${min_pass}。テスト名が変わって空振りしている可能性)" >&2
  grep -c 'no tests to run' "$TMP/test.out" >/dev/null 2>&1 && echo "  → [no tests to run] が出ている" >&2
  exit 1
fi
calls=$(wc -l < "$LOG" | tr -d ' ')
if [ "$calls" != "0" ]; then
  echo "✗ テストが実マシンのコマンドを ${calls} 回叩いた (issue 216):" >&2
  sort "$LOG" | uniq -c | sort -rn | head -10 >&2
  echo "  newTestBrowse の installInertDoctor か、そのテスト固有の seam を確かめること" >&2
  exit 1
fi

echo "OK: glogx のテストは実マシンのコマンドを叩かない (scope=$scope / PASS $passed 件 / shim $(ls "$SHIM" | wc -l | tr -d ' ') 本)"
