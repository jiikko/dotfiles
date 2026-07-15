# shellcheck shell=bash
# tmux-session テスト共通の assertion ヘルパー。source して使う。
#
# 方式: 各テストは 1 つの bash サブシェルで "CASE:<id> ..." 行を集めて $OUT に格納し、
# その後 $OUT に対して行単位でアサートする (test_tt / test_debounced_save /
# test_resurrect_save_lock 共通)。呼び出し側で $OUT が定義済みであることが前提。
#
# case_line の `printf | grep` は grep -q ではなく全入力を読む plain grep なので、
# pipefail 下でも SIGPIPE レース (scripts/lib/tmux_resurrect_guards.sh の教訓) は起きない
# (grep が早期 exit せず printf が書き終わる)。`|| true` で不一致 (grep rc=1) を吸収する。

case_line() { printf '%s\n' "$OUT" | grep "CASE:$1 " || true; }

assert_eq_line() {  # 行全体が期待文字列と一致するか
  local id="$1" expect="$2" msg="$3" line
  line="$(case_line "$id")"
  if [[ "$line" != "CASE:$id $expect" ]]; then
    printf '✗ %s\n  expected: CASE:%s %s\n  actual:   %s\n' "$msg" "$id" "$expect" "$line"
    exit 1
  fi
  printf '✓ %s\n' "$msg"
}
