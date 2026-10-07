#!/usr/bin/env bash
# scripts/check_cdpath_before_cd_capture.sh (CDPATH を外さない `$(cd …)` を落とす検査) の契約を fixture で固定する。
#
# 守るもの: unset CDPATH の前の $(cd を落とす / unset の後・CDPATH='' の置換・コメント・allow の印は通す /
# zsh のファイルも見る / 発見が壊れたら緑にしない / Makefile から配線されている /
# 検査が落とす形を、CDPATH を export した本物の bash が実際に読み違える (正解役)。
# fixture は検査に渡す shell の文字列なので `$(cd` を展開させない。
# shellcheck disable=SC2016
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_cdpath_before_cd_capture.sh"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

# ターゲットの直下の行 (= そのターゲットのレシピ) が検査の呼び出しであることまで見る (別のターゲットに置かれても通さない)
recipe="$(awk 'prev { print; exit } /^test-cdpath-capture:/ { prev = 1 }' "$ROOT_DIR/Makefile")"
if [ "$recipe" = $'\t@scripts/check_cdpath_before_cd_capture.sh' ] \
  && grep -qE 'run_make_targets_parallel\.sh .*test-cdpath-capture' \
     <<< "$(awk '/^test-lint:/ { in_t = 1; next } in_t && /^[^\t]/ { exit } in_t' "$ROOT_DIR/Makefile")"; then
  ok "Makefile の test-lint から配線されている"
else
  bad "Makefile の test-lint が check_cdpath_before_cd_capture.sh を回していない (配線が外れている)"
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-check-cdpath.XXXXXX")" || { echo "✗ mktemp -d 失敗"; exit 1; }
trap 'rm -rf "$WORK"' EXIT

# $1=名前 $2=fixture の中身 [$3=ファイル名] → RC と OUT
check_one() {
  local d="$WORK/$1" name="${3:-t.sh}"
  mkdir -p "$d"
  printf '%s\n' "$2" > "$d/$name"
  RC=0
  OUT="$(CHECK_CDPATH_FILES="$d/$name" "$CHECK" 2>&1)" || RC=$?
}
expect_red()   { check_one "$@"; if [ "$RC" -ne 0 ] && grep -qF "${3:-t.sh}:" <<< "$OUT"; then ok "$1 は落とす"; else bad "$1 を落とさない (rc=$RC): $OUT"; fi; }
expect_green() { check_one "$@"; if [ "$RC" -eq 0 ] && grep -qF '該当なし' <<< "$OUT"; then ok "$1 は通す"; else bad "$1 を通さない (rc=$RC): $OUT"; fi; }

printf 'Test 1: CDPATH を外す前の $(cd を落とす\n'
expect_red   "unset なし"        $'#!/bin/bash\nROOT="$(cd "$(dirname "$0")/.." && pwd)"'
expect_red   "unset が後ろ"      $'#!/bin/bash\nROOT="$(cd "$(dirname "$0")/.." && pwd)"\nunset CDPATH'
expect_red   "置換の中の空白"    $'#!/bin/bash\nROOT=$( cd "$(dirname "$0")" && pwd )'
expect_red   "zsh のファイル"    $'#!/usr/bin/env zsh\nROOT="$(cd "${0:A:h}/.." && pwd)"' t.zsh
expect_red   "別の変数の unset"  $'#!/bin/bash\nunset CDPATHX\nROOT="$(cd sub && pwd)"'
expect_red   "同じ行の後ろの unset" $'#!/bin/bash\nROOT="$(cd sub && pwd)"; unset CDPATH'
expect_red   "行末コメントの中の unset" $'#!/bin/bash\ntrue # unset CDPATH\nROOT="$(cd sub && pwd)"'
expect_red   "文字列の中の unset" $'#!/bin/bash\nprintf "%s\\n" "Please unset CDPATH first"\nROOT="$(cd sub && pwd)"'
expect_red   "前置代入は次の置換を免除しない" $'#!/bin/bash\nA="$(CDPATH=\'\' cd sub && pwd)"\nB="$(cd sub && pwd)"'

printf 'Test 2: 外してある形・対象外の形は通す\n'
expect_green "unset が先"         $'#!/bin/bash\nset -u\nunset CDPATH\nROOT="$(cd "$(dirname "$0")/.." && pwd)"'
expect_green "複数の名前の unset" $'#!/bin/bash\nunset FOO CDPATH\nROOT="$(cd sub && pwd)"'
expect_green "unset --"           $'#!/bin/bash\nunset -- CDPATH\nROOT="$(cd sub && pwd)"'
expect_green "空の代入"           $'#!/bin/bash\nCDPATH=\nROOT="$(cd sub && pwd)"'
expect_green "後ろの unset と前の空の代入" $'#!/bin/bash\nCDPATH=; ROOT="$(cd sub && pwd)"; unset CDPATH'
expect_green "同じ行の前の unset" $'#!/bin/bash\nunset CDPATH; ROOT="$(cd sub && pwd)"'
expect_green "行末コメントの中の \$(cd" $'#!/bin/bash\nx=1  # 例: $(cd sub && pwd) は CDPATH の下で壊れる'
expect_green "行末コメント付きの unset" $'#!/bin/bash\nunset CDPATH  # $(cd が壊れるので外す\nROOT="$(cd sub && pwd)"'
expect_green "lib の CDPATH=空"   $'# shellcheck shell=bash\nROOT="$(CDPATH=\'\' cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"'
expect_green "lib の CDPATH= "    $'# shellcheck shell=bash\nROOT="$(CDPATH= cd .. && pwd)"'
expect_green "コメントの中"       $'#!/bin/bash\n# ROOT="$(cd .. && pwd)" は壊れる'
expect_green "allow の印"         $'#!/bin/bash\nROOT="$(cd /abs && pwd)"  # cdpath: allow 絶対パスなので CDPATH を見ない'
expect_green "置換の外の cd"      $'#!/bin/bash\ncd "$d" || exit 1'

