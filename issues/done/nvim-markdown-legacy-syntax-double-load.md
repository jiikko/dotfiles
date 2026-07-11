# nvim: markdown を開くとレガシー Vimscript syntax が treesitter と二重ロードされる (~16ms)

起票日: 2026-07-11（レイテンシ計測セッションのぼやきから issue 化）
優先度: P3（機能は正常。markdown の初回オープンが ~16ms 遅いだけ）

## 現象（実測済み・nvim v0.11.5）

markdown ファイルを開くと、treesitter highlight が有効なのにレガシー Vimscript syntax
一式が source される:

```
--startuptime (README.md を開いた場合):
  syntax/css.vim       3.5ms
  syntax/html.vim      5.9ms   (markdown.vim が include)
  syntax/markdown.vim  6.7ms
  合計                ~16ms
```

開いた後の状態は `vim.treesitter.highlighter.active` = true / `&syntax` = 空。
つまり **treesitter (nvim-treesitter master の highlight モジュール) は attach 時に
legacy syntax を無効化するが、それはファイル読み込みで syntax/*.vim が source された後**。
無効化されるものを毎回 ~16ms かけてロードしている。

- `b:current_syntax` guard は buffer-local のため、**別の markdown バッファを開くたびに再 source される**
  （軽くなるのは同じバッファでの再設定程度）
- markdown が重いのは markdown.vim が html.vim → css.vim を連鎖 include するため。
  他の ft (lua/go 等) でも legacy syntax の source は起きるが数 ms 未満で実害が薄い

## 対応方針の候補（着手時に要検証）

1. **ts parser が実在する ft では legacy syntax の読み込み自体を止める**:
   抑止点は `synload.vim` の `syntaxset` autocmd 経路 (`Syntax` autocmd は `syn clear` 後に
   `b:current_syntax` を unlet してから `runtime! syntax/{name}` するため、`FileType` で
   `b:current_syntax` を先置きしても**抑止にならない** — codex 検証済み)。効くのは
   `b:ts_highlight` 側の分岐か `syntaxset` の順序制御。また parser 実在判定は
   `vim.treesitter.language.get_lang(ft)` では不十分 (ft→lang 変換のみ) で、
   `pcall(vim.treesitter.get_parser, buf)` 相当の確認が必要 (parser/query 不在時に
   ハイライトなしへ落とさないため)。**着手時は nvim-treesitter master の highlight
   モジュールと synload.vim の実装を先に読むこと**。
2. **現状維持**: ~16ms は markdown バッファのオープン時のみ。render-markdown / 診断など
   他機能に影響なし。

判断の目安: 案 1 が synload.vim / nvim-treesitter の実装と衝突せず数行で書けるなら採用、
fragile になるなら案 2（現状維持 + 本 issue クローズ時に rationale をコードへ）。
なお「`g:markdown_fenced_languages` 系で html/css 連鎖だけ切る」案は不成立:
syntax/markdown.vim は `runtime! syntax/html.vim` を無条件実行し、css は html.vim 側の
embedded style include のため、この変数では切れない (codex 検証済み)。

## 検証手順（再現）

```sh
nvim --headless -u ~/.config/nvim/init.lua --startuptime /tmp/st.log README.md "+qa!"
grep -E "syntax/(markdown|html|css)" /tmp/st.log
```

修正後は上記 grep が消える（または縮む）こと、かつ markdown のハイライトが
treesitter で維持されること（`vim.treesitter.highlighter.active` = true）を確認する。
なお tests/nvim/bench_nvim.sh の bufload は 5000 行 lua 固定のため markdown 固有の
回帰は直接追えない (全体の補助指標として使う。必要なら md 版メトリクスを足す)。

## 関連

- 計測の出典: 2026-07-11 のレイテンシ計測（tests/nvim/bench_nvim.sh 導入セッション）。
  fold の再評価コスト（678ms→27ms）と mason-lspconfig 廃止は対応済みで、これが残件
- `docs/nvim-plugins.md` の軽量化方針（重い Vimscript の排除）と同系だが、これは
  プラグインでなく nvim ランタイムの syntax ファイル
