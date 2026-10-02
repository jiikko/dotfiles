#!/usr/bin/env bash
# scripts/check_var_before_multibyte.sh (`$var` の直後の ASCII 以外の文字を落とす検査。issue 633) の契約を fixture で固定する。
#
# 守るもの: 引用の外・二重引用符の中・引用の無い heredoc の本文の `$var<全角>` を落とす / 単引用符・`$'…'`・コメント・
# 引用付きの heredoc・`${var}`・`\$`・位置パラメータは通す / allow の印で通す / zsh のファイルを除く /
# 対象が少なすぎたら緑にしない / Makefile から配線されている / 検査が落とす形を本物の bash 3.2 が実際に読み違える。
# fixture の全角は @J@ (= 「。」の UTF-8) を実行時に置き換えて作る (このファイル自身に検査の対象の並びを置かない)。
# fixture は検査に渡す shell の文字列なので `$var` を展開させない。配線の検査は A && B || C で「どれかが欠けたら落とす」。
# shellcheck disable=SC2016,SC2015
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_var_before_multibyte.sh"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

grep -qE '^test-var-multibyte:' "$ROOT_DIR/Makefile" && grep -qE '^\t@scripts/check_var_before_multibyte\.sh$' "$ROOT_DIR/Makefile" \
  && grep -qE 'run_make_targets_parallel\.sh .*test-var-multibyte' "$ROOT_DIR/Makefile" \
  || { printf '✗ Makefile の test-lint が check_var_before_multibyte.sh を回していない (配線が外れている)\n'; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-check-var-multibyte.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
J="$(printf '\343\200\202')"

# $1=名前 $2=fixture の中身 [$3=ファイル名] → RC と OUT
check_one() {
  local d="$WORK/$1" name="${3:-t.sh}" body="${2//@J@/$J}"
  mkdir -p "$d"
  printf '%s\n' "$body" > "$d/$name"
  RC=0
  OUT="$( (cd "$ROOT_DIR" && CHECK_VAR_MULTIBYTE_FILES="$d/$name" CHECK_VAR_MULTIBYTE_MIN_FILES="${MIN:-1}" "$CHECK") 2>&1 )" || RC=$?
}
# 正解役との突き合わせ (Test 5) のために、判定した fixture を覚えておく: 名前|パス|red か green
SEEN=()
expect_red()   { SEEN+=("$1|$WORK/$1/${3:-t.sh}|red"); check_one "$@"; if [ "$RC" -ne 0 ] && grep -q "${3:-t.sh}:" <<< "$OUT"; then ok "$1 は落とす"; else bad "$1 を落とさない (rc=$RC): $OUT"; fi; }
expect_green() { SEEN+=("$1|$WORK/$1/${3:-t.sh}|green"); check_one "$@"; if [ "$RC" -eq 0 ] && grep -q '該当なし' <<< "$OUT"; then ok "$1 は通す"; else bad "$1 を通さない (rc=$RC): $OUT"; fi; }

printf 'Test 1: 展開される場所の $var<全角> を落とす\n'
expect_red   dq          'echo "(rc=$rc@J@x)"'
expect_red   bare        'echo $rc@J@'
expect_red   bracket     'fail "dispatcher.log に「$E2E_ALONE_EXIT@J@」"'
expect_red   apostrophe  'echo "it'"'"'s $rc@J@"'
expect_red   dq_multiline 'x="a'$'\n''b $rc@J@"; echo "$x"'
expect_red   heredoc     'cat <<EOF'$'\n''$rc@J@'$'\n''EOF'
expect_red   heredoc_hash 'cat <<EOF'$'\n''# $rc@J@'$'\n''EOF'
expect_red   herestring  'cat <<< x'$'\n''echo "$rc@J@"'
expect_red   after_quoted_heredoc "cat <<'EOF'"$'\n''$rc@J@'$'\n''EOF'$'\n''echo "$rc@J@"'
expect_red   after_comment 'echo ok # $rc@J@'$'\n''echo "$rc@J@"'
expect_red   brace_hash  'v=${line% #*}; echo "x$rc@J@"'   # ${…} の中の # はコメントではない (敵対的レビュー)
expect_red   brace_hash_quote 'v=${line%% #*}; printf x | awk '"'"''$'\n''{print}'$'\n'"'"''$'\n''echo "x$rc@J@"'
expect_red   hash_word   'echo a#$rc@J@'   # 語の途中の # はコメントではない
expect_red   bang_tag    'cat <<!'$'\n'"don't"$'\n''!'$'\n''echo "x$rc@J@"'   # 識別子でない区切り
expect_red   dbl_bslash  'cat <<EOF'$'\n''\\$rc@J@'$'\n''EOF'   # \\ の後ろの $rc は展開される
expect_red   dash_hd_after "cat <<-'EOF'"$'\n''x'$'\n'$'\t''EOF'$'\n''echo "$rc@J@"'
expect_red   dq_ansi     'echo "$'"'"'$rc@J@'"'"'"'   # 二重引用符の中の $'"'"' は ANSI-C 引用ではない
expect_red   arith_hash  'echo $((16#ff)) "$rc@J@"'   # 算術の中の # はコメントではない
expect_red   bats        'echo "$rc@J@"' t.bats
expect_red   bash_bang_mentions_zsh '#!/bin/bash   # zsh が無い環境向け'$'\n''echo "$rc@J@"' t
expect_red   arith_brace 'n=$(( ${y} + 1 ))'$'\n''echo "}"'$'\n''echo "it'"'"'s $rc@J@"'   # 算術の中の ${…} を閉じる (2 周目の P1)
expect_red   arith_var   '(( rc@J@ > 0 )); (( $rc@J@ > 0 ))'   # 算術の中の $var も見る
expect_red   nested_dq_brace 'echo "${x:-"a"} $rc@J@"'   # ${…} の中の入れ子の二重引用符
expect_red   nested_cmd  'echo "$(echo "a b") $rc@J@"'   # $(…) の中の入れ子の二重引用符
expect_red   backtick    'echo "`echo "a"` $rc@J@"'
expect_red   backtick_sq 'echo "`echo "it'"'"'s"` $rc@J@"'$'\n''# it'"'"'s'   # `…` の中の入れ子の "…" と ' を追う (追わないと ' で単引用符に入り、次の行の ' で閉じ直して末尾の検査も素通り)
expect_red   eof_open    'echo "unterminated'   # 末尾で引用が閉じない = 状態を見失った
expect_red   eof_heredoc 'cat <<EOF'$'\n''x'   # 末尾まで heredoc が閉じない

printf 'Test 2: 展開されない場所・別の形は通す\n'
expect_green braces      'echo "(rc=${rc}@J@x)"'
expect_green single      "echo '\$rc@J@'"
expect_green ansi_c      "echo \$'\$rc@J@'"
expect_green ansi_c_esc  "echo \$'a\\'b \$rc@J@'"   # \' は $'…' を閉じない (単引用符と同じに読むと後ろが地の文になる)
expect_green comment     '# $rc@J@'$'\n''echo ok'
expect_green trailing    'echo ok # $rc@J@'
expect_green quoted_hd   "cat <<'EOF'"$'\n''$rc@J@'$'\n''EOF'
expect_green dquoted_hd  'cat <<"EOF"'$'\n''$rc@J@'$'\n''EOF'
expect_green bslash_hd   'cat <<\EOF'$'\n''$rc@J@'$'\n''EOF'
expect_green dash_hd     "cat <<-'EOF'"$'\n''$rc@J@'$'\n'$'\t''EOF'
expect_green escaped     'echo "\$rc@J@"'
expect_green positional  'echo "$1@J@ $@@J@"'
expect_green arith       'echo "$((rc))@J@"'
expect_green allow       'echo "$rc@J@"   # var-multibyte: allow (検査の fixture)'
expect_green partial_tag 'cat <<E"OF"'$'\n''$rc@J@'$'\n''EOF'   # 区切りの一部が引用されていても本文は展開されない
expect_green hd_escaped  'cat <<EOF'$'\n''\$rc@J@'$'\n''EOF'
expect_green hd_allow    'cat <<EOF'$'\n''$rc@J@   var-multibyte: allow (検査の fixture)'$'\n''EOF'
expect_green herestring_sq 'cat <<< "$x"'$'\n''echo '"'"'$rc@J@'"'"
expect_green herestring_word 'cat <<< x'$'\n''echo '"'"'$rc@J@'"'"   # <<< の 2 文字目からを <<x の heredoc と読まない
expect_green two_heredocs "cat <<'A' <<'B'"$'\n''a'$'\n''A'$'\n''$rc@J@'$'\n''B'
expect_green arith_shift '(( m = 1<<n ))'$'\n''echo '"'"'$rc@J@'"'"
expect_green arith_shift_dollar 'x=$((1<<n))'$'\n''echo '"'"'$rc@J@'"'"
expect_green arith_nested '(( (y)+(y) << 1 ))'$'\n''echo '"'"'$rc@J@'"'"   # 算術の中の入れ子の括弧を数える
expect_green pid         'echo "$$rc@J@" | sed "s/^[0-9]*//"'   # $$ は pid。その後ろの rc は変数ではない
expect_green hd_pid      'cat <<EOF | sed "s/^[0-9]*//"'$'\n''$$rc@J@'$'\n''EOF'   # heredoc の本文の $$ も pid
expect_green not_arith   'if !((1<<2)); then :; fi; echo ok'   # ! の直後の (( も算術
expect_green brace_ansi  'echo ${unset_v:-$'"'"'a b $rc@J@'"'"'}'   # 引用の外の ${…} の中の $'…' は ANSI-C 引用 (bash 3.2 で通る)
expect_red   brace_ansi_dq 'echo "${unset_v:-$'"'"'a b $rc@J@'"'"'}"'   # 二重引用符の中では ANSI-C 引用にならず展開される (bash 3.2 で死ぬ)
expect_red   brace_sq_dq 'echo "${unset_v:-'"'"'a $rc@J@'"'"'}"'   # 二重引用符の中の ${…} の '…' は単引用符ではない (bash 3.2 で死ぬ)
expect_red   nested_brace_sq 'echo "${a:-${b:-it'"'"'s}}"'$'\n''echo '"'"'x $rc@J@'"'"''$'\n''echo "it'"'"'s}}"'   # 入れ子の ${ の中では ' が引用を開く (3 周目)
expect_red   hd_after_multiline_quote 'cat <<EOF; perl -e '"'"''$'\n''print 1;'$'\n'"'"''$'\n''body'$'\n''EOF'$'\n''echo "$rc@J@"'$'\n''echo it'$'\\'"'"'s'   # heredoc の本文は引用が閉じた後から (3 周目)
expect_red   eof_cmd     'x=$(echo a'   # 末尾で $(…) が閉じない
expect_red   tab_only_dash 'cat <<EOF'$'\n'$'\t''EOF'$'\n''# $rc@J@'$'\n''EOF'   # <<EOF (- 無し) では行頭のタブの区切りは区切りではない
expect_green case_paren  'case $x in 1) echo '"'"'$rc@J@'"'"' ;; esac'

printf 'Test 3: 対象の選び方\n'
# zsh のファイルは除く。除いた結果の 0 件で下限に掛からないよう、きれいな .sh を 1 本添えて「1 ファイル」を見る
expect_zsh_skipped() {
  local d="$WORK/$1"; mkdir -p "$d"
  printf '%s\n' "${2//@J@/$J}" > "$d/$3"; printf 'echo ok\n' > "$d/clean.sh"
  RC=0
  OUT="$( (cd "$ROOT_DIR" && CHECK_VAR_MULTIBYTE_FILES="$d/$3"$'\n'"$d/clean.sh" CHECK_VAR_MULTIBYTE_MIN_FILES=1 "$CHECK") 2>&1 )" || RC=$?
  if [ "$RC" -eq 0 ] && grep -q ' 1 ファイルに該当なし' <<< "$OUT"; then ok "$1 は対象から除く"; else bad "$1 を除かない (rc=$RC): $OUT"; fi
}
expect_zsh_skipped zsh_ext  'echo "$rc@J@"' t.zsh
expect_zsh_skipped zsh_bang '#!/bin/zsh'$'\n''echo "$rc@J@"' t.sh   # .sh でも zsh の shebang なら除く (tests/ に多い)
expect_zsh_skipped zsh_env  '#!/usr/bin/env zsh'$'\n''echo "$rc@J@"' t.sh
expect_red   bash_bang   '#!/bin/bash'$'\n''echo "$rc@J@"' t
check_one noshebang_ext 'echo "$rc@J@"' t.txt   # .sh / .bats 以外で shebang が無ければ対象外 (0 件で落ちる)
if [ "$RC" -ne 0 ] && grep -q '0 件' <<< "$OUT"; then ok "拡張子が違い shebang の無いファイルは対象外"; else bad "拡張子が違い shebang の無いファイルを数えた (rc=$RC): $OUT"; fi

printf 'Test 3b: 検査できなかったときに緑にしない\n'
# 発見を通す形 (FILES を渡さない)。$1=名前 残り=env の代入
run_full() { shift; RC=0; OUT="$( (cd "$ROOT_DIR" && env -u CHECK_VAR_MULTIBYTE_FILES "$@" "$CHECK") 2>&1 )" || RC=$?; }
expect_fail() { if [ "$RC" -ne 0 ] && grep -q "$2" <<< "$OUT"; then ok "$1 は緑にしない"; else bad "$1 で緑 (rc=$RC): $OUT"; fi; }
mkdir -p "$WORK/full/tests" "$WORK/full/hooks" "$WORK/full/emptytests"
printf 'echo ok\n' > "$WORK/full/a.sh"; printf 'echo ok\n' > "$WORK/full/tests/b.sh"; printf '#!/bin/sh\necho ok\n' > "$WORK/full/hooks/pre-push"
printf '#!/bin/sh\necho %s\n' "$WORK/full/a.sh" > "$WORK/full/discover"; chmod +x "$WORK/full/discover"
base=(CHECK_VAR_MULTIBYTE_DISCOVER="$WORK/full/discover" CHECK_VAR_MULTIBYTE_TESTS_DIR="$WORK/full/tests" CHECK_VAR_MULTIBYTE_HOOKS_DIR="$WORK/full/hooks" CHECK_VAR_MULTIBYTE_MIN_FILES=1)
run_full ok "${base[@]}"
if [ "$RC" -eq 0 ] && grep -q ' 3 ファイルに該当なし' <<< "$OUT"; then ok "3 つの入力をすべて数える"; else bad "3 つの入力を数えない (rc=$RC): $OUT"; fi
run_full discover_fails "${base[@]}" CHECK_VAR_MULTIBYTE_DISCOVER=false;                       expect_fail "発見の失敗" '失敗した'
run_full tests_missing  "${base[@]}" CHECK_VAR_MULTIBYTE_TESTS_DIR="$WORK/full/none";          expect_fail "tests/ が無い" 'が無い'
run_full tests_empty    "${base[@]}" CHECK_VAR_MULTIBYTE_TESTS_DIR="$WORK/full/emptytests";    expect_fail "tests/ が 0 件" '発見の壊れ'
run_full hooks_missing  "${base[@]}" CHECK_VAR_MULTIBYTE_HOOKS_DIR="$WORK/full/none";          expect_fail "githooks/ が無い" 'が無い'
run_full too_few        "${base[@]}" CHECK_VAR_MULTIBYTE_MIN_FILES=2;                          expect_fail "下限に届かない入力" '発見の壊れ'
run_full perl_fails     "${base[@]}" CHECK_VAR_MULTIBYTE_PERL=false;                           expect_fail "perl の失敗" 'perl が失敗した'
chmod 000 "$WORK/full/tests/b.sh"
run_full unreadable     "${base[@]}";                                                          expect_fail "読めないファイル" '読めないファイル'
chmod 644 "$WORK/full/tests/b.sh"
# 既定の対象 (repo 全体) でも下限を超えて緑になる
RC=0; OUT="$( (cd "$ROOT_DIR" && "$CHECK") 2>&1 )" || RC=$?
if [ "$RC" -eq 0 ] && grep -qE '[0-9]{3,} ファイルに該当なし' <<< "$OUT"; then ok "repo 全体で緑 ($OUT)"; else bad "repo 全体で緑にならない (rc=$RC): $OUT"; fi

printf 'Test 4: 検査が落とす形を、本物の bash 3.2 が UTF-8 のロケールで実際に読み違える\n'
if [ -x /bin/bash ] && [ "$(/bin/bash -c 'echo ${BASH_VERSINFO[0]}')" = 3 ]; then
  rc32=0; env -i PATH=/usr/bin:/bin LC_ALL=en_US.UTF-8 /bin/bash -c "set -u; rc=3; echo \"(rc=\$rc$J)\"" >/dev/null 2>&1 || rc32=$?
  rcok=0; env -i PATH=/usr/bin:/bin LC_ALL=en_US.UTF-8 /bin/bash -c "set -u; rc=3; echo \"(rc=\${rc}$J)\"" >/dev/null 2>&1 || rcok=$?
  if [ "$rc32" -ne 0 ] && [ "$rcok" -eq 0 ]; then ok "bash 3.2 は \$rc<全角> で死に、\${rc}<全角> は通る"; else bad "bash 3.2 の挙動が前提と違う (\$rc: rc=$rc32 / \${rc}: rc=$rcok)"; fi
else
  printf '  - /bin/bash が 3.2 でないので Test 4 を飛ばす (macOS 以外)\n'
fi

printf 'Test 5: 検査の判定が、本物の bash 3.2 の答え (C と UTF-8 のロケールで挙動が食い違うか) と全 fixture で一致する\n'
# この不具合は UTF-8 のロケールでだけ起きるので、2 つのロケールで同じ fixture を走らせて食い違えば本物の違反。
# 字句の近似 (検査) を、近似でない正解役 (bash 3.2 そのもの) と突き合わせる。変数は全部定義して走らせる
# (未定義のまま set -u で死ぬのは両方のロケールで同じなので、食い違いに出ない)。
# 突き合わせから外す fixture: 字句の状態を見失ったことを報告する eof_* (検査は意図して保守側に倒す)・印で意図して通す allow・
# bash で走らない zsh / bats
ORACLE_SKIP=" eof_open eof_heredoc eof_cmd allow hd_allow bats zsh_ext zsh_bang zsh_env "
if [ -x /bin/bash ] && [ "$(/bin/bash -c 'echo ${BASH_VERSINFO[0]}')" = 3 ]; then
  prelude='rc=1; x=1; y=1; n=1; m=0; line=a; E2E_ALONE_EXIT=z; fail() { printf "%s\n" "$*"; }; e2e_fail() { fail "$@"; }'
  run_locale() {  # $1=ロケール $2=fixture → stdout+stderr と rc を 1 つの文字列で返す
    local d out rc=0; d="$(dirname "$2")"
    out="$(cd "$d" && env -i PATH=/usr/bin:/bin LC_ALL="$1" /bin/bash -c "$prelude"'; . "$1"' _ "$2" 2>&1)" || rc=$?
    printf '%s|rc=%d' "${out//$2/F}" "$rc"
  }
  # $1=fixture → red (ロケールで食い違う = 本物の違反) / green / unknown:<理由> (正解役が答えを出せない)
  oracle_verdict() {
    local c_out u_out
    c_out="$(run_locale C "$1")"; u_out="$(run_locale en_US.UTF-8 "$1")"
    if [ "$c_out" != "$u_out" ]; then printf 'red'; return; fi
    case "$c_out" in *"syntax error"*|*"unexpected EOF"*) printf 'unknown:構文エラー %s' "${c_out:0:80}"; return ;; esac
    [ "$c_out" = "|rc=0" ] && { printf 'unknown:何も出力しない (突き合わせが何も見ていない)'; return; }
    printf 'green'
  }
  mkdir -p "$WORK/oracle"
  printf 'echo "unterminated\n' > "$WORK/oracle/syntax.sh"; printf 'x=1\n' > "$WORK/oracle/silent.sh"; printf 'echo ok\n' > "$WORK/oracle/plain.sh"
  v="$(oracle_verdict "$WORK/oracle/syntax.sh")"; [[ $v == unknown:構文エラー* ]] && ok "正解役は構文エラーを判定不能にする" || bad "正解役が構文エラーを $v と判定した"
  v="$(oracle_verdict "$WORK/oracle/silent.sh")"; [[ $v == unknown:何も出力しない* ]] && ok "正解役は出力の無い fixture を判定不能にする" || bad "正解役が出力の無い fixture を $v と判定した"
  v="$(oracle_verdict "$WORK/oracle/plain.sh")"; [ "$v" = green ] && ok "正解役は普通の fixture を green にする" || bad "正解役が普通の fixture を $v と判定した"
  compared=0
  for e in "${SEEN[@]}"; do
    name="${e%%|*}"; rest="${e#*|}"; file="${rest%|*}"; want="${rest##*|}"
    [[ $ORACLE_SKIP == *" $name "* ]] && continue
    [ -f "$file" ] || { bad "Test 5: $name の fixture が無い ($file)"; continue; }
    truth="$(oracle_verdict "$file")"
    case "$truth" in unknown:*) bad "Test 5: $name は正解役が判定できない (${truth#unknown:})"; continue ;; esac
    compared=$((compared + 1))
    [ "$truth" = "$want" ] || bad "Test 5: $name は検査が $want だが bash 3.2 の答えは $truth"
  done
  if [ "$compared" -ge 40 ]; then ok "検査の判定と bash 3.2 の答えを ${compared} 件で突き合わせた"; else bad "Test 5 で突き合わせた fixture が ${compared} 件しかない"; fi
else
  printf '  - /bin/bash が 3.2 でないので Test 5 を飛ばす (macOS 以外)\n'
fi

if [ "$fails" -ne 0 ]; then printf '✗ %d 件失敗\n' "$fails"; exit 1; fi
printf '✓ check_var_before_multibyte.sh の契約\n'
