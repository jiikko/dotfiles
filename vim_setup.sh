#!/bin/sh

curl https://raw.githubusercontent.com/Shougo/neobundle.vim/master/bin/install.sh | sh
cp ~/.vimrc ~/.temp_vimrc

$(cat << EOH > ~/.vimrc
if 0 | endif
if &compatible
  set nocompatible
endif
set runtimepath^=~/.vim/bundle/neobundle.vim/
call neobundle#begin(expand('~/.vim/bundle/'))
  NeoBundleFetch 'Shougo/neobundle.vim'
call neobundle#end()
filetype plugin indent on
NeoBundleInstall
EOH
)
vim -c q

ruby << EOH > ~/.vimrc
file = File.read File.expand_path("~/.vimrc")
file =~ /" NEOBUNDLE_START(.*?)" NEOBUNDLE_END/m
puts \$1
EOH
vim -c q

cat ~/.temp_vimrc > ~/.vimrc
