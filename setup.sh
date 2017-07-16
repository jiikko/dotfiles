#!/bin/sh

# set rc limlink
for file in gemrc screenrc bashrc bash_profile vimrc zshrc rspec gitconfig gitignore_global pryrc zlogin; do
  echo 'making symlink' _$file '->' ~/.$file
  ln -s -F ~/dotfiles/_$file ~/.$file
done

# set dict symlink
mkdir -p ~/.vim/dict
DOTFILE_FULLPATH=`ls -al ~/.zshrc | awk '{print $11}' |  sed -e "s|/_zshrc||"`
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/javascript.dict $HOME/.vim/dict/
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/jquery.dict     $HOME/.vim/dict/
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/ruby2.1.0.dict  $HOME/.vim/dict/
