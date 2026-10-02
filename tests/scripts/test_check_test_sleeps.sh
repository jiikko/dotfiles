#!/usr/bin/env bash
# scripts/check_test_sleeps.sh (印の無い sleep を落とす検査。issue 615) の契約を fixture で固定する。
#
# 守るもの: 印の無い sleep を落とす (sleep N / sleep "$x" / /bin/sleep / command sleep) / 同じ行・直前の行の印で通す /
# heredoc の本文は開始行の前の印でまとめて通し、印が無ければ本文の sleep も落とす / 語彙に無い分類・理由の無い印は通さない /
# コメント行・sleeper のような別の語・wait_until.sh は数えない / 対象が少なすぎたら緑にしない / Makefile から配線されている。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_test_sleeps.sh"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

grep -qE '^test-test-sleeps:' "$ROOT_DIR/Makefile" && grep -qE '^\t@scripts/check_test_sleeps\.sh$' "$ROOT_DIR/Makefile" \
  && grep -qE 'run_make_targets_parallel\.sh .*test-test-sleeps' "$ROOT_DIR/Makefile" \
  || { printf '✗ Makefile の test-lint が check_test_sleeps.sh を回していない (配線が外れている)\n'; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-check-test-sleeps.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# $1=名前 $2=fixture の中身 → RC と OUT。fixture は 1 ファイルだけ置く (下限は 1 に下げる)
check_one() {
  local d="$WORK/$1"
  mkdir -p "$d/tests"
  printf '%s\n' "$2" > "$d/tests/t.sh"
  RC=0
  OUT="$( (cd "$ROOT_DIR" && CHECK_TEST_SLEEPS_DIR="$d/tests" CHECK_TEST_SLEEPS_MIN_FILES=1 CHECK_TEST_SLEEPS_GO_DIR=none "$CHECK") 2>&1 )" || RC=$?
}
expect_red()   { check_one "$1" "$2"; if [ "$RC" -ne 0 ] && grep -q 't.sh:' <<< "$OUT"; then ok "$1 は落とす"; else bad "$1 を落とさない (rc=$RC): $OUT"; fi; }
expect_green() { check_one "$1" "$2"; if [ "$RC" -eq 0 ]; then ok "$1 は通す"; else bad "$1 を通さない (rc=$RC): $OUT"; fi; }

printf 'Test 1: 印の無い sleep を落とす\n'   # sleep-ok: other: 見出しの文字列
expect_red   bare        'sleep 0.5'   # sleep-ok: other: 検査の fixture
expect_red   quoted      'sleep "$delay"'   # sleep-ok: other: 検査の fixture
expect_red   abspath     '/bin/sleep 1'   # sleep-ok: other: 検査の fixture
expect_red   command     'command sleep 0.1'   # sleep-ok: other: 検査の fixture
expect_red   background  'sleep 30 &'   # sleep-ok: other: 検査の fixture

printf 'Test 2: 印があれば通す\n'
expect_green sameline    'sleep 30 &   # sleep-ok: dummy: kill される前提'   # sleep-ok: other: 検査の fixture
expect_green prevline    '# sleep-ok: negative: 起きないことの確認
sleep 0.3'   # sleep-ok: other: 検査の fixture

printf 'Test 3: 印の形が違えば通さない\n'
expect_red   badkind     'sleep 1   # sleep-ok: wait: 語彙に無い分類'   # sleep-ok: other: 検査の fixture
expect_red   noreason    'sleep 1   # sleep-ok: dummy:'   # sleep-ok: other: 検査の fixture
expect_red   prev2lines  '# sleep-ok: negative: 2 行上の印は効かない
echo x
sleep 0.3'   # sleep-ok: other: 検査の fixture

printf 'Test 3b: 印のある sleep の行は次の行を許さない (直前の行の印はコメントだけの行に限る)\n'   # sleep-ok: other: 見出しの文字列
expect_red   nextline    'sleep 0.1  # sleep-ok: tick: 刻み
sleep 5'   # sleep-ok: other: 検査の fixture
expect_green realtime    'sleep 1   # sleep-ok: realtime: 本番の猶予を実時間で過ぎさせる'   # sleep-ok: other: 検査の fixture

printf 'Test 4: heredoc の本文\n'
# 複数行の fixture は 1 行の文字列で組み立てる ($'\n' で改行)。検査はこのファイル自身も読むので、複数行の文字列にすると
# 2 行目以降が地の文に見える (検査は引用符の状態を行をまたいで追わない)
H="cat > stub <<'EOS'"
expect_green heredoc_ok  "$H   # sleep-ok: stub: 生成する stub の中の待ち"$'\n'"sleep 0.05"$'\n'"EOS"   # sleep-ok: other: 検査の fixture
expect_red   heredoc_bad "$H"$'\n'"sleep 0.05"$'\n'"EOS"   # sleep-ok: other: 検査の fixture
expect_red   after_heredoc "$H   # sleep-ok: stub: 本文だけ"$'\n'"x"$'\n'"EOS"$'\n'"sleep 1"   # sleep-ok: other: 検査の fixture (heredoc の後ろの行は heredoc の印で通らない)
expect_red   hyphen_tag  "cat <<EOS-A   # sleep-ok: stub: 本文だけ"$'\n'"x"$'\n'"EOS-A"$'\n'"sleep 30"$'\n'"cat <<EOS"$'\n'"y"$'\n'"EOS"   # sleep-ok: other: 検査の fixture (タグのハイフンで終端を読み違えない)
expect_green quoted_file "cat > \"\$d/stub\" <<\"EOS\"   # sleep-ok: stub: 本文"$'\n'"sleep 0.05"$'\n'"EOS"   # sleep-ok: other: 検査の fixture (前の引用符で開始を見落とさない)
expect_green backslash   "cat <<\\EOS   # sleep-ok: stub: 本文"$'\n'"sleep 0.05"$'\n'"EOS"   # sleep-ok: other: 検査の fixture
expect_red   comment_heredoc "sleep 5 \& pid=\$! # sleep-ok: dummy: see the cat <<EOF stub"$'\n'"sleep 3"$'\n'"cat <<EOF"$'\n'"x"$'\n'"EOF"   # sleep-ok: other: 検査の fixture (行末コメントの中の << を開始と読まない)
expect_red   concat_tag  "cat <<\"EOF\"x   # sleep-ok: stub: 本文"$'\n'"y"$'\n'"EOFx"$'\n'"sleep 3"$'\n'"cat <<EOF"$'\n'"z"$'\n'"EOF"   # sleep-ok: other: 検査の fixture (タグは区切りまで連結する)
expect_red   escaped_lt  "echo \\<<EOF # sleep-ok: other: literal"$'\n'"sleep 3"$'\n'"cat <<EOF"$'\n'"z"$'\n'"EOF"   # sleep-ok: other: 検査の fixture (\< は heredoc ではない)
expect_green nested_arith 'x=$(( (a) << b ))'$'\n'"sleep 1   # sleep-ok: negative: x"   # sleep-ok: other: 検査の fixture

printf 'Test 4b: heredoc でないものを heredoc と読まない (読むと印 1 つで後ろが素通りする)\n'
expect_red   arith       'sleep 0.05  # sleep-ok: tick: 刻み
n=$((n << shift))
sleep 10'   # sleep-ok: other: 検査の fixture
expect_red   herestring  'grep -q x <<< foo  # sleep-ok: other: x
sleep 30'   # sleep-ok: other: 検査の fixture
expect_red   quotedheredoc 'echo "a <<EOF b"   # sleep-ok: other: x
sleep 40'   # sleep-ok: other: 検査の fixture
# 偽の red にしない: 算術・文字列の中の << を heredoc と読むと、終端が来ずに末尾で落ちる
expect_green arith_ok    'n=$((n << shift))
sleep 1   # sleep-ok: negative: x'   # sleep-ok: other: 検査の fixture
expect_green quoted_ok   'echo "a <<EOF b"
sleep 1   # sleep-ok: negative: x'   # sleep-ok: other: 検査の fixture
# sleep-ok: other: 検査の fixture (終端の来ない heredoc はファイルの末尾で落とす)
expect_red   unterminated "cat > stub <<EOS   # sleep-ok: stub: 終端が無い"$'\n'"echo x"

printf 'Test 5: 数えないもの\n'
expect_green comment     '# ここで sleep 3 していた (コメント行は数えない)'   # sleep-ok: other: 検査の fixture
expect_green otherword   'spawn_sleeper j1; STUB_REAL_SLEEP=1 run; tt_comm_is_sleep 1'   # sleep-ok: other: 検査の fixture

printf 'Test 6: wait_until.sh は印なしで通す\n'
d="$WORK/waituntil"; mkdir -p "$d/tests/lib"
printf 'sleep "${TT_WAIT_TICK:-0.1}"\n' > "$d/tests/lib/wait_until.sh"   # sleep-ok: other: 検査の fixture
RC=0; OUT="$( (cd "$ROOT_DIR" && CHECK_TEST_SLEEPS_DIR="$d/tests" CHECK_TEST_SLEEPS_MIN_FILES=1 CHECK_TEST_SLEEPS_GO_DIR=none "$CHECK") 2>&1 )" || RC=$?
[ "$RC" -eq 0 ] && ok 'tests/lib/wait_until.sh は通す' || bad "wait_until.sh を落とした: $OUT"

printf 'Test 7: 対象が下限より少なければ緑にしない\n'
d="$WORK/few"; mkdir -p "$d/tests"; printf 'echo ok\n' > "$d/tests/t.sh"
RC=0; OUT="$( (cd "$ROOT_DIR" && CHECK_TEST_SLEEPS_DIR="$d/tests" CHECK_TEST_SLEEPS_MIN_FILES=5 CHECK_TEST_SLEEPS_GO_DIR=none "$CHECK") 2>&1 )" || RC=$?
[ "$RC" -ne 0 ] && grep -q '件しかない' <<< "$OUT" && ok '下限未満は失敗' || bad "下限未満が緑 (rc=$RC): $OUT"

printf 'Test 8: Go の *_test.go の time.Sleep\n'
# $1=名前 $2=_test.go の中身 (shell 側は空の fixture 1 本で下限を満たす)
check_go() {
  local d="$WORK/go-$1"
  mkdir -p "$d/tests" "$d/src/m"
  printf 'echo ok\n' > "$d/tests/t.sh"
  printf '%s\n' "$2" > "$d/src/m/x_test.go"
  printf 'time.Sleep(1)\n' > "$d/src/m/x.go"   # sleep-ok: other: 本番コードは数えないことの fixture
  RC=0
  OUT="$( (cd "$ROOT_DIR" && CHECK_TEST_SLEEPS_DIR="$d/tests" CHECK_TEST_SLEEPS_MIN_FILES=1 CHECK_TEST_SLEEPS_GO_DIR="$d/src" CHECK_TEST_SLEEPS_MIN_GO_FILES=1 "$CHECK") 2>&1 )" || RC=$?
}
check_go bare 'time.Sleep(10 * time.Millisecond)'   # sleep-ok: other: 検査の fixture
[ "$RC" -ne 0 ] && grep -q 'x_test.go:1' <<< "$OUT" && ok 'Go: 印の無い time.Sleep を落とす' || bad "Go: 印の無い time.Sleep を落とさない (rc=$RC): $OUT"
check_go same 'time.Sleep(time.Second) // sleep-ok: dummy: kill される前提'   # sleep-ok: other: 検査の fixture
[ "$RC" -eq 0 ] && ok 'Go: 同じ行の印で通す (本番の .go は数えない)' || bad "Go: 同じ行の印で通らない: $OUT"
check_go prev '// sleep-ok: tick: helper の刻み
time.Sleep(5 * time.Millisecond)'   # sleep-ok: other: 検査の fixture
[ "$RC" -eq 0 ] && ok 'Go: 直前の行の印で通す' || bad "Go: 直前の行の印で通らない: $OUT"
check_go space 'time.Sleep (time.Second)'   # sleep-ok: other: 検査の fixture
[ "$RC" -ne 0 ] && ok 'Go: 括弧の前に空白がある time.Sleep も落とす' || bad "Go: time.Sleep ( を通した: $OUT"
check_go value 'sl := time.Sleep'   # sleep-ok: other: 検査の fixture
[ "$RC" -ne 0 ] && ok 'Go: メソッド値の time.Sleep も落とす' || bad "Go: メソッド値を通した: $OUT"
check_go next 'time.Sleep(1) // sleep-ok: tick: x
time.Sleep(time.Second)'   # sleep-ok: other: 検査の fixture
[ "$RC" -ne 0 ] && ok 'Go: 印のある行は次の行を許さない' || bad "Go: 次の行まで許した: $OUT"
check_go comment '// time.Sleep(3 * ttl) していた'
[ "$RC" -eq 0 ] && ok 'Go: コメント行は数えない' || bad "Go: コメント行を数えた: $OUT"

printf 'Test 9: 本物の tests/ と src/ が通る (印の付け漏れが無い)\n'
RC=0; OUT="$(cd "$ROOT_DIR" && "$CHECK" 2>&1)" || RC=$?
[ "$RC" -eq 0 ] && ok "tests/ が通る ($(tail -1 <<< "$OUT"))" || bad "tests/ に印の無い sleep がある: $(head -5 <<< "$OUT")"   # sleep-ok: other: メッセージの文字列

if [ "$fails" -ne 0 ]; then
  printf 'FAIL: %d 件\n' "$fails"
  exit 1
fi
printf 'OK check_test_sleeps (40 件)\n'
