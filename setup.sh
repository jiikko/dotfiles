#!/bin/sh

# set rc limlink
for file in gemrc screenrc bashrc bash_profile gvimrc vimrc zshrc rspec gitconfig gitignore_global pryrc zlogin railsrc ideavimrc globalrc; do
  echo 'making symlink' _$file '->' ~/.$file
  ln -s -F ~/dotfiles/_$file ~/.$file
done

mkdir -p ~/.config/nvim
ln -s -F ~/dotfiles/_nvimconfig ~/.config/nvim/init.vim

# set dict symlink
mkdir -p ~/.vim/dict
DOTFILE_FULLPATH=`ls -al ~/.zshrc | awk '{print $11}' |  sed -e "s|/_zshrc||"`
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/javascript.dict $HOME/.vim/dict/
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/jquery.dict     $HOME/.vim/dict/
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/ruby2.1.0.dict  $HOME/.vim/dict/
