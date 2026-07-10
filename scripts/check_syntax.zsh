#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
tmp_log=""
tmux_tmpdir=""

cleanup() {
  [[ -n "$tmp_log" && -f "$tmp_log" ]] && rm -f "$tmp_log"
  [[ -n "$tmux_tmpdir" && -d "$tmux_tmpdir" ]] && rm -rf "$tmux_tmpdir"
}
trap cleanup EXIT

print "[syntax] checking _zshrc"
zsh -n "$ROOT_DIR/_zshrc"

print "[syntax] checking _zlogin"
zsh -n "$ROOT_DIR/_zlogin"

# setup.sh は #!/usr/bin/env zsh の zsh スクリプトで nullglob 修飾子 *(N) 等の zsh 構文を使う。
# bash -n では *(N) が構文エラーになるため zsh -n で検査する (静的解析は shellcheck が shell=bash で別途担う)。
print "[syntax] checking setup.sh with zsh -n"
zsh -n "$ROOT_DIR/setup.sh"

print "[syntax] checking _nviminit.lua via Neovim"
if ! command -v nvim >/dev/null 2>&1; then
  print -u2 "Neovim not found; skipping _nviminit.lua syntax check"
else
  tmp_log=$(mktemp)
  if ! nvim --headless -u "$ROOT_DIR/_nviminit.lua" "+lua vim.cmd('qa')" >"$tmp_log" 2>&1; then
    cat "$tmp_log" >&2
    exit 1
  fi
fi

print "[syntax] checking _tmux.conf"
tmux_tmpdir=$(mktemp -d)
export TMUX_TMPDIR="$tmux_tmpdir"
socket="syntax-check-$$"
# HOME を隔離して起動する: _tmux.conf は conf source のたびに観測フック
# (run-shell -b で $HOME/.cache/tt-restore-trigger.log へ conf-source 行を追記。restore
# 不発調査の観測装置) を走らせるため、実 HOME のまま起動すると構文チェックのたびに実ログへ
# 偽エントリが混ざり次の調査を誤導する。resurrect の保存先候補 (~/.tmux/resurrect 等) も
# 同時に temp へ倒す。DOTFILES_DIR は明示固定する (conf の plugin パスは
# ${DOTFILES_DIR:-$HOME/dotfiles} で、HOME を temp にすると壊れるため。
# tests/tmux/test_tmux.sh の隔離と同方式)。
syntax_home="$tmux_tmpdir/home"
mkdir -p "$syntax_home"
if ! TMUX_TMPDIR="$tmux_tmpdir" HOME="$syntax_home" XDG_DATA_HOME="$syntax_home/.local/share" \
     DOTFILES_DIR="$ROOT_DIR" \
     tmux -L "$socket" -f "$ROOT_DIR/_tmux.conf" new-session -d -s syntax_check "exit" >"$tmux_tmpdir/tmux.log" 2>&1; then
  if grep -qiE "operation not permitted|permission denied" "$tmux_tmpdir/tmux.log"; then
    print -u2 "[syntax] skipped tmux check: insufficient permissions to create tmux socket"
  else
    cat "$tmux_tmpdir/tmux.log" >&2
    exit 1
  fi
else
  tmux -L "$socket" kill-server >/dev/null 2>&1 || true
fi
