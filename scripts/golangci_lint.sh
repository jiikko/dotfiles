#!/usr/bin/env bash
# golangci_lint.sh — 版を固定した golangci-lint を、版ごとの決まった場所に一度だけビルドして起動する。
#
#   scripts/golangci_lint.sh <版 (v2.5.0 など)> <golangci-lint の引数…>
#
# src/*/Makefile の lint が呼ぶ (`make -C src/<proj> lint`。CI も同じ入口)。以前は各 Makefile が
# `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<版>` を直接呼んでいた。
#
# ## なぜ go run をやめたか
#
# `go run pkg@版` はビルドした実行ファイルを go のビルドキャッシュに取っておき、その決まった場所から起動する
# (`go run -x` で確認。なので macOS の起動前の検査 (XprotectService) は初回だけで、毎回ではない)。遅かったのは起動のたびの
# go コマンドの側 (モジュールの解決とビルドが最新かの確認) で、`--version` だけで 0.73〜1.05 秒かかっていた。
# 決まった場所に置いたものを直接起動すると以後 0.05 秒。実測 2026-09-26 (go1.26.0 / macOS 15.7):
# `make -C src/pro-con lint` 1 回は 1.22〜1.49 秒 → 0.70 秒前後。
#
# ## 置き場所と鍵
#
# `${DOTFILES_GO_TOOLS_CACHE:-${XDG_CACHE_HOME:-$HOME/Library/Caches}/dotfiles-go-tools}/golangci-lint@<版>-<鍵>/golangci-lint`。
# 鍵は go の版・GOOS・GOARCH・CGO_ENABLED (どれかが変われば go run と同じく作り直す)。go の版は呼び出した
# ディレクトリで読む (go.mod の toolchain の選び方を go run と揃える)。
# 🚨 root の make は複数のプロジェクトの lint を並列に回すので、作るときは一時ディレクトリに install してから
# rename で置く (書きかけのバイナリを他の lint に起動させない。同時に作った場合は後の rename が勝つが中身は同じ)。
# go の版を上げると新しい鍵のディレクトリができ、古いものは残る (1 つ約 55MB。消す仕組みは作らない。要らなければ
# ディレクトリごと消してよい。次の lint が作り直す)。
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: ${0##*/} <golangci-lint の版> [引数…]" >&2
  exit 2
fi
version=$1
shift
case "$version" in
  v[0-9]*) ;;
  *) echo "${0##*/}: 版は v で始める (${version})" >&2; exit 2 ;;
esac

# go env は先に変数へ受ける (プロセス置換に流すと失敗が set -e に届かない)。1 行ずつ読む (GOROOT に空白があっても崩れない)
envs=$(go env GOROOT GOVERSION GOOS GOARCH CGO_ENABLED)
{ read -r goroot; read -r gover; read -r goos; read -r goarch; read -r cgo; } <<<"$envs"
if [ -z "$goroot" ] || [ -z "$gover" ]; then
  echo "${0##*/}: go env から GOROOT / GOVERSION を読めない" >&2
  exit 1
fi
key="$gover-$goos-$goarch-$cgo"
root=${DOTFILES_GO_TOOLS_CACHE:-${XDG_CACHE_HOME:-$HOME/Library/Caches}/dotfiles-go-tools}
dir="$root/golangci-lint@$version-$key"
bin="$dir/golangci-lint"

if [ ! -x "$bin" ] || [ ! -s "$bin" ]; then # 空のファイルは作り直す (bash は空の実行ファイルを rc=0 で「実行」してしまう)
  mkdir -p "$dir"
  tmp=$(mktemp -d "$dir/.install.XXXXXX")
  trap 'rm -rf "$tmp"' EXIT
  GOBIN="$tmp" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$version"
  mv -f "$tmp/golangci-lint" "$bin"
  rm -rf "$tmp"
  trap - EXIT
fi
# go run と同じく、起動するプログラムの PATH の先頭に $GOROOT/bin を足す (golangci-lint は中で go list 等を何度も呼ぶ。
# 足さないと goenv の shim を毎回通り、lint 1 回で約 0.5 秒遅くなった。実測 2026-09-26)
PATH="$goroot/bin:$PATH" exec "$bin" "$@"
