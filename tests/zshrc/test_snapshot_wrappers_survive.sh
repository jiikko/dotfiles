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

# --- 4. 静的検査: 新しい公開ラッパーが self-heal ガードなしで増えるのを止める (issue 203 候補 A) ---
# 上の実行時テストが固定しているのは concat と tt の 2 本だけで、**新しい公開ラッパーが
# 増えたときは何も見ない**。snapshot は `_` 始まりの関数を含めないので、公開関数が
# `_` helper を参照していてガードが無ければ、その関数は Claude Code の Bash から
# `command not found: _helper` で壊れる (issue 149 / 152 で実発生)。
#
# 検査対象は「snapshot に載る定義」= `_zshrc` と、`_zshrc` が **列 0 で無条件に** source する
# zshlib。⚠️ lazy source される lib (`zshlib/_av1ify.zsh` 等) の中の公開関数は snapshot に
# 載らないので対象外 (条件つき source = インデントされた行は拾わない)。
# 判定: 本体が `_` 始まりの名前を参照しているなら、本体に self-heal の `source` があること。
# repo にある 3 つのガードの形 (`${+functions[_x]} || source` / `if (( ! ... )) && [[ -r ]]` /
# `local _l=...; [[ -r "$_l" ]] && source "$_l"`) はいずれも `source` を含む。
targets=("$ROOT_DIR/_zshrc")
while IFS= read -r lib; do
  [[ -n "$lib" ]] || continue
  [[ -f "$ROOT_DIR/$lib" ]] && targets+=("$ROOT_DIR/$lib")
done < <(grep -E '^source .*zshlib/[A-Za-z0-9_.-]+\.zsh' "$ROOT_DIR/_zshrc" \
         | sed -E 's|.*(zshlib/[A-Za-z0-9_.-]+\.zsh).*|\1|' | sort -u)
[[ "${#targets[@]}" -ge 2 ]] || fail "検査対象が _zshrc だけ (無条件 source の抽出が壊れている)"

# 公開関数を「名前 <TAB> 本体を 1 行に潰したもの」で列挙する。
# 定義の形は `name() {` / `name () {` / `function name() {` の 3 つ、本体は 1 行にも複数行にも
# なる (複数行は列 0 の `}` で閉じる = この repo の書き方)
extract_public_funcs() {
  awk '
    function emit() { if (name != "") { gsub(/\t/, " ", body); printf "%s\t%s\n", name, body; name = ""; body = "" } }
    /^(function[[:space:]]+)?[A-Za-z][A-Za-z0-9_-]*[[:space:]]*\(\)[[:space:]]*\{/ {
      emit()
      line = $0
      sub(/^function[[:space:]]+/, "", line)
      name = line; sub(/[[:space:]]*\(\).*/, "", name)
      body = $0
      if ($0 ~ /\}[[:space:]]*$/) emit()
      next
    }
    name != "" { body = body " " $0; if ($0 ~ /^\}/) emit() }
    END { emit() }
  ' "$1"
}

offenders=()
scanned=0
for f in "${targets[@]}"; do
  while IFS=$'\t' read -r fname fbody; do
    [[ -n "$fname" ]] || continue
    scanned=$((scanned + 1))
    # 本体から「`_` 始まりの識別子への参照」を探す。定義行自身と、コメントは対象外
    # ⚠️ `|| true` が必須。set -euo pipefail 下では grep の無マッチ (rc=1) が代入ごと
    #    スクリプトを殺し、**1 本目の関数で静かに終わる** (実測 2026-09-03: 検査が
    #    1 件も見ずに緑を返していた。`_claude/rules/adversarial-review-own-safeguards.md`
    #    の「失敗はするが原因が消える」形)
    refs=$(printf '%s' "$fbody" | grep -oE '(^|[^A-Za-z0-9_$"{])_[A-Za-z][A-Za-z0-9_]*' | tr -d ' ' | sort -u | head -5 || true)
    [[ -n "$refs" ]] || continue
    # self-heal の source があるか (3 つのガードの形すべてが source を含む)
    if printf '%s' "$fbody" | grep -q 'source '; then continue; fi
    offenders+=("$(basename "$f"):$fname → $(printf '%s' "$refs" | tr '\n' ' ')")
  done < <(extract_public_funcs "$f")
done
[[ "$scanned" -gt 0 ]] || fail "公開関数が 1 つも抽出できない (静的検査が空振り)"
if [[ "${#offenders[@]}" -gt 0 ]]; then
  printf '✗ self-heal ガードの無い公開ラッパー (snapshot 下で command not found になる):\n'
  printf '   %s\n' "${offenders[@]}"
  printf '   直し方: 本体の先頭で helper を source して自己修復する\n'
  printf '     (( ${+functions[_helper]} )) || source "$HOME/dotfiles/zshlib/_helper.zsh"\n'
  exit 1
fi
ok "公開関数 $scanned 本すべてが self-heal ガードつき (静的検査)"
