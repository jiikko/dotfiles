#!/usr/bin/env bash
# tuikit と、tuikit を go.mod に持つ src/* が、(1) 同じ版の x/ansi で解決し、(2) 折り返しの x/ansi の直呼びを forbidigo で禁止していることを pin する。
#
# なぜ: tuikit は幅・切り詰め・折り返しの単一の出典 (termwidth) を、自分の go.mod の x/ansi の版でテストする。消費者が別の版で
# tuikit をビルドすると、tuikit のテストが通ったまま消費者の表示だけがずれうる (issue 603: schedkeys だけ v0.11.8 だった)。
# ansi の禁止は各 module の .golangci.yml にコピーされていて、新しい消費者・新しい禁止で漏れる (issue 590 の折り返しの禁止が 2 module だけだった)。
# 版は go.mod の行ではなく `go list -m` (MVS の解決結果) で見る: go.mod に x/ansi の行を持たない消費者もある。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

ansi=github.com/charmbracelet/x/ansi
wrap_forbid='pattern: ^ansi\.(Hardwrap|Wrap|Wordwrap)$'
fail=0; n=0; want=""
for mod in src/*/go.mod; do
  [ -f "$mod" ] || continue
  dir=${mod%/go.mod}
  if [ "$dir" != src/tuikit ] && ! grep -qF 'src/tuikit' "$mod"; then
    continue
  fi
  n=$((n + 1))
  # 🚨 判定できない (go list が失敗した) を「揃っている」にしない
  if ! ver=$(cd "$dir" && go list -m -f '{{.Version}}' "$ansi" 2>&1); then
    printf '  ✗ %s: x/ansi の版を解決できない: %s\n' "$dir" "$ver"; fail=1; continue
  fi
  want=${want:-$ver}
  if [ "$ver" != "$want" ]; then
    printf '  ✗ %s: x/ansi %s (ほかは %s。tuikit がテストした版と揃える)\n' "$dir" "$ver" "$want"; fail=1
  fi
  if ! grep -qF -- "$wrap_forbid" "$dir/.golangci.yml"; then
    printf '  ✗ %s: .golangci.yml の forbidigo に %s が無い (折り返しは termwidth.Wrap / WordWrap。issue 590)\n' "$dir" "$wrap_forbid"; fail=1
  fi
  printf '  · %s: x/ansi %s\n' "$dir" "$ver"
done
# 発見 0 件は失敗 (tuikit の配置や require の書き方が変わって対象を見失っても緑にしない)。tuikit 自身と消費者 1 つ以上
[ "$n" -ge 2 ] || { printf '✗ tuikit とその消費者が %d module しか見つからない (発見が壊れている)\n' "$n"; exit 1; }
[ "$fail" -eq 0 ] || { printf '✗ tuikit の消費者の x/ansi の版か折り返しの禁止がずれている (%d module 検査)\n' "$n"; exit 1; }
printf '✓ tuikit と消費者 %d module が x/ansi %s で揃い、折り返しの直呼びを禁止している\n' "$n" "$want"
