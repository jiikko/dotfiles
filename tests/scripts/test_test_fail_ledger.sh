#!/usr/bin/env bash
# make test の赤で落ちたテストが scripts/test_fail_ledger.sh の記録に 1 行ずつ残り、緑では何も書かず、
# テスト名ごとに数えて出せることを固定する。runner (直列・並列・go) を実際に動かして見る。
#
# runner が起動するテストへ TEST_FAIL_LEDGER を空で渡すことも見る。渡さないと、runner をわざと落とすテスト
# (このファイル自身も含む) の中の make が、開発機の本物の記録に偽の失敗を足す。
set -uo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
LEDGER_SH="$ROOT_DIR/scripts/test_fail_ledger.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/fail-ledger.XXXXXX") || { echo "✗ mktemp -d 失敗"; exit 1; }
trap 'rm -rf "$work"' EXIT
# runner は名前の // を潰して記録するので、比べる側も潰した実パスにする (TMPDIR は末尾に / を持つことがある)
work=$(cd "$work" && pwd -P) || exit 1

fails=0
ok() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

# CI の上でも記録の経路を通すため CI を外す。HOME / XDG も一時 dir へ倒し、既定のパスが本物へ向かないようにする。
run_ledger() { env -u CI HOME="$work/home" XDG_CACHE_HOME="$work/xdg" "$LEDGER_SH" "$@"; }
run_make() {
  env -u CI -u MAKEFLAGS -u MAKELEVEL -u MFLAGS HOME="$work/home" XDG_CACHE_HOME="$work/xdg" \
    make -C "$ROOT_DIR" --no-print-directory "$@"
}

# --- record ---
list="$work/list"
: > "$list"
TEST_FAIL_LEDGER="$work/a.tsv" run_ledger record sh "$list"
[ ! -e "$work/a.tsv" ] && ok "record: 一覧が空なら何も書かない" || bad "record: 一覧が空なのに書いた"

printf 'tests/x/test_one.sh\ntests/x/test_two.sh\n' > "$list"
TEST_FAIL_LEDGER="$work/a.tsv" run_ledger record sh "$list"
lines=$(wc -l < "$work/a.tsv" 2>/dev/null | tr -d ' ')
[ "${lines:-0}" = 2 ] && ok "record: 2 件の一覧で 2 行" || bad "record: 行数が ${lines:-0} (期待 2)"
fields=$(awk -F'\t' '{print NF}' "$work/a.tsv" 2>/dev/null | sort -u)
[ "$fields" = 5 ] && ok "record: 各行が 5 列" || bad "record: 列数が [$fields] (期待 5)"
awk -F'\t' 'NR==1 && $1 ~ /^[0-9]{4}-[0-9]{2}-[0-9]{2}T/ && $3 ~ /^[0-9.]+$/ && $4 == "sh" && $5 == "tests/x/test_one.sh" {f=1} END {exit !f}' "$work/a.tsv" \
  && ok "record: 日時・load average・種類・名前が入る" || bad "record: 1 行目の中身が違う: $(head -1 "$work/a.tsv")"

TEST_FAIL_LEDGER="" run_ledger record sh "$list"
[ ! -e "$work/xdg/dotfiles-test-fails/log.tsv" ] && ok "record: TEST_FAIL_LEDGER が空なら書かない" || bad "record: 空なのに既定の場所へ書いた"
env -u TEST_FAIL_LEDGER CI=1 HOME="$work/home" XDG_CACHE_HOME="$work/xdg" "$LEDGER_SH" record sh "$list"
[ ! -e "$work/xdg/dotfiles-test-fails/log.tsv" ] && ok "record: CI では書かない" || bad "record: CI なのに書いた"
env -u TEST_FAIL_LEDGER -u CI HOME="$work/home" XDG_CACHE_HOME="$work/xdg" "$LEDGER_SH" record go "$list"
[ "$(wc -l < "$work/xdg/dotfiles-test-fails/log.tsv" 2>/dev/null | tr -d ' ')" = 2 ] \
  && ok "record: 未設定なら XDG_CACHE_HOME の下の既定の場所へ書く" || bad "record: 既定の場所に書かれていない"
