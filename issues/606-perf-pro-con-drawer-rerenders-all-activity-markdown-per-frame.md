# 606 (perf): pro-con の詳細 (引き出し) が、View のたびに活動の全件を整形し直す (実在する最大の session で約 1.8 ms、上限 1000 件で約 7.5 ms)

> 🚨 **担当中: dotfiles-05**（2026-10-02〜）

> 2026-10-02: ユーザーの依頼 (「書き出した issue を今対応して。codex に設計と実装レビューさせて」) で pending から戻して着手する。下の「再開の条件」は依頼前の判断として残す

起票日: 2026-10-02

## 再開の条件 (pending の理由)

**今の実データではフレームの枠 (33 ms) に対して小さいので着手しない** (591 の 2026-10-01 の決着と同じ判断を、件数を振って測り直した)。trigger:

- pro-con の詳細を開いているときに、操作や j/k のスクロールが重い・CPU が高いと報告されたとき
- 活動が数百件を超える PG の session が普通になったとき (`~/.claude/projects` の transcript の応答の数で見る。591 の時点の最大は応答 70 件)
- 607 の「件数を増やしたときの確保量の比」の検査を置くとき (その検査がこの経路で赤くなるので、同じ変更で直す)

出典: [605](605-research-tuikit-perf-breakthrough-2026-10-02.md) の実測 C。前の判断: 591 (done) の「決着」節

## 概要

`ui.(*Model).drawerPanel` (`src/pro-con/ui/drawer.go`) は毎回 `drawerBody()` を呼ぶ。`addActivity` (`src/pro-con/ui/activity.go`) は活動の全件
(上限 `live.activityKeep` = 1000) を `markdown.Render` に通し、さらに 1 行ずつ `drawerBody` の `add` (`termwidth.Wrap` と色の掛け直し) を通す。
画面に出るのはそのうちの `drawerBodyRows()` 行 (数十行) だけで、整形の結果はどこにも保持していない。

View は Update のたびに呼ばれる (bubbletea v2.0.8 の `eventLoop` が `model.Update` の直後に `p.render(model)`)。盤面のどこかに処理中のカードがあれば
spinner (`spinInterval` 100 ms、`anyProcessing`) で毎秒 10 回、本文の j/k の glide では毎コマ (`frameInterval` 33 ms) 走る。さらに tick (1 秒) ごとの
`fetchActivity` → `onActivity` が `drawerBody()` を 2〜3 回呼ぶ。

## 実測 (2026-10-02、この Mac、go1.26.0、master 715782de、`-count 3` の範囲)

盤面は `benchModel` (200 × 50)、R1 の詳細を開ききった状態 (`drawerDuration` 112 ms の後)。活動は応答 : 道具 = 1 : 2、
code block (go 3 行) を含む応答は 25 件に 1 件 (実データは直近 30 transcript の応答 1016 件中 40 件 = 4%):

| 活動 | View | B/op | allocs/op |
|---|---|---|---|
| 0 | 0.21〜0.22 ms | 380 KB | 1516 |
| 210 (応答 70 件 = 591 の時点の実在する最大) | 1.75〜1.91 ms | 1.8 MB | 14.3k |
| 1000 (上限) | 7.2〜8.0 ms | 6.9 MB | 62k |

- 🚨 応答の全件に code block を入れた入力では 1000 件で 27〜31 ms・24 MB になる (code block の字句解析 `highlight.Lang` が drawerBody の約 7 割)。
  code block の多い session ほど重くなるが、今の実データの割合では上の表の値
- 210 件で毎秒 10 回なら CPU 1 core の約 2%、1000 件で約 8%
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

## 対応方針 (着手するとき)

不変条件: **詳細を開いた View の時間と確保は、活動の件数に比例しない** (整形は活動が届いたときか幅が変わったときに 1 回だけ)。

- **保持するのは、字下げと `termwidth.Wrap` を済ませた最終の行** (活動 1 件ごと。鍵は 本文・幅・色の有無)。`markdown.Render` の出力だけを
  保持しても、後段の `Wrap` と色の掛け直しが 1000 件で約 2.7 ms 残る (反証レビューの profile: `drawerBody.func1` → `termwidth.Wrap` が drawerBody の 1 割)
- 本文全体は保持しない: 先頭の行の経過時間 (`SinceText(fmtDur(...))`。1 分までは秒単位、以降は分単位) と、`WorktreeLines` / `ProgressLines` /
  `DoingLines` が `snap.Now` を使う
- glogx の `src/glogx/issues/body.go` の `Body.Lines` (幅・色が同じならキャッシュを返す) は markdown の出力だけを保持する形で、上の理由で
  pro-con にはそのまま使えない。共通の型を tuikit に置くかは、着手時に「保持する単位が揃うか」で決める (揃わないなら pro-con に置き、理由をコードに残す)
- 守り: `TestDiffBoardViewAllocDoesNotGrowWithLines` (`src/pro-con/ui/diffview_perf_test.go`) と同じ形で「活動 30 件 → 1000 件で View の確保が 1.3 倍以内」。
  保持を外す変異で red になることを確かめる (ベンチと同じ罠: backend が `ActivityReader` を持たないと活動の節が出ず、件数で何も変わらない)

## 受け入れ条件 (着手するとき)

- [ ] 活動 1000 件の詳細を開いた View が、活動 0 件の View の 2 倍以内
- [ ] 上の守り (件数で確保が伸びない) と、その変異の red

## 関連ファイル

- `src/pro-con/ui/drawer.go` (`drawerPanel` / `drawerBody`)、`src/pro-con/ui/activity.go` (`addActivity` / `onActivity`)、`src/pro-con/ui/spinner.go`
- `src/pro-con/live/live.go` (`activityKeep`)
- `src/tuikit/markdown/render.go`、`src/tuikit/highlight/highlight.go` (`Lang` / `codeLine`)
- `src/glogx/issues/body.go` (`Body.Lines`)

## 進捗

- [x] 件数を振った実測と、591 の判断の再確認 (2026-10-02)
- [ ] trigger 待ち
