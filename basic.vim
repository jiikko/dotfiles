"----------------------------------------------------
" 基本設定(base basic)
"----------------------------------------------------
" バックスペースキーで削除できるものを指定
" indent  : 行頭の空白
" eol     : 改行
" start   : 挿入モード開始位置より手前の文
set backspace=indent,eol,start
set number
set history=10000
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
set clipboard=unnamedplus

set smarttab
set smartindent

" showbreaks
set showbreak=↪

" 挿入モードでCtrl+kを押すとクリップボードの内容を貼り付けられるようにする "
" imap "*pa


" 改行
nnoremap ; :<C-u>call append(expand('.'), '')<CR>j
nnoremap <Return><Return> <c-w><c-w>

"左右
noremap <Right> <Cmd>BufferNext<CR>
noremap <Left> <Cmd>BufferPrevious<CR>
noremap gt <Cmd>BufferNext<CR>
noremap gT <Cmd>BufferPrevious<CR>


" 上下, bw でもいいけどバッファから消えてほしくない
" nnoremap <silent><Down>  :<C-u>bw<CR>
" nnoremap <silent><Down>  :<C-u>q<CR>
nnoremap <C-a><C-a> :BufferClose<CR>

" QuickFix
nnoremap <C-p> :cprevious<CR>   " 前へ
nnoremap <C-n> :cnext<CR>       " 次へ
" nnoremap <C-P> :<C-u>cfirst<CR> " 最初へ
" nnoremap <C-N> :<C-u>clast<CR>  " 最後へ

"ノーマルモードでクリップボードからペースト
" nnoremap <C-p> "+p

" emacs like
nnoremap <C-f> <Right>
nnoremap <C-b> <Left>
inoremap <C-f> <Right>
inoremap <C-b> <Left>

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
" nnoremap <leader>wtf oputs "#" * 90<c-m>puts caller<c-m>puts "#" * 90<esc>
autocmd FileType ruby nnoremap <buffer> <leader>bi obinding.pry<esc>
autocmd FileType javascript,typescript,typescriptreact,javascriptreact nnoremap <buffer> <leader>bi odebugger<esc>
autocmd FileType eruby nnoremap <buffer> <leader>bi o<% binding.pry %><esc>

nnoremap <leader>rw obegin; raise; rescue => e; File.write("/tmp/ruby_caller", e.backtrace.join("\n")) && raise; end<esc>
nnoremap <leader>rr :cfile /tmp/ruby_caller<CR>:cw<esc>
nnoremap <leader>re :e /tmp/ruby_caller<esc>
nnoremap <leader>ds :e db/schema.rb<esc>
nnoremap <leader>yr o@return []<esc>
nnoremap <leader>yp o@param []<esc>

" 便利
nnoremap <leader>aa :enew<CR>
nnoremap <leader>lr :%s/ *$//g<CR>:noh<CR>
inoremap <C-y><C-w> <ESC>:w<CR>
nnoremap <C-y><C-w> :w<CR>
nnoremap <leader>sp :sp<CR>
nnoremap <leader>vs :vs<CR>

"Yankした情報を他のアプリケーションでも利用
set clipboard=unnamed


"----------------------------------------------------
" 表示
"----------------------------------------------------
" 全角スペースの表示
highlight ZenkakuSpace cterm=underline ctermfg=lightblue guibg=darkgray
match ZenkakuSpace /　/

" コメント文の色を変更
highlight Comment ctermfg=DarkCyan
" " コマンドライン補完を拡張モードにする
set wildmenu

" 括弧入力時の対応する括弧を表示
set showmatch

" タイトルをウインドウ枠に表示する
set title

" カレントバッファにのみ罫線を引く
augroup cch
  autocmd! cch
  autocmd WinLeave * set nocursorline
  autocmd WinEnter,BufRead * set cursorline
augroup END

" コマンド実行中は再描画しない
set lazyredraw

"----------------------------------------------------
" その他
"----------------------------------------------------
" ビープ音を鳴らさない
set vb t_vb=

" コマンド補完を開始するキー
set wildchar=<tab>
