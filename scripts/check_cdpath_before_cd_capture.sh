#!/usr/bin/env bash
# check_cdpath_before_cd_capture.sh — `$(cd …)` でパスを取る前に CDPATH を外していない行を落とす。
#
# なぜ (2026-10-07):
#   CDPATH が export された環境では、相対パスへの cd が CDPATH の要素 (`.` を含む) で解決すると、
#   解決先を stdout に出す。`ROOT="$(cd "$(dirname "$0")/.." && pwd)"` はパスを 2 行持ち、
#   後続のパスが全部壊れる。手元の CDPATH の下で tests/bin/test_mutate_verify*.sh が
#   「bin/mutate-verify が無い」で落ち、同じ形が 25 本あった (CI は CDPATH を export しないので緑のまま)。
#
# 判定 (合格の形・脅威モデル・検出しないと決めた形・偽陽性) の正本は bin/check-cdpath-capture。ここは dotfiles の対象を探して一覧を渡すだけ
# (bin/check-cdpath-capture は git の知っているファイルを全部見るので、vendor/ の他人のコードまで拾う。dotfiles では対象を lint の発見規約に揃える)。
#
# 対象: discover_shell_scripts.sh の結果 + tests/ の *.sh・*.bats・*.zsh・拡張子の無いもの + githooks/。
#   zsh も含める (zsh の cd も cdpath で解決すると解決先を出す)。
# テスト用の差し替え口: CHECK_CDPATH_FILES (改行区切りの対象の一覧。発見を飛ばす) /
#   _DISCOVER (発見のコマンド) / _TESTS_DIR / _HOOKS_DIR / _MIN_FILES (入力ごとの下限)
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

# 判定へ渡す一覧は絶対パスにする (bin/check-cdpath-capture は相対パスを自分の cwd から読む)
abs_list=""
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in /*) abs_list+="$f"$'\n' ;; *) abs_list+="$ROOT_DIR/$f"$'\n' ;; esac
done <<< "$files_raw"
[ -n "$abs_list" ] || { printf '✗ 検査対象が 0 件。緑にしない\n'; exit 1; }
CHECK_CDPATH_FILES="$abs_list" exec "$ROOT_DIR/bin/check-cdpath-capture"
