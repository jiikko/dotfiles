#!/bin/bash

# set rc limlink
for file in gemrc screenrc bashrc bash_pofile vimrc zshrc rspec gitconfig gitignore_global pryrc zlogin
do
  echo 'making symlink' _$file '->' $HOME/.$file
  ln -s -F `pwd`/_$file $HOME/.$file
done

# set dict symlink
mkdir -p $HOME/.vim/dict
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/javascript.dict $HOME/.vim/dict
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/jquery.dict     $HOME/.vim/dict
ln -s -F $DOTFILE_FULLPATH/lib/vim/dict/ruby2.1.0.dict  $HOME/.vim/dict
