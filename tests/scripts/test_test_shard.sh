#!/usr/bin/env bash
# 並列の runner の TEST_SHARD=k/n が、発見したテストを n 組に欠けなく重ならずに振り、k 組目だけを走らせることを固定する。
# CI は 1 つの腕をこの組で複数の job に分けるので、組が欠ける・重なると、どの job でも走らないテストや 2 度走るテストができる。
# workflow の組が 1/n〜n/n を揃えていることの検査 (scripts/check_ci_group_deps.sh の 4) も、欠けた workflow を読ませて見る。
set -uo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/test-shard.XXXXXX") || { echo "✗ mktemp -d 失敗"; exit 1; }
trap 'rm -rf "$work"' EXIT
work=$(cd "$work" && pwd -P) || exit 1

fails=0
ok() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

# 外側の runner の集計先・記録先を外し、偽の suite の結果が本物に混ざらないようにする
run_make() {
  env -u CI -u MAKEFLAGS -u MAKELEVEL -u MFLAGS TEST_FAIL_LEDGER= TEST_FAILS_SUMMARY= TEST_SKIPS_SUMMARY= \
    HOME="$work/home" XDG_CACHE_HOME="$work/xdg" make -C "$ROOT_DIR" --no-print-directory "$@"
}

# 走ったテストが自分の名前を ran に書く偽の suite。名前の順と組の振り分けが合うよう、大小の混ざる名前も入れる
mkdir -p "$work/suite/sub"
ran="$work/ran"
names="test_a.sh test_B.sh test_c.sh sub/test_d.sh test_e.sh"
for n in $names; do
  printf '#!/bin/sh\necho "%s" >> "%s"\n' "$work/suite/$n" "$ran" > "$work/suite/$n"
  chmod +x "$work/suite/$n"
done
all=$(for n in $names; do echo "$work/suite/$n"; done | LC_ALL=C sort)

run_shard() {  # $1=TEST_SHARD の値 ("" なら渡さない)。走った名前を整列して返す。rc は make の rc
  : > "$ran"
  if [ -n "$1" ]; then
    run_make test-discovered-heavy CI_HEAVY_TEST_DIRS="$work/suite" TEST_SHARD="$1" > "$work/out" 2>&1
  else
    run_make test-discovered-heavy CI_HEAVY_TEST_DIRS="$work/suite" > "$work/out" 2>&1
  fi
}

run_shard ""; rc=$?
got=$(LC_ALL=C sort "$ran")
[ "$rc" -eq 0 ] && [ "$got" = "$all" ] && ok "TEST_SHARD を渡さなければ全部を走らせる" \
  || { bad "TEST_SHARD なし: rc=$rc 走ったもの [$got]"; cat "$work/out"; }

run_shard 1/2; rc1=$?; s1=$(LC_ALL=C sort "$ran")
grep -qF "(並列度" "$work/out" && grep -qF "組 1/2" "$work/out" || bad "1/2: [並列] の行に組が出ない"
run_shard 2/2; rc2=$?; s2=$(LC_ALL=C sort "$ran")
union=$(printf '%s\n%s\n' "$s1" "$s2" | grep . | LC_ALL=C sort)
dup=$(printf '%s\n' "$union" | uniq -d)
if [ "$rc1" -eq 0 ] && [ "$rc2" -eq 0 ] && [ -n "$s1" ] && [ -n "$s2" ] && [ "$union" = "$all" ] && [ -z "$dup" ]; then
  ok "1/2 と 2/2 は空でなく、重ならず、合わせると全部になる"
else
  bad "1/2 と 2/2 の振り分けが違う: rc=$rc1/$rc2 1/2=[$s1] 2/2=[$s2]"
fi
# LC_ALL=C の名前の順 (sub/test_d < test_B < test_a < test_c < test_e) で交互に振る。locale の順や前後半に分ける形では組の中身が変わる
want1=$(printf '%s\n' "$work/suite/sub/test_d.sh" "$work/suite/test_a.sh" "$work/suite/test_e.sh" | LC_ALL=C sort)
want2=$(printf '%s\n' "$work/suite/test_B.sh" "$work/suite/test_c.sh" | LC_ALL=C sort)
[ "$s1" = "$want1" ] && [ "$s2" = "$want2" ] \
  && ok "LC_ALL=C の名前の順で交互に振る (1/2 は d・a・e、2/2 は B・c)" || bad "組の中身が違う: 1/2=[$s1] 2/2=[$s2]"

