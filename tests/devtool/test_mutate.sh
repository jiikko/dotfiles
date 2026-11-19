#!/usr/bin/env bash
# devtool/bin/mutate の回帰テスト。
#
# 固定すること:
#   - 抽出 (--summarize) が、実際の swift test / xcodebuild の出力 (bin/testdata/mutate/) から実行件数 (Swift Testing と
#     XCTest の集計行の合計) と落ちたテストの名前を出す。xcodebuild の行頭のゼロ幅スペース付きの行も拾う
#   - 集計行が無い出力は「抽出できなかった」(rc 3) で、0 件と読ませない
#   - 判定: 件数が baseline と同じで rc≠0 かつ落ちたテストあり → RED (rc 1) / rc 0 かつ落ちたテスト無し → GREEN (rc 0) /
#     件数が違う・集計行が無い・rc と落ちたテストが食い違う・baseline が green でない → 判定不能 (rc 3)
#   - どの判定でも対象ファイルは復元される。未 commit の変更があれば変異を当てずに拒否する (rc 70)
#
# e2e のケースは一時 git repo に本物の bin/mutate を置き、検証コマンドに偽のランナー (対象ファイルの中身で出力を変える) を使う。
# 偽のランナーが出す行の形は、fixture の実際の出力の形に合わせてある。
# obaket の道具が出す行 (`<label>: complete log:`・`<label>: parallel test cases:`・run-with-test-profile-residue の lock の文言) は手で写したもので、
# 形の正本は obaket の道具。obaket が形を変えてもここは緑のまま通るので、ずれは obaket の check-mutate-format-contract と
# test_mutate_obaket_contract.sh (この dotfiles の読み手で obaket の検査を回す) が見る。
#
# 検出しない形: 実際の swift test / xcodebuild を変異ごとに走らせる経路そのもの (時間がかかるので fixture で代える)。
#   Swift Testing / xcodebuild の出力の形が版で変わったときは、fixture を採り直して抽出が落ちることで気づく
#   (生成元の test は bin/testdata/mutate/*.swift.txt。一時ディレクトリの package で swift test と
#   xcodebuild test -scheme <名前>-Package -destination 'platform=macOS' を走らせ、出力の一時パスを <TMP> に置き換えた)。
#
# Usage: tests/devtool/test_mutate.sh [--runtimeout <binary>]   (引数なしは make test の runner から)
set -uo pipefail
unset CDPATH

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$TEST_DIR/../.." && pwd)"
# shellcheck source=tests/lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh" || { echo "✗ tests/lib/wait_until.sh を読めない"; exit 1; }
# none_alive <pid…>: どの pid も生きていなければ 0 (tt_wait_until の条件)
none_alive() { local pid; for pid in "$@"; do kill -0 "$pid" 2>/dev/null && return 1; done; return 0; }
MUTATE="$ROOT_DIR/devtool/bin/mutate"
FIXTURES="$TEST_DIR/testdata/mutate"
WORK="$(mktemp -d)"
WORK="$(cd "$WORK" && pwd -P)"
# CI の契約検査は stub を使う。--runtimeout <binary> は実際の停止まで検証する入口。
REAL_RUNTIMEOUT=""
if [ "${1:-}" = --runtimeout ] && [ "$#" = 2 ] && [ -x "$2" ]; then
  REAL_RUNTIMEOUT="$2"
elif [ "$#" -ne 0 ]; then
  echo "usage: tests/devtool/test_mutate.sh [--runtimeout <binary>]" >&2; exit 2
fi
mkdir -p "$WORK/dotfiles/bin"
cat > "$WORK/dotfiles/bin/runtimeout" <<'EOF'
#!/bin/bash
seconds="$1"; shift
if [ "$(cat impl.txt)" = slow-term ]; then
  exec python3 - <<'PYEOF'
import os, signal, time
open("tmp/hang.pids", "w").write(str(os.getpid()))
def stop(*_):
    open("impl.txt", "w").write("signal-write")
    open("tmp/term.observed", "w").write("TERM")
    time.sleep(1)
    raise SystemExit(143)
signal.signal(signal.SIGTERM, stop)
time.sleep(60)
PYEOF
fi
if [ "$(cat impl.txt)" = hang ]; then
  # 成功の集計が出た後でも timeout を GREEN にしてはいけない。
  echo '✔ Test run with 2 tests in 1 suite passed after 0.001 seconds.'
  sleep "$seconds"  # sleep-ok: window: 時間切れまで子を待たせる runtimeout を演じる (124 で終わる形の入力)
  exit 124
fi
exec "$@"
EOF
chmod +x "$WORK/dotfiles/bin/runtimeout"
# skill-tmpdir の契約 (本体 checkout の tmp/<名前>/<session>/ を作って返す) だけを写した偽物。本体 checkout の求め方は dotfiles 側のテストが持つ。
# MUTATE_TEST_TMPDIR_ROOT で、別の repo から同じ置き場を使う形 (worktree が本体 checkout の tmp/ を共有する) を作る
cat > "$WORK/dotfiles/bin/skill-tmpdir" <<'EOF'
#!/bin/bash
[ "${1:-}" = --if-ignored ] && shift
root="${MUTATE_TEST_TMPDIR_ROOT:-$(git rev-parse --show-toplevel)}"
d="$root/tmp/$1/test-session"
mkdir -p "$d" && printf '%s\n' "$d"
EOF
chmod +x "$WORK/dotfiles/bin/skill-tmpdir"
if [ -n "$REAL_RUNTIMEOUT" ]; then
  cp "$REAL_RUNTIMEOUT" "$WORK/dotfiles/bin/runtimeout"
fi
# dotfiles の bin/devtool-run が渡す形 (解決済みの絶対パス) を写す
export DEVTOOL_RUNTIMEOUT="$WORK/dotfiles/bin/runtimeout" DEVTOOL_SKILL_TMPDIR="$WORK/dotfiles/bin/skill-tmpdir"
PASS=0
FAIL=0
trap 'rm -rf "$WORK"' EXIT

ok() { PASS=$((PASS + 1)); echo "  ok   - $1"; }
ng() { FAIL=$((FAIL + 1)); echo "  NG   - $1"; [ -f "$WORK/last.out" ] && sed 's/^/           /' "$WORK/last.out"; }

# expect <名前> <期待 rc> <実際の rc> <出力に在るべき固定文字列>...
expect() {
  local name="$1" want="$2" got="$3" needle
  shift 3
  if [ "$got" != "$want" ]; then ng "$name (rc=${got}、期待 $want)"; return; fi
  for needle in "$@"; do
    if ! grep -qF -- "$needle" "$WORK/last.out"; then ng "$name (出力に「${needle}」が無い)"; return; fi
  done
  ok "$name"
}

echo "[mutate-test] running"
"$MUTATE" --help > "$WORK/last.out" 2>&1
expect "--help: 使い方・例・置換スクリプトの呼ばれ方・終了コードを rc 0 で出す" 0 $? "使い方:" "例 (" "MUTATE_TARGET" "終了コード:"
"$MUTATE" --bogus a b c d > "$WORK/last.out" 2>&1
expect "不明なオプション: rc 64 で --help へ案内する" 64 $? "使い方は bin/mutate --help"
"$MUTATE" > "$WORK/last.out" 2>&1
expect "引数が足りない: rc 64 で --help へ案内する" 64 $? "詳しくは bin/mutate --help"

