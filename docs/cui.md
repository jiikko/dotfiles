# よく使う操作
## Vim
### current buffer to tab
```
tab split
```

### buffrer の前後移動
```
[ctrl] + i, o
```

### new buffer
```
[ctrl+w] + w + n
```

### puts caller strings
```
\ + w + t + f
```

### open stacktrace with quickfix
```
$ cat stacktrace.log
app/decorators/article_decorator.rb:6:in `canonical_url'
app/views/articles/show.html.slim:4:in `_app_views_articles_show_html_slim___1326588904754898243_132601740'
app/controllers/article_admin/articles_controller.rb:220:in `render_preview'
app/controllers/article_admin/articles_controller.rb:56:in `update'
app/controllers/article_admin/articles_controller.rb:13:in `user_time_zone'
$ vim -q stacktrace.log
```

### git-vim
```
\ + g + b
```

### nerdtree
ファイラーの階層を現在のファイルパスで開く
```
:NERDTREEFind
```

### vim-toggle
`+` と`-` とトグルする
```
SHIFT + \+
```

### unite.vim
バッファや最近開いたファイルをインクリメンタルサーチする
```
, + u + u
```

### 全文検索
```
:grep def **/*.rb
, + g
```

### surround.vim
指定した文字の前後を任意の文字で囲む(erbテンプレートで\= を囲むと<%= %>になる)
```
(visual modeで選択して)
[Shift + s] + (囲みたい文字)
```

### vim-easymotion
タブを超えてカーソルを移動する
```
\ + [l, w, f, s]
```

### emmet-vim
マークアップするのに便利
```
<c-g> + \,
```


## screen
### 分割
縦分割
横分割

### セッション
デタッチ
アタッチ

### タブ
キル
トグル
横移動