# 落ちた理由 (書き方の誤りか、組が空か) を文言で言う。runner の件数の突き合わせも空の組を落とすが、理由を言わない
for case in '3/2|組が 0 件' '0/2|k/n (1 桁の数) で渡す' 'x|k/n (1 桁の数) で渡す' '1/10|k/n (1 桁の数) で渡す' '7/9|組が 0 件'; do
  bad_shard=${case%%|*}; want=${case#*|}
  run_shard "$bad_shard"; rc=$?
  if [ "$rc" -ne 0 ] && [ ! -s "$ran" ] && grep -qF "$want" "$work/out"; then
    ok "TEST_SHARD=${bad_shard} は何も走らせずに「${want}」で落とす"
  else
    bad "TEST_SHARD=$bad_shard: rc=$rc 走ったもの [$(cat "$ran")] 理由の文言 ($want) $(grep -qF "$want" "$work/out" && echo あり || echo なし)"
  fi
done

# --- workflow の組の検査 (scripts/check_ci_group_deps.sh の 4) ---
check_wf() {  # $1=workflow の本文。rc は検査の rc
  printf '%s\n' "$1" > "$work/wf.yml"
  CI_GROUP_WORKFLOW="$work/wf.yml" run_make test-ci-group-deps > "$work/wf.out" 2>&1
}
r='  make-target: test-runtime-rest'
p='  make-target: test-discovered-rest-parallel'
check_wf "$r TEST_SHARD=1/2"$'\n'"$p TEST_SHARD=2/2"$'\n'"  # $p TEST_SHARD=3/3 (コメントは数えない)" \
  && ok "組の検査: rest の並列腕に 1/2 と 2/2 が揃っていれば通す" || { bad "組の検査: 揃っているのに落とした"; cat "$work/wf.out"; }
check_wf "$r"$'\n' \
  && ok "組の検査: TEST_SHARD が無ければ通す" || { bad "組の検査: TEST_SHARD なしで落とした"; cat "$work/wf.out"; }
# 壊れた workflow と、落ちた理由の文言 (別の理由で落ちたものを「落とした」に数えない)
miss='を 1 回ずつ持つ'
while IFS='|' read -r label want wf; do
  if check_wf "$(printf '%b' "$wf")"; then
    bad "組の検査: $label を通した"
  elif grep -qF "$want" "$work/wf.out"; then
    ok "組の検査: ${label} を「${want}」で落とす"
  else
    bad "組の検査: $label を別の理由で落とした: $(grep '✗' "$work/wf.out" | head -1)"
  fi
done <<EOF
1/2 だけ (欠け)|$miss|$r TEST_SHARD=1/2
1/2 が 2 回|$miss|$r TEST_SHARD=1/2\n$p TEST_SHARD=1/2
揃ったうえで 2/2 が 2 回 (重なり)|$miss|$r TEST_SHARD=1/2\n$p TEST_SHARD=2/2\n$p TEST_SHARD=2/2
組の数が食い違う|組の数がそろっていない|$r TEST_SHARD=1/2\n$p TEST_SHARD=2/3\n$p TEST_SHARD=3/3
2/2 が行頭のコメントの中|$miss|$r TEST_SHARD=1/2\n# $p TEST_SHARD=2/2
2/2 が行末のコメントの中|$miss|$r TEST_SHARD=1/2  # TEST_SHARD=2/2 は別
heavy の腕に掛けた|rest の並列腕以外|$r TEST_SHARD=1/2\n  make-target: test-discovered-heavy TEST_SHARD=2/2
matrix の変数で渡す|make-target の TEST_SHARD=k/n 以外の書き方|  shard: ['1/2']\n$p TEST_SHARD=\${{ matrix.shard }}
env: で渡す|make-target の TEST_SHARD=k/n 以外の書き方|  env:\n    TEST_SHARD: 1/2\n$p
両方を並列腕だけにした (直列腕が消える)|ちょうど 1 つ|$p TEST_SHARD=1/2\n$p TEST_SHARD=2/2
両方を test-runtime-rest にした (直列腕が 2 回)|ちょうど 1 つ|$r TEST_SHARD=1/2\n$r TEST_SHARD=2/2
run が読まない key に書いた|rest の並列腕以外|$r TEST_SHARD=1/2\n  old-make-target: test-discovered-rest-parallel TEST_SHARD=2/2
EOF
mkdir -p "$work/wf-dir"
if CI_GROUP_WORKFLOW="$work/wf-dir" run_make test-ci-group-deps > "$work/wf.out" 2>&1; then
  bad "組の検査: workflow のパスがディレクトリでも通した"
else
  grep -qF '読めない' "$work/wf.out" && ok "組の検査: workflow のパスがディレクトリなら「読めない」で落とす" \
    || bad "組の検査: ディレクトリを別の理由で落とした: $(grep '✗' "$work/wf.out" | head -1)"
fi

# --- runner はテストへ MAKEFLAGS を渡さない ---
# 外側の make がコマンド行で受けた TEST_SHARD は MAKEFLAGS に乗る。テストの中の make に届くと、そのテストの偽の suite まで組で割る
mkdir -p "$work/envsuite"
printf '#!/bin/sh\nprintf "%%s|%%s|%%s|%%s" "${MAKEFLAGS-unset}" "${MFLAGS-unset}" "${MAKELEVEL-unset}" "${TEST_SHARD-unset}" > "%s"\n' "$work/child-env" > "$work/envsuite/test_env.sh"
chmod +x "$work/envsuite/test_env.sh"
run_make test-discovered-heavy CI_HEAVY_TEST_DIRS="$work/envsuite" TEST_SHARD=1/1 > "$work/env.out" 2>&1
seen=$(cat "$work/child-env" 2>/dev/null)
if [ -n "$seen" ] && [ "$seen" = "|||" ]; then
  ok "runner はテストへ MAKEFLAGS・MFLAGS・MAKELEVEL・TEST_SHARD を空で渡す (コマンド行の TEST_SHARD が漏れない)"
else
  bad "runner の子が見た MAKEFLAGS|MFLAGS|MAKELEVEL|TEST_SHARD が [${seen:-走っていない}] (期待 |||)"; cat "$work/env.out"
fi

[ "$fails" -eq 0 ] || exit 1
