#!/usr/bin/env bash
# check_pipefail_grep_q.sh — `… | grep -q` を pipefail 下で判定に使っている箇所を落とす。
#
# なぜ (issue 096 / CI run 32570242557 で実測):
#   `grep -q` は一致した瞬間に exit する。書き手がまだ書き続けていると EPIPE を受け、
#   `set -o pipefail` (zsh の setopt pipe_fail) 下では**一致していてもパイプライン全体が
#   非 0** になる。判定が反転し、正しい実装が red になる。しかも「実装の不具合」に見えるので
#   調査が逸れる (実際に別セッションが自分の変更を疑う往復をした)。決定的な再現:
#     set -euo pipefail
#     slow() { printf "MATCH\n"; sleep 0.3; printf "tail\n"; }
#     slow | grep -Eq MATCH   # => 非 0 (一致しているのに偽)
#
# 直し方: パイプを外す。
#   printf '%s' "$x" | grep -q PAT   →  grep -q PAT <<< "$x"
#   cmd | grep -q PAT                →  grep -q PAT <<< "$(cmd)"
#   cat file | grep -q PAT           →  grep -q PAT file
#
# 意図的な例外は行内に `pipefail-grep-q: allow` を書く (理由も添えること)。逃げ道が無いと
# 「検査に食われるから書き方を変える」運用になる (no-comment-line-starting-with-shellcheck.md と同族)。
#
# 検査しないもの:
#   - heredoc の本文。テストが作る mock (`cat > mock <<SOMETAG … SOMETAG`) は別プロセスの sh で
#     走り pipefail を継承しないため、同じ形でも判定は反転しない
#   - `|| grep` / `&& grep` (パイプではなく、grep がファイルを読む形)
#   - pipefail を持たないファイル
# 本ファイルの説明文・メッセージには `$x` や `| grep -q` が字として入る (検査の説明そのもの)。
# shellcheck disable=SC2016
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

for c in grep find sort awk; do
  command -v "$c" >/dev/null 2>&1 || { printf '✗ %s が無い。検査できないので緑にしない\n' "$c"; exit 1; }
done

# 対象: discover_shell_scripts.sh が見つける lint 対象 + tests/ 配下の shell script。
# 発見は 2 つとも rc を見る (片方の失敗を、もう片方の件数で緑にしない)
discovered="$(scripts/discover_shell_scripts.sh)" || { printf '✗ discover_shell_scripts.sh が失敗した。検査できないので緑にしない\n'; exit 1; }
test_files="$(find tests -type f \( -name '*.sh' -o -name '*.zsh' \))" || { printf '✗ tests/ の列挙に失敗した。検査できないので緑にしない\n'; exit 1; }
[ -n "$test_files" ] || { printf '✗ tests/ に shell のファイルが 1 つも無い (発見の壊れ)。緑にしない\n'; exit 1; }
files_raw="$(printf '%s\n%s\n' "$discovered" "$test_files" | sort -u)"

files=()
while IFS= read -r f; do
  [ -n "$f" ] && files+=("$f")
done <<< "$files_raw"

# 対象 0 件・極端に少ない = 発見の壊れ。緑にしない (沈黙で無検査になるのを防ぐ)
if [ "${#files[@]}" -lt 20 ]; then
  printf '✗ 検査対象が %d 件しかない (発見の壊れ)。緑にしない\n' "${#files[@]}"
  exit 1
fi

# コメント行と heredoc を飛ばす関数 (検査どうしで共有する)
scl="$(cat scripts/lib/shell_code_lines.awk)" || { printf '✗ scripts/lib/shell_code_lines.awk を読めない\n'; exit 1; }

offenders=""
checked=0
for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  [ -r "$f" ] || { printf '✗ 読めないファイル: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
  checked=$((checked + 1))
  out="$(
    awk "$scl"'
      FNR == 1 { n = 0 }
      { L[++n] = $0 }
      END {
        shell_code_all(n)
        has_pipefail = 0
        for (i = 1; i <= n; i++) if (L[i] ~ /set +-[a-zA-Z]*o[[:space:]]*pipefail|set +-o +pipefail|setopt.*pipe_fail/) has_pipefail = 1
        if (!has_pipefail) exit
        for (i = 1; i <= n; i++) {
          line = C[i]
          if (line == "") continue
          s = line; gsub(/^[[:space:]]+/, "", s)
          if (line ~ /pipefail-grep-q: allow/) continue
          if (line ~ /(\|\||&&)[[:space:]]*(command[[:space:]]+)?grep/) continue
          if (line ~ /\|[[:space:]]*(command[[:space:]]+)?grep[^|]*-[A-Za-z]*q/) printf "%s:%d: %s\n", FILENAME, i, s
        }
      }
    ' "$f"
  )" || { printf '✗ awk が失敗した (%s)。検査できないので緑にしない\n' "$f"; exit 1; }
  [ -n "$out" ] && offenders="${offenders}${out}"$'\n'
done

if [ -n "$offenders" ]; then
  printf '✗ pipefail 下で `… | grep -q` を判定に使っている箇所がある (issue 096: 一致していても非 0 になる):\n'  # pipefail-grep-q: allow (説明文。パイプではない)
  printf '%s' "$offenders" | sed 's/^/    /'
  printf '  直し方: grep -q PAT <<< "$x" / grep -q PAT <<< "$(cmd)" / grep -q PAT file\n'
  printf '  意図的な例外は行内に `pipefail-grep-q: allow` を書く (理由も添える)\n'
  exit 1
fi

printf '✓ pipefail + `| grep -q` の判定反転: %d ファイルに該当なし\n' "$checked"  # pipefail-grep-q: allow (説明文。パイプではない)
