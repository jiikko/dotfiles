#!/bin/sh
# discover_shell_scripts.sh — lint 対象の shell script を機械的に発見して 1 行ずつ出力する。
# Makefile が SHELLCHECK_FILES の導出 (発見集合 − ZSH_SYNTAX_FILES の補集合) に使う。
# 発見規約: LINT_DIRS 配下の *.sh / *.zsh、または拡張子なしで shell shebang を持つファイル。
#
# 旧 tests/test_lint_coverage.sh の discover() を移設。リスト網羅の meta テストは補集合構造への
# 反転で不要になった: 「未登録」は構造的に発生せず (発見 = 即 lint 対象)、zsh 例外の登録漏れは
# SC1071 (shellcheck) で、例外リストの削除残りは zsh -n の does-not-exist で、それぞれ実行時に
# loud に落ちる。
# 注意: この file 内のコメント行を `# shellcheck` で始めない (directive と誤認され SC1072 になる)。
set -eu
unset CDPATH
cd "$(dirname "$0")/.."

# lint 対象を持つディレクトリ。tests/ と vendor/ は対象外 (テストは test-* ターゲット側の管轄)。
LINT_DIRS="setup.sh bin scripts zshlib _claude/hooks"

# find の失敗 (ディレクトリ不在・権限エラー) をパイプに隠さない: 失敗時は番兵を stdout に出して
# 非 0 で終わる。Make の $(shell) は exit code を捨てるが、番兵が実在しないファイル名として
# SHELLCHECK_FILES に混ざり shellcheck が does-not-exist で loud に落ちる (静かな部分 lint を防ぐ)。
# shellcheck disable=SC2086 # LINT_DIRS は意図的に単語分割する (find の複数起点)
all_files=$(find $LINT_DIRS -type f) || { printf '__DISCOVERY_FAILED__\n'; exit 1; }

printf '%s\n' "$all_files" | while IFS= read -r f; do
  case "$f" in
    *.sh|*.zsh) printf '%s\n' "$f" ;;
    *) head -1 "$f" 2>/dev/null | grep -qE '^#!.*[/ ](sh|bash|zsh|dash|ksh)( |$)' && printf '%s\n' "$f" ;;
  esac
done | sort -u
