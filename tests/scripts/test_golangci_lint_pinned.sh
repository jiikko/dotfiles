#!/usr/bin/env bash
# `scripts/golangci_lint.sh` が、版ごとの決まった場所に一度だけビルドして起動することを確かめる。
#
# 手段: 本物の go install は遅い (golangci-lint のビルドは数十秒〜数分) ので、偽の go を PATH の先頭に置く。
# 偽の go は `go env` に決めた値を返し、`go install` を呼ばれた回数を記録して、引数を出すだけの golangci-lint を GOBIN に置く。
# 確かめるのはヘルパーの判断 (いつ作るか・どこに置くか・失敗と並列の扱い) で、golangci-lint そのものではない。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail=0
note() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

mkdir -p "$TMP_DIR/fakebin"
cat > "$TMP_DIR/fakebin/go" <<'GO'
#!/usr/bin/env bash
case "$1" in
  env)
    shift
    for v in "$@"; do
      case "$v" in
        GOROOT) echo "${FAKE_GOROOT:-/fake/goroot}" ;;
        GOVERSION) echo "${FAKE_GOVERSION:-go1.26.0}" ;;
        GOOS) echo darwin ;;
        GOARCH) echo arm64 ;;
        CGO_ENABLED) echo 1 ;;
      esac
    done
    ;;
  install)
    echo "$2" >> "$FAKE_LOG"
    if [ -n "${FAKE_FAIL:-}" ]; then echo "fake go install: boom" >&2; exit 1; fi
    sleep "${FAKE_SLEEP:-0}"
    ver=${2##*@}
    printf '#!/bin/sh\necho "ran:%s:$*"\necho "$PATH" > "$FAKE_PATH_SEEN"\n' "$ver" > "$GOBIN/golangci-lint"
    chmod +x "$GOBIN/golangci-lint"
    ;;
  *) echo "fake go: unexpected $*" >&2; exit 99 ;;
esac
GO
chmod +x "$TMP_DIR/fakebin/go"

export PATH="$TMP_DIR/fakebin:$PATH" FAKE_LOG="$TMP_DIR/installs.log" FAKE_PATH_SEEN="$TMP_DIR/path_seen"
export DOTFILES_GO_TOOLS_CACHE="$TMP_DIR/cache"
: > "$FAKE_LOG"
installs() { wc -l < "$FAKE_LOG" | tr -d ' '; }
helper=scripts/golangci_lint.sh

# 1. 初回は作ってから起動し、引数をそのまま渡す
out=$("$helper" v2.5.0 run ./... 2>&1) || bad "初回が失敗した: $out"
if [ "$out" = "ran:v2.5.0:run ./..." ]; then note "初回は作ってから起動し、引数をそのまま渡す"; else bad "初回の出力: $out"; fi
[ "$(installs)" = 1 ] || bad "初回の install 回数 $(installs) (期待 1)"
[ "$(sed -n 1p "$FAKE_LOG")" = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0" ] || bad "install した対象: $(sed -n 1p "$FAKE_LOG")"

# 1b. go run と同じく、起動したプログラムの PATH の先頭は $GOROOT/bin (golangci-lint が中で呼ぶ go が shim を通らない)
if [ "$(cut -d: -f1 < "$FAKE_PATH_SEEN")" = "/fake/goroot/bin" ]; then note "起動したプログラムの PATH の先頭は \$GOROOT/bin"; else bad "PATH の先頭が \$GOROOT/bin でない: $(cut -d: -f1 < "$FAKE_PATH_SEEN")"; fi

# 2. 2 回目は作らない (決まった場所のものを起動する)
out=$("$helper" v2.5.0 run ./x 2>&1) || bad "2 回目が失敗した: $out"
if [ "$out" = "ran:v2.5.0:run ./x" ] && [ "$(installs)" = 1 ]; then note "2 回目は作らずに起動する"; else bad "2 回目: 出力 $out / install $(installs) 回"; fi

# 3. go の版が変わると作り直す (go run と同じく、今の toolchain でビルドしたものを使う)
FAKE_GOVERSION=go1.27.0 "$helper" v2.5.0 run >/dev/null 2>&1 || bad "go の版を変えた回が失敗した"
if [ "$(installs)" = 2 ]; then note "go の版が変わると作り直す"; else bad "go の版を変えても作り直さない (install $(installs) 回)"; fi
[ -x "$DOTFILES_GO_TOOLS_CACHE/golangci-lint@v2.5.0-go1.26.0-darwin-arm64-1/golangci-lint" ] || bad "置き場所の鍵が想定と違う: $(ls "$DOTFILES_GO_TOOLS_CACHE")"

# 4. 作るのに失敗したら非 0 で終わり、書きかけを残さない (次の回が作り直せる)
rc=0
FAKE_FAIL=1 FAKE_GOVERSION=go1.28.0 "$helper" v2.5.0 run >/dev/null 2>&1 || rc=$?
d="$DOTFILES_GO_TOOLS_CACHE/golangci-lint@v2.5.0-go1.28.0-darwin-arm64-1"
left=$(find "$d" -mindepth 1 2>/dev/null | wc -l | tr -d ' ')
if [ "$rc" != 0 ] && [ "$left" = 0 ]; then note "作るのに失敗したら非 0 で終わり、何も残さない"; else bad "失敗の扱い: rc=$rc / 残ったもの $left 個"; fi
if FAKE_GOVERSION=go1.28.0 "$helper" v2.5.0 run >/dev/null 2>&1 && [ -x "$d/golangci-lint" ]; then note "失敗の後の回は作り直せる"; else bad "失敗の後に作り直せない"; fi

# 5. 版の書式が違えば使い方の誤り (rc=2)
rc=0
"$helper" 2.5.0 run >/dev/null 2>&1 || rc=$?
if [ "$rc" = 2 ]; then note "v で始まらない版は使い方の誤り (rc=2)"; else bad "版の書式の誤りの rc=$rc (期待 2)"; fi

# 6. 並列に起動しても全部成功し、一時ディレクトリを残さない (root の make は lint を並列に回す)
export FAKE_GOVERSION=go1.29.0 FAKE_SLEEP=0.3
pids=()
for i in 1 2 3 4 5 6; do
  "$helper" v2.5.0 run "p$i" > "$TMP_DIR/par$i.out" 2>&1 &
  pids+=($!)
done
okn=0
for i in "${!pids[@]}"; do
  if wait "${pids[$i]}" && [ "$(cat "$TMP_DIR/par$((i + 1)).out")" = "ran:v2.5.0:run p$((i + 1))" ]; then okn=$((okn + 1)); fi
done
d="$DOTFILES_GO_TOOLS_CACHE/golangci-lint@v2.5.0-go1.29.0-darwin-arm64-1"
tmpleft=$(find "$d" -name '.install.*' 2>/dev/null | wc -l | tr -d ' ')
if [ "$okn" = 6 ] && [ "$tmpleft" = 0 ]; then note "並列に 6 本起動しても全部成功し、一時ディレクトリを残さない"; else bad "並列: 成功 $okn/6 / 残った一時ディレクトリ $tmpleft"; fi

if [ "$fail" = 0 ]; then echo "PASS test_golangci_lint_pinned"; else echo "FAIL test_golangci_lint_pinned"; exit 1; fi