# --- 抽出: 実際の出力 ---
# 前提: xcodebuild の fixture にゼロ幅スペース付きの失敗行があること (無いと下の assert が何も検査しない)
ZWSP="$(printf '\342\200\213')"
if grep -qF "${ZWSP}✘ Test \"表示名つき 失敗\" failed" "$FIXTURES/xcodebuild.out"; then
  ok "前提: xcodebuild の fixture にゼロ幅スペース付きの失敗行がある"
else
  ng "前提: xcodebuild の fixture にゼロ幅スペース付きの失敗行が無い (fixture を採り直した?)"
fi

"$MUTATE" --summarize "$FIXTURES/swift-test.out" > "$WORK/last.out" 2>&1
expect "抽出 (swift test): 件数 7 = Swift Testing 5 + XCTest 2、落ちた 4 件の名前" 0 $? \
  "MUTATE_SUMMARY count=7 failed=4" "集計行 (Swift Testing): 5 件" "集計行 (XCTest): 2 件" \
  'Swift Testing: "表示名つき 失敗"' "Swift Testing: failsUnnamed()" 'Swift Testing: "parameterized 失敗"' \
  "XCTest: -[LibTests.XCLegacyTests testFails]"

"$MUTATE" --summarize "$FIXTURES/xcodebuild.out" > "$WORK/last.out" 2>&1
expect "抽出 (xcodebuild): ゼロ幅スペース付きの行と Failing tests のブロックも拾う" 0 $? \
  "MUTATE_SUMMARY count=7 failed=4" 'Swift Testing: "表示名つき 失敗"' "xcodebuild の Failing tests (4):" \
  "NamedSuite.failsNamed()" "-[NamedSuite param(x:)]"

# make test-only の実際の失敗 (Swift Testing 15 本中 12 本。xcodebuild の Failing tests は issue ごとに 22 行並ぶ)
"$MUTATE" --summarize "$FIXTURES/xcodebuild-swift-testing-failed.out" > "$WORK/last.out" 2>&1
expect "抽出 (xcodebuild の Swift Testing だけの失敗): 12 本の名前を読み、Failing tests の重複を除く (issue 1391)" 0 $? \
  "MUTATE_SUMMARY count=15 failed=12 run=failed" "xcodebuild の Failing tests (12):" \
  'Swift Testing: "remember / cursor: path ごとに単一 cursor 位置を保存・取得できる"'
{ grep -F "Test run with 15 tests" "$FIXTURES/xcodebuild-swift-testing-failed.out"
  sed -n '/^Failing tests:$/,/^$/p' "$FIXTURES/xcodebuild-swift-testing-failed.out"; echo "** TEST FAILED **"; } > "$WORK/failing-dup-only.out"
"$MUTATE" --summarize "$WORK/failing-dup-only.out" > "$WORK/last.out" 2>&1
expect "抽出: 名前の行が無く Failing tests だけのとき、同じテストの重複を 1 本と数え、表示も同じ数にする (issue 1391)" 0 $? \
  "MUTATE_SUMMARY count=15 failed=12 run=failed" "落ちたテスト: 12 件 (名前の行が無いので xcodebuild の Failing tests の数)"
cat "$WORK/failing-dup-only.out" "$WORK/failing-dup-only.out" > "$WORK/failing-dup-twice.out"
"$MUTATE" --summarize "$WORK/failing-dup-twice.out" > "$WORK/last.out" 2>&1
expect "抽出: 重複を除くのは Failing tests のブロックの中だけ (別の run の同名のテストを潰さない。issue 1391)" 0 $? \
  "MUTATE_SUMMARY count=30 failed=24 run=failed"

"$MUTATE" --summarize "$FIXTURES/xcodebuild-only-testing.out" > "$WORK/last.out" 2>&1
expect "抽出 (xcodebuild -only-testing): 最上位の suite が Selected tests でも件数を読む (issue 1002)" 0 $? \
  "MUTATE_SUMMARY count=1 failed=0 run=passed"
sed "s/'Selected tests' passed/'Selected tests' failed/" "$FIXTURES/xcodebuild-only-testing.out" > "$WORK/selected-failed.out"
"$MUTATE" --summarize "$WORK/selected-failed.out" > "$WORK/last.out" 2>&1
expect "抽出: Selected tests failed で run=failed" 0 $? "run=failed"

cat "$FIXTURES/swift-test.out" "$FIXTURES/swift-test.out" > "$WORK/twice.out"
"$MUTATE" --summarize "$WORK/twice.out" > "$WORK/last.out" 2>&1
expect "抽出: 集計行が複数あれば全部を合計する (1 本だけ拾わない)" 0 $? "実行件数: 14 (集計行 4 本の合計)"

# 並列で走るテスト (shared の package。集計行が無く `Test case '…' passed on '…'` だけ)。fixture は make test-only の全文の末尾
#   (2 suite を -only-testing で流した実物。パラメータつきの resumableMoveBar(kind:) は同じ名前で 4 行)
"$MUTATE" --summarize "$FIXTURES/xcodebuild-parallel-passed.out" > "$WORK/last.out" 2>&1
expect "抽出 (並列): Test case … on の行数を件数にする。同じ名前の行も 1 本ずつ数える (issue 1418)" 0 $? \
  "MUTATE_SUMMARY count=9 failed=0 run=passed" "集計行 (並列の Test case 行): 9 件"
"$MUTATE" --summarize "$FIXTURES/xcodebuild-parallel-failed.out" > "$WORK/last.out" 2>&1
expect "抽出 (並列の失敗): failed on の行から名前を読み、run=failed にする (issue 1418)" 0 $? \
  "MUTATE_SUMMARY count=9 failed=1 run=failed" "並列: PreviewScreenDebugObservationTests/operableTransportRequiresVisibleCard()"
grep -vF "** TEST FAILED **" "$FIXTURES/xcodebuild-parallel-failed.out" | sed '/^Failing tests:$/,/^$/d' > "$WORK/parallel-failed-line-only.out"
"$MUTATE" --summarize "$WORK/parallel-failed-line-only.out" > "$WORK/last.out" 2>&1
expect "抽出 (並列): failed on の行だけで run=failed (TEST FAILED と Failing tests が無くても)" 0 $? "MUTATE_SUMMARY count=9 failed=1 run=failed"
# skipped も件数に入れる (XCTest の Executed・Swift Testing の Test run with も skip を数える)。行は実際の make test の出力から取った
{ cat "$FIXTURES/xcodebuild-parallel-passed.out"
  echo "Test case 'WebDAVServerTrustContainerE2ETests/connectionTestProbeIsEvaluatedAfterBrowse()' skipped on 'My Mac - xctest (92489)' (0.000 seconds)"; } > "$WORK/parallel-skipped.out"
"$MUTATE" --summarize "$WORK/parallel-skipped.out" > "$WORK/last.out" 2>&1
expect "抽出 (並列): skipped の行も件数に入れ、落ちたテストには数えない" 0 $? "MUTATE_SUMMARY count=10 failed=0 run=passed"
# withKnownIssue を通ったテストは `recorded an expected failure on` の 1 行だけを出す (passed の行は出ない)。件数に入れ、落ちたテストには数えない。
# 入れないと、変異で known issue が起きなくなった (Known issue was not recorded で落ちる) run の件数が baseline より 1 多くなり判定不能になる。行は実際の make test の出力から取った
{ cat "$FIXTURES/xcodebuild-parallel-passed.out"
  echo "Test case 'StateChangeSignalTests/missingNotificationTimesOut()' recorded an expected failure on 'My Mac - xctest (95099)' (0.548 seconds)"; } > "$WORK/parallel-known-issue.out"
