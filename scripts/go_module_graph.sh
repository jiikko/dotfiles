#!/usr/bin/env bash
# go_module_graph.sh — 今いるディレクトリの Go module について、go.mod / go.sum が `go mod tidy` の形で、module グラフ全体が
# 解決できる (`go list -m all` が通る) ことを確かめる (issue 787)。
#
# src/*/Makefile の lint が golangci-lint の前に呼ぶ。手元の `make test` (test-src → test-go-lint)・`make -C src/<proj> lint`・
# CI の Lint step (`_go-project.yml` の `make -C <dir> lint`) が同じ入口を通る。呼び忘れと workflow の paths の書き忘れは
# scripts/check_go_project_lanes.sh (make test-lint) が止める。
#
# なぜ要るか: replace は依存元へ伝わらない。依存先の module が別の module を新しく require すると、依存元の go.mod に
# その replace が無い限り `go list -m all` は `malformed module path "<名前>": missing dot in first path element` で落ちるが、
# go build / go test / golangci-lint は落ちない (依存元が import する package がその module に届かなければ)。issue 736 と 787 で 2 回、
# CI だけが落ちて手元の make test は緑のまま push された。
set -uo pipefail
unset CDPATH
# 手元の環境で結果を変えない (CI と同じ条件で見る): go.work があると workspace の module が replace の抜けを埋めて素通りする。
# GOFLAGS=-mod=mod だと go list が go.sum を書き換え、-mod=vendor だと筋違いの red になる (どちらも 2026-10-16 の敵対的レビューで再現)
export GOWORK=off GOFLAGS=-mod=readonly

if [ ! -f go.mod ]; then
  echo "✗ go_module_graph: go.mod が無い ($(pwd))。src/<proj> の Makefile から呼ぶ" >&2
  exit 2
fi

out="$(go mod tidy -diff 2>&1)"
rc=$?
if [ "$rc" -ne 0 ]; then
  printf '%s\n' "$out" >&2
  echo "✗ go_module_graph: go mod tidy -diff が rc=${rc} ($(pwd))。差分なら go mod tidy をかける" >&2
  exit 1
fi

out="$(go list -m all 2>&1 >/dev/null)"
rc=$?
if [ "$rc" -ne 0 ]; then
  printf '%s\n' "$out" >&2
  echo "✗ go_module_graph: go list -m all が rc=${rc} ($(pwd))" >&2
  missing="$(printf '%s\n' "$out" | sed -n 's/.*malformed module path "\([^"]*\)".*/\1/p' | head -n 1)"
  if [ -n "$missing" ]; then
    # replace を足すと scripts/check_go_project_lanes.sh (make test-lint) が workflow の paths も要求するので、両方を案内する
    echo "  直し方: go.mod の replace に「${missing} => ../${missing}」を足し、.github/workflows/src_$(basename "$(pwd)").yml の" \
      "push.paths と pull_request.paths に「src/${missing}/**」を足す (依存先が require する module。replace は依存元へ伝わらない)" >&2
  fi
  exit 1
fi
