#!/usr/bin/env bash
# tuikit を import するパッケージのテストが、幅の環境の検査 (widthenv.ExitIfUnsupported) を呼ぶことを pin する (issue 701 の 2)。
#
# なぜ: RUNEWIDTH_EASTASIAN=1 では幅の数え方が変わり、幅を見るテストが大量に落ちる (pro-con/ui で「行の幅 131 が画面の幅 120 を
# 超える」)。検査を呼ばないパッケージでは、落ちた理由が環境だと分からない。
# 脅威モデル: うっかりの漏れ (tuikit を使うパッケージにテストを足したが TestMain で呼ばない) を止める。
# 拾う形: src/<module> の .go のどれかが github.com/jiikko/dotfiles/src/tuikit/ を import しているディレクトリで、_test.go を持つもの。
# 検出しない形: 呼んでいるが m.Run の後 (効かない)・TestMain 以外の関数の中・`//go:build ignore` などで外れる _test.go の中・
# コメントの中の文字列・testdata の下・tuikit 自身 (部品どうしの import は数えない)
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

pkgs=0; bad=0
while IFS= read -r dir; do
  case "$dir" in src/tuikit|src/tuikit/*) continue ;; esac
  shopt -s nullglob
  gofiles=("$dir"/*.go); tests=("$dir"/*_test.go)
  shopt -u nullglob
  [ "${#tests[@]}" -gt 0 ] || continue
  # import の行の形 (引用符で始まる path) で探す (コメントに path を書いただけのファイルを数えない)。読めないファイルは
  # 判定不能として落とす (grep の rc=2 を「無い」に丸めない)
  grep -lq '"github.com/jiikko/dotfiles/src/tuikit/' "${gofiles[@]}"; rc=$?
  [ "$rc" -le 1 ] || { printf '✗ %s の .go を読めない (判定できない)\n' "$dir" >&2; exit 1; }
  [ "$rc" -eq 0 ] || continue
  pkgs=$((pkgs + 1))
  grep -lq 'widthenv\.ExitIfUnsupported()' "${tests[@]}"; rc=$?
  [ "$rc" -le 1 ] || { printf '✗ %s の _test.go を読めない (判定できない)\n' "$dir" >&2; exit 1; }
  if [ "$rc" -ne 0 ]; then
    printf '✗ %s: tuikit を使うのに、テストが widthenv.ExitIfUnsupported を呼ばない (TestMain で呼ぶ)\n' "$dir" >&2
    bad=$((bad + 1))
  fi
done < <(find src -name '*_test.go' ! -path '*/testdata/*' -exec dirname {} \; | sort -u)

# 空振り (走査の根や判定が壊れて 0 件) を緑にしない。2026-10-09 に 9 パッケージ
if [ "$pkgs" -lt 8 ]; then
  printf '✗ tuikit を使うテストのパッケージが %d 個しか見つからない (走査が壊れている)\n' "$pkgs" >&2
  exit 1
fi
[ "$bad" -eq 0 ] || exit 1
printf '✓ tuikit を使うテストのパッケージ %d 個がすべて幅の環境の検査を呼ぶ\n' "$pkgs"
