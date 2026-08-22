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
# ⚠️ `locale -a | grep -q` にしないこと: grep が先に抜けて locale が SIGPIPE で死に、
# pipefail 下では一致しても偽になる (手元で偽陰性を実測)。一覧は変数に取ってから照合する。
_avail_locales="$(locale -a 2>/dev/null)" || _avail_locales=''
for _loc in "${DOTFILES_TEST_LOCALE:-}" C.UTF-8 en_US.UTF-8; do
  [ -n "$_loc" ] || continue
  case "
$_avail_locales
" in
    *"
$_loc
"*) export LC_ALL="$_loc" LANG="$_loc"; break ;;
  esac
done
unset _loc _avail_locales
case "${LC_ALL:-}" in
  *UTF-8|*utf8) ;;
  # 潰し先が無い環境では黙って `_` 化させず、原因が分かる形で出す (沈黙 = 成功にしない)
  *) printf 'WARN: UTF-8 ロケールが無い (locale -a)。tmux が非 ASCII を _ に潰すテストが落ちる\n' >&2 ;;
esac
