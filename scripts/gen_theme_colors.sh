#!/bin/sh
# theme/colors.yml (色カタログの単一ソース) から成果物を生成する:
#   - scripts/lib/theme_colors.sh : shell 定数 (sh スクリプトが source)
# (かつては src/git-popup/theme_gen.go も生成していたが、git-popup の退役
#  2026-08-06 で Go の消費者が消えたため生成ごと畳んだ)
# 使い方:
#   scripts/gen_theme_colors.sh              # 生成して上書き
#   scripts/gen_theme_colors.sh --stdout     # stdout へ (テストの drift 検査用)
# yml のパースは「role: / 2 スペース indent の cterm:/hex:」という本 repo の固定書式にのみ
# 対応する意図的に薄い awk (yq 等の依存を増やさない)。書式を崩すとテストが落ちて気づける。
set -eu

root_dir=$(cd "$(dirname "$0")/.." && pwd)
yml="$root_dir/theme/colors.yml"
out="$root_dir/scripts/lib/theme_colors.sh"

generate() {
  printf '# shellcheck shell=sh\n'
  printf '# AUTO-GENERATED from theme/colors.yml — 手で編集しない。\n'
  printf '# 変更は theme/colors.yml を編集して scripts/gen_theme_colors.sh を実行する。\n'
  printf '# (一致検査: tests/theme/test_theme_colors.sh)\n'
  printf '# 定数は source 先で使われる (SC2034 は誤検知)\n'
  printf '# shellcheck disable=SC2034\n'
  awk '
    /^[a-z_]+:/ {
      role = $1
      sub(/:.*/, "", role)
      name = toupper(role)
      next
    }
    /^  cterm:/ { printf "THEME_%s=%s\n", name, $2 }
    /^  hex:/   { gsub(/"/, "", $2); printf "THEME_%s_HEX=\"%s\"\n", name, $2 }
  ' "$yml"
}

case "${1:-}" in
--stdout) generate ;;
*)
  generate > "$out"
  printf 'generated: %s\n' "$out"
  ;;
esac