err=$(TEST_FAIL_LEDGER="" run_ledger record sh "$list" 2>&1); rc=$?
[ "$rc" = 0 ] && [ -z "$err" ] && ok "record: 空の記録先は書こうとしない (rc=0・stderr なし)" || bad "record: 空の記録先で rc=${rc} [$err]"

printf './tests//x/test_one.sh\n%s/tests/x/test_one.sh\n' "$ROOT_DIR" > "$list"
TEST_FAIL_LEDGER="$work/norm.tsv" run_ledger record sh "$list"
[ "$(cut -f5 "$work/norm.tsv" | sort -u)" = tests/x/test_one.sh ] \
  && ok "record: sh の名前は repo root からの相対パスに揃える" || bad "record: 名前が揃っていない: [$(cut -f5 "$work/norm.tsv")]"

# --- top ---
mkdir -p "$work/issues/done"
printf 'test_two.sh が落ちた\n' > "$work/issues/done/422-x.md"
printf 'foo_test_two.sh と TestTopper と gononame と (テスト名なし)\n' > "$work/issues/500-other.md"
printf 'tests/x/test_two.sh\n' > "$list"
TEST_FAIL_LEDGER="$work/a.tsv" run_ledger record sh "$list"
top_out=$(TEST_FAIL_LEDGER="$work/a.tsv" TEST_FAIL_LEDGER_ISSUES_DIR="$work/issues" run_ledger top 2>&1)
first=$(printf '%s\n' "$top_out" | sed -n 3p)
case "$first" in
  *"   2  "*"tests/x/test_two.sh  (issue 422)") ok "top: 回数の多い順に、名前を書いた issue を添えて出す" ;;
  *) bad "top: 先頭の行が違う: [$first]"; printf '%s\n' "$top_out" ;;
esac
[ "$(printf '%s\n' "$top_out" | sed -n 4p | awk '{print $1}')" = 1 ] \
  && ok "top: 2 番目は 1 回のテスト" || bad "top: 2 番目の行が違う: $(printf '%s\n' "$top_out" | sed -n 4p)"
top_n=$(TEST_FAIL_LEDGER="$work/a.tsv" run_ledger top 1 | tail -n +3 | wc -l | tr -d ' ')
[ "$top_n" = 1 ] && ok "top N: N 件で切る" || bad "top N: $top_n 件出た (期待 1)"
for n in １ 0; do
  TEST_FAIL_LEDGER="$work/a.tsv" run_ledger top "$n" > /dev/null 2>&1; rc=$?
  [ "$rc" = 2 ] && ok "top N: $n は件数として受けない" || bad "top N: $n で rc=${rc} (期待 2)"
done
grep -qF '記録はまだ無い' <<< "$(TEST_FAIL_LEDGER="$work/none.tsv" run_ledger top)" \
  && ok "top: 記録が無ければそう言う" || bad "top: 記録が無いときの出力が違う"

# --- 直列 runner (make test-dir) ---
# 外側の runner (make test の腕) の集計先が環境に在る状況を作る。テストへ空で渡さないと、ここへ偽の失敗が足される。
export TEST_FAILS_SUMMARY="$work/outer-fails" TEST_SKIPS_SUMMARY="$work/outer-skips"
mkdir -p "$work/suite"
seen="$work/seen-ledger"
printf '#!/bin/sh\nprintf "%%s|%%s|%%s" "${TEST_FAIL_LEDGER-unset}" "${TEST_FAILS_SUMMARY-unset}" "${TEST_SKIPS_SUMMARY-unset}" > "%s"\nexit 1\n' "$seen" > "$work/suite/test_a_fails.sh"
printf '#!/bin/sh\nexit 0\n' > "$work/suite/test_b_passes.sh"
chmod +x "$work/suite/"test_*.sh
TEST_FAIL_LEDGER="$work/serial.tsv" run_make test-dir DIR="$work/suite" > "$work/serial.out" 2>&1
grep -qF "✗ 失敗したテスト:" "$work/serial.out" || { bad "前提が崩れた: 直列 runner が失敗を出していない"; cat "$work/serial.out"; }
if [ "$(cut -f5 "$work/serial.tsv" 2>/dev/null)" = "$work/suite/test_a_fails.sh" ]; then
  ok "直列 runner: 落ちたテストだけが 1 行残る"
