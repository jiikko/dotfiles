# glogx: 1 フレームの確保バイトが出力の 5〜6.5 倍 (枠組み立てが中間文字列を捨て続けている)

起票日: 2026-08-14
種別: perf (実測駆動)
優先度: **P2** (フレームごとの GC 圧。046 の次)

## 観測した事実

1 フレームの**出力バイト数**と、そのフレームで**確保したバイト数** (`MemStats.TotalAlloc` の差分 /
200 フレーム) の比:

| 画面 | 出力 | 確保/frame | 増幅 | allocs/frame |
|---|---|---|---|---|
| 一覧 | 8028 B | 40219 B | **5.01x** | 184 |
| status viewer | 8570 B | 55469 B | **6.47x** | 467 |
| diff popup | 9581 B | 58512 B | **6.11x** | 260 |

確保の内訳 (`BenchmarkViewSteady` の alloc_space。ベンチ別に採り直したもの):

| 関数 | flat | 備考 |
|---|---|---|
| `buildPanelBoxImpl` | 24.1% | 最外周フレーム |
| `strings.(*Builder).grow` | 24.1% | 呼び出し元は 100% `Builder.Grow` |
| `wrapWindowFrame` | 20.5% | cum 50.4% |
| `scrollbarColumn` | 13.1% | |
| `browseModel.viewLines` | 5.5% (cum 99.3%) | |

`CursorMoveView` でもほぼ同じ内訳 (`wrapWindowFrame` 22.6% / `buildPanelBoxImpl` 22.6% /
`grow` 21.6% / `scrollbarColumn` 14.2%) なので、**画面によらずフレーム組み立てが本体**。

CPU 側にも波及している: `ViewSteady` の CPU サンプル 4.0s のうち `runtime.kevent` が 1.68s
(42%) を占め、その経路は `runtime.gcStart.func4 → startTheWorldWithSema → netpoll` =
**GC の start-the-world**。つまり 40KB/frame の churn が GC の回転数として跳ね返っている
(`num_gc` は soak の scroll 連打 30 秒で 10 → 31 に増えた)。

## 何を疑うか (まだ原因は確定していない)

`Builder.grow` の呼び出し元が 100% `Builder.Grow` である = **サイズ指定は既にしてある**。
したがって「Grow を足す」類の修正ではない。疑うのは次の 3 つで、**着手前にどれが効くかを
実測で切り分ける**こと:

1. `Grow` の見積もりが過大 (必要量の数倍を確保している)
2. 同じ内容を段階ごとに作り直している (行 → 窓 → 枠 → 最終結合で中間文字列を毎段捨てる)
3. `scrollbarColumn` が毎フレーム列全体を作っている (可視行数ぶんで足りるのに)

## 未確認 (推測として明記)

- 上記 3 つのどれが支配的かは**まだ切り分けていない**。増幅率と内訳が分かっているだけ
- 「5x が過大」という判断自体は相対評価。TUI の段階組み立てである程度の増幅は避けられない。
  **どこまで下げられるかは実測で決める** (目標値を先に決めて追わない)
- 046 (dispWidth) を先に入れると CPU の内訳が変わるため、**本 issue の再測定は 046 の後に行う**

## 検証条件

- `testing.AllocsPerRun` で allocs/frame の減少を assert するテストを足す
  (ns/op だけだとノイズに埋もれる)
- 出力の**バイト一致**を旧実装と比較するテスト (見た目を変えないことの機械的な保証)。
  枠・スクロールバー・overlay 合成は目視では差分に気づきにくい
- 変異検証: 確保を減らす変更で出力が 1 バイトでも変わったら red になること

## 測定ログ

- `tmp/glogx-perf/perbench/{ViewSteady,CursorMoveView,StatusViewFrame,ViewWithDiff}_mem.prof`
- 増幅率: `tmp/glogx-perf/frame_amplification.txt`


---

# 対応の記録 (2026-08-14)

## 切り分けの結果: 原因は「1 行を 1 フレームで 4 回コピーしている」こと

起票時に挙げた 3 つの疑いのうち **(2) 同じ内容を段階ごとに作り直している** が当たりだった。
`Builder.grow` の呼び出し元が 100% `Builder.Grow` = サイズ指定は既に効いていて、
`grow` の 8.8 KB/frame は**出力そのもの** (8,028 バイト) なので削れない。削れるのは
段階ごとのコピーで、1 行が次の 4 段でそれぞれ 1 部作られていた:

| 段 | per-frame | 削れるか |
|---|---|---|
| `scrollbarColumn` の行連結 | 5.2 KB | 融合が要る (今回は触らない) |
| `buildPanelBoxImpl` の行連結 | 9.0 KB | 枠を足す本体なので必要 |
| `wrapWindowFrame` の `" " + l` | **8.0〜8.2 KB** | **丸ごと無駄** (空白 1 桁を足すために全行を作り直す) |
| 最終 join の `Builder` | 8.8 KB | 出力そのもの |

## 直したこと

