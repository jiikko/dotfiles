
# vim-ambiwidth
これは`set ambiwidth=single` でVimを使用したいけど、文字幅が曖昧な文字が残念な表示になってしまうのを `setcellwidths()` にて文字幅調整してくれるVimプラグインです。
2022年5月18日時点で `setcellwidths()` はVimのみ実装されているためNeovimではこのVimプラグインは機能しませんが、2022年8月9日時点で取り込まれたためそれ以降のNeovimでは使用できます( https://github.com/neovim/neovim/pull/13883 )。  

## 対象文字
このプラグインにより全角幅にしている文字一覧は[list.txt](https://github.com/rbtnn/vim-ambiwidth/blob/main/list.txt)に出力されています。

## 設定

### `g:ambiwidth_add_list`
`g:ambiwidth_add_list` を定義しておくことでこのプラグインで指定している文字に加えて別の文字も追加できます。
既定のレンジと重複する範囲を指定した場合は add_list 全体が無効になり既定のみ適用されます (WARN 通知あり)。
```
" 例: Supplemental Symbols and Pictographs を全角幅にする (既定レンジには含まれない)
let g:ambiwidth_add_list = [[0x1f900, 0x1f9ff, 2]]
```

### `g:ambiwidth_cica_enabled`
https://miiton.github.io/Cica/ に掲載されている大量の絵文字もデフォルトで全角幅にしています。
もしこれが不要であれば、.vimrcに以下の設定を行い無効にしてください。
```
let g:ambiwidth_cica_enabled = v:false
```
