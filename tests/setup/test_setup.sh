#!/usr/bin/env zsh

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
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

# .claude directory: agents/skills should be real dirs with individual symlinks
if [[ -L "$HOME/.claude/agents" ]]; then
  print -u2 "~/.claude/agents should be a real directory, not a symlink"
  exit 1
fi
if [[ -L "$HOME/.claude/skills" ]]; then
  print -u2 "~/.claude/skills should be a real directory, not a symlink"
  exit 1
fi
# spot-check: at least one agent and one skill should be linked
agent_count=$(find "$HOME/.claude/agents" -maxdepth 1 -type l | wc -l | tr -d ' ')
skill_count=$(find "$HOME/.claude/skills" -maxdepth 1 -type l | wc -l | tr -d ' ')
if [[ "$agent_count" -eq 0 ]]; then
  print -u2 "No agent symlinks found in ~/.claude/agents"
  exit 1
fi
if [[ "$skill_count" -eq 0 ]]; then
  print -u2 "No skill symlinks found in ~/.claude/skills"
  exit 1
fi
ensure_config_link "$HOME/.claude/keybindings.json" "$HOME/dotfiles/_claude/keybindings.json"

# migration: setup.sh should convert legacy directory symlinks to individual links
print "[test-setup:zsh] testing claude migration..."
# simulate legacy state: replace dirs with directory symlinks
rm -rf "$HOME/.claude/agents" "$HOME/.claude/skills"
ln -sfn "$HOME/dotfiles/_claude/agents" "$HOME/.claude/agents"
ln -sfn "$HOME/dotfiles/_claude/skills" "$HOME/.claude/skills"
# run setup.sh again
HOME="$HOME" "$ROOT_DIR/setup.sh"
if [[ -L "$HOME/.claude/agents" ]]; then
  print -u2 "Migration failed: ~/.claude/agents is still a symlink"
  exit 1
fi
if [[ -L "$HOME/.claude/skills" ]]; then
  print -u2 "Migration failed: ~/.claude/skills is still a symlink"
  exit 1
fi

if [[ -e "$HOME/.bashrc" || -e "$HOME/.bash_profile" ]]; then
  print -u2 "Legacy bash symlinks were not removed"
  exit 1
fi

print "[test-setup:zsh] done"
