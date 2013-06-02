" Viとの互換断ち
set nocompatible
filetype off


"----------------------------------------------------
" NeoBundle
"----------------------------------------------------
let s:neobundledir   = expand('~/.vim/neobundle')

if isdirectory(s:neobundledir)
  if has('vim_starting')
    execute 'set runtimepath+=' . s:neobundledir . '/neobundle.vim'
  endif
  call neobundle#rc(s:neobundledir)

  NeoBundle 'Shougo/vimproc', {
        \ 'build' : {
        \     'windows' : 'echo "Sorry, cannot update vimproc binary file in Windows."',
        \     'cygwin' : 'make -f make_cygwin.mak',
        \     'mac' : 'make -f make_mac.mak',
        \     'unix' : 'make -f make_unix.mak',
        \    },
        \ }

  NeoBundle 'tpope/vim-rails'
  NeoBundle "unite.vim"
  NeoBundle 'scrooloose/nerdtree'
  NeoBundle 'taku-o/vim-toggle'
  NeoBundle 'Lokaltog/vim-easymotion'
  NeoBundle 'motemen/git-vim'
  NeoBundle 'Shougo/neocomplcache'
  NeoBundle 'surround.vim'
  NeoBundle 'skwp/vim-rspec'
  NeoBundle 'nathanaelkane/vim-indent-guides'
  NeoBundle 'vim-jp/vimdoc-ja'
  NeoBundle 'mattn/zencoding-vim'

else
  command! NeoBundleInit call s:neobundle_init()
  function! s:neobundle_init()
    call mkdir(s:neobundledir, 'p')
    execute 'cd' s:neobundledir
    call system('git clone git://github.com/Shougo/neobundle.vim')
    execute 'set runtimepath+=' . s:neobundledir . '/neobundle.vim'
    call neobundle#rc(s:neobundledir)
    NeoBundle 'Shougo/vimproc', {
          \ 'build' : {
          \     'cygwin' : 'make -f make_cygwin.mak',
          \     'mac' : 'make -f make_mac.mak',
          \     'unix' : 'make -f make_unix.mak',
          \    },
          \ }

    NeoBundleInstall
  endfunction
endif
 

"----------------------------------------------------
" プラギンの設定
"----------------------------------------------------
" for vimdoc-ja
" helptags ~/.vim/bundle/vimdoc-ja/doc

" for vim-indent-guides conf
" http://chiiiiiiiii.hatenablog.com/entry/2012/12/02/102815
colorscheme default
set tabstop=2
set shiftwidth=2
set expandtab
"vim立ち上げたときに、自動的にvim-indent-guidesをオンにする
let g:indent_guides_enable_on_vim_startup=1
" ガイドをスタートするインデントの量
let g:indent_guides_start_level=2
" 自動カラーを無効にする
let g:indent_guides_auto_colors=0
" 奇数インデントのカラー
autocmd VimEnter,Colorscheme * :hi IndentGuidesOdd  guibg=#262626 ctermbg=gray
" 偶数インデントのカラー
autocmd VimEnter,Colorscheme * :hi IndentGuidesEven guibg=#3c3c3c ctermbg=darkgray
" ハイライト色の変化の幅
let g:indent_guides_color_change_percent = 30
" ガイドの幅
let g:indent_guides_guide_size = 1

"" for easymotion
let g:EasyMotion_leader_key = '<Space><Space>'
let g:EasyMotion_keys = 'fjdkslaureiwoqpvncm'

" for neocomplcache
" http://teppeis.hatenablog.com/entry/20100926/1285502391
let g:neocomplcache_enable_at_startup = 1
let g:neocomplcache_max_list = 30
let g:neocomplcache_auto_completion_start_length = 2
let g:neocomplcache_enable_smart_case = 1
let g:neocomplcache_enable_auto_select = 1
let g:neocomplcache_enable_camel_case_completion = 1
let g:neocomplcache_enable_underbar_completion = 1
inoremap <expr><C-g> neocomplcache#undo_completion()
inoremap <expr><C-l> neocomplcache#complete_common_string()
inoremap <expr><CR> pumvisible() ? neocomplcache#close_popup() : "\<CR>"
inoremap <expr><TAB> pumvisible() ? "\<C-n>" : "\<TAB>"
inoremap <expr><C-h> neocomplcache#smart_close_popup() . "\<C-h>"
inoremap <expr><BS> neocomplcache#smart_close_popup() . "\<C-h>"
inoremap <expr><C-y> neocomplcache#close_popup()
inoremap <expr><C-e> neocomplcache#cancel_popup()


