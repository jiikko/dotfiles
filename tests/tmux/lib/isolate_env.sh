# shellcheck shell=bash
# tmux テストの状態隔離。resurrect / debounce 保存の状態ファイルを実データ ($HOME/.cache 等) から
# 隔離するため、HOME/XDG_DATA_HOME/TT_DEBOUNCE_STATE_DIR を TMUX_TMPDIR 配下へ逃がす。
# 呼び出し前に TMUX_TMPDIR (mktemp -d 済み) と ROOT_DIR (リポジトリルート) を用意し source すること。
# test_tmux.sh / bench_tmux.sh / test_smooth_scroll.sh 共通 (以前は4行が各自にコピペされ、
# test_fork_scratch.sh だけ subset に乖離していた)。smooth_scroll は追加で TMPDIR も隔離する
# (source 後に自前で export)。
export HOME="$TMUX_TMPDIR/home"
export DOTFILES_DIR="$ROOT_DIR"
export XDG_DATA_HOME="$HOME/.local/share"
export TT_DEBOUNCE_STATE_DIR="$HOME/.cache/tt-debounce"
mkdir -p "$HOME" "$XDG_DATA_HOME" "$TT_DEBOUNCE_STATE_DIR"

# ロケールは UTF-8 に固定する。tmux は LC_CTYPE が UTF-8 でないと非 ASCII を `_` へ潰すため
# (utf8_sanitize)、絵文字を含む pane option がテスト内で化ける。LANG 未設定の CI runner で
# 実際に踏んだ (tests/tmux/test_mark_seen.sh の 5 件)。テストが直接呼ぶ tmux だけでなく、
# hook が内部で起動する tmux クライアントにも効かせる必要があるので env で渡す。
#
# ⚠️ 名前の表記ゆれを畳んでから照合する: glibc の locale -a は `C.utf8` (小文字・ハイフン無し)、
#   macOS は `C.UTF-8` と出す。`C.UTF-8` 決め打ちだと Linux CI で一致せず素通りする (実測)。
# ⚠️ `locale -a | grep -q` にしないこと: grep が先に抜けて locale が SIGPIPE で死に、
#   pipefail 下では一致しても偽になる (実測)。一覧は変数に取ってから照合する。
_avail_locales="$(locale -a 2>/dev/null)" || _avail_locales=''
# 表記ゆれを畳む鍵を作る (小文字化 + 区切り除去)。⚠️ tr -d のセットは `-` を末尾に置くこと
# (先頭だと BSD tr がオプションと解釈して失敗し、鍵が空になって全候補が一致してしまう)
_lockey() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '_-'; }
_found=''
for _want in "${DOTFILES_TEST_LOCALE:-}" C.UTF-8 en_US.UTF-8; do
  [ -n "$_want" ] || continue
  _want_key="$(_lockey "$_want")"
  while IFS= read -r _cand; do
    [ -n "$_cand" ] || continue
    [ "$(_lockey "$_cand")" = "$_want_key" ] || continue
    _found="$_cand"
    break
  done <<EOF_LOCALES
$_avail_locales
EOF_LOCALES
  [ -n "$_found" ] && break
done
# ⚠️ 判定に LC_ALL の中身を使わないこと: 外側から非 UTF-8 (POSIX 等) が来ていると
# 「もう決まった」と誤読して次の候補を試さなくなる。専用のフラグで見る
if [ -n "$_found" ]; then
  export LC_ALL="$_found" LANG="$_found"
else
  # 潰し先が無い環境では黙って `_` 化させず、原因が分かる形で出す (沈黙 = 成功にしない)
  printf 'WARN: UTF-8 ロケールが無い (locale -a)。tmux が非 ASCII を _ に潰すテストが落ちる\n' >&2
fi
unset _want _want_key _cand _found _avail_locales
unset -f _lockey