1. **最外周の左余白を組み立てへ織り込んだ**。`panelBoxStyle` に `indent` を足し、
   `buildPanelBoxImpl` が 4 種の行 (上辺 / content / 下辺 / 下端の影) すべてで
   `pre := padSpaces(st.indent)` を連結に含める。既にある連結の一部になるので追加確保が要らない
2. **カーソル行の背景再適用を正規表現から手書き走査へ**。`bgLine` (tui.go) と
   `statusCursorPaint` (status_view.go) に**重複していた** `ansiResetRe.ReplaceAllString` を
   `render.go` の `reapplyAfterReset` へ一本化した (毎フレーム 1.1〜1.2 KB の確保を落とし、
   同時に重複も解消)
3. `strings.Repeat(" ", n)` を既存の無確保ヘルパ `padSpaces(n)` へ置換 (box.go 2 / tui.go 1)

## 再測定 (min of 4、`-benchtime=4000x`、046 適用後を基準)

| bench | B/op | allocs/op | ns/op |
|---|---|---|---|
| ViewSteady | 40,144 → **30,745** (−23.4%) | 178 → **135** | 28,670 → 25,792 (−10%) |
| ViewSteadyJA | 40,868 → **31,464** (−23.0%) | 178 → **135** | 41,609 → 38,684 (−7%) |
| ViewWithPanel | 46,790 → **36,424** (−22.2%) | 199 → **156** | — |
| StatusViewFrame | 54,228 → **43,755** (−19.3%) | 359 → **316** | 38,036 → 32,587 (−14%) |
| StatusViewFrame2000 | 105,100 → **92,502** (−12.0%) | 364 → **321** | — |
| CursorMoveView | 80,500 → **61,488** (−23.6%) | 356 → **270** | 60,720 → 52,187 (−14%) |
| ViewWithDiff | 59,029 → **47,470** (−19.6%) | 254 → **211** | 38,992 → 33,383 (−14%) |
| Calibrate (較正器) | — | — | 71,139 → 69,544 |

allocs が全ベンチで **−43** (2 フレーム測る CursorMoveView は −86) = 枠の 43 行ぶんの
作り直しが消えた分。増幅率は **5.00x → 3.83x** (出力 8,028 B に対して)。

## 見た目を変えていないことの証拠

TUI の見た目は目視できないので、**フレームのバイト列の完全一致**を代理にした。
幅 5 種 × 高さ 2 種 × 日本語/ASCII × (一覧 / job パネル / diff オーバーレイ / カーソル移動後)
\+ status viewer 4 件数 (0/1/40/2000) + 色なしモード = 計 842,363 バイトを旧実装と `cmp` して
**完全一致**。R2 が独立に別 worktree で同じ一致を再現した。

## 検証

- 差分 fuzz 800 万実行で `reapplyAfterReset` が旧 regexp 実装と不一致 0 件
- `go test -race ./...` green / lint 0 issues
- `TestFrameAllocBudget` でフレームの確保回数に上限ゲートを置いた (list 135/150・status 316/340)

## 変異検証 (6 種すべて red)

| 変異 | 結果 |
|---|---|
| `indent` を 0 にする | ✅ red (左余白が消える) |
| content 行の `pre` を落とす | ✅ red |
| 下辺の `pre` を落とす | ✅ red |
| 短縮形 `ESC[m` を拾わない | ✅ red |
| `bg` を足さない | ✅ red |
| 余白を行の作り直しへ戻す | ✅ red (確保 173 > 上限 150) |

## fuzz が見つけたテスト側のバグ 2 件 (production ではない)

参照実装 (旧 regexp) は `bg` を置換テンプレートへ埋める形なので、`bg` 自体が
テンプレートとして解釈される:

- `bg="0"` → `$00` という別の変数参照になり `""` が返る
- `bg="$0"` → `bg` 内の `$0` がマッチ全体に展開される

production の `bg` は固定 SGR 定数 (`ansiCursorBg = "\x1b[48;5;24m"`) なので `$` も
英数字始まりも含まず**実運用では起こらない**。参照実装側を `${0}` + `$` のエスケープに直し、
2 例を fuzz の種として固定した。

## 敵対的レビュー

- **R2 (回帰と副作用)**: 指摘なし。別 worktree で旧実装と交互に測り、benchstat で
  無関係ベンチ (`Calibrate` / `HighlightDiff` / `RenderLinesLargePatch` / `ModelInit200`) が
  有意差なし、影響ベンチが 5.6〜9.9% 改善を確認。`reapplyAfterReset` は
  「リセット 100 個」「リセット無しの 2KB 行」「ESC はあるがリセット無し」の
  いずれの敵対パターンでも旧 regexp より速く確保も少ないことを実測。
  `strings.Index` の繰り返しが O(n^2) にならないことも 1k/10k/100k で線形を確認。
  `padSpaces(n) == strings.Repeat(" ", n)` を n=-1..300 で総当たり確認
