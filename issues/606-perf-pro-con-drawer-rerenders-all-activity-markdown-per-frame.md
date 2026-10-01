# 606 (perf): pro-con の詳細 (引き出し) が、View のたびに活動の全件を markdown で整形し直す (1000 件で 1 回 約 28 ms)

起票日: 2026-10-02

出典: [605](605-research-tuikit-perf-breakthrough-2026-10-02.md) の実測 C

## 概要

`ui.(*Model).drawerPanel` (`src/pro-con/ui/drawer.go`) は毎回 `drawerBody()` を呼び、`addActivity` (`src/pro-con/ui/activity.go`) が
活動の全件 (上限 `live.activityKeep` = 1000) を `markdown.Render` に通す。画面に出るのはその中の `drawerBodyRows()` 行 (数十行) だけ。
整形の結果はどこにも保持していないので、**View のたびに全件を整形し直す**。

View は Update のたびに呼ばれる (bubbletea v2.0.8 の `eventLoop` が `model.Update` の直後に `p.render(model)`)。処理中のカードを開いていれば
spinner (`spinInterval` 100 ms) だけで毎秒 10 回、本文の j/k の glide では毎コマ (`frameInterval` 33 ms) 走る。

## 実測 (2026-10-02、この Mac、go1.26.0、master 715782de、`-count 3` の範囲)

盤面は `benchModel` (200 × 50)、R1 の詳細を開ききった状態。活動は 3 件に 1 件が markdown (見出し・強調・箇条書き・go の code block 3 行)、残りは道具の 1 行:

| 活動 | 本文の行 | View | B/op | allocs/op |
|---|---|---|---|---|
| 0 | 9 | 237〜241 µs | 380 KB | 1516 |
| 30 | 128 | 1.15〜1.21 ms | 1.16 MB | 10.6k |
| 300 | 1208 | 8.5〜9.3 ms | 7.5 MB | 90.8k |
| 1000 | 4014 | **27〜31 ms** | **24 MB** | **300k** |

- CPU (1000 件): `markdown.RenderLinks` 44% (そのうち `highlight.Lang` → chroma の字句解析 33%、`renderMarkdown` 9.5%)、残りは GC
- 1000 件で毎秒 10 回なら、詳細を開いているだけで CPU 1 core の約 3 割と毎秒 240 MB の確保。glide はフレームの枠 (33 ms) をほぼ使い切る
- 使い捨てのベンチ (commit していない。`activity_test.go` の `activitySpy` を使う):

```go
m, sp, clk := benchModel()
m.be = &activitySpy{spy: sp, items: map[string][]backend.Activity{}} // spy は ActivityReader を持たないので、そのままだと活動の節が出ない
m.selected = "R1"
m.Update(frameMsg{})
m.openDrawer()
clk.t = clk.t.Add(2 * time.Second) // 開ききった後
m.act = activityView{card: m.drawerCard, items: items} // items: n 件
// 前提: len(m.drawerBody()) が件数に応じて増えること (増えないなら活動の節が描かれていない)
for b.Loop() { _ = m.View() }
```

## 対応方針

不変条件: **詳細を開いた View の時間と確保は、活動の件数に比例しない** (整形は活動が届いたときか幅が変わったときに 1 回だけ)。

- 整形の結果を活動 1 件ごとに保持する (鍵は 本文・幅・色の有無)。本文全体の保持にしない: `drawerBody` の先頭の行は
  経過時間 (`SinceText(fmtDur(m.snap.Now.Sub(c.Since)))`) を含み、毎秒変わる
- 同じ形の保持が glogx に既にある (`src/glogx/issues/body.go` の `Body.Lines`: 前回と同じ幅・色ならキャッシュを返す)。
  pro-con に 2 つ目を書く前に、**`tuikit/markdown` に「本文 1 つ分の整形を保持する型」を置いて glogx と pro-con の両方がそれを使う**形を検討する
  (2 実装にしない)。tuikit の層の約束 (フレームワーク非依存・純粋な層。`src/tuikit/README.md`) に反しないかを先に読む
- 守り: pro-con の `TestDiffBoardViewAllocDoesNotGrowWithLines` と同じ形で「活動 30 件 → 1000 件で View の確保が 1.3 倍以内」を置く。
  保持を外す変異で red になることを確かめる (このベンチの前提の罠: backend が `ActivityReader` を持たないと活動の節が出ず、件数で何も変わらない)
- `onActivity` も 1 回の取り込みで `drawerBody()` を 2〜3 回呼ぶ (位置の補正のため)。保持があれば同じ修正で軽くなる

## 受け入れ条件

- [ ] 活動 1000 件の詳細を開いた View が、活動 0 件の View の 2 倍以内 (今は約 120 倍)
- [ ] 上の守り (件数で確保が伸びない) と、その変異の red
- [ ] glogx と pro-con の保持が 1 つの型に寄っている (寄せないなら理由をコードに残す)

## 関連ファイル

- `src/pro-con/ui/drawer.go` (`drawerPanel` / `drawerBody`)、`src/pro-con/ui/activity.go` (`addActivity` / `onActivity`)
- `src/pro-con/live/live.go` (`activityKeep`)
- `src/tuikit/markdown/render.go`、`src/tuikit/highlight/highlight.go` (`Lang` / `codeLine`)
- `src/glogx/issues/body.go` (`Body.Lines`)

## 進捗

- [ ] 未着手
