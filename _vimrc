" Viとの互換断ち
set nocompatible
syntax on
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

  NeoBundle 'scrooloose/syntastic'
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
  NeoBundle 'thinca/vim-splash'
  NeoBundle 'kchmck/vim-coffee-script'

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

" vim-rspec
map <Leader>t :call RunCurrentSpecFile()<CR>
map <Leader>s :call RunNearestSpec()<CR>
map <Leader>l :call RunLastSpec()<CR>

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
" inoremap <expr><CR> pumvisible() ? neocomplcache#close_popup() : "\<CR>"
inoremap <expr><TAB> pumvisible() ? "\<C-n>" : "\<TAB>"
inoremap <expr><C-h> neocomplcache#smart_close_popup() . "\<C-h>"
inoremap <expr><BS> neocomplcache#smart_close_popup() . "\<C-h>"
inoremap <expr><C-y> neocomplcache#close_popup()
inoremap <expr><C-e> neocomplcache#cancel_popup()

" for nerafree
" Vim起動時にNerdTreeが起動するようにする
autocmd vimenter * if !argc() | NERDTree | endif
autocmd bufenter * if (winnr("$") == 1 && exists("b:NERDTreeType") && b:NERDTreeType == "primary") | q | endif
nmap <silent> <C-e>      :NERDTreeToggle<CR>
vmap <silent> <C-e> <Esc>:NERDTreeToggle<CR>
omap <silent> <C-e>      :NERDTreeToggle<CR>
imap <silent> <C-e> <Esc>:NERDTreeToggle<CR>
cmap <silent> <C-e> <C-u>:NERDTreeToggle<CR>
 let g:NERDTreeIgnore=['\.clean$',  '\.swp$',  '\.bak$',  '\~$']
""let g:NERDTreeShowHidden=1
let g:NERDTreeMinimalUI=1
let g:NERDTreeDirArrows=0
let g:NERDTreeMouseMode=2



"----------------------------------------------------
" 基本設定(base basic)
"----------------------------------------------------
" バックスペースキーで削除できるものを指定
" indent  : 行頭の空白
" eol     : 改行
" start   : 挿入モード開始位置より手前の文
set backspace=indent,eol,start
set number
set history=100
" show the cursor position all the time
set ruler
"コマンドを表示する
set showcmd
"検索ワードの最初の文字を入力した時点で検索が開始されます。
set incsearch
" ステータスラインを常に表示
set laststatus=2

" 検索結果文字列のハイライトを有効にする
set hlsearch

" ウィンドウの幅より長い行は折り返して、次の行に続けて表示する
set wrap

" tab
set expandtab "タブの代わりに空白文字挿入
set ts=2 sw=2 sts=0 "タブは半角4文字分のスペース

" スクロール時の余白確保
set scrolloff=5
" テキスト整形オプション，マルチバイト系を追加
set formatoptions=lmoq

 " 現在のモードを表示
set showmode

" モードラインは無効
" set modelines=0
" OSのクリップボードを使用する
set clipboard+=unnamed

" ターミナルでマウスを使用できるようにする
set mouse=a
set guioptions+=a
set ttymouse=xterm2

"ヤンクした文字は、システムのクリップボードに入れる"
" "set clipboard=unnamed
" 挿入モードでCtrl+kを押すとクリップボードの内容を貼り付けられるようにする "
" imap "*pa

"ノーマルモードでクリップボードからペースト
nnoremap <C-p> "+p

""インサートモードでクリップボードの内容をペースト
inoremap <C-p> <ESC>"*pa

"Yankした情報を他のアプリケーションでも利用
set clipboard=unnamed

filetype plugin indent on


"----------------------------------------------------
" ステータスライン
"----------------------------------------------------
"入力モード時、ステータスラインのカラーを変更
augroup InsertHook
  autocmd!
  autocmd InsertEnter * highlight StatusLine guifg=#ccdc90 guibg=#2E4340
  autocmd InsertLeave * highlight StatusLine guifg=#2E4340 guibg=#ccdc90
