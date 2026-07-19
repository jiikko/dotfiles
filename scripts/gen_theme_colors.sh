#!/bin/sh
# theme/colors.yml (色カタログの単一ソース) から 2 つの成果物を生成する:
#   - scripts/lib/theme_colors.sh : shell 定数 (sh スクリプトが source)
#   - src/git-popup/theme_gen.go  : Go の cterm マップ (git-popup TUI が参照)
# 使い方:
#   scripts/gen_theme_colors.sh              # 両方を生成して上書き
#   scripts/gen_theme_colors.sh --stdout     # shell 版を stdout へ (テストの drift 検査用)
#   scripts/gen_theme_colors.sh --go-stdout  # Go 版を stdout へ (テストの drift 検査用)
# yml のパースは「role: / 2 スペース indent の cterm:/hex:」という本 repo の固定書式にのみ
# 対応する意図的に薄い awk (yq 等の依存を増やさない)。書式を崩すとテストが落ちて気づける。
set -eu

root_dir=$(cd "$(dirname "$0")/.." && pwd)
yml="$root_dir/theme/colors.yml"
out="$root_dir/scripts/lib/theme_colors.sh"
out_go="$root_dir/src/git-popup/theme_gen.go"

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

# git-popup (Go TUI) 用の cterm マップ。全 role を map リテラルに入れるので Go の
# unused (staticcheck U1000) には当たらない。TUI は themeCterm[role] で必要な色を引く。
generate_go() {
  # 出力は gofmt に通して整形する (map リテラルの桁揃え。golangci-lint の gofmt 検査対策)。
  {
    printf '// Code generated from theme/colors.yml by scripts/gen_theme_colors.sh; DO NOT EDIT.\n'
    printf 'package main\n\n'
    printf '// themeCterm は theme role → 256 色番号。単一ソースは theme/colors.yml。\n'
    printf '// 色を変えたら colors.yml を編集して scripts/gen_theme_colors.sh を実行する。\n'
    printf 'var themeCterm = map[string]int{\n'
    awk '
      /^[a-z_]+:/ { role = $1; sub(/:.*/, "", role); next }
      /^  cterm:/ { printf "\t\"%s\": %s,\n", role, $2 }
    ' "$yml"
    printf '}\n'
  } | gofmt
}

case "${1:-}" in
--stdout) generate ;;
--go-stdout) generate_go ;;
*)
  generate > "$out"
  printf 'generated: %s\n' "$out"
  generate_go > "$out_go"
  printf 'generated: %s\n' "$out_go"
  ;;
esac
