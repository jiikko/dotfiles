#!/bin/bash

# set rc limlink
for file in gemrc screenrc bashrc bash_pofile vimrc zshrc rspec gitconfig gitignore_global pryrc zlogin
do
  echo 'making symlink' _$file '->' $HOME/.$file
  ln -s -F `pwd`/_$file $HOME/.$file
done

# set dict symlink
ln -s $DOTFILE_FULLPATH/lib/vim/dict/javascript.dict $HOME/.vim/dict/javascript.dict
