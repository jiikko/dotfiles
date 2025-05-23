#!/bin/sh

# set rc limlink
for file in gemrc screenrc bashrc bash_profile gvimrc zshrc rspec gitconfig pryrc zlogin railsrc ideavimrc gitignore_global; do
  echo 'making symlink' _$file '->' ~/.$file
  ln -s -F ~/dotfiles/_$file ~/.$file
done

mkdir -p ~/.config/nvim
ln -s -F ~/dotfiles/_nviminit.lua ~/.config/nvim/init.lua
ln -s -F ~/dotfiles/_tmux.conf ~/.tmux.conf
ln -s -F ~/dotfiles/_coc-settings.json ~/.config/nvim/coc-settings.json
