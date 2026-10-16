#!/usr/bin/env bash
# scripts/go_module_graph.sh (go.mod が tidy の形で、module グラフ全体が解決できるか) のテスト (issue 787)。
#
# fixture は issue 736 / 787 で落ちた形を写す: a → b (replace あり) → c (b の go.mod だけが replace を持つ)。a が import するのは
# c に届かない b の package だけなので、go mod tidy -diff と go build は通り、go list -m all だけが落ちる。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/go_module_graph.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail=0
ok() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

if ! command -v go >/dev/null 2>&1; then
  echo "skipped: go が無い (scripts/go_module_graph.sh は go を呼ぶ)"
  exit 77
fi
# 手元の go.work・GOFLAGS・proxy に左右されない (fixture は replace だけで閉じる)
export GOWORK=off GOFLAGS='' GOPROXY=off GOTOOLCHAIN=local

# fixture を作る。$1 = a の go.mod に足す行
mk() {
  local d="$TMP_DIR/$1"
  rm -rf "$d"; mkdir -p "$d/a" "$d/b/lan" "$d/c" "$d/d"
  printf 'module c\n\ngo 1.24\n' > "$d/c/go.mod"
  printf 'package c\n\nconst X = 1\n' > "$d/c/c.go"
  printf 'module d\n\ngo 1.24\n' > "$d/d/go.mod"
  printf 'package d\n\nconst W = 4\n' > "$d/d/d.go"
  printf 'module b\n\ngo 1.24\n\nrequire c v0.0.0\n\nreplace c => ../c\n' > "$d/b/go.mod"
  printf 'package b\n\nconst Y = 2\n' > "$d/b/b.go"
  printf 'package lan\n\nimport "c"\n\nconst Z = c.X\n' > "$d/b/lan/lan.go"
  printf 'module a\n\ngo 1.24\n\nrequire b v0.0.0\n\nreplace b => ../b\n%s' "$2" > "$d/a/go.mod"
  printf 'package main\n\nimport "b"\n\nfunc main() { _ = b.Y }\n' > "$d/a/main.go"
  printf '%s/a' "$d"
}

run() {  # $1 = dir。rc / out を残す
  rc=0
  out="$(cd "$1" && "$SCRIPT" 2>&1)" || rc=$?
}

# 1. 依存先が require する module の replace が無い → go list -m all で落ち、足す replace と paths を案内する
dir="$(mk missing '')"
# 前提: この fixture は go build が通る (落ちるのが build なら、この検査が要る理由の再現になっていない)
if (cd "$dir" && go build ./... >/dev/null 2>&1); then ok "前提: replace の抜けがあっても go build は通る"; else bad "前提が崩れた: replace の抜けで go build が落ちる"; fi
run "$dir"
if [ "$rc" -eq 1 ] && grep -q 'go list -m all が rc=1' <<<"$out" && grep -q '「c => ../c」' <<<"$out" && grep -q '「src/c/\*\*」' <<<"$out"; then
  ok "replace の抜けを go list -m all で止め、足す replace と workflow の paths を案内する"
else
  bad "replace の抜けを止めない / 案内が違う (rc=${rc}):"; printf '%s\n' "$out"
fi

# 2. replace を足せば通る
run "$(mk fixed $'\nreplace c => ../c\n')"
if [ "$rc" -eq 0 ]; then ok "replace が揃っていれば通る"; else bad "replace が揃っているのに落ちた (rc=${rc}):"; printf '%s\n' "$out"; fi

# 3. tidy の形でない (使わない module を require している) → go mod tidy -diff で落ちる
run "$(mk untidy $'\nreplace c => ../c\n\nrequire d v0.0.0\n\nreplace d => ../d\n')"
if [ "$rc" -eq 1 ] && grep -q 'go mod tidy -diff が rc=' <<<"$out"; then ok "tidy の形でない go.mod を止める"; else bad "tidy の形でない go.mod を通した (rc=${rc}):"; printf '%s\n' "$out"; fi

# 4. go.mod の無い場所で呼ばれたら 2 (呼び出し方の誤り。0 で通さない)
mkdir -p "$TMP_DIR/nomod"
run "$TMP_DIR/nomod"
if [ "$rc" -eq 2 ]; then ok "go.mod の無い場所では rc=2"; else bad "go.mod の無い場所で rc=${rc}"; fi

# src/*/Makefile の lint が全部これを呼ぶことは scripts/check_go_project_lanes.sh (make test-lint) が止める

[ "$fail" -eq 0 ] || exit 1
echo "[test-go-module-graph] all ok"
