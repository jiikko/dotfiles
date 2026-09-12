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
潰れているかを見る。母集合は `_nviminit.lua` の `ts.install(...)` が入れる 33 言語。

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
- [ ] 本 issue は反証レビュー未通過 → 通してから状態を更新する

## 測定に使ったもの

計測スクリプトは `./tmp` (gitignore) にあり残らないので、**測り方だけ**上に残した。
再現するなら: rtp へ `nvim-treesitter/runtime` を足して `queries/*/highlights.scm` を舐め、
`@capture.<lang>` で `nvim_get_hl(link=false)` を引く。**canary は
「markdown の `@markup.heading` が潰れていると報告されないこと」**
(報告されたら 4887d531 の効いていない条件で測っている = 画面を測れていない。実際に
markdown バッファを開かずに測って 1 度踏んだ)。
