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
#   ⚠️ shim は実体を**絶対パスで**解決してから exec する (相対名だと PATH 先頭の自分自身に
#      解決して無限再帰する。~/.claude/rules/path-shim-must-resolve-real-binary.md)。
#      解決結果が shim 自身なら即座に失敗させる。
#
# 対象は browseModel を作る経路のテストに絞る (全体を回すと 30 秒かかり、同じ go test を
# make test が二重に回すことになる)。CI の src_glogx.yml は全体を回すので、そちらが本番。
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
for cmd in brew xcrun pgrep launchctl sfltool; do
  real=$(command -v "$cmd" 2>/dev/null || true)
  [ -n "$real" ] || continue
  case "$real" in
    "$SHIM"/*) echo "✗ shim が自分自身に解決した: $real" >&2; exit 1 ;;
  esac
  cat > "$SHIM/$cmd" <<SHIMEOF
#!/bin/sh
echo "\$(basename "\$0") \$*" >> "$LOG"
exec "$real" "\$@"
SHIMEOF
  chmod +x "$SHIM/$cmd"
done

if [ ! -e "$SHIM/brew" ] && [ ! -e "$SHIM/xcrun" ]; then
  echo "✗ shim を 1 つも作れなかった (brew / xcrun がどちらも無い)" >&2
  exit 1
fi

cd "$ROOT/src/glogx"
# browseModel を作る経路 (= doctor の seam が要る経路) に絞る
if ! PATH="$SHIM:$PATH" go test -count=1 \
     -run 'TestUpdateKeys|TestDoctor|TestRestartPrompt|TestBrowse|TestDelete' . > "$TMP/test.out" 2>&1; then
  echo "✗ go test が落ちた (このテストの前提が崩れている):" >&2
  tail -20 "$TMP/test.out" >&2
  exit 1
fi

calls=$(wc -l < "$LOG" | tr -d ' ')
if [ "$calls" != "0" ]; then
  echo "✗ テストが実マシンのコマンドを $calls 回叩いた (issue 216):" >&2
  sort "$LOG" | uniq -c | sort -rn | head -10 >&2
  echo "  newTestBrowse の installInertDoctor か、そのテスト固有の seam を確かめること" >&2
  exit 1
fi

echo "OK: glogx のテストは実マシンのコマンドを叩かない (shim $(ls "$SHIM" | wc -l | tr -d ' ') 本を PATH 先頭に置いて確認)"
