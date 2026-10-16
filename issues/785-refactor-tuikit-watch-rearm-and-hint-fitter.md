# 785 (refactor): 合図の待ち直しと最下行の案内の詰め方を tuikit に寄せる (mac-lan-manager / glogx / treefiler)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

起票日: 2026-10-16

## 概要

lan-manager の監査 (issue 747 の第 2 波。2026-10-16) で、module をまたいで同じ判断が写っているものが 2 つ見つかった。747 は lan-manager の中だけを
直すので、module をまたぐ共通化はここに切り出す。

> 反証レビュー (2026-10-16、sonnet) の訂正を下の「反証レビュー」節に書いた。本文の 2 の「lanmgr は優先度の表を持たない」は誤りだった。

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
- `src/mac-lan-manager/lanmgr/view.go` `Model.footer` (747 で `keyHint{key,label,prio}` の優先度つきにした。ただし尺度は逆 (大きいほど残す) で、落とし方も別: 最低を 1 つずつ消して描き直す)
- `src/treefiler/filer/model.go` (凡例 → 右の欄 → 案内の段で落とす。項目ごとの優先度は無く、パンくずと幅を分け合う)

glogx の利用者は status_view のほか doctor / issues / ratelimit / filer / lan の 10 か所。

## 対応方針

- tuikit に、合図の待ち直しの部品 (bubbletea の `tea.Cmd` を返す。tuikit で bubbletea を import してよいのは `caret` だけなので、置き場所を先に決める)
  と、優先度つきの案内の詰め方 (`hintItem{text, prio}` を仕様にする) を置き、3〜4 か所を寄せる
- 寄せる前に、各呼び出し元のコメントの 🚨 と前提 (glogx の tick との関係・treefiler の単体と glogx の板の違い) を表にし、部品がその全部を表せるかを確かめる
  (`survey-receiver-guards-before-passing-new-values.md` の「N 個の手組みを 1 つの道具に寄せるとき」)

## 関連ファイル

上の 7 か所。issue 747 (lan-manager)。

## 反証レビュー (2026-10-16、sonnet 1 体。P1 3 / P2 2 / P3 2)

- P1 (採用): lanmgr は優先度つき (尺度が逆・方式が別)。treefiler は段で落とす方式で、項目ごとの優先度式ではない。glogx の利用者の記載漏れ (doctor ほか)
- P2 (採用): 合図の待ち直しの 4 か所は「同じ判断」ではない。違うところは次の 4 つで、`Watch{cur}` のような部品にすると filerView と treefiler の前提を表せない。
  - 状態の持ち主 (自分で持つ / model の `WatchingChan` / 比べない treefiler)
  - 待ちの張り方 (Update の末尾で毎回 / キーの後 / 受信のときだけ)
  - ok=false で取り込むか (glogx は先に return、mac-lan-manager は Advance が走る)
  - tick との関係
- P3 (採用): tuikit は bubbletea を import できない (caret だけ例外。depguard)。tea.Cmd を返す部品は置けない
- 意見は「範囲を縮めてやる」。2 は glogx と lanmgr の 2 方式の統一だけにし、1 はやらない

## 決着

- 2 (やった): glogx の `fitHintItems` の実装を `tuikit/hintfit.Fit` に移し、glogx と lanmgr の footer が使う。treefiler は方式が違うので寄せない。
  - 前提が変わったので fitHintItems のコメントの「移していないのは意図的」を覆した。その注記は glogx の中だけに利用者がいたころのもの。
    今は別 module の lanmgr が同じ判断を別に持ち、同じ穴を踏み直していた
- 1 (やらない): 比べるのは「今のチャネルか」の 1 行で、上の 4 つの違いのため、部品にしても前提を引数で表すだけになる (複雑性は下がらない)。
  - 寄せない理由と「1 か所で穴を直したら残り 3 か所を grep で探す」は、`src/mac-lan-manager/main.go` の `waitChange` と `src/glogx/lan_view.go` の `rearm` のコメントに書いた

## 進捗

- [x] 反証レビュー
- [x] 1. 合図の待ち直し (やらない。理由はコードのコメント)
- [x] 2. 案内の詰め方 (commit「tuikit: 最下行の案内の詰め方を hintfit に移し…」)。変異で赤を確かめた: 席の予約を外す / 入らない項目で打ち切る / 最長の出口を返す →
  hintfit のテスト。lanmgr の q・s の優先度を下げる → TestFooterKeepsActionKeys。glogx の幅を無視する → TestEveryHintKeepsExitWithinWidth
- [x] opus の敵対レビュー (P1 なし):
  - glogx の出力は、移す前と後で比べて 1 文字も変わらない (ランダム 20 万ケース・21 面 × 幅 1〜200)
  - lanmgr の footer は 10,000 セルのうち 875 セルで項目が増え、項目が消えたセルは 0
  - 直したもの:
    - P2: hintfit が depguard の部品のリストに無かった (足した)
    - P3: 落とした assert を戻した。doc を足した。テストのコメントを貪欲の挙動に合わせた
  - 残したもの: spinner / gutter / foreground も depguard の部品のリストに無い (以前から)。リストと実際の package を突き合わせる検査は無い