"----------------------------------------------------
" 基本設定(base)
"----------------------------------------------------
" バックスペースキーで削除できるものを指定
" indent  : 行頭の空白
" eol     : 改行
" start   : 挿入モード開始位置より手前の文
set backspace=indent,eol,start
set number

set history=100		" keep 50 lines of command line history
set ruler		" show the cursor position all the time
set showcmd		"コマンドを表示する
set incsearch		"検索ワードの最初の文字を入力した時点で検索が開始されます。
set laststatus=2 " ステータスラインを常に表示

" 検索結果文字列のハイライトを有効にする
set hlsearch

" ウィンドウの幅より長い行は折り返して、次の行に続けて表示する
set wrap

" tab
set expandtab "タブの代わりに空白文字挿入
set ts=2 sw=2 sts=0 "タブは半角4文字分のスペース


"----------------------------------------------------
" 表示
"----------------------------------------------------
" 全角スペースの表示
highlight ZenkakuSpace cterm=underline ctermfg=lightblue guibg=darkgray
match ZenkakuSpace /　/

" ステータスラインに表示する情報の指定
set statusline=%n\:%y%F\ \|%{(&fenc!=''?&fenc:&enc).'\|'.&ff.'\|'}%m%r%=
" ステータスラインの色
highlight StatusLine   term=NONE cterm=NONE ctermfg=black ctermbg=white

" 現バッファの差分表示(変更箇所の表示)
if !exists(":DiffOrig")
  command! DiffOrig vert new | set bt=nofile | r # | 0d_ | diffthis | wincmd p | diffthis
endif

" カレントウィンドウにのみ罫線を引く
augroup cch
  autocmd! cch
  autocmd WinLeave * set nocursorline
  autocmd WinEnter,BufRead * set cursorline
augroup END


"----------------------------------------------------
" バックアップ
"----------------------------------------------------
" vmsオプションをつけたらバックアップファイルを作らない
if has("vms")
  set nobackup
else
  set backup
endif

" バックアップファイルを作るディレクトリ
" set backupdir=~/.vim/backup
" スワップファイルを作るディレクトリ
" set directory=~/.vim/swap


"----------------------------------------------------
" オートコマンド
"----------------------------------------------------
if has("autocmd")
  " ファイルタイプ別インデント、プラグインを有効にする
  filetype plugin indent on
  " カーソル位置を記憶する
  autocmd BufReadPost *
  \ if line("'\"") > 0 && line("'\"") <= line("$") |
  \   exe "normal g`\"" |
  \ endif
endif

"CTRL-nでシンタックスチェック eで実行
autocmd FileType ruby :map <C-n> <ESC>:!ruby -cW %<CR>
autocmd FileType ruby :map <C-e> <ESC>:!ruby %<CR>


"----------------------------------------------------
" 国際化関係
"----------------------------------------------------
" 文字コードの設定
" fileencodingsの設定ではencodingの値を一番最後に記述する
set encoding=utf-8
set termencoding=utf-8
set fileencoding=utf-8
set fileencodings=ucs-bom,euc-jp,cp932,iso-2022-jp
set fileencodings+=,ucs-2le,ucs-2,utf-8


"----------------------------------------------------
" その他
"----------------------------------------------------
" ビープ音を鳴らさない
set vb t_vb=

" 入力保管系
" コンマの後に自動的にスペースを挿入
inoremap , ,<Space>

" コマンド補完を開始するキー
set wildchar=<tab>


filetype plugin indent on
