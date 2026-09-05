# perf: doctor の hint 行が、選択が空でも毎フレーム全 Item を舐めて文字列を確保する

起票日: 2026-09-05
カテゴリ: perf
優先度: 中（doctor のフレームで最も件数比例が強い経路。issue 270 の修正では減らない）

## 何が起きているか

最下行の hint を組む経路が、**選択されているものが 1 つも無くても**、毎フレーム
disk タブの全エントリの全 Item を走査し、**Item ごとに文字列を 1 本確保する**。

```
tui.go:hintLine  →（毎フレーム）
  doctor_view.go:hint
    → doctor_delete.go:selectionSummary
      → doctor_delete.go:selectedResults
        → diskItemKey(r.Entry.ID, it.Path)   ← Item ごとに文字列連結 = 確保 1 本
```

`hint` の書き方は

```go
if n, total := v.selectionSummary(); n > 0 && v.tab == tabDisk {
```

で、**呼び出しが条件の前**にある。`v.tab != tabDisk` でも、選択が空でも、全走査が走る。

`selectedResults` は `v.selectedItems[diskItemKey(r.Entry.ID, it.Path)]` を引くために
キーを組む必要があり、そのキーは map 参照のためだけに作られて即捨てられる。

## 実測

環境: darwin/arm64, Apple M3 Max。合成 32 エントリ × 200 items（総 6,400 items）の
doctor フレームで、`detail` / `copyText` を無効化した状態（= issue 270 の修正を当てた後を模す）の
`-memprofile` を `alloc_objects` で見た:

```
   2031709 88.93%  glogx.diskItemKey
-peek: glogx.(*doctorView).selectionSummary → selectedResults → diskItemKey (inline) 100%
```

**270 の修正を入れた後の確保の 88.9% がここ。** さらに `copyPath` も外した変異でも
allocs は 7,082 → 7,016 でほとんど動かず、**件数比例はそのまま残る**。

🚨 **実機の規模では小さい**。著者の実機 snapshot（2026-09-04 走査）は総 Items **29 件**なので、
この経路の実コストは無視できる。**本 issue は「Items が増えたときに最初に効いてくる形」**
として起票するもので、今すぐ体感に出る話ではない（規模の主張は issue 270 と同じ注意）。

## 発火条件

- doctor を開いている（`d`）こと。**タブは問わない**（`v.tab` の判定より前に呼ばれる）
- 走査中・削除中は 12.5fps、走査後もスクロール打鍵ごと
- **選択の有無に関わらず**発火する
- **silent に壊れる**: 機能は正しい。確保が増えるだけ

## 推奨対応

`selectionSummary`（または `selectedResults`）の先頭で、
**`len(v.selected) == 0 && len(v.selectedItems) == 0` なら即 return** する。
これだけで「選択していないとき」= ほとんどの時間のコストが 0 になる。

呼び出し側で `v.tab == tabDisk` を先に見る形へ直すのも併せて有効だが、
**本体側で閉じる方が確実**（次に別の場所から呼ぶ人が同じ書き忘れをしない）。

選択がある場合の走査は必要な仕事なので残してよい（選択件数に比例するのは正当）。

## 検証の作法（修正時）

- 早期 return を入れた前後で、`-memprofile` の `diskItemKey` の `alloc_objects` が
  消えることを確認する
- 🚨 変異検証: 早期 return を外す変異を当てて red になるテストを置くこと。
  「選択が空のとき `selectedResults` が 0 回しか走査しない」を数える形が素直
  （`~/.claude/rules/mutation-verify-new-tests.md`）

## 由来

issue 270 の敵対的レビュー（2026-09-05）が、270 の「残余は `diskItemRows` など別経路」という
記述を反証する過程で見つけた。`diskItemRows` は `detail: nil` にすると到達不能なので、
270 の初版の帰属は誤りだった（270 側も訂正済み）。

## 関連

- 270（doctor が畳まれた行の detail / copyText / copyPath を毎フレーム構築する件。
  **この修正ではこちらは減らない**）
