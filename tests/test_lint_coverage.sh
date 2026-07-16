#!/bin/sh
# test_lint_coverage.sh — 全 shell script が Makefile の lint リストに登録され、かつ列挙が
# 実在ファイルを指すか検証する meta テスト。
#
# 目的: scripts を増減したとき Makefile の SHELLCHECK_FILES / ZSH_SYNTAX_FILES への追従を忘れ、
#   (a) 新 script が lint されないまま放置 / (b) 削除済み script がリストに残り shellcheck が
#   "openBinaryFile: does not exist" で落ちる、を構造的に防ぐ。実際に (b) で Lint CI が
#   赤くなった (2026-07-16) のを機に導入。
#
# なぜリスト自体を自動生成しないか: shellcheck 可否 (SHELLCHECK 側か ZSH_SYNTAX 側か) は
#   shebang や拡張子から機械的に決まらない (同じ .zsh でも _av1ify.zsh は shellcheck 側 /
#   _concat.zsh は zsh -n 側)。よってリストは手動維持し、その"網羅"だけをこのテストで守る。
#
# 判定: lint 対象を持つディレクトリ配下の shell script (*.sh / *.zsh / shell shebang) を発見し、
#   各々が SHELLCHECK_FILES ∪ ZSH_SYNTAX_FILES (make print-lint-files) に載っているか、
#   および列挙側が全て実在するかを突き合わせる。意図的に lint 対象外にするなら ALLOWLIST へ。
set -eu
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT_DIR"

# lint 不要な shell script (意図的に lint 対象外にしたい場合のみ、相対パスをスペース区切りで追加)
ALLOWLIST=""

# lint 対象を持つディレクトリ。tests/ と vendor/ は lint リストの対象外なので含めない
# (既存の SHELLCHECK_FILES / ZSH_SYNTAX_FILES のスコープと一致させること)。
LINT_DIRS="setup.sh bin scripts zshlib _claude/hooks"

# 上記配下の shell script を発見する: *.sh / *.zsh 拡張子、または拡張子なしで shell shebang を
# 持つ実行スクリプト (bin/ の av1ify 等)。
discover() {
  # shellcheck disable=SC2086 # LINT_DIRS は意図的に単語分割する (find の複数起点)
  find $LINT_DIRS -type f 2>/dev/null | while IFS= read -r f; do
    case "$f" in
      *.sh|*.zsh) printf '%s\n' "$f" ;;
      *) head -1 "$f" 2>/dev/null | grep -qE '^#!.*[/ ](sh|bash|zsh|dash|ksh)( |$)' && printf '%s\n' "$f" ;;
    esac
  done | sort -u
}

listed=$(make -s print-lint-files | sort -u)
discovered=$(discover)

# (a) 発見したのに lint リスト未登録 = lint されないまま放置される
unregistered=$(printf '%s\n' "$discovered" | while IFS= read -r f; do
  [ -n "$f" ] || continue
  case " $ALLOWLIST " in (*" $f "*) continue ;; esac
  printf '%s\n' "$listed" | grep -qxF "$f" || printf '%s\n' "$f"
done)

# (b) lint リストに載っているが実在しない = shellcheck が does-not-exist で落ちる
missing=$(printf '%s\n' "$listed" | while IFS= read -r f; do
  [ -n "$f" ] || continue
  [ -f "$f" ] || printf '%s\n' "$f"
done)

rc=0
if [ -n "$unregistered" ]; then
  echo "✗ lint リスト未登録の shell script (lint されません):" >&2
  printf '%s\n' "$unregistered" | sed 's/^/  - /' >&2
  echo "→ Makefile の SHELLCHECK_FILES (shellcheck が解析できる) か ZSH_SYNTAX_FILES (zsh 固有構文) に追加。" >&2
  echo "  迷ったら shellcheck <file> が SC1071/SC2148 を出すなら ZSH_SYNTAX_FILES、clean なら SHELLCHECK_FILES。" >&2
  echo "  意図的に対象外なら test_lint_coverage.sh の ALLOWLIST へ。" >&2
  rc=1
fi
if [ -n "$missing" ]; then
  echo "✗ lint リストに実在しないファイル (shellcheck が does-not-exist で落ちます):" >&2
  printf '%s\n' "$missing" | sed 's/^/  - /' >&2
  echo "→ Makefile の SHELLCHECK_FILES / ZSH_SYNTAX_FILES から削除。" >&2
  rc=1
fi

if [ "$rc" -eq 0 ]; then
  echo "[lint-coverage] shell script $(printf '%s\n' "$discovered" | grep -c .) 件すべて lint リストに登録済み・列挙は全て実在"
fi
exit "$rc"
