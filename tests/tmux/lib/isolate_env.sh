# shellcheck shell=bash
# tmux テストの状態隔離。resurrect / debounce 保存の状態ファイルを実データ ($HOME/.cache 等) から
# 隔離するため、HOME/XDG_DATA_HOME/TT_DEBOUNCE_STATE_DIR/TMPDIR を TMUX_TMPDIR 配下へ逃がす。
# 呼び出し前に TMUX_TMPDIR (mktemp -d 済み) と ROOT_DIR (リポジトリルート) を用意し source すること。
# test_tmux.sh / bench_tmux.sh / test_smooth_scroll.sh / test_mark_seen.sh 共通
# (以前は4行が各自にコピペされ、
# test_fork_scratch.sh だけ subset に乖離していた)。
#
# 🚨 TMPDIR を隔離するのは smooth-scroll の状態ファイル置き場のためだけではない。
# **テストが起動する repo のスクリプトの `mktemp` は `${TMPDIR:-/tmp}` 配下へ落ちる**ので、
# 隔離しないとテストが実機の TMPDIR へ一時ファイルを撒く。実測 2026-09-06 (issue 298/325):
# `test_schedule_keys.sh` が 1 回で 61 個、累計 14,147 個 (TMPDIR 全 18,297 エントリの 77%) を
# 残していた。macOS の `/var/folders/.../T` は**起動時にしか一掃されない** (`/etc/periodic/` は
# 存在せず、実測で uptime 64 日 / 7 日より古いエントリ 2,766 個) ので、撒いたものは残り続ける。
# 掃除機構ではなく発生源をここで断つ (issue 325 の層 1)。
export HOME="$TMUX_TMPDIR/home"
export DOTFILES_DIR="$ROOT_DIR"
export XDG_DATA_HOME="$HOME/.local/share"
export TT_DEBOUNCE_STATE_DIR="$HOME/.cache/tt-debounce"
export TMPDIR="$TMUX_TMPDIR/tmp"
mkdir -p "$HOME" "$XDG_DATA_HOME" "$TT_DEBOUNCE_STATE_DIR" "$TMPDIR"

# ロケールの UTF-8 固定は tests/lib/utf8_locale.sh に切り出した (isolate_env を source しない
# tmux テストからも要るため)。ここはその薄い参照。
# shellcheck source=tests/lib/utf8_locale.sh
. "$ROOT_DIR/tests/lib/utf8_locale.sh"
