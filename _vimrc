set nobackup
set re=1

set ambiwidth=double
set backupskip=/tmp/*,/private/tmp/*

"use neocomplete.
"http://naoyashiga.hatenablog.com/entry/2013/10/16/005443
let g:neocomplete#enable_at_startup = 1

" http://okuhiiro.daiwa-hotcom.com/wordpress/?cat=28
let g:neocomplcache_force_overwrite_completefunc = 1
let g:neocomplcache_dictionary_filetype_lists = {
    \ 'default' : '',
    \ 'js' :     [$HOME.'/.vim/dict/javascript.dict', $HOME.'/.vim/dict/jquery.dict'],
    \ 'coffee' : [$HOME.'/.vim/dict/javascript.dict', $HOME.'/.vim/dict/jquery.dict'],
    \ 'html' :    $HOME.'/.vim/dict/javascript.dict',
    \ 'rb' :      $HOME.'/.vim/dict/ruby2.1.0.dict'
    \ }


" Viとの互換断ち
set nocompatible
syntax on
filetype off

command Q quit
command W write
command Wq wq
command WQ wq
command Vs vs
command VS vs
command Sp sp
command SP sp
command Tabe tabe
command TAbe tabe
command TABe tabe
command TABE tabe

nnoremap Q <Nop>

"----------------------------------------------------
" NeoBundle
"----------------------------------------------------
" NEOBUNDLE_START
let s:neobundledir   = expand('~/.vim/bundle')
execute 'set runtimepath+=' . s:neobundledir . '/neobundle.vim'
call neobundle#begin(s:neobundledir)
  NeoBundle 'tpope/vim-rails'
  NeoBundle "unite.vim"
  " NeoBundle 'Shougo/neomru'
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
  NeoBundle 'kana/vim-smartinput'

call neobundle#end()
" NEOBUNDLE_END


"----------------------------------------------------
" プラギンの設定
"----------------------------------------------------


" unite
""" unite.vim

" autocmd vimenter * if !argc() | Unite file | endif

" 入力モードで開始する
let g:unite_enable_start_insert=1
call unite#custom_default_action('file', 'tabopen')
" バッファ一覧
nnoremap <silent> ,ub :<C-u>Unite buffer<CR>
" ファイル一覧
nnoremap <silent> ,uf :<C-u>UniteWithBufferDir -buffer-name=files file<CR>
" レジスタ一覧
nnoremap <silent> ,ur :<C-u>Unite -buffer-name=register register<CR>
" 最近使用したファイル一覧
nnoremap <silent> ,um :<C-u>Unite file_mru<CR>
" 常用セット
nnoremap <silent> ,uu :<C-u>Unite buffer file_mru<CR>
" 全部乗せ
nnoremap <silent> ,ua :<C-u>UniteWithBufferDir -buffer-name=files buffer file_mru bookmark file<CR>
" ウィンドウを分割して開く
au FileType unite nnoremap <silent> <buffer> <expr> <C-j> unite#do_action('split')
au FileType unite inoremap <silent> <buffer> <expr> <C-j> unite#do_action('split')
" ウィンドウを縦に分割して開く
au FileType unite nnoremap <silent> <buffer> <expr> <C-l> unite#do_action('vsplit')
au FileType unite inoremap <silent> <buffer> <expr> <C-l> unite#do_action('vsplit')
" ESCキーを2回押すと終了する
au FileType unite nnoremap <silent> <buffer> <ESC><ESC> q
au FileType unite inoremap <silent> <buffer> <ESC><ESC> <ESC>q

" for vimdoc-ja
" helptags ~/.vim/bundle/vimdoc-ja/doc

" for vim-indent-guides conf
" http://chiiiiiiiii.hatenablog.com/entry/2012/12/02/102815
colorscheme default

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
" <Leader>f{char} to move to {char}
map  <Leader>f <Plug>(easymotion-bd-f)
nmap <Leader>f <Plug>(easymotion-overwin-f)

" s{char}{char} to move to {char}{char}
nmap s <Plug>(easymotion-overwin-f2)
vmap s <Plug>(easymotion-bd-f2)

" Move to line
map <Leader>l <Plug>(easymotion-bd-jk)
nmap <Leader>l <Plug>(easymotion-overwin-line)

" Move to word
map  <Leader>w <Plug>(easymotion-bd-w)
nmap <Leader>w <Plug>(easymotion-overwin-w)

" for neocomplcache
" http://teppeis.hatenablog.com/entry/20100926/1285502391
let g:neocomplcache_enable_at_startup = 1
let g:neocomplcache_max_list = 30
let g:neocomplcache_auto_completion_start_length = 2
let g:neocomplcache_enable_smart_case = 1
let g:neocomplcache_enable_auto_select = 1
let g:neocomplcache_enable_camel_case_completion = 1
let g:neocomplcache_enable_underbar_completion = 1
let g:neocomplcache_min_syntax_length = 3
inoremap <expr><C-g> neocomplcache#undo_completion()
inoremap <expr><C-l> neocomplcache#complete_common_string()
inoremap <expr><CR> pumvisible() ? neocomplcache#close_popup() : "\<CR>"
inoremap <expr><TAB> pumvisible() ? "\<C-n>" : "\<TAB>"
inoremap <expr><C-h> neocomplcache#smart_close_popup() . "\<C-h>"
inoremap <expr><BS> neocomplcache#smart_close_popup() . "\<C-h>"
inoremap <expr><C-y> neocomplcache#close_popup()
inoremap <expr><C-e> neocomplcache#cancel_popup()

" for nerdtree
" Vim起動時にNerdTreeが起動するようにする
" autocmd vimenter * if !argc() | NERDTree | endif
" autocmd bufenter * if (winnr("$") == 1 && exists("b:NERDTreeType") && b:NERDTreeType == "primary") | q | endif
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

" vim-slim
syntax enable
filetype plugin indent on

map <Leader>c <Plug>(operator-camelize)
map <Leader>C <Plug>(operator-decamelize)


"----------------------------------------------------
" 基本設定(base basic)
"----------------------------------------------------
" バックスペースキーで削除できるものを指定
" indent  : 行頭の空白
" eol     : 改行
" start   : 挿入モード開始位置より手前の文
set backspace=indent,eol,start
set number
set history=1000
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
" set mouse=a
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


" ========== 検索結果のハイライト&Escで消す
" ハイライトON
set hlsearch

" Esc Esc でハイライトOFF
nnoremap <Esc><Esc> :<C-u>set nohlsearch<Return>

" 「/」「?」「*」「#」が押されたらハイライトをON にしてから「/」「?」「*」「#」
nnoremap / :<C-u>set hlsearch<Return>/
nnoremap ? :<C-u>set hlsearch<Return>?
nnoremap * :<C-u>set hlsearch<Return>*
nnoremap # :<C-u>set hlsearch<Return>#

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
" set statusline=%n\:%y%F\ \|%{(&fenc!=''?&fenc:&enc).'\|'.&ff.'\|'}%m%r%=
set statusline=%<%f\ %m%r%h%w%{'['.(&fenc!=''?&fenc:&enc).']['.&ff.']'}%=%l,%c%V%8P
" ステータスラインの色
highlight StatusLine   term=NONE cterm=NONE ctermfg=black ctermbg=white

" 現バッファの差分表示(変更箇所の表示)
if !exists(":DiffOrig")
  command! DiffOrig vert new | set bt=nofile | r # | 0d_ | diffthis | wincmd p | diffthis
endif

" カレントウィンドウにのみ罫線を引く
" augroup cch
"   autocmd! cch
"   autocmd WinLeave * set nocursorline
"   autocmd WinEnter,BufRead * set cursorline
" augroup END

" コメント文の色を変更
highlight Comment ctermfg=DarkCyan
" " コマンドライン補完を拡張モードにする
set wildmenu

" 括弧入力時の対応する括弧を表示
set showmatch

" タイトルをウインドウ枠に表示する
set title

" カーソル行をハイライト
" set cursorline

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


" URLをひらくやつ
function! HandleURL()
  let s:uri = matchstr(getline("."), '[a-z]*:\/\/[^ >,;]*')
  echo s:uri
  if s:uri != ""
    silent exec "!open '".s:uri."'"
    :redraw!
  else
    echo "No URI found in line."
  endif
endfunction
nmap <silent> <Leader>b <Esc>:call HandleURL()<CR>
