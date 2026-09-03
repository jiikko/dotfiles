#!/usr/bin/env bash
# 新しい Go プロジェクトが「CI レーン無し」で入るのを止める検査 (issue 203 候補 B)。
#
# なぜ: 出典は issue 080 / 087。以前は Makefile の GO_PROJECT_DIRS を手で列挙しており、
# 追記を忘れたプロジェクトが **無音で lint / test から外れた**。Makefile 側は
# `wildcard src/*/go.mod` で解決済み (Makefile:392) だが、**CI の paths filter と
# プロジェクト側の Makefile target は今も手で用意する**ので、同じ穴が残っている。
#
# 検査する不変条件 (src/*/go.mod を出典に、3 つ):
#   1. src/<name>/Makefile に lint: と test: の両方がある
#      (Makefile の run_go_projects が `make -C <dir> lint` / `test` を呼ぶ)
#   2. .github/workflows/src_<name>.yml がある (paths filter つきの薄い caller)
#   3. その workflow の paths が src/<name>/ を含む (含まないと push で 1 度も起動しない)
#
# ⚠️ 検査できなかったときに緑を返さない。依存コマンド不在・発見 0 件はすべて失敗にする
#   (`_claude/rules/adversarial-review-own-safeguards.md` の false green)。
# ⚠️ paths の検査は「行として現れるか」の静的検査。YAML として解釈しないので、
#   コメントアウトされた paths も通ってしまう。取りこぼすより出す側へ倒す方針は変えない
#   (誤って通す形は下の quoted/unquoted 両対応で狭めてある)。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

fail() { printf '✗ %s\n' "$1"; exit 1; }

for c in grep find sed basename; do
  command -v "$c" >/dev/null 2>&1 || fail "$c が無い。検査できないので緑にしない"
done
grep -q probe <<< 'probe' || fail "grep が正常に動作しない"

# 出典は Makefile と同じ `src/*/go.mod` (手で列挙しない)
projects=$(find src -mindepth 2 -maxdepth 2 -name go.mod 2>/dev/null | sed 's|^src/||; s|/go.mod$||' | sort)
[ -n "$projects" ] || fail "src/*/go.mod が 1 件も見つからない (発見の仕方が壊れている)"

bad=0 n=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  n=$((n + 1))
  mk="src/$name/Makefile"
  wf=".github/workflows/src_$name.yml"

  if [ ! -f "$mk" ]; then
    printf '✗ %s: %s が無い (make -C で lint / test を呼べない)\n' "$name" "$mk"; bad=$((bad + 1)); continue
  fi
  for t in lint test; do
    # ⚠️ 行頭の target 定義だけを見る (`.PHONY: lint test` の行や `test-foo:` に当てない)
    grep -qE "^$t:" "$mk" || { printf '✗ %s: %s に %s: target が無い\n' "$name" "$mk" "$t"; bad=$((bad + 1)); }
  done

  if [ ! -f "$wf" ]; then
    printf '✗ %s: %s が無い (push しても CI が起動しない)\n' "$name" "$wf"; bad=$((bad + 1)); continue
  fi
  # paths の指定は quoted / unquoted の両方が使われている ('src/glogx/**' と - src/doctor/**)
  grep -qE "^[[:space:]]*-[[:space:]]*'?\"?src/$name/" "$wf" \
    || { printf '✗ %s: %s の paths が src/%s/ を含まない (この変更では CI が走らない)\n' "$name" "$wf" "$name"; bad=$((bad + 1)); }
done <<< "$projects"

[ "$n" -gt 0 ] || fail "検査対象が 0 件 (発見が壊れている)"
if [ "$bad" -gt 0 ]; then
  printf '  直し方: src/<name>/Makefile に lint:/test: を足し、.github/workflows/src_<name>.yml を\n'
  printf '  既存の src_*.yml に倣って作る (paths に src/<name>/** と _go-project.yml を含める)\n'
  exit 1
fi
printf '✓ Go プロジェクト %s 件すべてに lint/test target と CI レーン (paths つき) がある\n' "$n"
