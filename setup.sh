for file in bashrc bash_pofile gemrc screenrc vimrc zshrc
do
  echo 'making symlink' _$file '->' $HOME/.$file
  ln -s `pwd`/_$file $HOME/.$file
done
