#!/usr/bin/env bash
# check_cdpath_before_cd_capture.sh — `$(cd …)` でパスを取る前に CDPATH を外していない行を落とす。
#
# なぜ (2026-10-07):
#   CDPATH が export された環境では、相対パスへの cd が CDPATH の要素 (`.` を含む) で解決すると、
#   解決先を stdout に出す。`ROOT="$(cd "$(dirname "$0")/.." && pwd)"` はパスを 2 行持ち、
#   後続のパスが全部壊れる。手元の CDPATH の下で tests/bin/test_mutate_verify*.sh が
#   「bin/mutate-verify が無い」で落ち、同じ形が 25 本あった (CI は CDPATH を export しないので緑のまま)。
#
# 🚨 脅威モデル: 止めるのは「`$(cd` を書いたのに CDPATH を外し忘れた」うっかりだけ。意図的な迂回は review の責務。
# 合格の形 (どちらか):
#   - その置換より前に、コマンドの位置で `unset CDPATH` (`-v` / `--` も可) か `CDPATH=` (空の代入) を書いている (実行ファイルの形)。
#     同じ行なら置換より左にあること。行末コメント・文字列の中の「unset CDPATH」は数えない
#   - その行の置換が `$(CDPATH='' cd …)` / `$(CDPATH= cd …)` (source される lib の形。unset すると呼び出し側の環境まで変える)
# 意図的な例外は行内に `cdpath: allow` を書く (理由も添える)。
# 🚨 検出しないと決めた形:
#   - 置換の外の cd (`cd "$d" || exit 1`)。出力を取り込まないので壊れない (端末に 1 行出るだけ)
#   - バッククォートの `` `cd …` `` / `$(builtin cd` / `$(command cd` / 変数・eval 経由 (2026-10-07 時点で対象に 0 件)
#   - `unset CDPATH` が関数の中にあり、その関数が置換より後に呼ばれる形 (行の順だけを見る)
#   - コメント行 (先頭が #) の中の `$(cd`
#   - 複数行にまたがる置換 (`$(` の後で改行・行継続してから cd) / `$(set -e; cd …)` のように cd の前に別のコマンドを置く形 /
#     `$(CDPATH=. cd …)` のような非空の前置代入 (2026-10-07 時点で対象に 0 件)
#   - unset のスコープ・順序: サブシェルや if の中の unset もファイル全体の免除に数える / unset の後で CDPATH を設定し直す形
#   - zsh の小文字の `cdpath` だけを外す形 (`unset CDPATH` を書けば通る)
#   - 引用文字列の中身は区別しない: 文字列の中の ` #` 以降をコメントとして捨てる (同じ行の後ろの置換を見落とす) /
#     文字列の中の `; unset CDPATH` や `echo todo unset CDPATH` を外したものと数える。字句の判定は直すたびに別の迂回が出る
#     (敵対的レビュー 2 周目, 2026-10-07)。うっかりの書き漏れでこの形になることは稀なので、ここで打ち切る
# 🚨 検出してしまうと受け入れた形 (偽陽性): `$(cd ..)` / `$(cd /abs)` / `$(cd ./x)` は CDPATH を見ないので壊れないが落とす。
#   「$(cd を書いたら CDPATH を外す」を一律の規則にした方が、どの形が安全かを書く人に判断させるより単純なため。`cdpath: allow` で通す
#
# 対象: discover_shell_scripts.sh の結果 + tests/ の *.sh・*.bats・*.zsh・拡張子の無いもの + githooks/。
#   zsh も含める (zsh の cd も cdpath で解決すると解決先を出す)。
# テスト用の差し替え口: CHECK_CDPATH_FILES (改行区切りの対象の一覧。発見を飛ばす) /
#   _DISCOVER (発見のコマンド) / _TESTS_DIR / _HOOKS_DIR / _MIN_FILES (入力ごとの下限)
# shellcheck disable=SC2016  # 案内文の $(cd …) は展開させない文字列
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)" || exit 1
cd "$ROOT_DIR" || exit 1

discover="${CHECK_CDPATH_DISCOVER:-scripts/discover_shell_scripts.sh}"
tests_dir="${CHECK_CDPATH_TESTS_DIR:-tests}"
hooks_dir="${CHECK_CDPATH_HOOKS_DIR:-githooks}"
min="${CHECK_CDPATH_MIN_FILES:-100}"