else
  bad "直列 runner: 記録が違う: [$(cat "$work/serial.tsv" 2>/dev/null)]"; cat "$work/serial.out"
fi
[ "$(cat "$seen" 2>/dev/null)" = "||" ] && ok "直列 runner: テストへ記録先と集計先を空で渡す" \
  || bad "直列 runner: テストが見た 記録先|失敗の集計先|skip の集計先 が [$(cat "$seen" 2>/dev/null)] (期待 ||)"

rm -f "$work/suite/test_a_fails.sh"
TEST_FAIL_LEDGER="$work/green.tsv" run_make test-dir DIR="$work/suite" > "$work/green.out" 2>&1 \
  || { bad "前提が崩れた: 緑の suite が落ちた"; cat "$work/green.out"; }
[ ! -e "$work/green.tsv" ] && ok "直列 runner: 緑の run では何も書かない" || bad "直列 runner: 緑なのに書いた"

# --- 並列 runner ---
rm -f "$seen"
printf '#!/bin/sh\nprintf "%%s|%%s|%%s" "${TEST_FAIL_LEDGER-unset}" "${TEST_FAILS_SUMMARY-unset}" "${TEST_SKIPS_SUMMARY-unset}" > "%s"\nexit 1\n' "$seen" > "$work/suite/test_c_fails.sh"
chmod +x "$work/suite/test_c_fails.sh"
TEST_FAIL_LEDGER="$work/par.tsv" run_make test-discovered-heavy CI_HEAVY_TEST_DIRS="$work/suite" > "$work/par.out" 2>&1
grep -qF "[並列] 対象 2 件" "$work/par.out" || { bad "前提が崩れた: 並列 runner が suite を回していない"; cat "$work/par.out"; }
if [ "$(cut -f5 "$work/par.tsv" 2>/dev/null)" = "$work/suite/test_c_fails.sh" ]; then
  ok "並列 runner: 落ちたテストだけが 1 行残る"
else
  bad "並列 runner: 記録が違う: [$(cat "$work/par.tsv" 2>/dev/null)]"; cat "$work/par.out"
fi
[ "$(cat "$seen" 2>/dev/null)" = "||" ] && ok "並列 runner: テストへ記録先と集計先を空で渡す" \
  || bad "並列 runner: テストが見た 記録先|失敗の集計先|skip の集計先 が [$(cat "$seen" 2>/dev/null)] (期待 ||)"

# 結果を報告しなかったテスト (runner の故障) は記録しない。テストが自分を起こした sh を殺すと、xargs が残りを捨てて打ち切る
mkdir -p "$work/suite_abort"
printf '#!/bin/sh\nkill -KILL "$PPID"\n' > "$work/suite_abort/test_k_kills_runner.sh"
chmod +x "$work/suite_abort/test_k_kills_runner.sh"
TEST_FAIL_LEDGER="$work/abort.tsv" run_make test-discovered-heavy CI_HEAVY_TEST_DIRS="$work/suite_abort" > "$work/abort.out" 2>&1
grep -qF "結果を報告しなかったテスト" "$work/abort.out" || { bad "前提が崩れた: 並列 runner が「結果を報告しなかった」を出していない"; cat "$work/abort.out"; }
[ ! -e "$work/abort.tsv" ] && ok "並列 runner: 結果を報告しなかったテストは記録しない" \
  || bad "並列 runner: 結果を報告しなかったテストを記録した: [$(cat "$work/abort.tsv")]"

