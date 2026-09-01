#!/usr/bin/env bash
# codex() が Claude Code の shell snapshot 条件で壊れないことの回帰テスト (issue 149)。
# snapshot は `_` 始まりの関数を含めないため、「codex の関数本体だけがあり
# _ensure_cli_with_brew が未定義」の zsh を作って呼ぶ。定義は _zshrc を source した
# zsh から `functions[codex]` で取り出す (コピーを持つと本体の変更に追従しない)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() {
  printf '✗ %s\n' "$*" >&2
  exit 1
}
ok() { printf '✓ %s\n' "$*"; }

# --- 準備: _zshrc を隔離環境で source して codex() の定義を取り出す ---
TMP_HOME="$TMP/home"
TMP_ZDOTDIR="$TMP/zdot"
mkdir -p "$TMP_HOME" "$TMP_ZDOTDIR"
ln -s "$ROOT_DIR" "$TMP_HOME/dotfiles"
printf 'source "%s/_zshrc"\n' "$ROOT_DIR" > "$TMP_ZDOTDIR/.zshrc"

HOME="$TMP_HOME" ZDOTDIR="$TMP_ZDOTDIR" zsh -i -c \
  'print -r -- "${functions[codex]}" > '"$TMP"'/codex_body.zsh' 2>/dev/null || true
[ -s "$TMP/codex_body.zsh" ] || fail "codex() の定義を _zshrc から取り出せなかった"
{
  printf 'codex() {\n'
  cat "$TMP/codex_body.zsh"
  printf '\n}\n'
} > "$TMP/codex_def.zsh"

# --- 偽の codex バイナリ (呼ばれたことを stdout マーカーで示す) ---
STUB_BIN="$TMP/bin"
mkdir -p "$STUB_BIN"
printf '#!/bin/sh\necho "STUB_CODEX_INVOKED $*"\n' > "$STUB_BIN/codex"
chmod +x "$STUB_BIN/codex"

# --- 1) snapshot 条件 (helper 未定義・dotfiles あり): 自己修復して実行できる ---
out="$(HOME="$TMP_HOME" PATH="$STUB_BIN:/usr/bin:/bin" zsh -f -c '
  source '"$TMP"'/codex_def.zsh
  (( ${+functions[_ensure_cli_with_brew]} )) && { echo PRECONDITION_BROKEN; exit 99; }
  codex --version
')" || fail "snapshot 条件で codex --version が非0 (out: $out)"
grep -q 'STUB_CODEX_INVOKED --version' <<< "$out" || fail "snapshot 条件で codex 本体が呼ばれていない (out: $out)"
ok "snapshot 条件 (helper 未定義) でも self-heal して codex が実行される"

# --- 2) helper ファイルも無い環境: ensure を諦めて素の実行に落ちる ---
TMP_HOME2="$TMP/home2"
mkdir -p "$TMP_HOME2"
out="$(HOME="$TMP_HOME2" PATH="$STUB_BIN:/usr/bin:/bin" zsh -f -c '
  source '"$TMP"'/codex_def.zsh
  codex exec hello
')" || fail "helper ファイル不在で codex が非0 (out: $out)"
grep -q 'STUB_CODEX_INVOKED exec hello' <<< "$out" || fail "helper ファイル不在で codex 本体が呼ばれていない (out: $out)"
ok "helper ファイルも無ければ ensure を諦めて素の codex 実行に落ちる"

# --- 3) 対話回帰: helper 定義済みなら従来どおり ensure を通す (呼び出しと引数を記録) ---
out="$(HOME="$TMP_HOME2" PATH="$STUB_BIN:/usr/bin:/bin" zsh -f -c '
  source '"$TMP"'/codex_def.zsh
  _ensure_cli_with_brew() { echo "ENSURE_CALLED $*"; return 0; }
  codex --version
')" || fail "helper 定義済みで codex が非0 (out: $out)"
grep -q 'ENSURE_CALLED codex codex' <<< "$out" || fail "helper 定義済みなのに ensure が呼ばれていない (out: $out)"
grep -q 'STUB_CODEX_INVOKED --version' <<< "$out" || fail "ensure 成功後に codex 本体が呼ばれていない (out: $out)"
ok "helper 定義済みなら従来どおり ensure が先に呼ばれる"

# --- 4) 対話回帰: ensure が失敗したら codex 本体を呼ばずに非0 ---
set +e
out="$(HOME="$TMP_HOME2" PATH="$STUB_BIN:/usr/bin:/bin" zsh -f -c '
  source '"$TMP"'/codex_def.zsh
  _ensure_cli_with_brew() { return 1; }
  codex --version
')"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "ensure 失敗でも codex が 0 を返した"
grep -q 'STUB_CODEX_INVOKED' <<< "$out" && fail "ensure 失敗なのに codex 本体が呼ばれた (out: $out)"
ok "ensure 失敗なら codex 本体を呼ばずに非0"

printf 'codex snapshot-survival: すべて成功\n'