"$MUTATE" --summarize "$WORK/parallel-known-issue.out" > "$WORK/last.out" 2>&1
expect "抽出 (並列): recorded an expected failure の行も件数に入れ、落ちたテストには数えない" 0 $? "MUTATE_SUMMARY count=10 failed=0 run=passed"
# xcodebuild の stderr の行が ` on ` の直後に割り込み、行が割れた形 (実際の make test の出力から取った 2 行)。割れても 1 件に数える
{ cat "$FIXTURES/xcodebuild-parallel-passed.out"
  echo "Test case 'TransferUploadBodyTests/multipartRouteAdmitsOneWholeProviderChunk(provider:chunkBytes:)' passed on 2026-10-11 18:37:20.698 xcodebuild[40533:134732819] [MT] IDETestOperationsObserverDebug: 6.535 elapsed -- Testing started completed."
  echo "'My Mac - xctest (40696)' (1.227 seconds)"; } > "$WORK/parallel-split.out"
"$MUTATE" --summarize "$WORK/parallel-split.out" > "$WORK/last.out" 2>&1
expect "抽出 (並列): stderr の行が割り込んで割れた Test case の行も 1 件に数える" 0 $? "MUTATE_SUMMARY count=10 failed=0 run=passed"
# 名前の途中で割れた形 (実際の make test の出力の行)。状態の語が読めなくても、行頭の Test case ' で 1 件に数える
{ cat "$FIXTURES/xcodebuild-parallel-passed.out"
  echo "Test case 'HTTPStrea2026-10-10 00:11:39.547 xcodebuild[24218:119663979] [MT] IDETestOperationsObserverDebug: 57.403 elapsed -- Testing started completed."; } > "$WORK/parallel-split-name.out"
"$MUTATE" --summarize "$WORK/parallel-split-name.out" > "$WORK/last.out" 2>&1
expect "抽出 (並列): 名前の途中で割れた行も行頭で 1 件に数える (落ちたテストには数えない)" 0 $? "MUTATE_SUMMARY count=10 failed=0 run=passed"
# make test では直列の集計行と並列の行が同じ出力に出る (実測: Test run with 2938 tests と Test case … on の行 4131 本)。両方を足す
cat "$FIXTURES/swift-test.out" "$FIXTURES/xcodebuild-parallel-passed.out" > "$WORK/serial-and-parallel.out"
"$MUTATE" --summarize "$WORK/serial-and-parallel.out" > "$WORK/last.out" 2>&1
expect "抽出: 直列の集計行と並列の行が混ざれば両方を足す" 0 $? "実行件数: 16 (集計行 3 本の合計)"
# run-logged-command の要約の行があれば、それだけを数える (失敗時の末尾 40 行に同じ Test case … on の行が残る)
{ echo "make test-only: parallel test cases: 9 (passed 8, failed 1, skipped 0, expected failure 0, unreadable 0)"; cat "$FIXTURES/xcodebuild-parallel-failed.out"; } > "$WORK/parallel-summary-and-tail.out"
"$MUTATE" --summarize "$WORK/parallel-summary-and-tail.out" > "$WORK/last.out" 2>&1
expect "抽出: parallel test cases の要約の行があれば、末尾に残った Test case … on の行を足さない (二重に数えない)" 0 $? \
  "MUTATE_SUMMARY count=9 failed=1 run=failed"
printf '%s\n' "make test-only: parallel test cases: 9 (passed 8, failed 1, skipped 0, expected failure 0, unreadable 0)" "make test-only: parallel test cases: 3 (passed 3, failed 0, skipped 0, expected failure 0, unreadable 0)" > "$WORK/parallel-summary-only.out"
"$MUTATE" --summarize "$WORK/parallel-summary-only.out" > "$WORK/last.out" 2>&1
expect "抽出: 要約の行が複数あれば合計し、failed が 1 以上なら run=failed (名前の行が窓の外でも)" 0 $? \
  "MUTATE_SUMMARY count=12 failed=0 run=failed"

printf 'Building for debugging...\nerror: something went wrong\n' > "$WORK/nosummary.out"
"$MUTATE" --summarize "$WORK/nosummary.out" > "$WORK/last.out" 2>&1
expect "抽出: 集計行が無ければ抽出できなかったとして rc 3 (0 件と読ませない)" 3 $? \
  "実行件数: 抽出できなかった" "MUTATE_SUMMARY count=- failed=0"

# xcodebuild の Failing tests だけが失敗を示す形 (✘ 行も Test Case 行も無い)。fixture の実際の行から組む
{ grep -F "Test Suite 'All tests' failed" "$FIXTURES/xcodebuild.out"; grep -F "Executed 2 tests" "$FIXTURES/xcodebuild.out" | tail -1
  sed -n '/^Failing tests:$/,/^$/p' "$FIXTURES/xcodebuild.out"; echo "** TEST FAILED **"; } > "$WORK/failing-only.out"
"$MUTATE" --summarize "$WORK/failing-only.out" > "$WORK/last.out" 2>&1
expect "抽出: Failing tests のブロックだけでも落ちたテストを数える" 0 $? "MUTATE_SUMMARY count=2 failed=4 run=failed"

printf '%s\n' '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.' > "$WORK/tail-cut.out"
"$MUTATE" --summarize "$WORK/tail-cut.out" > "$WORK/last.out" 2>&1
expect "抽出: 失敗行が無くても集計行の failed を読む (| tail で窓の外に落ちた形)" 0 $? "MUTATE_SUMMARY count=2 failed=0 run=failed"

# run=failed を集計行と xcodebuild の結果行のそれぞれ単独で立てる (片方を消しても、もう片方で緑にならないように分ける)
{ grep -F "Test Suite 'All tests' failed" "$FIXTURES/xcodebuild.out"; grep -F "Executed 2 tests" "$FIXTURES/xcodebuild.out" | tail -1; } > "$WORK/xctest-failed-only.out"
"$MUTATE" --summarize "$WORK/xctest-failed-only.out" > "$WORK/last.out" 2>&1
expect "抽出: XCTest の All tests failed だけで run=failed" 0 $? "run=failed"
printf '%s\n' '✔ Test run with 2 tests in 1 suite passed after 0.001 seconds.' '** TEST FAILED **' > "$WORK/marker-only.out"
"$MUTATE" --summarize "$WORK/marker-only.out" > "$WORK/last.out" 2>&1
expect "抽出: xcodebuild の ** TEST FAILED ** だけで run=failed (集計行は passed)" 0 $? "run=failed"

