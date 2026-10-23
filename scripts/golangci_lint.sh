#!/usr/bin/env bash
# golangci_lint.sh — 版を固定した golangci-lint を、版ごとの決まった場所に一度だけビルドして起動する。
#
#   scripts/golangci_lint.sh <版 (v2.5.0 など)> <golangci-lint の引数…>
#
# src/*/Makefile の lint が呼ぶ (`make -C src/<proj> lint`。CI も同じ入口)。各 Makefile が
# `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<版>` を直接呼ぶ形にしない。
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
unset CDPATH

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
if [ "${1:-}" != run ]; then
  PATH="$goroot/bin:$PATH" exec "$bin" "$@"
fi

# run の設定は、共通の塊 (src/.golangci.yml) と、呼んだディレクトリ (モジュールの根) の差分 (.golangci.local.yml。無くてよい) から
# 毎回合成して -c で渡す (scripts/golangci_config.rb。合成した設定は commit しない)。次の形は rc=2 で止める (どれも黙って差分を読まずに緑になる):
#   - モジュールの根 (go.mod のあるディレクトリ) 以外から起動する (下のディレクトリで打つと、その下の .golangci.local.yml を探して見つけない)
#   - .golangci.local.yml 以外の .golangci.* を置く (.golangci.yml の写し・.golangci.local.yaml の綴りの違い)
#   - -c / --config を渡す (後に書いた方が勝ち、合成した設定が捨てられる)
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [ ! -f go.mod ]; then
  echo "${0##*/}: $PWD はモジュールの根でない (go.mod が無い)。make -C src/<module> lint から起動する" >&2
  exit 2
fi
for f in .golangci.*; do
  [ -e "$f" ] || continue
  if [ "$f" != .golangci.local.yml ]; then
    echo "${0##*/}: $PWD/$f がある。モジュールの設定は差分だけを .golangci.local.yml に書く (共通の塊は src/.golangci.yml)" >&2
    exit 2
  fi
done
shift
for a in "$@"; do
  case "$a" in
    -c|--config|--config=*|-c?*)
      echo "${0##*/}: run に $a を渡さない (設定は src/.golangci.yml と .golangci.local.yml から合成する)" >&2
      exit 2 ;;
  esac
done
locals=()
[ -e .golangci.local.yml ] && locals=(.golangci.local.yml)
json=$(ruby "$repo/scripts/golangci_config.rb" "$repo/src/.golangci.yml" ${locals[@]+"${locals[@]}"})
# 合成した設定は中身の sha256 で決まる置き場に書く (exec したいので後で消せない。同じ中身なら同じファイルを使い回す。
# 1 つ数 KB で、共通の塊か差分を変えたときだけ増える。消す仕組みは作らない。要らなければディレクトリごと消してよい)。
# 一時ファイルに書いてから rename で置く (並列の lint に書きかけを読ませない)。拡張子で golangci-lint が書式を決める
sum=$(printf '%s\n' "$json" | shasum -a 256)
cfg="$root/golangci-config/${sum%% *}.json"
if [ ! -s "$cfg" ]; then
  mkdir -p "${cfg%/*}"
  part=$(mktemp "$cfg.XXXXXX")
  printf '%s\n' "$json" >"$part"
  mv -f "$part" "$cfg"
fi
# cgo の生成物 (go/packages が go list -compiled で作る) は -trimpath の下でも `//line <checkout の絶対パス>` を持つ。
# -trimpath (go env の GOFLAGS。setup.sh が入れる) は鍵から checkout の場所を外すので、その生成物が別の checkout にも使い回され、
# 最初に作った checkout (worktree) を消すと gofmt が `//line` の先を開けずに lint が落ちる (2026-10-23 実測: -trimpath ありで落ち、なしで通る)。
# -trimpath を外すと共有そのものを失う (2 つ目の worktree の増分が +73 MB) ので、cgo の鍵にだけ checkout を入れる (同じ実験で +16 MB)。
# CGO_CPPFLAGS は cgo の package の鍵にしか入らない。定義の名前はどのソースも参照しない
tag=$(printf '%s' "$repo" | shasum -a 256)
cpp=${CGO_CPPFLAGS:-$(go env CGO_CPPFLAGS)} # 空文字は go と同じく未設定として扱い、go env -w の値へ落とす
export CGO_CPPFLAGS="${cpp:+$cpp }-DDOTFILES_LINT_CHECKOUT_${tag:0:16}"
PATH="$goroot/bin:$PATH" exec "$bin" run -c "$cfg" "$@"