augroup END

function! GetB()
  let c = matchstr(getline('.'),  '.',  col('.') - 1)
  let c = iconv(c,  &enc,  &fenc)
  return String2Hex(c)
endfunction
" help eval-examples
" The function Nr2Hex() returns the Hex string of a number.
func! Nr2Hex(nr)
  let n = a:nr
  let r = ""
  while n
    let r = '0123456789ABCDEF'[n % 16] . r
    let n = n / 16
  endwhile
  return r
endfunc
" The function String2Hex() converts each character in a string to a two
" character Hex string.
func! String2Hex(str)
  let out = ''
  let ix = 0
  while ix < strlen(a:str)
    let out = out . Nr2Hex(char2nr(a:str[ix]))
    let ix = ix + 1
  endwhile
  return out
endfunc


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

" コメント文の色を変更
highlight Comment ctermfg=DarkCyan
" " コマンドライン補完を拡張モードにする
set wildmenu

" 括弧入力時の対応する括弧を表示
set showmatch

" タイトルをウインドウ枠に表示する
set title

" カーソル行をハイライト
set cursorline

" コマンド実行中は再描画しない
set lazyredraw

" 高速ターミナル接続を行う
set ttyfast


"----------------------------------------------------
" 編集 edit
"----------------------------------------------------
" ターミナルタイプによるカラー設定
if &term =~ "xterm-debian" || &term =~ "xterm-xfree86" || &term =~ "xterm-256color"
  set t_Co=16
  set t_Sf=^[[3%dm
  set t_Sb=^[[4%dm
elseif &term =~ "xterm-color"
  set t_Co=8
  set t_Sf=^[[3%dm
  set t_Sb=^[[4%dm
endif

"ポップアップメニューのカラーを設定
hi Pmenu guibg=#666666
hi PmenuSel guibg=#8cd0d3 guifg=#666666
hi PmenuSbar guibg=#333333

" ハイライト on
syntax enable

" 補完候補の色づけ for vim7
hi Pmenu ctermbg=white ctermfg=darkgray
hi PmenuSel ctermbg=blue ctermfg=white
hi PmenuSbar ctermbg=0 ctermfg=9




"----------------------------------------------------
" 編集 edit
"----------------------------------------------------
" insertモードを抜けるとIMEオフ
set noimdisable
set iminsert=0 imsearch=0
set noimcmdline
inoremap <silent> <ESC> <ESC>:set iminsert=0<CR>

" コンマの後に自動的にスペースを挿入
" "inoremap , ,<Space>

" 保存時に行末の空白を除去する
autocmd BufWritePre * :%s/\s\+$//ge




"----------------------------------------------------
" インデント
"----------------------------------------------------
"set noexpandtab

"----------------------------------------------------
" バックアップ
"----------------------------------------------------
" vmsオプションをつけたらバックアップファイルを作らない
set backup
set noswapfile
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
  " カーソル位置を記憶する
  autocmd BufReadPost *
  \ if line("'\"") > 0 && line("'\"") <= line("$") |
  \   exe "normal g`\"" |
  \ endif
endif

"CTRL-nでシンタックスチェック eで実行
" autocmd FileType ruby :map <C-n> <ESC>:!ruby -cW %<CR>
" autocmd FileType ruby :map <C-e> <ESC>:!ruby %<CR>


"----------------------------------------------------
" 国際化関係
"----------------------------------------------------
" 文字コードの設定
" fileencodingsの設定ではencodingの値を一番最後に記述する
set termencoding=utf-8
set fileencodings=utf-8
set encoding=utf-8
set fileencoding=utf-8


"----------------------------------------------------
" その他
"----------------------------------------------------
" ビープ音を鳴らさない
set vb t_vb=

" コマンド補完を開始するキー
set wildchar=<tab>