# --- e2e: 一時 repo で本物の bin/mutate を回す ---
REPO="$WORK/repo"
mkdir -p "$REPO/bin"
cat > "$REPO/bin/reap-orphan-testing-helper" <<'EOF'
#!/bin/bash
[ "${1:-}" = --kill ] || exit 0
printf '%s\n' "$@" > tmp/reaper.args
EOF
chmod +x "$REPO/bin/reap-orphan-testing-helper"
cp "$MUTATE" "$REPO/bin/mutate"
# obaket の wrapper が渡す形。vgp の wrapper は渡さない (渡さないときの形は下で別に見る)
export MUTATE_REAP_HELPER="$REPO/bin/reap-orphan-testing-helper"
cat > "$REPO/run-tests" <<'EOF'
#!/bin/bash
# 偽のランナー: impl.txt の中身で Swift Testing 形式の出力を変える
# MUT_LOCK_AT=<impl>: その中身のとき、run-with-test-profile-residue が lock を取れずにテストを始めなかった形で終わる
#   (MUT_LOCK_RC=2 は make に包まれた形)
if [ -n "${MUT_LOCK_AT:-}" ] && [ "$(cat impl.txt)" = "$MUT_LOCK_AT" ]; then
  echo "run-with-test-profile-residue: another checked run owns /tmp/obaket-test.lock; retry after it ends" >&2
  [ "${MUT_LOCK_RC:-75}" = 2 ] && echo "make: *** [test-only] Error 75" >&2
  exit "${MUT_LOCK_RC:-75}"
fi
# 起動のたびに、そのときの impl.txt を記録する (mutator の失敗の後に変異の検証を走らせていないことを、出力の有無ではなく起動の記録で見る)
mkdir -p tmp && cat impl.txt >> tmp/runner-calls && echo >> tmp/runner-calls
# MUT_INNER_LOG=1: run-logged-command のように全文を別のファイルに書き、そのパスを出す (同じ行を 2 回と、repo の外を指す行も出す)
if [ -n "${MUT_INNER_LOG:-}" ]; then
  inner="$PWD/tmp/inner-$(cat impl.txt).log"
  echo "inner full $(cat impl.txt)" > "$inner"
  echo "make test-only: complete log: $inner"
  echo "make test-only: complete log: $inner"
  echo "quoted fixture: complete log: ${MUT_OUTSIDE_LOG:-/nonexistent}"
  # 同じファイルを別の書き方で (.. を挟む)、repo の外を指す symlink を経由して
  echo "make test-only: complete log: $PWD/tmp/../tmp/inner-$(cat impl.txt).log"
  [ -L tmp/outside-link ] || ln -s "$(dirname "${MUT_OUTSIDE_LOG:-/nonexistent}")" tmp/outside-link
  echo "quoted fixture: complete log: $PWD/tmp/outside-link/$(basename "${MUT_OUTSIDE_LOG:-/nonexistent}")"
fi
# MUT_QUOTE_LOCK=<impl>: テストは走ったが、出力に lock の文言を引用しただけの形 (件数は baseline と違う)。
# MUT_QUOTE_LOCK_NO_SUMMARY=1 は、集計行を出す前に落ちた形 (落ちたテストの行だけがある)
if [ -n "${MUT_QUOTE_LOCK:-}" ] && [ "$(cat impl.txt)" = "$MUT_QUOTE_LOCK" ]; then
  echo 'run-with-test-profile-residue: another checked run owns /tmp/obaket-test.lock; retry after it ends'
  echo 'diagnostic fixture: run-with-test-profile-residue: another checked run owns /tmp/x.lock; retry after it ends'
  echo '✘ Test "guard" failed after 0.001 seconds with 1 issue.'
  [ -n "${MUT_QUOTE_LOCK_NO_SUMMARY:-}" ] || echo '✘ Test run with 1 test in 1 suite failed after 0.001 seconds with 1 issue.'
  exit 1
fi
case "$(cat impl.txt)" in
  ok|ok-cosmetic)
    echo '✔ Test "guard" passed after 0.001 seconds.'
    echo '✔ Test run with 2 tests in 1 suite passed after 0.001 seconds.'
    exit 0 ;;
  broken)
    echo '✘ Test "guard" recorded an issue at X.swift:1:1: Expectation failed'
    echo '✘ Test "guard" failed after 0.001 seconds with 1 issue.'
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 1 ;;
  lost)
    echo '✔ Test run with 1 test in 1 suite passed after 0.001 seconds.'
    exit 0 ;;
  parallel-ok)
    # 並列で走るテストの形 (集計行が無い。bin/testdata/mutate/xcodebuild-parallel-*.out の行の形)
    echo "Test case 'GuardTests/guard()' passed on 'My Mac - xctest (1)' (0.001 seconds)"
    echo "Test case 'GuardTests/other()' passed on 'My Mac - xctest (1)' (0.001 seconds)"
    echo '** TEST SUCCEEDED **'
    exit 0 ;;
  parallel-broken)
    echo "Test case 'GuardTests/guard()' failed on 'My Mac - xctest (1)' (0.001 seconds)"
    echo "Test case 'GuardTests/other()' passed on 'My Mac - xctest (1)' (0.001 seconds)"
    echo '** TEST FAILED **'
    exit 65 ;;
  crash)
    echo 'Fatal error: crashed before the summary'
    exit 1 ;;
  silent-fail)
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 1 ;;
  tail-cut)
    # 検証コマンドが `swift test | tail -1` で、失敗行が窓の外に落ち、rc は tail の 0 になった形
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 0 ;;
  zero)
    echo '✔ Test run with 0 tests in 0 suites passed after 0.001 seconds.'
    exit 0 ;;
  masked-name-only)
    echo '✘ Test "other" failed after 0.001 seconds with 1 issue.'
    echo '✔ Test run with 2 tests in 1 suite passed after 0.001 seconds.'
    exit 0 ;;
  masked-run-only)
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 0 ;;
  masked)
    # 別 suite の失敗を `|| true` で隠して rc 0 にした形
    echo '✘ Test "other" failed after 0.001 seconds with 1 issue.'
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 0 ;;
  commit)
    git add impl.txt && git -c user.email=t@t -c user.name=t commit -qm "変異を commit した"
    echo '✘ Test "guard" failed after 0.001 seconds with 1 issue.'
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 1 ;;
  hang)
    echo '✔ Test run with 2 tests in 1 suite passed after 0.001 seconds.'
    python3 - <<'PYEOF'
import os, signal, subprocess, time
def stop(*_):
    if os.environ.get("MUT_TO") == "hang":
        open("impl.txt", "w").write("signal-write")
    open("tmp/term.observed", "w").write("TERM")
signal.signal(signal.SIGTERM, stop)
child = subprocess.Popen(["sleep", "60"])  # sleep-ok: dummy: 子孫が残らないことを見るための、kill される前提の子
open("tmp/hang.pids", "w").write(f"{os.getpid()} {child.pid}")
time.sleep(60)
PYEOF
    exit 0 ;;
  return124)
    echo '✘ Test "guard" failed after 0.001 seconds with 1 issue.'
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 124 ;;
  wait)
    : > started
    for _ in $(seq 400); do [ -f release ] && break; sleep 0.05; done  # sleep-ok: tick: 偽のランナーの中の刻み (tt_wait_until を source できない)
    echo '✘ Test "guard" failed after 0.001 seconds with 1 issue.'
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 1 ;;
  stage)
    git add impl.txt
    echo '✘ Test "guard" failed after 0.001 seconds with 1 issue.'
    echo '✘ Test run with 2 tests in 1 suite failed after 0.001 seconds with 1 issue.'
    exit 1 ;;
esac
exit 99
EOF
chmod +x "$REPO/run-tests"
cat > "$REPO/mutator.py" <<'EOF'
import os
if os.environ.get("MUT_FAIL_EARLY"):
    raise SystemExit("mutator が書く前に失敗した")