# 中断した run は記録しない。テストの中から runner のシェルへ TERM を送り、そのテストは落ちて終わる (INT は使わない:
# 非対話のシェルの下では INT を無視した状態で起動していることがあり、無視で始まったシグナルは trap できない)。
# runner のシェルは、このテスト ($LEDGER_TEST_PID) より下で command line に suite_td= を持つ祖先の一番上
# (直列の while は同じ command line の subshell で、subshell は trap を受け継がないので、そちらへ送ると印が立たずに
# loop ごと死ぬ)。上限を置かないと、make test の中で走るときに外側の本物の runner まで辿って TERM を送る。
export LEDGER_TEST_PID=$$
mkdir -p "$work/suite_int"
cat > "$work/suite_int/test_i_interrupts_runner.sh" <<SH
#!/bin/sh
p=\$PPID; target=; reached=
while [ "\$p" -gt 1 ]; do
  [ "\$p" = "\$LEDGER_TEST_PID" ] && { reached=1; break; }
  cmd=\$(ps -ww -o command= -p "\$p") || break
  case "\$cmd" in *suite_td=*) target=\$p ;; esac
  p=\$(ps -o ppid= -p "\$p" | tr -d ' ')
done
[ -n "\$reached" ] || target=
printf '%s' "\$target" > "$work/int-target"
[ -n "\$target" ] && kill -TERM "\$target"
exit 1
SH
chmod +x "$work/suite_int/test_i_interrupts_runner.sh"
for arm in 直列 並列; do
  rm -f "$work/int.tsv" "$work/int-target"
  if [ "$arm" = 直列 ]; then
    TEST_FAIL_LEDGER="$work/int.tsv" run_make test-dir DIR="$work/suite_int" > "$work/int.out" 2>&1
  else
    TEST_FAIL_LEDGER="$work/int.tsv" run_make test-discovered-heavy CI_HEAVY_TEST_DIRS="$work/suite_int" > "$work/int.out" 2>&1
  fi
  [ -s "$work/int-target" ] || { bad "前提が崩れた: $arm runner のシェルが見つからない"; cat "$work/int.out"; }
  grep -qF '中断した run なので' "$work/int.out" || { bad "前提が崩れた: $arm runner が中断を受けていない"; cat "$work/int.out"; }
  [ ! -e "$work/int.tsv" ] && ok "$arm runner: 中断した run の落ちたテストは記録しない" \
    || bad "$arm runner: 中断した run の落ちたテストを記録した: [$(cat "$work/int.tsv")]"
done

# --- go (run_go_projects の test) ---
# 本物の go は回さない: run_go_projects は go の有無と std の温めにしか go を使わないので、何もしない go を置く。
mkdir -p "$work/bin" "$work/gonamed" "$work/gononame" "$work/gogreen"
printf '#!/bin/sh\nexit 0\n' > "$work/bin/go"; chmod +x "$work/bin/go"
# go test ./... の出力の形: package ごとに `ok  \t` / `FAIL\t` / `?   \t` の行で閉じる。ex/b は build に失敗して名前が出ない
printf -- '--- FAIL: TestTop (0.01s)\n    --- FAIL: TestTop/sub (0.00s)\nFAIL\nFAIL\tex/a\t0.1s\nFAIL\tex/b [build failed]\nok  \tex/c\t0.1s\n?   \tex/d\t[no test files]\nFAIL\n' > "$work/gonamed/out.txt"
printf 'test:\n\t@cat out.txt; exit 1\n' > "$work/gonamed/Makefile"
printf 'test:\n\t@echo "build failed"; exit 1\n' > "$work/gononame/Makefile"
printf 'test:\n\t@echo ok\n' > "$work/gogreen/Makefile"
PATH="$work/bin:$PATH" TEST_FAIL_LEDGER="$work/go.tsv" \
  run_make test-go GO_PROJECT_DIRS="$work/gonamed $work/gononame $work/gogreen" > "$work/go.out" 2>&1
