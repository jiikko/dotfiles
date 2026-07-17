# shellcheck shell=bash
# PATH スタブ方式テストの共有アサーション (test_confirm_scripts.sh から抽出。
# 複製すると isolate_env.sh 抽出前と同じ乖離が起きるため一本化)。
#
# 契約: 呼び出し側が CALLS (呼び出し記録ログの絶対パス) を export し、stub 側は
#   echo "<コマンド名> $*" >> "$CALLS"
# で 1 呼び出し 1 行を記録する。run はコマンドの exit code を $RC に格納する
# (set -e 下でも落ちない)。stdout/stderr は RUN_OUT / RUN_ERR (未設定なら /dev/null) へ。
# ファイル名に test_ を含まないため run_tests の発見対象にはならない。

reset_calls() { : > "$CALLS"; }

run() {  # $1=PATH, 残り=コマンド
  local p="$1"; shift
  RC=0
  # shellcheck disable=SC2034 # RC は呼び出し側テストが参照する出力変数
  PATH="$p" "$@" >"${RUN_OUT:-/dev/null}" 2>"${RUN_ERR:-/dev/null}" || RC=$?
}

assert_called() {  # $1=部分文字列 $2=説明
  grep -qF -- "$1" "$CALLS" || { printf '✗ %s\n  期待した呼び出しが無い: %s\n--- calls ---\n' "$2" "$1"; cat "$CALLS"; exit 1; }
  printf '✓ %s\n' "$2"
}

assert_not_called() {  # $1=部分文字列 $2=説明
  if grep -qF -- "$1" "$CALLS"; then
    printf '✗ %s\n  呼ばれてはいけないものが呼ばれた: %s\n--- calls ---\n' "$2" "$1"; cat "$CALLS"; exit 1
  fi
  printf '✓ %s\n' "$2"
}
