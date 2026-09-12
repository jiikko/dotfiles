# 365 colorscheme が定義していない treesitter capture の「潰れ」を誰も検出していない

種別: research / test
優先度: low-mid
起票: 2026-09-12
関連: commit 4887d531 (markdown の見出しを直した実例) / `nvim/lua/dotfiles/hl.lua`

## 何が問題か

`dotfiles.hl.set` は **「gui 色のみで cterm 併記が無い」** を WARN で拾う (256色運用で
無言に効かなくなる事故の再発防止)。しかしそれは *自分がカスタム色を書いたとき* にしか
働かない。**colorscheme がその capture 自体を定義しておらず、兄弟が同じ見た目へ潰れる**
形は、誰も検出していない。

実例 (commit 4887d531 で修正済み): gruvbox / retrobox はどちらも
`@markup.heading.N.markdown` を定義せず `@markup.heading` → `Title` に落ちるため、
**markdown の H1〜H6 が全部おなじ緑 (ctermfg=142 bold)** だった。目で見て気づくまで
誰も気づけない (テストも lint も緑のまま)。

## 全数勘定 (実測 2026-09-12 / retrobox 256色 = SUPPORT_TRUECOLOR=false の実環境)

測り方: `queries/*/highlights.scm` から capture を集め、**ハイライタが実際に解決する名前**
(`@capture.<lang>`) で `nvim_get_hl(link=false)` を引き、同じ族の兄弟が 1 種類の見た目に
潰れているかを見る。

**母集合 = 33 言語**。内訳は `_nviminit.lua:249` の `ts.install(...)` が入れる **31 言語**
(機械で数えた: `sed -n '249p' _nviminit.lua | grep -oE '"[a-z_]+"' | wc -l` → 31。
同ファイル 246 行目付近のコメントも「31 個」と書いている) に、**nvim 同梱の
`vimdoc` / `query` の 2 つ**を足したもの。
🚨 `vimdoc` と `query` は `ts.install` に**含まれない** — Neovim 本体が
`/opt/homebrew/Cellar/neovim/0.11.5/lib/nvim/parser/` に同梱しており
(`c lua markdown markdown_inline query vim vimdoc` の 7 つ。うち 5 つは 31 言語側と重複)、
下の Tier1 の `vimdoc` の所見はこの同梱ぶんから出ている。

- 対象 33 言語 / 検査した族 **92**
- **Tier1 (区別が存在理由なのに潰れている): 2 件**
- Tier2 (下位種の色が揃っている。設計判断でありうる): 42 件

### Tier1 — 対処を検討する 2 件

| 言語 | 族 | 実測 | 何が起きているか |
|---|---|---|---|
| `vimdoc` | `@markup.heading.1`〜`.4` | 全部 `ctermfg=142 bold` | **`:help` の見出し 4 階層が同一**。markdown と同じ穴が別 ft に残っている |
| `markdown` | `@markup.list.checked` / `.unchecked` | 両方 `ctermfg=208` | **`[ ]` と `[x]` が同じ色**。render-markdown の `RenderMarkdownChecked/Unchecked` はこの 2 つへの link なので上書きされない |

🚨 **4887d531 の射程**: 直したのは `@markup.heading.N.markdown` (言語サフィックス付き) で、
かつ **markdown バッファを開いたとき** (render-markdown の `config` が走る) だけ。
generic な `@markup.heading.N` は手つかずで、上表のとおり vimdoc は今も潰れている。

### Tier2 — 却下候補 (理由つき。次の監査が同じ指摘を再生成しないため)

| 族 | 言語数 | 却下の理由 |
|---|---|---|
| `@punctuation.bracket/delimiter/special` | 23 | 括弧・区切り・特殊記号を同色にするのは gruvbox の一貫した設計。区別すると本文がうるさくなる |
| `@string.escape/regexp/special` | 9 | どれも「文字列の中の特別なもの」で、`@string` 本体とは既に別色。下位種どうしの区別に実用上の要求が無い |
| `@keyword.conditional/exception/...` | 5 | 同上 (キーワードは 1 色が一般的) |
| `@variable.member/parameter` | 2 | 同上 |
| `@markup.link.*` / `@string.special` | 3 | 同上 |

**これらは「見つからなかった」のではなく「見つけたうえで対処しない」と決めたもの。**
方針を変えるときはこの表を更新する。

