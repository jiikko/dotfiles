#!/usr/bin/env zsh
# shellcheck shell=bash

set -o pipefail

# set rc limlink
for file in gemrc screenrc gvimrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global; do
  echo 'making symlink' _$file '->' ~/.$file
  ln -sf ~/dotfiles/_$file ~/.$file
done

mkdir -p ~/.config/nvim
ln -sf ~/dotfiles/_nviminit.lua ~/.config/nvim/init.lua
ln -sf ~/dotfiles/_tmux.conf ~/.tmux.conf
ln -sf ~/dotfiles/_coc-settings.json ~/.config/nvim/coc-settings.json

# setup .claude directory
mkdir -p ~/.claude
ln -sfn ~/dotfiles/_claude/agents ~/.claude/agents
ln -sf ~/dotfiles/_claude/keybindings.json ~/.claude/keybindings.json

# cleanup legacy bash symlinks (extendable)
legacy_links="bashrc bash_profile"
for legacy in $legacy_links; do
  target="$HOME/.${legacy}"
  if [ -L "$target" ]; then
    linked_path=$(readlink "$target")
    case "$linked_path" in
      *dotfiles/_bashrc|*dotfiles/_bash_profile)
        echo "removing legacy symlink $target -> $linked_path"
        rm "$target"
        ;;
    esac
  fi
done
