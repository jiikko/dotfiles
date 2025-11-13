#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

export HOME="$TMPDIR/home"
mkdir -p "$HOME"

ln -s "$ROOT_DIR" "$HOME/dotfiles"

# Seed legacy links that should be removed
ln -s "$HOME/dotfiles/_bashrc" "$HOME/.bashrc"
ln -s "$HOME/dotfiles/_bash_profile" "$HOME/.bash_profile"

echo "[test-setup] running setup.sh with HOME=$HOME"
HOME="$HOME" "$ROOT_DIR/setup.sh"

check_link() {
  local name="$1"
  local expected="$HOME/dotfiles/_$name"
  local target="$HOME/.${name}"
  if [ ! -L "$target" ]; then
    echo "Expected $target to be a symlink" >&2
    exit 1
  fi
  local actual
  actual=$(readlink "$target")
  if [ "$actual" != "$expected" ]; then
    echo "symlink mismatch for $target: $actual != $expected" >&2
    exit 1
  fi
}

for file in gemrc screenrc gvimrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global; do
  check_link "$file"
done

for config in nvim/init.lua tmux.conf nvim/coc-settings.json; do
  case "$config" in
    nvim/init.lua) target="$HOME/.config/nvim/init.lua"; source="$HOME/dotfiles/_nviminit.lua" ;;
    tmux.conf) target="$HOME/.tmux.conf"; source="$HOME/dotfiles/_tmux.conf" ;;
    nvim/coc-settings.json) target="$HOME/.config/nvim/coc-settings.json"; source="$HOME/dotfiles/_coc-settings.json" ;;
  esac
  if [ "$(readlink "$target")" != "$source" ]; then
    echo "Config symlink mismatch for $target" >&2
    exit 1
  fi
done

if [ -e "$HOME/.bashrc" ] || [ -e "$HOME/.bash_profile" ]; then
  echo "Legacy bash symlinks were not removed" >&2
  exit 1
fi

echo "[test-setup] done"