grep -qF "✗ [go-test] 失敗したプロジェクト:" "$work/go.out" || { bad "前提が崩れた: go の runner が失敗を出していない"; cat "$work/go.out"; }
expected="$work/gonamed ex/a TestTop
$work/gonamed ex/b (テスト名なし)
$work/gononame (テスト名なし)"
if [ "$(cut -f5 "$work/go.tsv" 2>/dev/null)" = "$expected" ] && [ "$(cut -f4 "$work/go.tsv" | sort -u)" = go ]; then
  ok "go: トップレベルのテスト名を module つきで残し、名前の無い失敗は package ごとに残す"
else
  bad "go: 記録が違う: [$(cat "$work/go.tsv" 2>/dev/null)]"; cat "$work/go.out"
fi
PATH="$work/bin:$PATH" TEST_FAIL_LEDGER="$work/golint.tsv" \
  run_make test-go-lint GO_PROJECT_DIRS="$work/gonamed" > "$work/golint.out" 2>&1
[ ! -e "$work/golint.tsv" ] && ok "go: lint の失敗は記録しない" || bad "go: lint の失敗を記録した"
top_go=$(TEST_FAIL_LEDGER="$work/go.tsv" TEST_FAIL_LEDGER_ISSUES_DIR="$work/issues" run_ledger top)
grep -qF 'issue' <<< "$(tail -n +3 <<< "$top_go")" \
  && { bad "top: 単語として一致しない issue・テスト名の無い行に issue を付けた"; printf '%s\n' "$top_go"; } \
  || ok "top: 単語として一致する issue だけを付け、テスト名の無い行には付けない"

# --- bats (test-bats) ---
# 本物の bats は回さない: 全部の .bats を落とす偽の bats を置き、記録と、bats へ渡す環境を見る。
nbats=$(cd "$ROOT_DIR" && find tests -type f -name '*.bats' ! -name '*helper*' | wc -l | tr -d ' ')
[ "${nbats:-0}" -gt 0 ] || bad "前提が崩れた: tests の下に .bats が無い"
rm -f "$seen"
printf '#!/bin/sh\nprintf "%%s|%%s|%%s" "${TEST_FAIL_LEDGER-unset}" "${TEST_FAILS_SUMMARY-unset}" "${TEST_SKIPS_SUMMARY-unset}" > "%s"\nexit 1\n' "$seen" > "$work/bin/bats"
chmod +x "$work/bin/bats"
PATH="$work/bin:$PATH" TEST_FAIL_LEDGER="$work/bats.tsv" run_make test-bats > "$work/bats.out" 2>&1
grep -qF "✗ 失敗した bats:" "$work/bats.out" || { bad "前提が崩れた: test-bats が失敗を出していない"; cat "$work/bats.out"; }
if [ "$(wc -l < "$work/bats.tsv" 2>/dev/null | tr -d ' ')" = "$nbats" ] && [ "$(cut -f4 "$work/bats.tsv" | sort -u)" = bats ]; then
  ok "bats: 落ちた .bats を種類 bats で 1 本 1 行残す"
else
  bad "bats: 記録が違う (期待 ${nbats} 行): [$(cat "$work/bats.tsv" 2>/dev/null)]"
fi
[ "$(cat "$seen" 2>/dev/null)" = "||" ] && ok "bats: bats へ記録先と集計先を空で渡す" \
  || bad "bats: bats が見た 記録先|失敗の集計先|skip の集計先 が [$(cat "$seen" 2>/dev/null)] (期待 ||)"

[ "$fails" -eq 0 ] || exit 1