printf 'Test 3: 発見が壊れたら緑にしない / 発見したファイルを全部見る\n'
RC=0; OUT="$(CHECK_CDPATH_FILES="$(printf '%s\n' "$ROOT_DIR/Makefile" "$WORK/__missing__.sh")" "$CHECK" 2>&1)" || RC=$?
if [ "$RC" -ne 0 ] && grep -qF '__missing__.sh' <<< "$OUT"; then ok "一覧にある不在のファイルは rc≠0"; else bad "不在のファイルを黙って飛ばした (rc=$RC): $OUT"; fi
# 本物の発見経路 (find の条件) を通す: tests 側の .zsh / .bats / 拡張子なし / .sh と hooks 側が全部診断されること
disc="$WORK/disc"; mkdir -p "$disc/tests/sub" "$disc/hooks"
bad_body=$'ROOT="$(cd sub && pwd)"'
printf '%s\n' "$bad_body" > "$disc/tests/a.zsh"
printf '%s\n' "$bad_body" > "$disc/tests/b.bats"
printf '#!/bin/bash\n%s\n' "$bad_body" > "$disc/tests/sub/c_noext"
printf '%s\n' "$bad_body" > "$disc/tests/d.sh"
printf '%s\n' "$bad_body" > "$disc/hooks/pre-push"
printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$ROOT_DIR/Makefile" > "$disc/discover"; chmod +x "$disc/discover"
RC=0; OUT="$(CHECK_CDPATH_DISCOVER="$disc/discover" CHECK_CDPATH_TESTS_DIR="$disc/tests" CHECK_CDPATH_HOOKS_DIR="$disc/hooks" CHECK_CDPATH_MIN_FILES=1 "$CHECK" 2>&1)" || RC=$?
for n in a.zsh b.bats c_noext d.sh pre-push; do
  if [ "$RC" -ne 0 ] && grep -qF "$n:" <<< "$OUT"; then ok "発見経路: $n を診断する"; else bad "発見経路: $n を診断しない (rc=$RC): $OUT"; fi
done
RC=0; OUT="$(cd "$ROOT_DIR" && CHECK_CDPATH_DISCOVER=false "$CHECK" 2>&1)" || RC=$?
if [ "$RC" -ne 0 ] && grep -qF '緑にしない' <<< "$OUT"; then ok "発見の失敗は rc≠0"; else bad "発見の失敗を緑にした (rc=$RC): $OUT"; fi
RC=0; OUT="$(cd "$ROOT_DIR" && CHECK_CDPATH_DISCOVER=true "$CHECK" 2>&1)" || RC=$?
if [ "$RC" -ne 0 ] && grep -qF '下限' <<< "$OUT"; then ok "発見が 0 件なら rc≠0"; else bad "発見 0 件を緑にした (rc=$RC): $OUT"; fi

printf 'Test 4: 正解役 — 落とす形は CDPATH を export した bash で実際にパスが壊れ、通す形は壊れない\n'
mkdir -p "$WORK/oracle/sub"
printf '%s\n' '#!/bin/bash' 'printf "%s\n" "$(cd sub && pwd)"' > "$WORK/oracle/red.sh"
printf '%s\n' '#!/bin/bash' 'unset CDPATH' 'printf "%s\n" "$(cd sub && pwd)"' > "$WORK/oracle/green.sh"
want="$(cd "$WORK/oracle/sub" && pwd)"
red_out="$(cd "$WORK/oracle" && CDPATH=. bash ./red.sh)"
green_out="$(cd "$WORK/oracle" && CDPATH=. bash ./green.sh)"
if [ "$red_out" != "$want" ] && [ "$(printf '%s\n' "$red_out" | wc -l | tr -d ' ')" = 2 ] && [ "$green_out" = "$want" ]; then
  ok "本物の bash: 落とす形はパスが 2 行に壊れ、通す形は正しいパス 1 行"
else
  bad "正解役が想定と違う (落とす形: [$red_out] / 通す形: [$green_out] / 期待: [$want]。CDPATH の挙動の前提が崩れた)"
fi
RC=0; OUT="$(CHECK_CDPATH_FILES="$WORK/oracle/red.sh" "$CHECK" 2>&1)" || RC=$?
if [ "$RC" -ne 0 ]; then ok "検査は正解役の壊れる形を落とす"; else bad "検査が正解役の壊れる形を通した: $OUT"; fi
RC=0; OUT="$(CHECK_CDPATH_FILES="$WORK/oracle/green.sh" "$CHECK" 2>&1)" || RC=$?
if [ "$RC" -eq 0 ]; then ok "検査は正解役の壊れない形を通す"; else bad "検査が正解役の壊れない形を落とした: $OUT"; fi

[ "$fails" -eq 0 ] || { printf '✗ %d 件失敗\n' "$fails"; exit 1; }
printf '=== check_cdpath_before_cd_capture: すべて成功 ===\n'
