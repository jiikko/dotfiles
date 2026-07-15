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