## 提案

1. **vimdoc の見出しに階層色を与える** — markdown と同じ形。ただし `:help` は既存の
   Vimscript syntax も絡むので、入れる前に実物で見る (`decide-layout-in-sample-renderer-first.md`)
2. **`[ ]` / `[x]` の色を分ける** — 同上。見本で合意してから
3. **検出手段を足すか** — 上の測り方はスクリプト化できる (族ごとに「兄弟が 1 種類の見た目に
   潰れているか」を見て、Tier2 を allowlist で除外する)

## 3 を今やらない理由と trigger

検査・ゲートの新設は `adversarial-review-own-safeguards.md` を丸ごと発動させる
(脅威モデルの明文化 / 段ごとの変異 / 異常系の実験 / opus 級の red team / CI 配線の確認)。
本件は **Tier1 が 2 件しか無く、どちらも 1 回直せば終わる**ので、いま検査を常設する
費用対効果が薄い。

- **trigger**: ①別の ft で同じ「階層が潰れている」が 3 件目として見つかったとき
  ②colorscheme を gruvbox 以外へ替えるとき (穴の位置がまるごと変わる)
- 検査を作るなら Tier2 の allowlist が要る。allowlist は
  `adversarial-review-own-safeguards.md` §8 の「迂回を直すたびに新しい迂回が出る」形に
  なりやすいので、脅威モデル (「うっかり潰れるのを拾う。設計判断の同色は対象外」) を
  先に書いてから着手する

## 残タスク

- [ ] Tier1 の 2 件について、見本で色を決めて適用するか判断する (未着手)
- [ ] 検出手段を常設するか判断する (上記 trigger 待ち。現時点では**やらない**)
- [x] 反証レビューを通した (read-only サブエージェント 1 体 / codex は不使用)

## 反証レビューの結果 (2026-09-12)

**P1 (採用・訂正済み)**: 「母集合は `ts.install` が入れる 33 言語」は二重に誤り。
`ts.install` は **31** 言語で、しかも Tier1 の片方である `vimdoc` は**そこに含まれない**
(nvim 同梱)。当初の書き方では、記載した方法論から Tier1 の vimdoc の所見が出てこない。
→ 上の「全数勘定」節で内訳を明示する形に訂正した。

**独立に確認が取れたもの** (レビュワーは別の方法で測った。実際に `:help` を開き
`vim.treesitter.highlighter.active` で treesitter が効いていることを確かめたうえで
ハイライトを読む形。こちらの query 走査とは経路が違うので、独立した確認として数える):

- Tier1 の 2 件はどちらも再現。`@markup.heading.1..4.vimdoc` が全部 `ctermfg=142 bold`、
  `@markup.list.checked/unchecked.markdown` が両方 `ctermfg=208`
- 「4887d531 の射程」(`.markdown` 付きだけ / markdown バッファを開いたときだけ) は
  `_nviminit.lua:970-1024` で確認
- `RenderMarkdownChecked/Unchecked` が capture への link であること
  (`render-markdown/core/colors.lua:53-54`)
- Tier2 の却下理由のうち「`@string` 本体とは既に別色」(lua で `@string`=142 / 下位種=203)
- 参照している commit hash とファイルパスの実在

🚨 **未検証のまま残るもの**: 「検査した族 92 / Tier2 42 件」の内訳は**こちらの 1 回の
計測しか根拠が無い**。レビュワーは再現を試みたが、計測スクリプトが `./tmp` (gitignore) で
既に無く、read-only の範囲では全言語の走査をやり直せなかった。**反証されたのではなく
確認が取れていない**。Tier1 の 2 件は上記のとおり独立に確認済みなので、この issue の
判断 (何を直すか / 検査を常設しないか) は Tier2 の正確な件数に依存しない

## 測定に使ったもの

計測スクリプトは `./tmp` (gitignore) にあり残らないので、**測り方だけ**上に残した。
再現するなら: rtp へ `nvim-treesitter/runtime` を足して `queries/*/highlights.scm` を舐め、
`@capture.<lang>` で `nvim_get_hl(link=false)` を引く。**canary は
「markdown の `@markup.heading` が潰れていると報告されないこと」**
(報告されたら 4887d531 の効いていない条件で測っている = 画面を測れていない。実際に
markdown バッファを開かずに測って 1 度踏んだ)。