path = os.environ["MUTATE_TARGET"] if os.environ.get("MUT_USE_TARGET_ENV") else "impl.txt"
open(path, "w").write(os.environ["MUT_TO"])
if os.environ.get("MUT_FAIL"):
    raise SystemExit("mutator が書いた後で失敗した")
EOF
printf 'tmp/\n' > "$REPO/.gitignore"
(
  cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE bash -c '
    git init -q && printf ok > impl.txt &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init'
) || { echo "[mutate-test] 判定不能: 一時 repo を作れない"; exit 2; }

# run_mutate <MUT_TO> [bin/mutate の前置オプション...]: 一時 repo で変異を回し、出力を last.out に、rc を返す
run_mutate() {
  local to="$1"
  shift
  (cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO="$to" \
    bin/mutate "$@" "変異 $to" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1
}
restored() { [ "$(cat "$REPO/impl.txt")" = "$1" ] && [ -z "$(git -C "$REPO" status --porcelain)" ]; }
# run_dir_of [出力]: bin/mutate が冒頭に出した、その run の出力の置き場
run_dir_of() { sed -n 's/^出力の置き場: //p' "${1:-$WORK/last.out}" | tail -1; }

for invalid_timeout in 0 -1 abc 99999999999999999999; do
  run_mutate broken --timeout "$invalid_timeout"; rc=$?
  expect "--timeout $invalid_timeout は使い方の誤り" 64 "$rc" "--timeout は 1 以上"
done
run_mutate broken --timeout; rc=$?
expect "--timeout に値が無ければ使い方の誤り" 64 "$rc" "--timeout は 1 以上"

rm -f "$REPO/tmp/reaper.args"
run_mutate hang --timeout 1 --baseline-count 2; rc=$?
restored ok || ng "時間切れ: 対象が戻っていない"
expect "成功の集計後でも変異の時間切れは判定不能" 3 "$rc" "判定: 判定不能 (mutant が --timeout 1 秒"
if [ -f "$REPO/tmp/reaper.args" ] && [ "$(cat "$REPO/tmp/reaper.args")" = "$(printf '%s\n' --kill --prefix "$REPO")" ]; then
  ok "時間切れだけ、自分の repo の孤児 helper を回収する"
else
  ng "時間切れ: reaper の引数が違う"
fi
if [ -n "$REAL_RUNTIMEOUT" ]; then
  if [ -f "$REPO/tmp/hang.pids" ]; then
    pids="$(cat "$REPO/tmp/hang.pids")"
    alive=0
    for pid in $pids; do kill -0 "$pid" 2>/dev/null && alive=1; done
    [ "$alive" = 0 ] && ok "実 runtimeout: TERM を無視する子と孫が残らない" || ng "実 runtimeout: 子孫が残った ($pids)"
  else
    ng "実 runtimeout: 子孫の fixture が実行されていない"
  fi
else
  echo "  NOTE - 子孫停止の結合検証は tests/devtool/test_mutate.sh --runtimeout <binary> で別途実行"
fi

(cd "$REPO" && printf hang > impl.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "baseline hangs")
run_mutate broken --timeout 1; rc=$?
restored hang || ng "baseline 時間切れ: 変異が当たった"
expect "baseline の時間切れは変異を当てず判定不能" 3 "$rc" "判定: 判定不能 (baseline が --timeout 1 秒"
(cd "$REPO" && printf ok > impl.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "baseline restored")

for signal_case in TERM:143 INT:130 HUP:129 QUIT:131; do
  signal_name="${signal_case%:*}"; want_rc="${signal_case#*:}"
  signal_mode=slow-term
  [ -z "$REAL_RUNTIMEOUT" ] || signal_mode=hang
  rm -f "$REPO/tmp/hang.pids" "$REPO/tmp/term.observed"
  (cd "$REPO" && exec env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO="$signal_mode" \
    perl -e '$SIG{INT} = $SIG{QUIT} = "DEFAULT"; exec { $ARGV[0] } @ARGV' \
    bin/mutate --baseline-count 2 --timeout 60 "中断中の中断" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1 &
  harness_pid=$!
  tt_wait_until test -f "$REPO/tmp/hang.pids" || true
  if [ -f "$REPO/tmp/hang.pids" ]; then
    kill -"$signal_name" "$harness_pid"
    tt_wait_until test -f "$REPO/tmp/term.observed" || true
    if [ -f "$REPO/tmp/term.observed" ]; then
      kill -"$signal_name" "$harness_pid" 2>/dev/null || true
      wait "$harness_pid"; rc=$?
      if restored ok && [ "$rc" = "$want_rc" ]; then ok "後始末中の2回目の${signal_name}でも停止後に復元する"; else ng "2回目の${signal_name}: rc=${rc}、対象が戻っていない"; fi
      pids="$(cat "$REPO/tmp/hang.pids")"
      alive=1
      tt_wait_until none_alive $pids && alive=0
      [ "$alive" = 0 ] && ok "${signal_name}中断でも子孫が残らない" || ng "${signal_name}中断で子孫が残った ($pids)"
    else
      kill -TERM "$harness_pid" 2>/dev/null || true; wait "$harness_pid"
      ng "${signal_name}中断: fixture にTERMが届かない"
    fi
  else
    kill -TERM "$harness_pid" 2>/dev/null || true; wait "$harness_pid"
    ng "${signal_name}中断: fixture が始まらない"
  fi
  # 壊す変異の検証でも、記録したfixture自身のPIDだけを後始末する。
  if [ -f "$REPO/tmp/hang.pids" ]; then
    for pid in $(cat "$REPO/tmp/hang.pids"); do kill -KILL "$pid" 2>/dev/null || true; done
  fi
  (cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git checkout -q HEAD -- impl.txt)
done

cp "$REPO/run-tests" "$REPO/run tests;literal.sh"
cat > "$REPO/stdin-tests" <<'EOF'
#!/bin/bash
printf 'input:%s\n' "$(cat)"
exec ./run-tests
EOF
chmod +x "$REPO/stdin-tests"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git add 'run tests;literal.sh' stdin-tests &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qm "argv and stdin fixtures")
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken \
  bin/mutate "literal command path" impl.txt mutator.py './run tests;literal.sh') > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "literal command path: 対象が戻っていない"
expect "コマンド1要素の空白・メタ文字をシェル構文として解釈しない" 1 "$rc" "判定: RED"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken \
  bin/mutate --baseline-count 2 "stdin" impl.txt mutator.py ./stdin-tests <<'EOF'
stdin payload
EOF
) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "stdin: 対象が戻っていない"
expect "stdinの検証コマンドも判定される" 1 "$rc" "判定: RED"
if grep -qF "input:stdin payload" "$(run_dir_of)/mutant.out"; then
  ok "検証コマンドにstdinを継承する"
else
  ng "検証コマンドがstdinを読めなかった"
fi

cat > "$REPO/fd-tests" <<'EOF'
#!/bin/bash
python3 - <<'PYEOF'
import os
try:
    os.fstat(3)
    print("fd3:open")
except OSError:
    print("fd3:closed")
PYEOF
exec ./run-tests
EOF
chmod +x "$REPO/fd-tests"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git add fd-tests &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qm "fd fixture")
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken \
  bin/mutate --baseline-count 2 "fd isolation" impl.txt mutator.py ./fd-tests) > "$WORK/last.out" 2>&1; rc=$?
