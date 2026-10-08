# shellcheck shell=bash
# runtimeout.sh — runtimeout (src/runtimeout。時間の上限付き実行) のバイナリを解決する。bash / zsh の両方から source する。
#
# runtimeout_resolve <repo root>: RUNTIMEOUT にバイナリの絶対パスを入れて export する (起動時に 1 回だけ。理由は bin/lib/go_tool.sh)。
# 呼び出しは "$RUNTIMEOUT" [-f] <秒> <コマンド>… (時間切れは rc=124)。
runtimeout_resolve() {  # $1=repo root
  # shellcheck source=bin/lib/go_tool.sh
  . "$1/bin/lib/go_tool.sh" || { echo "✗ runtimeout_resolve: $1/bin/lib/go_tool.sh を読めない" >&2; return 1; }
  go_tool_resolve RUNTIMEOUT "$1" runtimeout runtimeout 0 true
}
