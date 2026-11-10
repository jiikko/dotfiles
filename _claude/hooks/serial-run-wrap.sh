#!/usr/bin/env bash
#
# PreToolUse(Bash) フック: 重いコマンド (make test / go test / xcodebuild 等) の直前に `serial-run -- ` を差し込み、
# 順番待ちにする (既定は同じ repo ごと。SERIAL_RUN_SCOPE=machine ならマシン全体)。判定と書き換えの正本は src/serialrun/hook.go (`serial-run hook`)。
# 長い検証を Bash 1 回の中の素の `&` で背景に回す形は書き換えずに deny する (正本と、止める形・検出しない形は src/serialrun/background.go)。
# 同じ hook が「長い検証か」の判定 (isHeavy) を持つので、別の hook に語の一覧を写さず、書き換えと deny の順番も 1 本の中で決まる。
#
# 🚨 fail-open: どこで失敗しても何も出さずに exit 0 (元のコマンドのまま走る。背景に回す形の deny も出ない)。直列化は便宜で、tool を止める理由にならない。
#    PATH 上の serial-run がこの checkout のものでなければ差し込まない (差し込んだ後に「見つからない」で make test を落とさない)。
# 止め方: SERIAL_RUN_HOOK_DISABLE=1 を Claude Code の環境に置く。
#
# 入力: PreToolUse の hook JSON (stdin)。出力: 差し込むときだけ updatedInput の JSON。
set -uo pipefail
unset CDPATH

[ "${SERIAL_RUN_HOOK_DISABLE:-}" = 1 ] && exit 0
input=$(cat)

# 手がかりが無ければ即通す (fast path。判定は変えない。判定の正本は hook.go の isHeavy)。
# 🚨 JSON から command を取り出さず、入力の全文に当てる。jq に頼ると、jq が無い環境で直列化が黙って全部止まる。
# 全文に当てると description などに語が出るだけでも Go の判定まで進むが、そこで見送るので結果は変わらない (遅れは数十 ms)。
# 語は isHeavy と longVerification の対象のコマンド名と揃える (tests/claude/test_serial_run_wrap.sh が、対象の例がここを通り抜けることを確かめる)
case "$input" in
*make*|*go*test*|*xcodebuild*|*swift*|*mutate-verify*|*skill-eval*|*tests/*) ;;
*) exit 0 ;;
esac

root="$(cd "$(dirname "$0")/../.." && pwd -P)" || exit 0
bin="$root/bin/serial-run"
found="$(command -v serial-run 2>/dev/null)" || exit 0
[ "$(cd "$(dirname "$found")" && pwd -P)/serial-run" = "$bin" ] || exit 0

printf '%s' "$input" | "$bin" hook
exit 0