expect "fd確認の検証コマンドも判定される" 1 "$rc" "判定: RED"
if grep -qF "fd3:closed" "$(run_dir_of)/mutant.out"; then
  ok "診断用fd3を検証コマンドに渡さない"
else
  ng "診断用fd3が検証コマンドへ漏れた"
fi

rm -f "$REPO/tmp/reaper.args"
run_mutate return124 --timeout 60; rc=$?
restored ok || ng "早期 rc124: 対象が戻っていない"
expect "上限前に payload が返す124は時間切れではない" 1 "$rc" "判定: RED"
[ ! -f "$REPO/tmp/reaper.args" ] && ok "通常の失敗では孤児 helper を止めない" || ng "通常の失敗で reaper を起こした"

(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE DEVTOOL_RUNTIMEOUT="$WORK/absent/runtimeout" MUT_TO=broken \
  bin/mutate "依存無し" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "runtimeout 不在: 対象が変わった"
expect "runtimeout 不在は上限なしで走らず判定不能" 3 "$rc" "runtimeout binary が無い"

run_mutate broken; rc=$?
restored ok || ng "RED: 対象ファイルが復元されていない"
expect "RED: 件数が同じで落ちたテストあり → rc 1、落ちたテストの名前を出す" 1 "$rc" "判定: RED" 'Swift Testing: "guard"' "実行件数: 2"

run_mutate ok-cosmetic; rc=$?
restored ok || ng "GREEN: 対象ファイルが復元されていない"
expect "GREEN: 件数が同じで全部 passed → rc 0" 0 "$rc" "判定: GREEN"

# 並列で走るテスト (集計行が無い形) でも RED / GREEN を読む (この行を読まないと件数が取れず判定不能になる)。件数は baseline の 2 に合わせてある
run_mutate parallel-broken; rc=$?
restored ok || ng "並列の RED: 対象ファイルが復元されていない"
expect "並列の RED: Test case … failed on の行で RED、名前を出す (issue 1418)" 1 "$rc" "判定: RED" "並列: GuardTests/guard()" "実行件数: 2"
run_mutate parallel-ok; rc=$?
restored ok || ng "並列の GREEN: 対象ファイルが復元されていない"
expect "並列の GREEN: Test case … passed on の行だけで GREEN (issue 1418)" 0 "$rc" "判定: GREEN"

run_mutate lost; rc=$?
restored ok || ng "件数の食い違い: 対象ファイルが復元されていない"
expect "件数が baseline と違えば判定不能 (rc 3)" 3 "$rc" "判定: 判定不能 (実行 1 / baseline 2"

run_mutate crash; rc=$?
restored ok || ng "集計行なし: 対象ファイルが復元されていない"
expect "変異の run に集計行が無ければ判定不能 (rc 3。RED と読ませない)" 3 "$rc" "実行件数を抽出できない"

run_mutate silent-fail; rc=$?
restored ok || ng "rc と落ちたテストの食い違い: 対象ファイルが復元されていない"
expect "rc≠0 なのに落ちたテストが無ければ判定不能 (rc 3)" 3 "$rc" "判定: 判定不能 (検証コマンドの rc=1"

run_mutate tail-cut; rc=$?
restored ok || ng "tail で失敗行が落ちた: 対象ファイルが復元されていない"
expect "rc 0 でも集計行が failed で失敗名が無ければ判定不能 (GREEN にしない)" 3 "$rc" "判定: 判定不能 (検証コマンドの rc=0"

run_mutate ok; rc=$?
restored ok || ng "変異が当たらない: 対象ファイルが変わった"
expect "変異が当たらなければ拒否 (rc 70)" 70 "$rc" "REFUSED: 変異が当たっていない"

run_mutate broken --baseline-count abc; rc=$?
expect "--baseline-count が整数でなければ使い方の誤り (rc 64)" 64 "$rc" "--baseline-count は 1 以上の整数"

run_mutate zero --baseline-count 0; rc=$?
restored ok || ng "--baseline-count 0: 対象ファイルが変わった"
expect "--baseline-count 0 は使い方の誤り (0 件の run と一致して何も検査しない GREEN を作らない)" 64 "$rc" "--baseline-count は 1 以上の整数"

rm -f "$REPO/tmp/runner-calls"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_FAIL=1 \
  bin/mutate "mutator の失敗" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "mutator が書いた後で失敗: 対象が戻っていない"
expect "mutator が書いた後で失敗したら判定不能 (rc 3。RED の rc 1 と区別する) で、検証コマンドを走らせない" 3 "$rc" \
  "判定: 判定不能 (mutator が失敗した。rc=1" "検証コマンドは走らせていない"
# 変異の検証コマンドを起動していないこと。偽のランナーの起動の記録で見る (出力のファイルの有無だけだと、リダイレクトせずに起動する退行を通す)。
# baseline の起動 (impl が ok) が記録されていることを先に見る (記録の取り違えで、無いことの確認が常に通らないように)
fail_dir="$(run_dir_of)"
if [ "$(grep -cx ok "$REPO/tmp/runner-calls" 2>/dev/null)" != 1 ] || [ -z "$fail_dir" ] || [ ! -s "$fail_dir/baseline.out" ]; then
  ng "mutator の失敗: ランナーの起動が baseline の 1 回でない、またはこの run の baseline の出力が無い: $(tr '\n' ' ' < "$REPO/tmp/runner-calls" 2>/dev/null) ${fail_dir:-置き場の行が無い}"
elif grep -qx broken "$REPO/tmp/runner-calls" || [ -e "$fail_dir/mutant.out" ]; then
  ng "mutator が失敗したのに変異の検証コマンドを走らせた"
else
  ok "mutator が失敗したら変異の検証コマンドを走らせない (ランナーの起動は baseline の 1 回だけ)"
fi

# --- 出力の置き場 ---
# 名前に使う日時を固定する偽の date (日時だけで名前を作る退行を、秒の境目に左右されずに必ず衝突させる)。それ以外の呼び方は本物へ渡す
real_date="$(command -v date)"
mkdir -p "$WORK/fixed-clock"
printf '#!/bin/bash\n[ "${1:-}" = +%%Y%%m%%d-%%H%%M%%S ] && { echo 20200101-000000; exit 0; }\nexec %q "$@"\n' "$real_date" > "$WORK/fixed-clock/date"
chmod +x "$WORK/fixed-clock/date"
run_mutate_fixed() { PATH="$WORK/fixed-clock:$PATH" run_mutate "$@"; }
run_mutate_fixed broken; rc=$?
first_dir="$(run_dir_of)"
run_mutate_fixed broken; rc2=$?
second_dir="$(run_dir_of)"
if [ "$rc" = 1 ] && [ "$rc2" = 1 ] && [ -n "$first_dir" ] && [ "$first_dir" != "$second_dir" ] &&
  grep -qF 'with 1 issue' "$first_dir/mutant.out" && grep -qF 'passed' "$first_dir/baseline.out" &&
  grep -qF 'with 1 issue' "$second_dir/mutant.out"; then
  ok "続けて回しても、前の run の baseline と変異の出力が別の置き場に残る"
else
  ng "続けて回すと前の run の出力が残らない (rc=$rc/$rc2, $first_dir / $second_dir)"
fi
case "$first_dir" in
  "$REPO"/tmp/mutate/test-session/run-*) ok "置き場は skill-tmpdir が返す場所の下" ;;
  *) ng "置き場が skill-tmpdir の返す場所の下に無い: $first_dir" ;;
