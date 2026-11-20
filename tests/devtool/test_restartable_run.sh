#!/bin/bash
#
# devtool/bin/restartable-run の self-test。偽の go (go install で偽の restartable を置く) と偽の restartable (引数を記録する) で、
#   - 印が無い・違うバージョンなら go install して印を書き、同じなら go install しない
#   - go が無い・go install が落ちたら restartable を起こさずに rc 1
#   - 残りの引数を数・順序・区切りを変えずに restartable へ渡す (先頭の -- は外す)
#   - --bin-dir が無ければ rc 2
# を確かめる。各 app の bin/dev-fg-loop の配線は各 app の自己テストが見る。
set -uo pipefail
unset CDPATH  # export された CDPATH の下では、相対パスの cd が解決先を stdout に出し $(cd … && pwd) を壊す

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
TOOL="$ROOT_DIR/devtool/bin/restartable-run"
[ -x "$TOOL" ] || { echo "NG: devtool/bin/restartable-run が無い"; exit 1; }
VERSION="$(sed -n 's/^readonly RESTARTABLE_VERSION="\(.*\)"$/\1/p' "$TOOL")"
[ -n "$VERSION" ] || { echo "NG: RESTARTABLE_VERSION を読めない"; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fails=0
ok() { echo "  ok   - $1"; }
ng() { echo "  NG   - $1"; fails=$((fails + 1)); }

mkdir -p "$work/fakebin"
cat > "$work/fakebin/go" <<'FAKE'
#!/bin/bash
printf 'GOBIN=%s arg=%s\n' "${GOBIN:-}" "$*" >> "$FAKE_GO_LOG"
[ "${FAKE_GO_RC:-0}" -eq 0 ] || exit "$FAKE_GO_RC"
cat > "$GOBIN/restartable" <<'R'
#!/bin/bash
printf 'ARGC=%s\n' "$#" >> "$FAKE_RUN_LOG"
printf 'ARG=%s\n' "$@" >> "$FAKE_RUN_LOG"
R
chmod +x "$GOBIN/restartable"
FAKE
chmod +x "$work/fakebin/go"
export FAKE_GO_LOG="$work/go.log" FAKE_RUN_LOG="$work/run.log"
bin="$work/bin"
go_calls() { [ -f "$FAKE_GO_LOG" ] && wc -l < "$FAKE_GO_LOG" | tr -d ' ' || echo 0; }

echo "[test-restartable-run]"
PATH="$work/fakebin:$PATH" "$TOOL" --bin-dir "$bin" -- --build 'make x' -- run 'a b' > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 0 ] && [ "$(go_calls)" = 1 ] && grep -qxF "GOBIN=$bin arg=install github.com/jiikko/dotfiles/src/restartable@$VERSION" "$FAKE_GO_LOG" \
  && [ "$(cat "$bin/restartable.version")" = "$VERSION" ]; then
  ok "印が無ければ決めたバージョンを go install して印を書く"
else ng "初回の install (rc=$rc): $(cat "$work/out") / go: $(cat "$FAKE_GO_LOG" 2>/dev/null)"; fi
if [ "$(printf 'ARGC=5\nARG=--build\nARG=make x\nARG=--\nARG=run\nARG=a b')" = "$(cat "$FAKE_RUN_LOG")" ]; then
  ok "先頭の -- を外し、残りの引数を数・順序・区切りを変えずに渡す"
else ng "restartable に渡った引数: $(cat "$FAKE_RUN_LOG")"; fi

rm -f "$FAKE_GO_LOG" "$FAKE_RUN_LOG"
PATH="$work/fakebin:$PATH" "$TOOL" --bin-dir "$bin" status > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 0 ] && [ "$(go_calls)" = 0 ] && grep -qxF 'ARG=status' "$FAKE_RUN_LOG"; then ok "同じバージョンの印なら go install しない"
else ng "2 回目 (rc=$rc, go=$(go_calls)): $(cat "$work/out")"; fi

rm -f "$FAKE_GO_LOG" "$FAKE_RUN_LOG"
printf 'old\n' > "$bin/restartable.version"
PATH="$work/fakebin:$PATH" "$TOOL" --bin-dir "$bin" status > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 0 ] && [ "$(go_calls)" = 1 ] && [ "$(cat "$bin/restartable.version")" = "$VERSION" ]; then ok "印のバージョンが違えば go install し直す"
else ng "印の違い (rc=$rc, go=$(go_calls))"; fi

rm -f "$FAKE_GO_LOG" "$FAKE_RUN_LOG"
printf 'old\n' > "$bin/restartable.version"
# /usr/bin と /bin だけの PATH に go が無いことを先に確かめる (在ると、この検査は go のある経路を通って緑になる)
if PATH=/usr/bin:/bin command -v go >/dev/null 2>&1; then ng "前提: /usr/bin か /bin に go がある (go が無い経路を作れない)"; fi
PATH=/usr/bin:/bin "$TOOL" --bin-dir "$bin" status > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 1 ] && grep -qF 'Go をインストールしてから' "$work/out" && [ ! -f "$FAKE_RUN_LOG" ]; then ok "go が無ければ restartable を起こさずに rc 1"
else ng "go が無いとき (rc=$rc): $(cat "$work/out")"; fi

rm -f "$FAKE_GO_LOG" "$FAKE_RUN_LOG" "$bin/restartable"
printf 'old\n' > "$bin/restartable.version"
FAKE_GO_RC=1 PATH="$work/fakebin:$PATH" "$TOOL" --bin-dir "$bin" status > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 1 ] && [ ! -f "$FAKE_RUN_LOG" ] && [ "$(cat "$bin/restartable.version")" = old ]; then ok "go install が落ちたら起こさず、印も書き換えない"
else ng "go install の失敗 (rc=$rc): $(cat "$work/out")"; fi

"$TOOL" status > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 2 ]; then ok "--bin-dir が無ければ rc 2"; else ng "--bin-dir 無しで rc=$rc"; fi

[ "$fails" -eq 0 ] || { echo "[test-restartable-run] $fails 件 NG"; exit 1; }
echo "[test-restartable-run] すべて ok"
