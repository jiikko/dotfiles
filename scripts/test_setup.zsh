#!/usr/bin/env zsh

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
TMPDIR=$(mktemp -d)

cleanup() {
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

export HOME="$TMPDIR/home"
mkdir -p "$HOME"
ln -s "$ROOT_DIR" "$HOME/dotfiles"

ln -s "$HOME/dotfiles/_bashrc" "$HOME/.bashrc"
ln -s "$HOME/dotfiles/_bash_profile" "$HOME/.bash_profile"

print "[test-setup:zsh] running setup.sh with HOME=$HOME"
HOME="$HOME" "$ROOT_DIR/setup.sh"

check_link() {
  local name="$1"
  local expected="$HOME/dotfiles/_$name"
  local target="$HOME/.${name}"
  if [[ ! -L "$target" ]]; then
    print -u2 "Expected $target to be a symlink"
    exit 1
  fi
  local actual
  actual=$(readlink "$target")
  if [[ "$actual" != "$expected" ]]; then
    print -u2 "symlink mismatch for $target: $actual != $expected"
    exit 1
  fi
}

for file in gemrc screenrc gvimrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global; do
  check_link "$file"
done

ensure_config_link() {
  local target="$1"
  local source="$2"
  if [[ "$(readlink "$target")" != "$source" ]]; then
    print -u2 "Config symlink mismatch for $target"
    exit 1
  fi
}

ensure_config_link "$HOME/.config/nvim/init.lua" "$HOME/dotfiles/_nviminit.lua"
ensure_config_link "$HOME/.tmux.conf" "$HOME/dotfiles/_tmux.conf"
ensure_config_link "$HOME/.config/nvim/coc-settings.json" "$HOME/dotfiles/_coc-settings.json"

if [[ -e "$HOME/.bashrc" || -e "$HOME/.bash_profile" ]]; then
  print -u2 "Legacy bash symlinks were not removed"
  exit 1
fi

print "[test-setup:zsh] done"
