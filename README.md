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
