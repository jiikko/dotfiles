set re=1

set ambiwidth=double
set nobackup
set noswapfile

set wildignore+=.git,.svn
set wildignore+=*.jpg,*.bmp,*.gif,*.png,*.jpeg
set wildignore+=*.sw?
set wildignore+=.DS_Store
set wildignore+=node_modules,bower_components,elm-stuff

set grepprg=jvgrep

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
  NeoBundle 'Shougo/vimproc', {
        \ 'build' : {
        \     'windows' : 'echo "Sorry, cannot update vimproc binary file in Windows."',
        \     'cygwin' : 'make -f make_cygwin.mak',
        \     'mac' : 'make -f make_mac.mak',
        \     'unix' : 'make -f make_unix.mak',
        \    },
        \ }
  NeoBundle 'tpope/vim-rails'
  " NeoBundle 'Shougo/neomru'
  NeoBundle 'scrooloose/nerdtree'
  NeoBundle 'taku-o/vim-toggle'
  NeoBundle 'easymotion/vim-easymotion'
  NeoBundle 'motemen/git-vim'
  NeoBundle 'Shougo/neocomplcache'
  NeoBundle 'surround.vim'
  NeoBundle 'vim-jp/vimdoc-ja'
  NeoBundle 'kchmck/vim-coffee-script'
  NeoBundle 'Shougo/vimproc.vim'
  NeoBundle 'mattn/emmet-vim'
  NeoBundle 'slim-template/vim-slim'
  NeoBundle 'kana/vim-operator-user'
  NeoBundle 'tyru/operator-camelize.vim'
  NeoBundle 'SQLUtilities' " SQLUtilities : SQL整形、生成ユーティリティ
  NeoBundle 'Align' " Align : 高機能整形・桁揃えプラグイン
  NeoBundle 'vim-ruby/vim-ruby'
  NeoBundle 'derekwyatt/vim-scala'
  NeoBundle 'xolox/vim-session', {
        \ 'depends' : 'xolox/vim-misc',
        \ }
  NeoBundle 'kien/tabman.vim'
  " NeoBundle 'Townk/vim-autoclose'
  NeoBundle 'othree/yajs.vim'
  NeoBundle 'mustache/vim-mustache-handlebars'
  NeoBundle 'vim-scripts/gtags.vim'
  NeoBundle 'hashivim/vim-terraform'
  NeoBundle 'fatih/vim-go'
  NeoBundle 'pangloss/vim-javascript'
  NeoBundle 'moll/vim-node'
  NeoBundle 'maxmellon/vim-jsx-pretty'
  NeoBundle 'leafgarland/typescript-vim'
  NeoBundle 'peitalin/vim-jsx-typescript'
  NeoBundleLazy 'kamykn/spelunker.vim',  {
    \ "autoload" : { "filetypes" : [ "ruby" ] } }

call neobundle#end()
" NEOBUNDLE_END


"----------------------------------------------------
" プラギンの設定
"----------------------------------------------------

let g:tabman_width = 50
let g:tabman_toggle = '<leader>mt'
let g:tabman_focus  = '<leader>mf'

let g:terraform_align=1
"let g:terraform_fold_sections=1
let g:terraform_fmt_on_save=1

" vim-session
" 現在のディレクトリ直下の .vimsessions/ を取得
let s:local_session_directory = xolox#misc#path#merge(getcwd(), '.vimsessions')
" 存在すれば
if isdirectory(s:local_session_directory)
  " session保存ディレクトリをそのディレクトリの設定
  let g:session_directory = s:local_session_directory
  " vimを辞める時に自動保存
  let g:session_autosave = 'yes'
  " 引数なしでvimを起動した時にsession保存ディレクトリのdefault.vimを開く
  let g:session_autoload = 'yes'
  " 1分間に1回自動保存
  let g:session_autosave_periodic = 1
else
  let g:session_autosave = 'no'
  let g:session_autoload = 'no'
endif
unlet s:local_session_directory

" typescript
" autocmd FileType typescript :set makeprg=tsc

" for vim-go
let g:go_null_module_warning = 0

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

