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

$(cat << EOH > ~/.vimrc
let s:neobundledir = expand('~/.vim/bundle')
execute 'set runtimepath+=' . s:neobundledir . '/neobundle.vim'
call neobundle#begin(s:neobundledir)
  NeoBundle 'tpope/vim-rails'
  NeoBundle "unite.vim"
  NeoBundle 'scrooloose/nerdtree'
  NeoBundle 'taku-o/vim-toggle'
  NeoBundle 'easymotion/vim-easymotion'
  NeoBundle 'motemen/git-vim'
  NeoBundle 'Shougo/neocomplcache'
  NeoBundle 'surround.vim'
  NeoBundle 'vim-jp/vimdoc-ja'
  NeoBundle 'kchmck/vim-coffee-script'
  NeoBundle 'mattn/emmet-vim'
  NeoBundle 'slim-template/vim-slim'
  NeoBundle 'kana/vim-operator-user'
  NeoBundle 'tyru/operator-camelize.vim'
call neobundle#end()
NeoBundleInstall
EOH
)
vim -c q

cat ~/.temp_vimrc > ~/.vimrc
