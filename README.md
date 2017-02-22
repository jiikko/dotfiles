dotfiles
========

# Installing

```
cd ~
git clone git@github.com:jiikko/dotfiles.git || https://github.com/jiikko/dotfiles.git
cd dotfiles
./setup.sh
```

[for Mac](./mac "for Mac")

# よく使う操作
## Vim
### new buffer
```
[ctrl+w] + w + n
```
### puts caller strings
```
\ + w + t + f
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

### surround.vim
指定した文字の前後を任意の文字で囲む(erbテンプレートで\= を囲むと<%= %>になる)
```
(visual modeで選択して)
[Shift + s] + (囲みたい文字)
```

### vim-easymotion
TODO

### emmet-vim
マークアップするのに便利
TODO

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