esac

# 別の repo (worktree が本体 checkout の tmp/ を共有する形) から同時に始めても、互いの出力を上書きしない
REPO2="$WORK/repo2"
(env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git clone -q "$REPO" "$REPO2") || ng "2 つ目の repo を作れない"
mkdir -p "$WORK/shared"
pids=()
for r in "$REPO" "$REPO2"; do
  (cd "$r" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE PATH="$WORK/fixed-clock:$PATH" MUTATE_TEST_TMPDIR_ROOT="$WORK/shared" MUT_TO=broken \
    bin/mutate "同時" impl.txt mutator.py ./run-tests) > "$WORK/par-$(basename "$r").out" 2>&1 &
  pids+=("$!")
done
par_rcs=""
for pid in "${pids[@]}"; do wait "$pid"; par_rcs="$par_rcs$?"; done
d1="$(run_dir_of "$WORK/par-repo.out")"
d2="$(run_dir_of "$WORK/par-repo2.out")"
if [ "$par_rcs" = 11 ] && [ -n "$d1" ] && [ -n "$d2" ] && [ "$d1" != "$d2" ] &&
  [ "$(grep -c 'with 1 issue' "$d1/mutant.out")" = 2 ] && [ "$(grep -c 'with 1 issue' "$d2/mutant.out")" = 2 ] &&
  [ "$(grep -c 'passed' "$d1/baseline.out")" = 2 ] && [ "$(grep -c 'passed' "$d2/baseline.out")" = 2 ]; then
  ok "同じ置き場を使う 2 本を同時に回しても、別の場所に欠けずに残る"
else
  ng "同時に回した 2 本の出力が衝突した (rc=$par_rcs, $d1 / $d2)"
fi

# 全文のログ (run-logged-command の complete log: の行) を run の置き場へ写す
echo "outside" > "$WORK/outside.log"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_INNER_LOG=1 MUT_OUTSIDE_LOG="$WORK/outside.log" \
  bin/mutate "全文の写し" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
log_dir="$(run_dir_of)"
expect "全文のログを指す行があっても判定は変わらない" 1 "$rc" "判定: RED" "全文のログの写し:" "repo の外か相対パスなので写さない"
if [ "$(cat "$log_dir/baseline-1-inner-ok.log" 2>/dev/null)" = "inner full ok" ] &&
  [ "$(cat "$log_dir/mutant-1-inner-broken.log" 2>/dev/null)" = "inner full broken" ]; then
  ok "baseline と変異の run の全文のログを、それぞれ run の置き場へ写す"
else
  ng "全文のログが run の置き場に写っていない: $log_dir"
fi
copies="$(find "$log_dir" -name '*.log' | wc -l | tr -d ' ')"
if [ "$copies" = 2 ] && ! grep -rqx outside "$log_dir"; then
  ok "同じパスは 1 回だけ写し、repo の外を指す行は写さない"
else
  ng "写しが $copies 本 (期待 2)、または repo の外のファイルを写した: $log_dir"
fi

# signal で終わった run も、判定不能で抜ける前に全文を写す
cat > "$REPO/signal-tests" <<'EOF'
#!/bin/bash
inner="$PWD/tmp/inner-signal-$(cat impl.txt).log"
mkdir -p tmp
echo "inner partial $(cat impl.txt)" > "$inner"
echo "make test-only: complete log: $inner"
[ "$(cat impl.txt)" = broken ] && exit 137
echo '✔ Test run with 2 tests in 1 suite passed after 0.001 seconds.'
EOF
chmod +x "$REPO/signal-tests"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git add signal-tests &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qm "signal fixture")
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken \
  bin/mutate "signal の全文" impl.txt mutator.py ./signal-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "signal の全文: 対象が戻っていない"
expect "signal で終わった変異の run は判定不能" 3 "$rc" "rc=137"
if [ "$(cat "$(run_dir_of)/mutant-1-inner-signal-broken.log" 2>/dev/null)" = "inner partial broken" ]; then
  ok "signal で終わった run の全文も、判定不能で抜ける前に写す"
else
  ng "signal で終わった run の全文が写っていない: $(run_dir_of)"
fi

# テストは走ったが lock の文言を引用しただけの run は、元の理由を残して注記だけ足す
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_QUOTE_LOCK=broken \
  bin/mutate "lock の文言の引用" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "lock の文言の引用: 対象が戻っていない"
expect "件数が取れた run では、lock の文言があっても元の理由 (件数の食い違い) を残す" 3 "$rc" \
  "判定: 判定不能 (実行 1 / baseline 2" "注記: 出力に test の lock の文言がある"
grep -qF "test の lock が空いていない" "$WORK/last.out" && ng "テストが走った run を lock 待ちと出した"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_QUOTE_LOCK=broken MUT_QUOTE_LOCK_NO_SUMMARY=1 \
  bin/mutate "集計の前に落ちた run の lock の文言の引用" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "集計の前の lock の文言の引用: 対象が戻っていない"
expect "集計行が無くても落ちたテストの行がある run は、lock 待ちと読まず元の理由を残す" 3 "$rc" \
  "実行件数を抽出できない" "注記: 出力に test の lock の文言がある"
grep -qF "test の lock が空いていない" "$WORK/last.out" && ng "落ちたテストの行がある run を lock 待ちと出した"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_QUOTE_LOCK=ok MUT_QUOTE_LOCK_NO_SUMMARY=1 \
  bin/mutate "baseline の集計の前に落ちた run の lock の文言" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "baseline の集計の前の lock の文言: 対象が変わった"
expect "baseline でも、落ちたテストの行がある run は lock 待ちと読まず元の理由を残す" 3 "$rc" \
  "baseline が green でない" "注記: 出力に test の lock の文言がある"
grep -qF "test の lock が空いていない" "$WORK/last.out" && ng "baseline の落ちたテストの行がある run を lock 待ちと出した"
# 行の途中に文言を引用しただけ (集計行も落ちたテストの行も無い) の run は、lock の文言として数えない
cat > "$REPO/quote-only-tests" <<'EOF'
#!/bin/bash
[ "$(cat impl.txt)" = broken ] && { echo 'fixture: run-with-test-profile-residue: another checked run owns /tmp/x.lock; retry after it ends'; echo 'Fatal error'; exit 1; }
exec ./run-tests
EOF
chmod +x "$REPO/quote-only-tests"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git add quote-only-tests &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qm "quote-only fixture")
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken \
  bin/mutate "行の途中の引用" impl.txt mutator.py ./quote-only-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "行の途中の引用: 対象が戻っていない"
expect "行の途中に lock の文言を引用しただけの run は、元の理由のまま" 3 "$rc" "実行件数を抽出できない"
grep -qE "test の lock が空いていない|注記: 出力に test の lock" "$WORK/last.out" && ng "行の途中の引用を lock の文言として数えた"
# lock の後ろの run-logged-command が complete log: を出した run は、件数が取れなくても lock 待ちと読まない
cat > "$REPO/logged-lock-tests" <<'EOF'
#!/bin/bash
if [ "$(cat impl.txt)" = broken ]; then
  echo 'run-with-test-profile-residue: another checked run owns /tmp/x.lock; retry after it ends'
  echo "make test-only: complete log: $PWD/tmp/none.log"
  exit 2
