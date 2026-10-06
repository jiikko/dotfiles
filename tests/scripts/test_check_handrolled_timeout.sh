#!/usr/bin/env bash
# scripts/check_handrolled_timeout.sh (上限付き実行を runtimeout 以外で書いた箇所を落とす検査。issue 643) の契約を fixture で固定する。
#
# 守るもの: gtimeout / timeout を探す / timeout を呼ぶ (前に代入が付く形・$() の中・オプション付き・heredoc の開始行も) /
# ( sleep; kill ) / ( sleep && kill ) / { sleep; kill; } の見張りを落とす / here-string・算術・終端の無い << の後ろも検査する /
# コメント・heredoc の本文・説明文 (秒数の後が日本語)・runtimeout の呼び出し・変数名の中の timeout を通す / allow の印で通す /
# 引数で渡したファイルが無ければ緑にしない / Makefile から配線されている / 今の repo 全体で緑。
# fixture は検査に渡す shell の文字列なので展開させない。配線の検査は A && B || C で「どれかが欠けたら落とす」。
# fixture の語は @T@ (timeout) / @G@ (gtimeout) / @K@ (kill) / @S@ (sleep) / @H@ (<<) を実行時に置き換えて作る
# (このファイル自身も repo 全体の検査で読まれるので、検査が落とす並びと heredoc の開始をここに置かない)。
# shellcheck disable=SC2016,SC2015
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_handrolled_timeout.sh"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

grep -qE '^test-handrolled-timeout:' "$ROOT_DIR/Makefile" && grep -qE '^\t@scripts/check_handrolled_timeout\.sh$' "$ROOT_DIR/Makefile" \
  && grep -qE 'run_make_targets_parallel\.sh .*test-handrolled-timeout' "$ROOT_DIR/Makefile" \
  || { printf '✗ Makefile の test-lint が check_handrolled_timeout.sh を回していない (配線が外れている)\n'; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-check-handrolled-timeout.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
T_WORD=timeout
E_WORD=eep

# $1=名前 $2=fixture の中身 → RC と OUT
check_one() {
  local body="$2"
  body="${body//@T@/timeout}"; body="${body//@G@/g$T_WORD}"; body="${body//@K@/kill}"; body="${body//@H@/<<}"; body="${body//@S@/sl$E_WORD}"
  mkdir -p "$WORK/$1"
  printf '%s\n' "$body" > "$WORK/$1/t.sh"
  RC=0
  OUT="$("$CHECK" "$WORK/$1/t.sh" 2>&1)" || RC=$?
}
expect_red()   { check_one "$@"; if [ "$RC" -ne 0 ] && grep -q 't.sh:' <<< "$OUT"; then ok "$1 は落とす"; else bad "$1 を落とさない (rc=$RC): $OUT"; fi; }
expect_green() { check_one "$@"; if [ "$RC" -eq 0 ] && grep -q '該当なし' <<< "$OUT"; then ok "$1 は通す"; else bad "$1 を通さない (rc=$RC): $OUT"; fi; }

printf 'Test 1: 昔の書き方を落とす\n'
expect_red g-prefixed     'TIMEOUT_BIN=@G@'
expect_red probe-v        'command -v @T@ >/dev/null 2>&1 && with=(@T@ 60)'
expect_red probe-typeP    'type -P @T@'
expect_red call-head      '@T@ 10 "$HOOK" < in.json'
expect_red call-assign    'out="$(PATH="$d:$PATH" @T@ 5 "$ROOT/bin/tool" 2>&1)"'
expect_red call-var       '@T@ "$LIMIT" ./run.sh'
expect_red call-flag      '@T@ -k 1 10s make test'
expect_red watchdog       '( @S@ "$T"; @K@ -9 "$pid" 2>/dev/null ) & killer=$!'

expect_red watchdog-and   '( @S@ 5 && @K@ "$pid" ) &'
expect_red watchdog-brace '{ @S@ 5; @K@ "$pid"; } &'
expect_red watchdog-arith '( @S@ $((t+1)); @K@ $pid ) &'
expect_red call-signal    '@T@ --signal=KILL 5 make test'
expect_red call-s-name    '@T@ -s KILL 5 make test'
expect_red call-default   '@T@ ${T:-5} make test'
expect_red g-abs-path     '/opt/homebrew/bin/@G@ 5 make test'
expect_red heredoc-line   '@T@ 10 "$HOOK" @H@'"'"'JSON'"'"'
{}
JSON'

printf 'Test 1b: heredoc と誤読しうる << の後ろも検査する (誤読で後半が黙って抜けない)\n'
expect_red heredoc-tail   'cat @H@EOF | @G@ 5 sh
x
EOF'
expect_red after-herestr  'x=$(cmd @H@< "y")
@T@ 5 make test'
expect_red after-arith    'echo $((1 @H@ 2))
@T@ 5 make test'
expect_red after-noend    'awk '"'"'
/@H@EOS/ { print }
'"'"' f
@T@ 5 make test'

printf 'Test 2: 昔の書き方でないものは通す\n'
expect_green comment      '# @T@ 10 make test / @G@ は使わない'
expect_green heredoc      'cat @H@EOF
@T@ 10 make test
( @S@ 1; @K@ 1 )
EOF'
expect_green prose-ja     'echo "zsh が非 0 終了 (@T@ 60s 超過を含む)"'
expect_green runtimeout   '"$RUNTIMEOUT" 10 "$HOOK" < in.json'
expect_green varname      'timeout="${MUTATE_VERIFY_TIMEOUT:-1800}"; run_timeout=5; echo "$timeout"'
expect_green flagname     'gh run watch --@T@ 10 "$id"'
expect_green allow        '@T@ 10 make test  # handrolled-timeout: allow (検査の説明に使う例)'

printf 'Test 3: 検査できないときは緑にしない\n'
RC=0; OUT="$("$CHECK" "$WORK/no-such-file.sh" 2>&1)" || RC=$?
if [ "$RC" -ne 0 ]; then ok "引数のファイルが無ければ rc=$RC"; else bad "無いファイルを緑にした: $OUT"; fi

printf 'Test 4: 今の repo 全体で緑\n'
RC=0; OUT="$("$CHECK" 2>&1)" || RC=$?
if [ "$RC" -eq 0 ] && grep -qE '[0-9]+ ファイルに該当なし' <<< "$OUT"; then ok "repo 全体: $OUT"; else bad "repo 全体 (rc=$RC): $OUT"; fi

if [ "$fails" -gt 0 ]; then
  printf '✗ check_handrolled_timeout: %d 件失敗\n' "$fails"
  exit 1
fi
printf '✓ check_handrolled_timeout: fixture と repo 全体\n'
