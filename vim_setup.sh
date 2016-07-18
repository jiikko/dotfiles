#!/bin/sh

curl https://raw.githubusercontent.com/Shougo/neobundle.vim/master/bin/install.sh | sh
cp ~/.vimrc ~/.original_vimrc

<< EOH > ~/.vimrc
if 0 | endif
if &compatible
  set nocompatible
endif
set runtimepath^=~/.vim/bundle/neobundle.vim/
call neobundle#begin(expand('~/.vim/bundle/'))
  NeoBundleFetch 'Shougo/neobundle.vim'
call neobundle#end()
filetype plugin indent on
call feedkeys(" ")
NeoBundleInstall
EOH
vim -c q

ruby << EOH > ~/.vimrc
file = File.read File.expand_path("~/.original_vimrc")
file =~ /" NEOBUNDLE_START(.*?)" NEOBUNDLE_END/m
puts <<-RUBY_EOH
#{\$1}
call feedkeys(" ")
NeoBundleInstall
RUBY_EOH
EOH
vim -c q

cat ~/.original_vimrc > ~/.vimrc
rm ~/.original_vimrc
