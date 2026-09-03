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

# --- 4. 静的検査: 新しい公開ラッパーが self-heal ガードなしで増えるのを止める (issue 203 候補 A) ---
# 上の実行時テストが固定しているのは concat と tt の 2 本だけで、**新しい公開ラッパーが
# 増えたときは何も見ない**。snapshot は `_` 始まりの関数と非 export 変数を含めないので、公開関数が
# `_` 始まりの helper (関数でも変数でも) を参照していてガードが無ければ、その関数は
# Claude Code の Bash から `command not found` / 空変数で壊れる (issue 149 / 152 で実発生)。
#
# ⚠️ **関数の列挙は zsh 自身にさせる** (`${(k)functions}` + `${functions[name]}`)。正規表現で
#    定義行を拾う形は、この repo に実在する書き方を取りこぼした (敵対的レビュー 2026-09-03 の
#    P1-1: `function history-all { ... }` の括弧なし / `if` の中でインデントされた `anyenv() {`)。
#    heredoc の中の偽定義を拾う・1 行 2 定義の 2 本目を落とす、も同時に消える
# ⚠️ 参照の検出は `$_X` / `${_X}` の**変数**も含める。issue 152 で `tt` が壊れた原因は
#    `_TMUX_SESSION_LIB` (非 export 変数) で、関数呼び出しだけを見ていると素通りする (同 P1-2)
# ⚠️ コメントと `local` 宣言は除く。除かないと「コメントに helper 名を書いた」「`local _l=`」だけで
#    落ちる (同 P2-6 / P2-7)
# ⚠️ ガードは **helper の source かどうか**を見る。body 全域の素の `source ` マッチだと、
#    無関係な source・コメント・文字列で通ってしまう (同 P2-4)
guard_probe="$TMP/guard_probe.zsh"
cat > "$guard_probe" <<'PROBE'
# 公開関数 (先頭が _ でない) を「名前 <TAB> 本体」で出す。本体の改行は \x01 に置き換える。
# ⚠️ **この repo が定義した関数だけ**に絞る (functions_source が定義元ファイルを返す)。
#    絞らないと zsh 同梱の compdef 等が入り、`_comps` 参照で誤検出する
#    (敵対的レビュー 2026-09-03 の指摘を受けた実測)
zmodload zsh/parameter 2>/dev/null
for k in ${(ko)functions}; do
  [[ "$k" == _* ]] && continue
  [[ "$k" == *-widget ]] && continue   # zle widget は snapshot の対象外 (対話シェル専用)
  src="${functions_source[$k]:-}"
  # ⚠️ パス名に "/dotfiles/" を期待しない。worktree や別名チェックアウトでは 1 件も
  #    一致せず、**何も走査しないまま緑**になる (実測 2026-09-03: 変異が全部素通りした)
  [[ "$src" == "$SK_ROOT"/* || "$src" == "$SK_FAKE"/* ]] || continue
  body="${functions[$k]}"
  print -rn -- "$k"$'\t'
  print -rn -- "${body//$'\n'/$'\x01'}"
  print -rn -- $'\n'
done
PROBE
funcs_out="$TMP/funcs.tsv"
SK_ROOT="$ROOT_DIR" SK_FAKE="$FAKE_HOME" HOME="$FAKE_HOME" ZDOTDIR="$ZDOT" \
  zsh -i -c "source '$guard_probe'" > "$funcs_out" 2>/dev/null || true
[[ -s "$funcs_out" ]] || fail "公開関数を zsh から列挙できない (静的検査が空振り)"
# ⚠️ **列挙が _zshrc に届いていることを確かめる**。定義元の絞り込みを間違えると、
#    走査 0 件のまま緑になる (上のパス前提で実際に踏んだ)。_zshrc 由来の既知の関数で見る
for sk_known in concat av1ify; do
  grep -qx "$sk_known" <<< "$(cut -f1 "$funcs_out")" \
    || fail "列挙に _zshrc の $sk_known が居ない (定義元の絞り込みが壊れている = 何も検査していない)"
done

# 参照の抽出は 1 つの関数に集約する。**canary と本走査が同じ経路を通る**ことが要点で、
# 別々に書くと canary が「コピーしたロジック」を検査するだけになる。
# ①変数形 ($_X / ${_X) と ②コマンド位置 (行頭・; & | ( ) { } 空白の直後) を分ける。
# ⚠️ 境界にクォートを含めない: `[[ "$f" =~ "_test.rb" ]]` の文字列リテラルを参照と読んで
#    誤検出した (実測 2026-09-03。rt() が引っかかった)。変数形は `$` 自体が目印なので
#    直前の文字を問わない
# ⚠️ sort は LC_ALL=C にする: 既定の照合順は locale 依存で、canary の期待値が環境で変わる
# ⚠️ 無マッチが正解のケースがあるので `|| true` が要る。付け忘れると set -euo pipefail 下で
#    grep の rc=1 が代入ごとスクリプトを殺し、**§4 が 1 行も出さずに rc=1** で終わる
#    (このセッションで 3 回踏んだ形)
SK_NAME_RE='[$][{]?_[A-Za-z][A-Za-z0-9_]*|(^|[;&|(){}[:space:]])_[A-Za-z][A-Za-z0-9_]*'

sk_names_in() { grep -oE "$SK_NAME_RE" | sed 's/^[^_]*//' | LC_ALL=C sort -u || true; }

# sk_refs_of は「本体が参照している `_` 名」を 1 行 1 件で返す。
# local / typeset で自分が宣言した名前は引く (自前の変数は snapshot と無関係)
sk_refs_of() {
  local code=$1 locals refs
  locals=$(printf '%s' "$code" | grep -oE '(local|typeset)[^;]*' | sk_names_in)
  refs=$(printf '%s' "$code" | sk_names_in)
  if [[ -n "$locals" ]]; then
    # ⚠️ comm も LC_ALL=C にする。sort だけ C にして comm を locale のままにすると、
    #    comm から見て入力が未ソートになり **引き算が黙って崩れる** (実測 2026-09-03)
    LC_ALL=C comm -23 <(printf '%s\n' "$refs" | sed '/^$/d') <(printf '%s\n' "$locals" | sed '/^$/d') || true
  else
    printf '%s\n' "$refs" | sed '/^$/d'
  fi
}

# ⚠️ **抽出そのものを canary で検査する**。sed / grep の式を壊すと refs が空になり、
#    「違反 0 件」として緑を返す (実測 2026-09-03: sed の括弧を壊して実際に緑になった)
sk_got=$(sk_refs_of 'foo() { local _l=/x; _reload_then_call foo "$_TMUX_SESSION_LIB" "$REPO_AND_PATH" }' | tr '\n' ' ')
[[ "$sk_got" == "_TMUX_SESSION_LIB _reload_then_call " ]] \
  || fail "参照の抽出が壊れている (canary: 期待 '_TMUX_SESSION_LIB _reload_then_call ' / 実際 '$sk_got')"
# 文字列リテラルを参照として拾わないこと (rt() の `=~ "_test.rb"` で誤検出した形)
sk_str=$(sk_refs_of 'bar() { [[ "$f" =~ "_test.rb" ]] && echo "_spec.rb" }' | tr -d '[:space:]')
[[ -z "$sk_str" ]] || fail "文字列リテラルを参照として拾っている (canary: 実際 '$sk_str')"
# local だけを使う関数は「参照なし」になること (comm による引き算が効いているか)
sk_loc=$(sk_refs_of 'baz() { local _l=/tmp/x; echo "$_l" }' | tr -d '[:space:]')
[[ -z "$sk_loc" ]] || fail "local 宣言を引けていない (canary: 実際 '$sk_loc')"

offenders=()
scanned=0
while IFS=$'\t' read -r fname fbody; do
  [[ -n "$fname" ]] || continue
  scanned=$((scanned + 1))
  # \x01 を改行へ戻し、コメントを落とす (行頭・行内の # 以降)
  code=$(printf '%s' "$fbody" | tr '\001' '\n' | sed -E 's/(^|[[:space:]])#.*$//')
  refs=$(sk_refs_of "$code")
  [[ -n "$(printf '%s' "$refs" | tr -d '[:space:]')" ]] || continue
  # self-heal: helper の source (`source`/`.` + zshlib のパス) か、canonical な
  # `${+functions[_x]}` ガードがあること
  # ガードの形は repo に 3 つある: ①`${+functions[_x]} || source "…zshlib/…"`
  #   ②`if (( ! ${+functions[_x]} )) && [[ -r … ]]; then source …`
  #   ③`local _l="${_LIB:-$HOME/dotfiles/zshlib/…}"; [[ -r "$_l" ]] && source "$_l"` (変数経由)
  # ③があるので「source の引数に zshlib/ が在る」では判定できない。**source/. の実行と
  # zshlib/ のパスが本体に両方在ること**を条件にする (近似。無関係な zshlib を source して
  # 別の helper を参照する形は通ってしまうが、それでも self-heal はしている)
  if grep -q '${+functions\[' <<< "$code"; then continue; fi
  if grep -qE '(^|[^A-Za-z0-9_])(source|\.)[[:space:]]' <<< "$code" \
     && grep -q 'zshlib/' <<< "$code"; then continue; fi
  offenders+=("$fname → $(printf '%s' "$refs" | tr '\n' ' ')")
done < "$funcs_out"
[[ "$scanned" -gt 0 ]] || fail "公開関数が 1 つも抽出できない (静的検査が空振り)"
if [[ "${#offenders[@]}" -gt 0 ]]; then
  printf '✗ self-heal ガードの無い公開ラッパー (snapshot 下で command not found / 空変数になる):\n'
  printf '   %s\n' "${offenders[@]}"
  printf '   直し方: 本体の先頭で helper を source して自己修復する\n'
  printf '     (( ${+functions[_helper]} )) || source "$HOME/dotfiles/zshlib/_helper.zsh"\n'
  exit 1
fi
ok "公開関数 $scanned 本すべてが self-heal ガードつき (静的検査)"
printf 'snapshot wrapper survival: すべて成功\n'