" http://okuhiiro.daiwa-hotcom.com/wordpress/?cat=28
let g:neocomplcache_dictionary_filetype_lists = {
    \ 'default' : '',
    \ 'javascript' :     [$HOME.'/.vim/dict/javascript.dict', $HOME.'/.vim/dict/jquery.dict'],
    \ 'coffee' : [$HOME.'/.vim/dict/javascript.dict', $HOME.'/.vim/dict/jquery.dict'],
    \ 'html' :    $HOME.'/.vim/dict/javascript.dict',
    \ 'ruby' :      $HOME.'/.vim/dict/ruby2.1.0.dict'
    \ }


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

" js
" Setup used libraries
let g:used_javascript_libs = 'jquery,underscore,react,flux,jasmine,d3'
let b:javascript_lib_use_jquery = 1
let b:javascript_lib_use_underscore = 1
let b:javascript_lib_use_react = 1
let b:javascript_lib_use_flux = 1
let b:javascript_lib_use_jasmine = 1
let b:javascript_lib_use_d3 = 1


" vim-slim
syntax enable
filetype plugin indent on

map <Leader>c <Plug>(operator-camelize)
map <Leader>C <Plug>(operator-decamelize)

" hangupする
map <S-k> <Esc>

" emmet
let g:user_emmet_leader_key = '<c-g>'

"map <C-g> :Gtags
"map <C-h> :Gtags -f %<CR>
"map <C-j> :GtagsCursor<CR>

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

set smarttab
set smartindent

set ttyfast

" showbreaks
set showbreak=↪

" 挿入モードでCtrl+kを押すとクリップボードの内容を貼り付けられるようにする "
" imap "*pa


" 改行
nnoremap ; :<C-u>call append(expand('.'), '')<CR>j

"左右
noremap <Right> gt
noremap <Left> gT

" 上下, bw でもいいけどバッファから消えてほしくない
" nnoremap <silent><Down>  :<C-u>bw<CR>
" nnoremap <silent><Down>  :<C-u>q<CR>
nnoremap <C-a><C-a> :<C-u>q<CR>
nnoremap <silent><Up>    :<C-u>UniteWithBufferDir -buffer-name=files buffer file_mru bookmark file<CR>

" QuickFix
nnoremap <C-p> :cprevious<CR>   " 前へ
nnoremap <C-n> :cnext<CR>       " 次へ
" nnoremap <C-P> :<C-u>cfirst<CR> " 最初へ
" nnoremap <C-N> :<C-u>clast<CR>  " 最後へ


"ノーマルモードでクリップボードからペースト
" nnoremap <C-p> "+p

" emacs like
" nnoremap <C-f> <Right>
" nnoremap <C-b> <Left>
" inoremap <C-f> <Right>
" inoremap <C-b> <Left>

""インサートモードでクリップボードの内容をペースト
inoremap <C-p> <ESC>"*pa

" silent ]
inoremap <C-]> <ESC>
nnoremap <C-]> <ESC>

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

" for coding
nnoremap <leader>wtf oputs "#" * 90<c-m>puts caller<c-m>puts "#" * 90<esc>
nnoremap <leader>bi obinding.pry<esc>
nnoremap <leader>rw obegin; raise; rescue => e; File.write("/tmp/ruby_caller", e.backtrace.join("\n")) && raise; end<esc>
nnoremap <leader>rr :cfile /tmp/ruby_caller<CR>:cw<esc>
nnoremap <leader>re :e /tmp/ruby_caller<esc>
nnoremap <leader>ds :e db/schema.rb<esc>
" \rwで入力待ちを消す
nmap none <Plug>RestoreWinPosn

" 便利
nnoremap <C-n><C-m> :TMToggle<CR>
nnoremap <leader>aa :tabedit<CR>
nnoremap <leader>lr :%s/ *$//g<CR>:noh<CR>
inoremap <C-y><C-w> <ESC>:w<CR>

nnoremap <C-y><C-w> :w<CR>
nnoremap <leader>sp :sp<CR>
nnoremap <leader>vs :vs<CR>


" NERDTREE, NERDTree
nnoremap <leader>nt :NERDTree<CR>
nnoremap <leader>nf :NERDTreeFind<CR>


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
" autocmd BufWritePre * :%s/\s\+$//ge

" cwindow を一緒に実行してくれる
autocmd QuickFixCmdPost *grep* cwindow

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



" for mobylette
au BufRead,BufNewFile *.mobile.erb set filetype=eruby
" 折りたたみ
set foldmethod=indent
set foldlevel=100

" for macvim
" http://taku25.hatenablog.com/entry/2014/06/02/012118
if !has('gui_running')
    set ttyfast
    set lazyredraw
endif
