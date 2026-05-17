#!/usr/bin/env zsh
# shellcheck shell=bash
# av1ify バックグラウンド先読み (prefetch) テスト
#
# 検証対象:
#   - __av1ify_prefetch: ファイル指定で bg を spawn し PID を track する。
#     不存在 / 空 / dry-run では spawn しない。
#   - __av1ify_kill_prefetches: PID 配列をクリアする (重複 kill しても安全)。
#   - __av1ify_run_batch: 複数ファイル時、次ファイルを prefetch する。
#     最後のファイル / 単一ファイル invocation では呼ばない。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Prefetch Tests ===\n\n'

# ----------------------------------------------------------------------
# Test 1: __av1ify_prefetch がファイル指定で bg を spawn し PID を track する
# ----------------------------------------------------------------------
printf '## Test 1: __av1ify_prefetch spawns bg job and tracks PID\n'
TEST_DIR="$TEST_TMP/prefetch_test1"
mkdir -p "$TEST_DIR"
echo "video content" > "$TEST_DIR/a.avi"
__AV1IFY_PREFETCH_PIDS=()
__AV1IFY_DRY_RUN=0
__av1ify_prefetch "$TEST_DIR/a.avi"
count=${#__AV1IFY_PREFETCH_PIDS[@]}
if (( count == 1 )); then
  printf '✓ One PID tracked after prefetch (count=%d)\n' "$count"
else
  printf '✗ Expected 1 PID tracked, got %d\n' "$count"
fi
# spawn された bg head -c 1 は即終了するが、テスト終了まで wait する必要は無い
__av1ify_kill_prefetches

# ----------------------------------------------------------------------
# Test 2: 不存在ファイルでは spawn しない
# ----------------------------------------------------------------------
printf '\n## Test 2: missing file -> no spawn\n'
__AV1IFY_PREFETCH_PIDS=()
__av1ify_prefetch "$TEST_TMP/does_not_exist.avi"
count=${#__AV1IFY_PREFETCH_PIDS[@]}
if (( count == 0 )); then
  printf '✓ No PID tracked for missing file\n'
else
  printf '✗ Expected 0 PIDs, got %d\n' "$count"
fi

# ----------------------------------------------------------------------
# Test 3: 空文字でも spawn しない
# ----------------------------------------------------------------------
printf '\n## Test 3: empty path -> no spawn\n'
__AV1IFY_PREFETCH_PIDS=()
__av1ify_prefetch ""
count=${#__AV1IFY_PREFETCH_PIDS[@]}
if (( count == 0 )); then
  printf '✓ No PID tracked for empty path\n'
else
  printf '✗ Expected 0 PIDs, got %d\n' "$count"
fi

# ----------------------------------------------------------------------
# Test 4: dry-run では spawn しない
# ----------------------------------------------------------------------
printf '\n## Test 4: dry-run -> no spawn\n'
TEST_DIR="$TEST_TMP/prefetch_test4"
mkdir -p "$TEST_DIR"
echo "video content" > "$TEST_DIR/b.avi"
__AV1IFY_PREFETCH_PIDS=()
__AV1IFY_DRY_RUN=1
__av1ify_prefetch "$TEST_DIR/b.avi"
count=${#__AV1IFY_PREFETCH_PIDS[@]}
if (( count == 0 )); then
  printf '✓ No PID tracked under dry-run\n'
else
  printf '✗ Expected 0 PIDs under dry-run, got %d\n' "$count"
fi
__AV1IFY_DRY_RUN=0

# ----------------------------------------------------------------------
# Test 5: __av1ify_kill_prefetches で PID 配列が空になる
# ----------------------------------------------------------------------
printf '\n## Test 5: __av1ify_kill_prefetches clears PID list\n'
TEST_DIR="$TEST_TMP/prefetch_test5"
mkdir -p "$TEST_DIR"
echo "v" > "$TEST_DIR/a.avi"
echo "v" > "$TEST_DIR/b.avi"
__AV1IFY_PREFETCH_PIDS=()
__av1ify_prefetch "$TEST_DIR/a.avi"
__av1ify_prefetch "$TEST_DIR/b.avi"
before=${#__AV1IFY_PREFETCH_PIDS[@]}
__av1ify_kill_prefetches
after=${#__AV1IFY_PREFETCH_PIDS[@]}
if (( before == 2 && after == 0 )); then
  printf '✓ kill_prefetches clears array (before=%d, after=%d)\n' "$before" "$after"
else
  printf '✗ Expected before=2, after=0; got before=%d, after=%d\n' "$before" "$after"
fi

# ----------------------------------------------------------------------
# Test 6: 既に exit した PID への kill でも __av1ify_kill_prefetches は失敗しない
# (err_exit 環境下でも安全に呼べることの担保)
# ----------------------------------------------------------------------
printf '\n## Test 6: kill_prefetches safe for already-exited PIDs\n'
# 即終了する短命 bg を spawn し、確実に exit させてから kill
( true ) &
dead_pid=$!
wait "$dead_pid" 2>/dev/null || true
__AV1IFY_PREFETCH_PIDS=("$dead_pid")
# err_exit 状態で kill -> 失敗しても script が死なないことを確認
if __av1ify_kill_prefetches; then
  printf '✓ kill_prefetches returns OK even when PID has exited\n'
else
  printf '✗ kill_prefetches failed on dead PID (would crash err_exit harness)\n'
fi

# ----------------------------------------------------------------------
# Test 7: 統合: __av1ify_run_batch が次ファイルを順に prefetch する
# 本物の __av1ify_prefetch を spy 版に差し替えて呼び出し順を検証する。
# ----------------------------------------------------------------------
printf '\n## Test 7: __av1ify_run_batch invokes prefetch for next file in order\n'
TEST_DIR="$TEST_TMP/prefetch_test7"
mkdir -p "$TEST_DIR"
echo "video content data" > "$TEST_DIR/one.avi"
echo "video content data" > "$TEST_DIR/two.mkv"
echo "video content data" > "$TEST_DIR/three.wmv"

SPY_LOG="$TEST_DIR/prefetch_calls.log"
: > "$SPY_LOG"

# spy 版で差し替え (本物は bg spawn するがテストでは呼び出し履歴だけ取りたい)
__av1ify_prefetch() {
  printf '%s\n' "$1" >> "$SPY_LOG"
}

cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/one.avi" "$TEST_DIR/two.mkv" "$TEST_DIR/three.wmv" > /dev/null 2>&1 || true
setopt err_exit

# 期待: 3 ファイル中、最後を除く各反復で次を prefetch → 2 回呼ばれる
#   iter 1 (one)   -> prefetch two
#   iter 2 (two)   -> prefetch three
#   iter 3 (three) -> next が空なので spawn しない
calls=$(wc -l < "$SPY_LOG" | tr -d ' ')
if (( calls == 2 )); then
  printf '✓ prefetch called twice for 3-file batch\n'
else
  printf '✗ Expected 2 prefetch calls, got %d\n' "$calls"
fi

line1=$(sed -n 1p "$SPY_LOG")
line2=$(sed -n 2p "$SPY_LOG")
if [[ "$line1" == "$TEST_DIR/two.mkv" ]]; then
  printf '✓ 1st prefetch target = two.mkv\n'
else
  printf '✗ Expected 1st prefetch=%s, got=%s\n' "$TEST_DIR/two.mkv" "$line1"
fi
if [[ "$line2" == "$TEST_DIR/three.wmv" ]]; then
  printf '✓ 2nd prefetch target = three.wmv\n'
else
  printf '✗ Expected 2nd prefetch=%s, got=%s\n' "$TEST_DIR/three.wmv" "$line2"
fi

# ----------------------------------------------------------------------
# Test 8: 単一ファイル invocation では prefetch されない (run_batch を通らない)
# ----------------------------------------------------------------------
printf '\n## Test 8: single-file invocation does NOT prefetch\n'
TEST_DIR="$TEST_TMP/prefetch_test8"
mkdir -p "$TEST_DIR"
echo "video content data" > "$TEST_DIR/solo.avi"

SPY_LOG="$TEST_DIR/prefetch_calls.log"
: > "$SPY_LOG"
# spy は Test 7 で定義済み

cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/solo.avi" > /dev/null 2>&1 || true
setopt err_exit

calls=$(wc -l < "$SPY_LOG" | tr -d ' ')
if (( calls == 0 )); then
  printf '✓ no prefetch for single-file invocation\n'
else
  printf '✗ Expected 0 prefetch calls, got %d\n' "$calls"
fi

# ----------------------------------------------------------------------
# Test 9: -f モード (リストファイル) でも prefetch が走る
# ----------------------------------------------------------------------
printf '\n## Test 9: -f mode also prefetches next file\n'
TEST_DIR="$TEST_TMP/prefetch_test9"
mkdir -p "$TEST_DIR"
echo "video content data" > "$TEST_DIR/p.avi"
echo "video content data" > "$TEST_DIR/q.mkv"
cat > "$TEST_DIR/list.txt" <<LISTEOF
$TEST_DIR/p.avi
$TEST_DIR/q.mkv
LISTEOF

SPY_LOG="$TEST_DIR/prefetch_calls.log"
: > "$SPY_LOG"

cd "$TEST_DIR"
unsetopt err_exit
av1ify -f "$TEST_DIR/list.txt" > /dev/null 2>&1 || true
setopt err_exit

calls=$(wc -l < "$SPY_LOG" | tr -d ' ')
if (( calls == 1 )); then
  printf '✓ -f mode prefetched once (next-of-first file)\n'
else
  printf '✗ Expected 1 prefetch call for -f, got %d\n' "$calls"
fi

line1=$(sed -n 1p "$SPY_LOG")
if [[ "$line1" == "$TEST_DIR/q.mkv" ]]; then
  printf '✓ -f mode prefetch target = q.mkv\n'
else
  printf '✗ Expected -f prefetch=%s, got=%s\n' "$TEST_DIR/q.mkv" "$line1"
fi

# ----------------------------------------------------------------------
# Test 10: __av1ify_skip_by_name の単体動作
# ファイル名/ローカル glob のみで SKIP 判定でき、ファイル本体は読まない。
# ----------------------------------------------------------------------
printf '\n## Test 10: __av1ify_skip_by_name predicate\n'
TEST_DIR="$TEST_TMP/prefetch_test10"
mkdir -p "$TEST_DIR"
# 入力自体が -enc.mp4 → SKIP
if __av1ify_skip_by_name "$TEST_DIR/foo-enc.mp4"; then
  printf '✓ -enc.mp4 suffix → skip\n'
else
  printf '✗ -enc.mp4 suffix should be skipped\n'
fi
# encoded. パターン → SKIP
if __av1ify_skip_by_name "$TEST_DIR/bar-encoded.mp4"; then
  printf '✓ encoded. suffix → skip\n'
else
  printf '✗ encoded. suffix should be skipped\n'
fi
# 既定出力が存在 → SKIP
echo "v" > "$TEST_DIR/baz.avi"
echo "out" > "$TEST_DIR/baz-enc.mp4"
if __av1ify_skip_by_name "$TEST_DIR/baz.avi"; then
  printf '✓ existing default output → skip\n'
else
  printf '✗ default output should trigger skip\n'
fi
# バリアント出力が存在 → SKIP
echo "v" > "$TEST_DIR/qux.avi"
echo "out" > "$TEST_DIR/qux-720p-enc.mp4"
if __av1ify_skip_by_name "$TEST_DIR/qux.avi"; then
  printf '✓ existing variant output → skip\n'
else
  printf '✗ variant output should trigger skip\n'
fi
# 既存出力なし → 処理候補 (skip しない)
echo "v" > "$TEST_DIR/fresh.avi"
if ! __av1ify_skip_by_name "$TEST_DIR/fresh.avi"; then
  printf '✓ no existing output → not skipped (prefetch candidate)\n'
else
  printf '✗ fresh file should NOT be skipped\n'
fi
# 命名規則に合わないバリアントは SKIP しない (誤一致防止)
echo "v" > "$TEST_DIR/weird.avi"
echo "out" > "$TEST_DIR/weird-junk-enc.mp4"
if ! __av1ify_skip_by_name "$TEST_DIR/weird.avi"; then
  printf '✓ non-conforming variant tag → not skipped\n'
else
  printf '✗ non-conforming variant should NOT trigger skip\n'
fi
# 空文字 → SKIP (prefetch 不要)
if __av1ify_skip_by_name ""; then
  printf '✓ empty input → skip\n'
else
  printf '✗ empty input should be treated as skip\n'
fi

# ----------------------------------------------------------------------
# Test 11: __av1ify_run_batch は SKIP 対象に対しては prefetch しない
# ディレクトリ指定で大量の既変換ファイルが含まれていても全 materialize しない、
# というのが本来の意図 (バグ修正の本丸)。
# ----------------------------------------------------------------------
printf '\n## Test 11: run_batch skips prefetch for already-converted next file\n'
TEST_DIR="$TEST_TMP/prefetch_test11"
mkdir -p "$TEST_DIR"
# a.avi は未変換, b.avi は b-enc.mp4 が既にある, c.avi は未変換
echo "v" > "$TEST_DIR/a.avi"
echo "v" > "$TEST_DIR/b.avi"
echo "out" > "$TEST_DIR/b-enc.mp4"
echo "v" > "$TEST_DIR/c.avi"

SPY_LOG="$TEST_DIR/prefetch_calls.log"
: > "$SPY_LOG"
# Test 7 で spy 化済み: __av1ify_prefetch は SPY_LOG にパスを記録するだけ

cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/a.avi" "$TEST_DIR/b.avi" "$TEST_DIR/c.avi" > /dev/null 2>&1 || true
setopt err_exit

# 期待:
#   iter 1 (a.avi): next=b.avi → b-enc.mp4 が既存なので skip_by_name → prefetch しない
#   iter 2 (b.avi): next=c.avi → c-enc.mp4 は無い → prefetch する
#   iter 3 (c.avi): next 無し → prefetch しない
calls=$(wc -l < "$SPY_LOG" | tr -d ' ')
if (( calls == 1 )); then
  printf '✓ prefetch called only for non-skippable next (count=1)\n'
else
  printf '✗ Expected 1 prefetch call, got %d\n' "$calls"
fi
line1=$(sed -n 1p "$SPY_LOG")
if [[ "$line1" == "$TEST_DIR/c.avi" ]]; then
  printf '✓ prefetch target = c.avi (b.avi was correctly skipped)\n'
else
  printf '✗ Expected prefetch=%s, got=%s\n' "$TEST_DIR/c.avi" "$line1"
fi

# ----------------------------------------------------------------------
# Test 12: 次ファイルが -enc.mp4 自体の場合も prefetch しない
# ディレクトリ列挙で foo.avi と foo-enc.mp4 が同時に拾われるケースを想定。
# ----------------------------------------------------------------------
printf '\n## Test 12: prefetch skipped when next is *-enc.mp4 itself\n'
TEST_DIR="$TEST_TMP/prefetch_test12"
mkdir -p "$TEST_DIR"
echo "v" > "$TEST_DIR/x.avi"
echo "out" > "$TEST_DIR/x-enc.mp4"

SPY_LOG="$TEST_DIR/prefetch_calls.log"
: > "$SPY_LOG"

cd "$TEST_DIR"
unsetopt err_exit
# 並び順: x.avi → x-enc.mp4 を batch に渡す
av1ify "$TEST_DIR/x.avi" "$TEST_DIR/x-enc.mp4" > /dev/null 2>&1 || true
setopt err_exit

# 期待:
#   iter 1 (x.avi): next=x-enc.mp4 → suffix match で skip_by_name → prefetch しない
#   iter 2 (x-enc.mp4): next 無し
calls=$(wc -l < "$SPY_LOG" | tr -d ' ')
if (( calls == 0 )); then
  printf '✓ no prefetch when next is -enc.mp4\n'
else
  printf '✗ Expected 0 prefetch calls, got %d\n' "$calls"
fi

printf '\n=== Prefetch Tests Completed ===\n'