count_lines() { local n=0 l; while IFS= read -r l; do [ -n "$l" ] && n=$((n + 1)); done <<< "$1"; printf '%d' "$n"; }
# $1=入力の名前 $2=一覧 $3=下限
need() {
  local n; n="$(count_lines "$2")"
  [ "$n" -ge "$3" ] || { printf '✗ %s の対象が %d 件しかない (下限 %d。発見の壊れ)。緑にしない\n' "$1" "$n" "$3"; exit 1; }
}

if [ -n "${CHECK_CDPATH_FILES:-}" ]; then
  files_raw="$CHECK_CDPATH_FILES"
else
  d_out="$("$discover")" || { printf '✗ %s が失敗した。検査できないので緑にしない\n' "$discover"; exit 1; }
  need "$discover" "$d_out" "$min"
  [ -d "$tests_dir" ] || { printf '✗ %s が無い。検査できないので緑にしない\n' "$tests_dir"; exit 1; }
  t_out="$(find "$tests_dir" -type f \( -name '*.sh' -o -name '*.bats' -o -name '*.zsh' -o ! -name '*.*' \))" \
    || { printf '✗ %s の列挙に失敗した。検査できないので緑にしない\n' "$tests_dir"; exit 1; }
  need "$tests_dir" "$t_out" "$min"
  [ -d "$hooks_dir" ] || { printf '✗ %s が無い。検査できないので緑にしない\n' "$hooks_dir"; exit 1; }
  h_out="$(find "$hooks_dir" -type f)" || { printf '✗ %s の列挙に失敗した。検査できないので緑にしない\n' "$hooks_dir"; exit 1; }
  need "$hooks_dir" "$h_out" 1
  files_raw="$(printf '%s\n%s\n%s\n' "$d_out" "$t_out" "$h_out" | sort -u)" \
    || { printf '✗ 対象の一覧を作れない (sort が失敗した)。検査できないので緑にしない\n'; exit 1; }
fi

n_files=0
bad=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  # 一覧にあるのにファイルでないものは、発見の壊れか一覧の誤り。黙って飛ばすと一部しか検査せずに緑になる
  [ -f "$f" ] || { printf '✗ 一覧にあるがファイルでない: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
  [ -r "$f" ] || { printf '✗ 読めないファイル: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
  n_files=$((n_files + 1))
  # 行の順に読み、CDPATH を外す前の `$(cd` を出す。awk の失敗は「検査できなかった」として止める。
  # 外す形はコマンドの位置 (行頭か ; & | ( { then do else の後) に限る。文字列・行末コメントの中の語を数えないため。
  # `CDPATH=` は単独の代入 (後ろが行末か ; & |) だけを数える。`$(CDPATH='' cd …)` の前置代入はその置換にしか効かない
  hits="$(awk '
    /^[[:space:]]*#/ { next }
    {
      code = $0
      sub(/[[:space:]]#.*$/, "", code)
      # unset と空の代入を別々に探し、左にある方を採る (片方だけ探すと、後ろの unset が前の CDPATH= を隠す)
      upos = 0
      if (match(code, /(^|[;&|({]|then|do|else)[[:space:]]*unset([[:space:]]+(-v|--))*[[:space:]]+([A-Za-z_][A-Za-z0-9_]*[[:space:]]+)*CDPATH([[:space:];&|)]|$)/)) upos = RSTART
      if (match(code, /(^|[;&|({]|then|do|else)[[:space:]]*CDPATH=[\047"]?[\047"]?[[:space:]]*($|[;&|])/) && (!upos || RSTART < upos)) upos = RSTART
      cpos = match(code, /\$\([[:space:]]*cd[[:space:]]/) ? RSTART : 0
      if (cpos && !seen && !(upos && upos < cpos) && index($0, "cdpath: allow") == 0) print FNR ": " $0
      if (upos) seen = 1
    }
  ' "$f")" || { printf '✗ awk が %s で失敗した。検査できないので緑にしない\n' "$f"; exit 1; }
  if [ -n "$hits" ]; then
    while IFS= read -r h; do printf '%s:%s\n' "$f" "$h"; done <<< "$hits"
    bad=1
  fi
done <<< "$files_raw"

[ "$n_files" -ge 1 ] || { printf '✗ 検査対象が 0 件。緑にしない\n'; exit 1; }
if [ "$bad" -ne 0 ]; then
  printf '✗ 上の行は CDPATH を外さずに $(cd …) でパスを取っている。ファイルの先頭 (set の直後) に unset CDPATH を置くか、\n'
  printf '  source される lib なら $(CDPATH='"''"' cd …) にする。意図的なら行に cdpath: allow と理由を書く\n'
  exit 1
fi
printf '✓ CDPATH を外さない $(cd …): %d ファイルに該当なし\n' "$n_files"
