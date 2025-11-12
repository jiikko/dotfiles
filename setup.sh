#!/bin/sh

# shellcheck disable=SC3040
set -o pipefail

# set rc limlink
for file in gemrc screenrc gvimrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global; do
  echo 'making symlink' _$file '->' ~/.$file
  ln -s -F ~/dotfiles/_$file ~/.$file
done

mkdir -p ~/.config/nvim
ln -s -F ~/dotfiles/_nviminit.lua ~/.config/nvim/init.lua
ln -s -F ~/dotfiles/_tmux.conf ~/.tmux.conf
ln -s -F ~/dotfiles/_coc-settings.json ~/.config/nvim/coc-settings.json

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
