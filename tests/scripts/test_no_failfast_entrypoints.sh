#!/usr/bin/env bash
# ローカルの入口 (make test / test-go* / test_changed.sh) が「1 本目の失敗で残りを隠さない」
# ことを実験で確かめる (issue 130)。
#
# なぜテストにするか: 集約かどうかは**失敗した日にしか観測されない**。緑の日は fail-fast でも
# 集約でも同じ出力になるため、prerequisite 形へ戻す変更が無音で通る。
# 実際 109 の 1 次修正では、集約にしたつもりの隣で 3 つの入口が短絡のまま残っていた。
#
# 手段: 本物の lint / テストは回さず、**呼ばれ方だけ**を偽の make で観測する
#   - Makefile 側: GO_PROJECT_DIRS を上書きして偽プロジェクト 2 つを回す (1 つ目が必ず落ちる)
#   - test_changed.sh 側: PATH 先頭に偽 make を置いて、呼び出しを記録させる
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
TMP_DIR="$(mktemp -d "$ROOT_DIR/tmp/failfast.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

fail=0
note() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

# --- Makefile: test-go-lint / test-go は全プロジェクトを回してから落ちる ---------------
# 偽プロジェクト: bad が先 (アルファベット順でも先) で必ず落ちる。good が走れば集約
mkdir -p "$TMP_DIR/bad" "$TMP_DIR/good"
printf 'lint:\n\t@echo bad-lint >> %s\n\t@exit 1\ntest:\n\t@echo bad-test >> %s\n\t@exit 1\n' \
  "$TMP_DIR/calls" "$TMP_DIR/calls" > "$TMP_DIR/bad/Makefile"
printf 'lint:\n\t@echo good-lint >> %s\ntest:\n\t@echo good-test >> %s\n' \
  "$TMP_DIR/calls" "$TMP_DIR/calls" > "$TMP_DIR/good/Makefile"

for goal in lint test; do
  : > "$TMP_DIR/calls"
  target="test-go"; [ "$goal" = lint ] && target="test-go-lint"
  if make "$target" GO_PROJECT_DIRS="$TMP_DIR/bad $TMP_DIR/good" > "$TMP_DIR/out" 2>&1; then
    bad "$target: 失敗したプロジェクトがあるのに成功した"
  else
    note "$target: 失敗を返す"
  fi
  grep -q "good-$goal" "$TMP_DIR/calls" \
    || bad "$target: 1 つ目が落ちた後、2 つ目が走っていない ($(tr '\n' ' ' < "$TMP_DIR/calls"))"
  grep -q "失敗したプロジェクト.*bad" "$TMP_DIR/out" \
    || bad "$target: 失敗したプロジェクト名がまとめて出ない: $(cat "$TMP_DIR/out")"
done
note "test-go-lint / test-go は全プロジェクトを回してから集約して落ちる"

# go が無い環境では skip して緑になる (0 件の失敗と混ぜない)。
# ⚠️ make の shim は作らない。PATH を絞った中で相対名の make を exec すると自分自身に
#   解決して無音で無限再帰する (_claude/rules/path-shim-must-resolve-real-binary.md。実際に踏んだ)。
#   go を PATH から外すだけにして、make は絶対パスで呼ぶ
real_make="$(command -v make)"
if PATH="/usr/bin:/bin" "$real_make" test-go GO_PROJECT_DIRS="$TMP_DIR/bad" >"$TMP_DIR/out" 2>&1; then
  grep -q 'go not found' "$TMP_DIR/out" || bad "go 無しで緑だが skip の理由が出ていない: $(cat "$TMP_DIR/out")"
  note "go 未導入は理由を出して skip する"
else
  # go が /usr/bin にある環境では検証できない。緑に畳まず「未検証」として出す
  printf 'skipped: go を PATH から外せなかったため skip 経路は未検証\n'
fi

# 0 件は「発見が壊れている」として落ちる (skip と別の結果)
if make test-go GO_PROJECT_DIRS="" >"$TMP_DIR/out" 2>&1; then
  bad "GO_PROJECT_DIRS が 0 件でも緑になる (発見の false green)"
else
  note "GO_PROJECT_DIRS 0 件は失敗する"
fi

# --- Makefile: test / test-src が集約であること -----------------------------------------
# $(MAKE) を偽物へ差し替えて「どのターゲットが呼ばれたか」だけを見る (本物は回さない)
cat > "$TMP_DIR/fakemake" <<EOF
#!/bin/sh
echo "\$@" >> "$TMP_DIR/calls"
case "\$1" in test-lint|test-go-lint) exit 1 ;; esac
EOF
chmod +x "$TMP_DIR/fakemake"

: > "$TMP_DIR/calls"
if make test MAKE="$TMP_DIR/fakemake" >"$TMP_DIR/out" 2>&1; then
  bad "make test: 子ターゲットが落ちたのに成功した"
fi
for t in test-lint test-runtime test-src; do
  grep -qx "$t" "$TMP_DIR/calls" || bad "make test: $t が呼ばれていない ($(tr '\n' ' ' < "$TMP_DIR/calls"))"
done
note "make test は lint が落ちても test-runtime / test-src を回す"

: > "$TMP_DIR/calls"
if make test-src MAKE="$TMP_DIR/fakemake" >"$TMP_DIR/out" 2>&1; then
  bad "make test-src: 子ターゲットが落ちたのに成功した"
fi
grep -qx "test-go" "$TMP_DIR/calls" \
  || bad "make test-src: lint の失敗で test-go が走っていない ($(tr '\n' ' ' < "$TMP_DIR/calls"))"
note "make test-src は test-go-lint が落ちても test-go を回す"

# --- test_changed.sh: 3 つのループが途中で止まらない -------------------------------------
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/make" <<EOF
#!/bin/sh
echo "\$*" >> "$TMP_DIR/tc_calls"
exit 1
EOF
chmod +x "$TMP_DIR/bin/make"
: > "$TMP_DIR/tc_calls"
if PATH="$TMP_DIR/bin:$PATH" ./scripts/test_changed.sh \
     _claude/settings.json tests/claude/test_statusline.sh src/glogx/tui.go \
     >"$TMP_DIR/out" 2>&1; then
  bad "test_changed.sh: make が全部落ちたのに成功した"
else
  note "test_changed.sh: 失敗を返す"
fi
# 3 種のループ (targets / test_dirs / go_dirs) が全部最後まで回ること。
# go は lint と test を**別々に**呼ぶ (1 make 2 goal だと lint の失敗で test が走らない)
for expected in 'test-json' 'test-dir DIR=tests/claude' '-C src/glogx lint' '-C src/glogx test'; do
  # -- を付ける: 期待文字列が "-C ..." で始まるとオプションとして食われる (実際に踏んだ)
  grep -qF -- "$expected" "$TMP_DIR/tc_calls" \
    || bad "test_changed.sh: '$expected' が呼ばれていない (途中で止まった):"$'\n'"$(cat "$TMP_DIR/tc_calls")"
done
grep -q '✗ \[test-changed\] 失敗:' "$TMP_DIR/out" \
  || bad "test_changed.sh: 失敗の一覧が出ない: $(cat "$TMP_DIR/out")"
note "test_changed.sh は 3 つのループを最後まで回して失敗をまとめる"

[ "$fail" -eq 0 ] || exit 1
printf '\n[test-no-failfast-entrypoints] all ok\n'
