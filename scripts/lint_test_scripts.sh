#!/bin/sh
# lint_test_scripts.sh — tests/ 配下のテストスクリプトを shebang で方言判別して lint する。
# Makefile の test-lint-tests から呼ばれる。
#
# discover_shell_scripts.sh (本体スクリプトの lint 発見) が tests/ を対象外にしている理由は
# 「zsh スクリプトが約半数を占め、ZSH_SYNTAX_FILES 方式 (手動例外リスト) では 40 本超の
# リスト維持が必要になる」ため。ここでは shebang による機械分類で例外リストなしに全数を
# カバーする: zsh shebang → zsh -n (構文のみ)、それ以外 → shellcheck。
#
# 静的解析 (shellcheck) は -S warning で回す (info 級は SC2016 「シングルクォート内の $ が
# 展開されない」等、テストの意図的な書き方への誤指摘が大量で S/N が悪い。warning/error のみ拾う)。
set -eu
unset CDPATH
cd "$(dirname "$0")/.."

command -v shellcheck >/dev/null 2>&1 || { echo "[lint-tests] shellcheck not found; skipping"; exit 0; }

# 発見 0 件は fail (discover_shell_scripts.sh と同じ規律: ディレクトリ改名や find の失敗を
# 「未実行なのに成功」にしない)
files=$(find tests -type f -name '*.sh' | sort)
[ -n "$files" ] || { echo "✗ tests/ 配下に *.sh が見つかりません (find 失敗 or 0 件)" >&2; exit 1; }

zsh_files=""
sh_files=""
while IFS= read -r f; do
  if head -1 "$f" | grep -qE '^#!.*zsh'; then
    zsh_files="$zsh_files $f"
  else
    sh_files="$sh_files $f"
  fi
done <<EOF
$files
EOF

# shellcheck disable=SC2086 # ファイル名の空白・改行は非対応 (run_tests と同じ repo 前提) のため意図的に単語分割する
{
  [ -z "$zsh_files" ] || zsh -n $zsh_files
  [ -z "$sh_files" ] || shellcheck -S warning $sh_files
}
echo "[lint-tests] zsh -n $(echo "$zsh_files" | wc -w | tr -d ' ') 本 + shellcheck $(echo "$sh_files" | wc -w | tr -d ' ') 本 OK"