- **R1 (正しさ)**: 本体の 2 主張は**反証できず**。出力のバイト一致は termW を −5〜300 の
  22 種 (`minPanelWidth` のクランプ境界と `padSpaces` の 256 桁テーブル境界を跨ぐ) ×
  colored × content 10 種で 2.28 MB 分一致、差分 fuzz 806 万実行で失敗なし。
  不正 UTF-8 中の 0x1b (`\xff\x1b[m` 等) でも一致 (Go のデコーダが不正バイトを
  1 バイトしか消費するため 0x1b が隠れない)。

  ただし **新規テストの false green を 1 件検出** (P2):
  - 旧 `TestWrapWindowFrameIndentKeepsGeometry` は「行頭が空白か」で見ていたため、
    **下端の影行**の `pre` を落としても green だった。`shadowBottomOffset = 2` で行頭が
    空白のままになり prefix 判定を素通りする + 2 桁チェックがその行を除外していたため。
    出力は 880 行ぶん変わるのに**リポジトリ全体のテストが green** (自分でも再現確認済み)
  - **反映**: 「indent=N の結果 == indent=0 の結果の全行に N 桁の空白を付けたもの」の
    完全一致 (`TestPanelBoxIndentIsPureLeftPad`) へ書き換え。幅 9 種 × content 4 種 ×
    colored × indent 4 種で 4 行種すべてを同時に守る。影行・上辺の変異が両方 red になることを確認

  P3 2 件も反映:
  - `i += 2` の刻み幅が load-bearing。「CSI の終端まで飛ばす」という自然に見える最適化
    (`isANSITerminator` が同じファイルにあり誘っている) に変えると `"\x1b[\x1b[m"` で
    リセットを取りこぼすが、表 22 件では 1 件も捕まえていなかった → 入れ子 ESC を 3 件追加し
    その変異で red になることを確認
  - doc の「43 行ぶん」が誤読を招く (43 は indent 38 + regexp 撤去 5 の合計) → 内訳を明記
- **R3 (検証の検証)**: **P1 を 2 件検出。反映済み**

  1. **予算テストの上限が緩すぎて退行を通していた**。当初 list を 150 (実測 135) に
     していたため、(a) 枠 38 行のうち **15 行を旧形に戻しても** green (削減の 35%)、
     (b) **regexp 撤去だけを revert しても** green (140 ≤ 150) だった。
     → 上限を **-race 側の実測のすぐ上**へ締めた (list 138 / status-40 322 /
     diff-overlay 217 / job-panel 162)。締めた直後に自分の緩さが露呈して job-panel が
     -race で落ちたので、`-race -count=10` で分布を採り直して設定した (doc に実測値を明記)
  2. **予算テストの fixture が `buildShadowPanelBox` 経路を 1 つも描いていなかった**。
     「返り値を全行作り直す」純粋な perf 退行 (出力は同一) が完全に不可視だった。
     → diff overlay と job パネルの fixture を追加

  P2/P3 も反映:
  - `TestPanelBoxIndentIsPureLeftPad` は相対比較だけだったため「両辺に同じ定数を足す」変異
    (`padSpaces(st.indent + 1)`) がキャンセルして素通りした → indent=0 のときの**絶対**の姿
    (色なしの上辺は罫線の角で始まる) を 1 点固定して基準を釘付けした
  - `width: 258` は `padSpaces` の n>256 fallback に届いていなかった (到達する最小幅は **265**)
    → 265 / 300 を追加し、`padSpaces` の直接テスト (n=-3..300 で `strings.Repeat` と同値 +
    n<=256 で無確保) を新設した。それまでリポジトリに `padSpaces` の直接テストは 0 本だった
  - `ansiCursorBg` (production が実際に渡す唯一の値) が bg リストと fuzz seed に無かった → 追加
  - `TestWrapWindowFrameGeometry` の `HasPrefix(" ")` は R1 が false green の原因と特定したのと
    同じ判定形だったため、行頭空白を**ちょうど何桁か**数える形へ変えた

  R3 が崩せなかった点: overlay 11 状態を横断した **7.1 MB のバイト列ダンプが旧実装と完全一致**
  (ダンプ自体に検出力があることも確認済み)。「1 行が 4 回コピーされる」は構造的に裏が取れた
  (旧 `" "+l` が触っていた行数はちょうど 38 = pageSize 35 + 枠 4 - 上余白 1 で、削減 38 allocs と
  一致。旧 178 = 4x38+26 / 新 135 = 3x38+21 で差の 5 が regexp 分)。新実装が旧より遅い入力は
  見つからず (ESC 皆無 0 alloc vs 旧 3 / リセットあり 1 vs 旧 4)

  **未確認リスク**: 上限の実測は darwin/arm64・GOMAXPROCS=14 のみ。CI (Linux) の -race の
  水増し量が違えば list の余裕 3 に食い込む可能性がある (超えたら実測値を書き足して上げる)

## やらなかったこと

- `scrollbarColumn` と `buildPanelBoxImpl` の融合 (5.2 KB/frame)。段の融合は
  レイヤーの結合を強めるので、実需要 (フレーム予算が足りない状況) が出るまで凍結する
- 最終 join の `Builder` (8.8 KB/frame) は出力そのものなので対象外
