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
cd "$(dirname "$0")/.." || exit 1

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

# ⚠️ zsh -n は 1 ファイルずつ回す (issue 056): `zsh -n f1 f2` は f1 しかパースせず、
# f2 以降は f1 への位置パラメータになる。多引数で渡すと 2 本目以降が無検査のまま
# 「N 本 OK」と報告する false green になる。件数は「渡した数」でなく検査した数を数える。
zsh_checked=0
rc=0
for f in $zsh_files; do
  zsh -n "$f" || rc=1
  zsh_checked=$((zsh_checked + 1))
done
[ "$rc" -eq 0 ] || exit 1

# 静的解析ツール (shellcheck) 不在でも zsh -n の結果は返す (不在を理由に全体を素通りさせない)。
# 何を検査しなかったかを明示する (「skipping」だけの緑は読み手に成功と誤読される)
sh_count=$(echo "$sh_files" | wc -w | tr -d ' ')
if command -v shellcheck >/dev/null 2>&1; then
  # shellcheck disable=SC2086 # ファイル名の空白・改行は非対応 (run_tests と同じ repo 前提) のため意図的に単語分割する
  [ -z "$sh_files" ] || shellcheck -S warning $sh_files
  echo "[lint-tests] zsh -n $zsh_checked 本 + shellcheck $sh_count 本 OK"
else
  echo "[lint-tests] zsh -n $zsh_checked 本 OK / ⚠️ shellcheck 未導入のため sh 系 $sh_count 本は未検査"
fi