fi
exec ./run-tests
EOF
chmod +x "$REPO/logged-lock-tests"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git add logged-lock-tests &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qm "logged-lock fixture")
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken \
  bin/mutate "complete log の後の lock の文言" impl.txt mutator.py ./logged-lock-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "complete log の後の lock の文言: 対象が戻っていない"
expect "complete log: の行がある run は、lock の文言があっても lock 待ちと読まない" 3 "$rc" "注記: 出力に test の lock の文言がある"
grep -qF "test の lock が空いていない" "$WORK/last.out" && ng "complete log: の行がある run を lock 待ちと出した"

# test の lock が空いておらず、テストを始めずに終わった run は、理由を lock 待ちと出す (rc 75 の直接と、make に包まれた rc 2 の両方)
for lock_rc in 75 2; do
  (cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_LOCK_AT=ok MUT_LOCK_RC=$lock_rc \
    bin/mutate "baseline の lock" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
  restored ok || ng "baseline の lock (rc $lock_rc): 対象が変わった"
  expect "baseline が lock で始まらなければ、理由を lock 待ちと出す (rc $lock_rc)" 3 "$rc" \
    "test の lock が空いていない" "another checked run owns"
  (cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_LOCK_AT=broken MUT_LOCK_RC=$lock_rc \
    bin/mutate "変異の run の lock" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
  restored ok || ng "変異の run の lock (rc $lock_rc): 対象が戻っていない"
  expect "変異の run が lock で始まらなければ、理由を lock 待ちと出す (rc $lock_rc)" 3 "$rc" \
    "test の lock が空いていない" "another checked run owns"
done

(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_FAIL_EARLY=1 \
  bin/mutate "mutator が書く前に失敗" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "mutator が書く前に失敗: 対象が変わった"
expect "mutator が書く前に失敗しても判定不能 (rc 3)" 3 "$rc" "判定: 判定不能 (mutator が失敗した。rc=1"

(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken MUT_USE_TARGET_ENV=1 \
  bin/mutate "MUTATE_TARGET で対象を受け取る" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "MUTATE_TARGET: 対象が戻っていない"
expect "mutator は対象のパスを MUTATE_TARGET で受け取れる" 1 "$rc" "判定: RED"

run_mutate stage; rc=$?
restored ok || ng "検証コマンドの git add: index か作業ツリーに変異が残った"
expect "検証コマンドが対象を git add しても index ごと戻す" 1 "$rc" "復元 OK" "判定: RED"

run_mutate broken --baseline-count 3; rc=$?
restored ok || ng "--baseline-count: 対象ファイルが復元されていない"
expect "--baseline-count と実行件数が違えば判定不能 (rc 3)" 3 "$rc" "実行 2 / baseline 3"

(cd "$REPO" && printf broken > impl.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "baseline red")
run_mutate ok; rc=$?
restored broken || ng "baseline red: 対象ファイルが変わった (変異を当ててはいけない)"
expect "baseline が green でなければ変異を当てずに判定不能 (rc 3)" 3 "$rc" "変異は当てていない"
(cd "$REPO" && printf ok > impl.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "back to ok")

(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken bin/mutate "対象が ." . mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "対象が .: 作業ツリーが変わった"
expect "対象が 1 ファイルでなければ拒否 (rc 70。復元が他のファイルを巻き込まない)" 70 "$rc" "tracked な 1 ファイル (symlink でない) ではない"

(cd "$REPO" && ln -s impl.txt link.txt && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git add link.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qm "tracked symlink")
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken bin/mutate "symlink" link.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
restored ok || ng "対象が symlink: 参照先が変わった"
expect "対象が symlink なら拒否 (rc 70。git は symlink 自体しか見ない)" 70 "$rc" "symlink でない"

printf 'untracked\n' > "$REPO/impl*.txt"
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=broken bin/mutate "glob" 'impl*.txt' mutator.py ./run-tests) > "$WORK/last.out" 2>&1; rc=$?
rm -f "$REPO/impl*.txt"
restored ok || ng "対象名に glob 文字: tracked のファイルが変わった"
expect "対象名の * を pathspec として展開しない (untracked の impl*.txt を tracked と読まない。rc 70)" 70 "$rc" "tracked な 1 ファイル (symlink でない) ではない"

start="$(git -C "$REPO" rev-parse HEAD)"
run_mutate commit; rc=$?
if [ "$(cat "$REPO/impl.txt")" = ok ] && grep -qF "復元 OK" "$WORK/last.out"; then
  ok "検証中に HEAD が進んでも、開始時の commit の内容に戻す"
else
  ng "検証中の HEAD の移動: impl=[$(cat "$REPO/impl.txt")] rc=${rc}"
fi
(cd "$REPO" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git reset -q --hard "$start")

rm -f "$REPO/started" "$REPO/release"
(cd "$REPO" && exec env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE MUT_TO=wait \
  bin/mutate "TERM" impl.txt mutator.py ./run-tests) > "$WORK/last.out" 2>&1 &
mutate_pid=$!
TT_WAIT_TICKS=200 tt_wait_until test -f "$REPO/started" || true
if [ -f "$REPO/started" ]; then
  kill -TERM "$mutate_pid"
  : > "$REPO/release"
  wait "$mutate_pid"; rc=$?
  rm -f "$REPO/started" "$REPO/release"
  if restored ok && [ "$rc" = 143 ]; then ok "検証の最中の TERM でも対象を戻して 143 で抜ける"; else ng "TERM: rc=${rc} impl=[$(cat "$REPO/impl.txt")]"; fi
else
  kill "$mutate_pid" 2>/dev/null; wait "$mutate_pid" 2>/dev/null
  ng "TERM: 偽のランナーが始まらない (上限 20 秒)"
fi

for masked_mode in masked-name-only masked-run-only; do
  (cd "$REPO" && printf '%s' "$masked_mode" > impl.txt &&
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "$masked_mode baseline")
  run_mutate broken; rc=$?
  restored "$masked_mode" || ng "$masked_mode: 対象ファイルが変わった"
  expect "baseline の出力に $masked_mode の失敗があれば green と見なさない (rc 3)" 3 "$rc" "変異は当てていない"
done

(cd "$REPO" && printf masked > impl.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "masked baseline")
run_mutate broken; rc=$?
restored masked || ng "失敗を隠した baseline: 対象ファイルが変わった"
expect "baseline が rc 0 でも出力に失敗があれば green と見なさない (rc 3)" 3 "$rc" "変異は当てていない"

(cd "$REPO" && printf zero > impl.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "zero tests")
run_mutate ok; rc=$?
restored zero || ng "baseline 0 件: 対象ファイルが変わった (変異を当ててはいけない)"
expect "baseline が 0 件なら変異を当てずに判定不能 (rc 3)" 3 "$rc" "変異は当てていない"
(cd "$REPO" && printf ok > impl.txt &&
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE git -c user.email=t@t -c user.name=t commit -qam "back to ok again")

printf 'dirty' > "$REPO/impl.txt"
run_mutate broken; rc=$?
[ "$(cat "$REPO/impl.txt")" = dirty ] || ng "未 commit の変更: 変更が消えた"
expect "未 commit の変更があれば拒否 (rc 70)" 70 "$rc" "REFUSED: 未 commit の変更がある"

echo ""
echo "[mutate-test] $PASS passed / $FAIL failed"
[ "$FAIL" -eq 0 ] && [ "$PASS" -gt 0 ]
