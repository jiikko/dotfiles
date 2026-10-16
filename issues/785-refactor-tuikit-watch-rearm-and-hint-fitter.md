# 785 (refactor): 合図の待ち直しと最下行の案内の詰め方を tuikit に寄せる (mac-lan-manager / glogx / treefiler)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

起票日: 2026-10-16

## 概要

lan-manager の監査 (issue 747 の第 2 波。2026-10-16) で、module をまたいで同じ判断が写っているものが 2 つ見つかった。747 は lan-manager の中だけを
直すので、module をまたぐ共通化はここに切り出す。

> 反証レビューはまだ通していない (起票の時点では「反証されていない仕様」)。数と場所は 2026-10-16 に grep で数え直した。

## 詳細

### 1. 合図のチャネルを待ち、今のチャネルからの合図のときだけ待ち直す (4 か所)

`_, ok := <-ch` で合図を待ち、受けたら「今待っているチャネルからか」を確かめて待ち直す形が 4 か所にある (2026-10-16 に
`grep -n '_, ok := <-ch'` で数えた。母集合は 4 module の非テストの Go):

- `src/mac-lan-manager/main.go` (`waitChange` / `app.watch`)
- `src/glogx/lan_view.go` (`waitLanChange` / `lanView.rearm` / `watchCmd`)
- `src/glogx/filer_view.go` (`waitFilerChange` / `filerView.rearm`)
- `src/treefiler/main.go`

747 では、この形の穴 (走査が替わった後に古いチャネルを待ち続ける・待ちが 2 本になる) を片方ずつ直した履歴がある (段 5 の敵対的レビューの P2)。
次に同じ穴が見つかったとき、4 か所を手で揃えることになる。

### 2. 最下行の案内を幅で落とすが、抜ける手段は残す (3 実装)

- `src/glogx/status_view.go` `fitHintItems` (`hintItem{text, prio}` で優先度つき)
- `src/mac-lan-manager/lanmgr/view.go` `Model.footer` (747 で `q` と `?` を残す形にした。優先度の表は持たない)
- `src/treefiler/filer/model.go` (凡例から落とし、`q` を残す)

glogx の `fitHintItems` だけが優先度を持つ。lanmgr は 747 の監査で「`?` が真っ先に消える」が出た (直した)。

## 対応方針

- tuikit に、合図の待ち直しの部品 (bubbletea の `tea.Cmd` を返す。tuikit で bubbletea を import してよいのは `caret` だけなので、置き場所を先に決める)
  と、優先度つきの案内の詰め方 (`hintItem{text, prio}` を仕様にする) を置き、3〜4 か所を寄せる
- 寄せる前に、各呼び出し元のコメントの 🚨 と前提 (glogx の tick との関係・treefiler の単体と glogx の板の違い) を表にし、部品がその全部を表せるかを確かめる
  (`survey-receiver-guards-before-passing-new-values.md` の「N 個の手組みを 1 つの道具に寄せるとき」)

## 関連ファイル

上の 7 か所。issue 747 (lan-manager)。

## 進捗

- [ ] 反証レビュー
- [ ] 1. 合図の待ち直し
- [ ] 2. 案内の詰め方
