#!/usr/bin/env bash
# tuikit と、tuikit を go.mod に持つ src/* が、(1) 同じ版の x/ansi・bubbletea・ultraviolet で解決し、(2) 折り返しの x/ansi の直呼びを forbidigo で禁止していることを pin する。
#
# なぜ: tuikit は幅・切り詰め・折り返しの単一の出典 (termwidth) を、自分の go.mod の x/ansi の版でテストする。消費者が別の版で
# tuikit をビルドすると、tuikit のテストが通ったまま消費者の表示だけがずれうる (issue 603: schedkeys だけ v0.11.8 だった)。
# ansi の禁止は各 module の .golangci.yml にコピーされていて、新しい消費者・新しい禁止で漏れる (issue 590 の折り返しの禁止が 2 module だけだった)。
# 版は go.mod の行ではなく `go list -m` (MVS の解決結果) で見る: go.mod に x/ansi の行を持たない消費者もある。
# 🚨 GOWORK=off で解決する: go.work があると全 module が workspace の版で解決され、ずれが見えなくなる。
# bubbletea の版も見る (603 で schedkeys の v2.0.9 を v2.0.8 に揃えた)。ultraviolet も見る (tuikit/framebench が bubbletea のレンダラを再現するのに使う。
# tuikit が f5a850f9、glogx・pro-con が 8b693049 でずれていた。2026-10-02)。tuikit 自身の版は見ない (restartable は go install のために疑似版で固定する)。
# 禁止の検査の脅威モデル: うっかりの漏れ (行を足し忘れる・コメントにする・forbidigo を enable から外す・exclusions で issue 590 を広く外す) を止める。
# 検出しない形: YAML の別の書き方 (flow style・アンカー)・forbid の節の外に同じ字面の行を置く・msg の書き換え。.golangci.yml を壊す変更は各 module の lint が止める
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

ansi=github.com/charmbracelet/x/ansi
tea=charm.land/bubbletea/v2
uv=github.com/charmbracelet/ultraviolet
wrap_forbid_re='^ +- pattern: \^ansi\\\.\(Hardwrap\|Wrap\|Wordwrap\)\$$'
fail=0; n=0; want=""; want_tea=""; want_uv=""
for mod in src/*/go.mod; do
  [ -f "$mod" ] || continue
  dir=${mod%/go.mod}
  if [ "$dir" != src/tuikit ] && ! grep -qF 'src/tuikit' "$mod"; then
    continue
  fi
  n=$((n + 1))
  # 🚨 判定できない (go list が失敗した) を「揃っている」にしない
  if ! vers=$(cd "$dir" && GOWORK=off go list -m -f '{{.Version}}' "$ansi" "$tea" "$uv" 2>&1) || [ "$(printf '%s\n' "$vers" | grep -c .)" -ne 3 ]; then
    printf '  ✗ %s: x/ansi / bubbletea / ultraviolet の版を解決できない: %s\n' "$dir" "$vers"; fail=1; continue
  fi
  ver=$(printf '%s\n' "$vers" | sed -n 1p); tver=$(printf '%s\n' "$vers" | sed -n 2p); uver=$(printf '%s\n' "$vers" | sed -n 3p)
  want=${want:-$ver}; want_tea=${want_tea:-$tver}; want_uv=${want_uv:-$uver}
  if [ "$ver" != "$want" ] || [ "$tver" != "$want_tea" ] || [ "$uver" != "$want_uv" ]; then
    printf '  ✗ %s: x/ansi %s / bubbletea %s / ultraviolet %s (ほかは %s / %s / %s。tuikit がテストした版と揃える)\n' "$dir" "$ver" "$tver" "$uver" "$want" "$want_tea" "$want_uv"; fail=1
  fi
  yml="$dir/.golangci.yml"
  if ! grep -qE -- "$wrap_forbid_re" "$yml"; then
    printf '  ✗ %s: forbidigo の forbid に ^ansi\\.(Hardwrap|Wrap|Wordwrap)$ が無い (コメントの行は数えない。折り返しは termwidth.Wrap / WordWrap。issue 590)\n' "$dir"; fail=1
  fi
  if ! grep -qE '^    - forbidigo([[:space:]]|$)' "$yml"; then
    printf '  ✗ %s: linters.enable に forbidigo が無い (禁止の行があっても効かない)\n' "$dir"; fail=1
  fi
  # issue 590 を外してよいのは tuikit の termwidth (x/ansi を包む層) とそのテストだけ
  while IFS= read -r prev; do
    if [ "$dir" != src/tuikit ] || [ "$prev" != '      - path: (termwidth/|_test\.go)' ]; then
      printf '  ✗ %s: exclusions が issue 590 (折り返しの禁止) を外している (直前の行: %s)\n' "$dir" "$prev"; fail=1
    fi
  done < <(awk '/^ +text: .*issue 590/ { print prev } { prev = $0 }' "$yml")
  printf '  · %s: x/ansi %s / bubbletea %s / ultraviolet %s\n' "$dir" "$ver" "$tver" "$uver"
done
# 発見 0 件は失敗 (tuikit の配置や require の書き方が変わって対象を見失っても緑にしない)。tuikit 自身と消費者 1 つ以上
[ "$n" -ge 2 ] || { printf '✗ tuikit とその消費者が %d module しか見つからない (発見が壊れている)\n' "$n"; exit 1; }
[ "$fail" -eq 0 ] || { printf '✗ tuikit の消費者の x/ansi の版か折り返しの禁止がずれている (%d module 検査)\n' "$n"; exit 1; }
printf '✓ tuikit と消費者 %d module が x/ansi %s / bubbletea %s / ultraviolet %s で揃い、折り返しの直呼びを禁止している\n' "$n" "$want" "$want_tea" "$want_uv"
