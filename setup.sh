#!/bin/sh

# set rc limlink
for file in gemrc screenrc bashrc bash_profile gvimrc zshrc rspec gitconfig gitignore_global pryrc zlogin railsrc ideavimrc globalrc; do
  echo 'making symlink' _$file '->' ~/.$file
  ln -s -F ~/dotfiles/_$file ~/.$file
done

mkdir -p ~/.config/nvim
ln -s -F ~/dotfiles/_nvimconfig ~/.config/nvim/init.vim
ln -s -F ~/dotfiles/_coc-settings.json ~/.config/nvim/coc-settings.json
