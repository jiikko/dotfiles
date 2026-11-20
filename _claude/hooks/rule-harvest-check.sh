#!/usr/bin/env bash
# Stop フック: このセッションで実装・仕組みを変えた commit があれば、1 回だけ止めて skill rule-harvest を促す。
# 判定・数えない commit・検出しない形は lib/rule_harvest_check.py の冒頭。
# python3 が無い・本体が読めないときは何もしない (Stop を止めない)。-B: import した lib の __pycache__ を dotfiles の作業ツリーに作らない。
unset CDPATH
command -v python3 >/dev/null 2>&1 || exit 0
exec python3 -I -B "$(dirname "$0")/lib/rule_harvest_check.py"
