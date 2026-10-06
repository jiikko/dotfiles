#!/usr/bin/env bash
# check_handrolled_timeout.sh — 時間の上限付き実行を runtimeout (bin/runtimeout) 以外で新しく書いた箇所を落とす (issue 643)。
#
# なぜ (issue 640):
#   - macOS に timeout(1) は無い。`command -v timeout && …` の形は手元でも CI でも黙って上限なしで走る
#     (tests/zshrc/bench_zsh.sh がこの形だった)。gtimeout は coreutils を入れた環境にしか無い
#   - `( sleep N; kill $pid ) &` の見張りは pid だけを持つので、子が先に終わると pid の再利用で別のプロセスを撃つ。
#     しかも直接の子しか止めず、孫が残る (scripts/check_assert_reaches_exit.sh の旧 run_probe)
#   直し方: bin/lib/runtimeout.sh の runtimeout_resolve で解決し、"$RUNTIMEOUT" <秒> <cmd>… で呼ぶ (時間切れは rc=124)。
#
# 脅威モデル: 昔の書き方をうっかり書くこと。意図的な迂回 (変数に入れた名前・eval・別名) は対象外。
# 検出する形 (コメント行と heredoc は除く。scripts/lib/shell_code_lines.awk):
#   1. gtimeout という語
#   2. timeout をコマンドとして呼ぶ: 語の境目の timeout の後に、秒数 (60 / 0.5 / 10s) か変数、その後にコマンドらしい語が続く形。
#      文の中の「timeout 60s 超過」のような説明は、秒数の後が日本語なので当たらない (英文の「timeout 10 seconds」は当たる。例外の印で逃がす)
#   3. timeout を探す (`command -v` / `type -P` / `which`)
#   4. 括弧の中の `sleep …; kill` / `sleep … && kill` (見張りの subshell。`{ …; }` も)
# 検出しない形: 変数経由の起動 ("$TIMEOUT_BIN" 等)・.bats ファイル (発見の対象外)・複数行に分けた見張り (`(` の次の行の sleep)・`kill -0` で回す待ちの手組み (書き方が多様で字句では決まらない。
#   レビューで見る)・Go の中 (exec.CommandContext は対象外。issue 640)・shell 以外のファイル。
# 既知の偽の red (安全側。例外の印で逃がす): 行の途中のコメントや文字列の中の gtimeout・`printf "( sleep 1; kill %s )"` の類。
# 意図的な例外は行内に `handrolled-timeout: allow` を書く (理由を添える)。
#
# 使い方: 引数なしで repo 全体 (discover_shell_scripts.sh + tests/ の shell)。引数を渡すとそのファイルだけ (テスト用)。
# 本ファイルの説明文・メッセージには検出する形が字として入るので、本ファイル自身は検査しない。
# 説明文の `$RUNTIMEOUT` は字として出す (展開させない)。
# shellcheck disable=SC2016
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }
self="scripts/check_handrolled_timeout.sh"

for c in awk find sort; do
  command -v "$c" >/dev/null 2>&1 || { printf '✗ %s が無い。検査できないので緑にしない\n' "$c"; exit 1; }
done
scl="$(cat scripts/lib/shell_code_lines.awk)" || { printf '✗ scripts/lib/shell_code_lines.awk を読めない\n'; exit 1; }

files=()
if [ "$#" -gt 0 ]; then
  files=("$@")
else
  # 発見は 2 つとも rc を見る (片方の失敗を、もう片方の件数で緑にしない)
  discovered="$(scripts/discover_shell_scripts.sh)" || { printf '✗ discover_shell_scripts.sh が失敗した。検査できないので緑にしない\n'; exit 1; }
  test_files="$(find tests -type f \( -name '*.sh' -o -name '*.zsh' \))" || { printf '✗ tests/ の列挙に失敗した。検査できないので緑にしない\n'; exit 1; }
  [ -n "$test_files" ] || { printf '✗ tests/ に shell のファイルが 1 つも無い (発見の壊れ)。緑にしない\n'; exit 1; }
  files_raw="$(printf '%s\n%s\n' "$discovered" "$test_files" | sort -u)"
  while IFS= read -r f; do
    [ -n "$f" ] && [ "$f" != "$self" ] && files+=("$f")
  done <<< "$files_raw"
  # 対象 0 件・極端に少ない = 発見の壊れ。緑にしない
  if [ "${#files[@]}" -lt 20 ]; then
    printf '✗ 検査対象が %d 件しかない (発見の壊れ)。緑にしない\n' "${#files[@]}"
    exit 1
  fi
fi

offenders=""
checked=0
for f in "${files[@]}"; do
  [ -f "$f" ] || { [ "$#" -gt 0 ] && { printf '✗ ファイルが無い: %s\n' "$f"; exit 1; }; continue; }
  [ -r "$f" ] || { printf '✗ 読めないファイル: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
  checked=$((checked + 1))
  out="$(
    awk "$scl"'
      FNR == 1 { n = 0 }
      { L[++n] = $0 }
      END {
        shell_code_all(n)
        for (i = 1; i <= n; i++) {
          line = C[i]
          if (line == "" || L[i] ~ /handrolled-timeout: allow/) continue
          why = ""
          if (line ~ /(^|[^A-Za-z0-9_.-])gtimeout([^A-Za-z0-9_-]|$)/) why = "gtimeout"
          else if (line ~ /(command[[:space:]]+-v|type[[:space:]]+-[a-zA-Z]*[Pp][a-zA-Z]*|which)[[:space:]]+timeout([^A-Za-z0-9_-]|$)/) why = "timeout を探している"
          else if (line ~ /(^|[[:space:];&|(!`\/])timeout[[:space:]]+(-[A-Za-z-]+(=[^[:space:]]+)?[[:space:]]+([A-Z]+[[:space:]]+|[0-9.]+[smhd]?[[:space:]]+)?)*("[^"]*"|\$[{][^}]*[}]|\$[A-Za-z_][A-Za-z0-9_]*|[0-9.]+[smhd]?)[[:space:]]+[A-Za-z0-9"$\/._~-]/) why = "timeout を呼んでいる"
          else if (line ~ /[({][[:space:]]*sleep[^;&]*(;|&&)[[:space:]]*kill([^A-Za-z0-9_-]|$)/) why = "( sleep …; kill ) の見張り"
          if (why != "") {
            s = L[i]; gsub(/^[[:space:]]+/, "", s)
            printf "%s:%d: [%s] %s\n", FILENAME, i, why, s
          }
        }
      }
    ' "$f"
  )" || { printf '✗ awk が失敗した (%s)。検査できないので緑にしない\n' "$f"; exit 1; }
  [ -n "$out" ] && offenders="${offenders}${out}"$'\n'
done

if [ -n "$offenders" ]; then
  printf '✗ 時間の上限付き実行を runtimeout 以外で書いている箇所がある (issue 640 / 643):\n'
  printf '%s' "$offenders" | sed 's/^/    /'
  printf '  直し方: . bin/lib/runtimeout.sh; runtimeout_resolve <repo root>; "$RUNTIMEOUT" <秒> <cmd>… (時間切れは rc=124)\n'
  printf '  意図的な例外は行内に `handrolled-timeout: allow` を書く (理由も添える)\n'
  exit 1
fi

printf '✓ 上限付き実行の手組み (timeout / gtimeout / sleep+kill の見張り): %d ファイルに該当なし\n' "$checked"
