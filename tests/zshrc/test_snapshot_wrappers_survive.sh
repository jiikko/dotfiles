#!/usr/bin/env bash
# Claude Code の shell snapshot 条件で公開ラッパーが壊れないことの回帰テスト (issue 152)。
# snapshot は `_` 始まりの関数と非 export 変数を含めないため、「公開ラッパーの本体だけがあり、
# _reload_then_call / _TMUX_SESSION_LIB / _t_impl が未定義」の zsh -f を作って呼ぶ。
# 定義は本物を source した zsh から `functions[<name>]` で取り出す (コピーは本体に追従しない)。
# 兄弟: tests/zshrc/codex-wrapper/test_codex_snapshot_survives.sh (codex() 版、issue 149)
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
fail() { printf '✗ %s\n' "$*" >&2; exit 1; }
ok() { printf '✓ %s\n' "$*"; }

# 偽 HOME: dotfiles/zshlib に本物の helper / tmux lib を link し、動画 lib はスタブに差し替える
# (ラッパーは "$HOME/dotfiles/zshlib/<lib>" をハードコードしている)
FAKE_HOME="$TMP/home"
mkdir -p "$FAKE_HOME/dotfiles/zshlib" "$FAKE_HOME/dotfiles/scripts/lib"
ln -s "$ROOT_DIR/zshlib/_reload_then_call.zsh" "$FAKE_HOME/dotfiles/zshlib/_reload_then_call.zsh"
ln -s "$ROOT_DIR/zshlib/_tmux_session.zsh"     "$FAKE_HOME/dotfiles/zshlib/_tmux_session.zsh"
ln -s "$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh" "$FAKE_HOME/dotfiles/scripts/lib/tmux_resurrect_guards.sh"
printf 'concat() { echo "STUB_CONCAT $*" }\n' > "$FAKE_HOME/dotfiles/zshlib/_concat.zsh"

# --- 定義の取り出し: _zshrc (concat) と tmux lib (tt) ---
ZDOT="$TMP/zdot"; mkdir -p "$ZDOT"
printf 'source "%s/_zshrc"\n' "$ROOT_DIR" > "$ZDOT/.zshrc"
HOME="$FAKE_HOME" ZDOTDIR="$ZDOT" zsh -i -c '
  print -r -- "concat() {"; print -r -- "${functions[concat]}"; print -r -- "}"
' > "$TMP/concat_def.zsh" 2>/dev/null || true
grep -q '_reload_then_call' "$TMP/concat_def.zsh" || fail "concat() の定義を _zshrc から取り出せなかった"
zsh -f -c 'source "'"$ROOT_DIR"'/zshlib/_tmux_session.zsh"; print -r -- "tt() {"; print -r -- "${functions[tt]}"; print -r -- "}"' \
  > "$TMP/tt_def.zsh" 2>/dev/null
grep -q '_tt_impl' "$TMP/tt_def.zsh" || fail "tt() の定義を tmux lib から取り出せなかった"

# --- 1) _reload_then_call 系: helper 未定義でも self-heal して実体が呼ばれ、ラッパーが復元される ---
out="$(HOME="$FAKE_HOME" zsh -f -c '
  source '"$TMP"'/concat_def.zsh
  (( ${+functions[_reload_then_call]} )) && { echo PRECONDITION_BROKEN; exit 99; }
  concat a b || exit 1
  # 実体 (スタブ lib の concat) がラッパーを上書きしたまま残っていないこと
  [[ "${functions[concat]}" == *_reload_then_call* ]] && echo WRAPPER_RESTORED
')" || fail "snapshot 条件で concat が非0 (out: $out)"
grep -q 'STUB_CONCAT a b' <<< "$out" || fail "snapshot 条件で concat 実体が呼ばれていない (out: $out)"
grep -q 'WRAPPER_RESTORED' <<< "$out" || fail "self-heal 後にラッパーが実体で固定された (out: $out)"
ok "snapshot 条件 (_reload_then_call 未定義) でも self-heal して concat が実行され、ラッパーが残る"

# --- 2) t/tt: _TMUX_SESSION_LIB も _tt_impl も無い状態から lib を読み直して実体に到達する ---
# _tt_impl は tmux を叩くので tmux / sleep を関数でスタブする (has-session 失敗 → 以降も全部失敗)。
# 見たいのは「_tt_impl not found にならず、実体が tmux まで到達する」ことだけ。
out="$(HOME="$FAKE_HOME" TT_SKIP_REAP=1 TT_ASSUME_TTY=1 zsh -f -c '
  source '"$TMP"'/tt_def.zsh
  [[ -n "${_TMUX_SESSION_LIB:-}" ]] && { echo PRECONDITION_BROKEN_VAR; exit 99; }
  (( ${+functions[_tt_impl]} )) && { echo PRECONDITION_BROKEN_IMPL; exit 99; }
  tmux() { echo "STUB_TMUX $1"; return 1 }
  sleep() { : }
  tt proj >/dev/null 2>&1 <&-; echo "rc=$?"
  (( ${+functions[_tt_impl]} )) && echo IMPL_LOADED
' 2>&1)"
grep -q 'IMPL_LOADED' <<< "$out" || fail "snapshot 条件で tt が lib を読み直していない (out: $out)"
grep -q 'rc=127' <<< "$out" && fail "snapshot 条件で tt が command not found (out: $out)"
ok "snapshot 条件 (_TMUX_SESSION_LIB 未定義) でも tt が既定パスから lib を読み直して実体に到達する"

# --- 3) 回帰: _TMUX_SESSION_LIB を差し替えた既存の再評価 idiom (test_tt.sh Part 2) を壊していない ---
LIVE="$TMP/live.zsh"
out="$(HOME="$FAKE_HOME" zsh -f -c '
  source '"$TMP"'/tt_def.zsh
  typeset -g _TMUX_SESSION_LIB="'"$LIVE"'"
  print "_tt_impl() { print LIVE_V1 }" > "'"$LIVE"'"; tt
  print "_tt_impl() { print LIVE_V2 }" > "'"$LIVE"'"; tt
')" || fail "差し替え lib で tt が非0 (out: $out)"
[[ "$out" == $'LIVE_V1\nLIVE_V2' ]] || fail "_TMUX_SESSION_LIB の差し替えが既定パスに負けている (out: $out)"
ok "_TMUX_SESSION_LIB が定義済みならそちらを優先して毎回再評価する"

printf 'snapshot wrapper survival: すべて成功\n'
