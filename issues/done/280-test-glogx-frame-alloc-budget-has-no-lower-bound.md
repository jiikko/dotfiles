# test: フレーム確保予算が上界だけを見ており、「何も描かないフレーム」が最良と判定される

起票日: 2026-09-06
カテゴリ: test
優先度: 高（描画が丸ごと死んだ状態を、確保ゲートが 8 ケース全部で緑にする）

## 何が起きているか

`frame_alloc_test.go:TestFrameAllocBudget` は各ケースで
`got > c.allocs` / `gotBytes > c.bytes` の **上界だけ**を見ている。
下界が無いので、**描画が何もしなくなった状態が最も good に見える**。

## 実測（変異検証。私が独立に再現済み）

`tui.go:browseModel.View` の 1 行を変異させた:

```go
-	v := tea.NewView(m.viewLines())
+	v := tea.NewView("")
```

- `go build ./...` = **BUILD_OK**（ビルド不能な変異の緑ではない）
- `git diff` を目視し、**意図した 1 行だけ**が変わったことを確認
- baseline が green であることを先に確認

結果:

```
list:         0 allocs/frame (上限 138) / 0 B/frame (上限 31700)
list-ja:      0 allocs/frame (上限 138) / 0 B/frame (上限 32500)
status-40:    0 allocs/frame (上限 322) / 0 B/frame (上限 44400)
diff-overlay: 0 allocs/frame (上限 217) / 0 B/frame (上限 49100)
job-panel:    0 allocs/frame (上限 162) / 0 B/frame (上限 37500)
issues-40:    0 allocs/frame (上限 213) / 0 B/frame (上限 34900)
usage-glance: 0 allocs/frame (上限 180) / 0 B/frame (上限 36700)
toast-holding: 0 allocs/frame (上限 186) / 0 B/frame (上限 38000)
--- PASS: TestFrameAllocBudget
```

**8 ケース全部が 0/0 を出して PASS。**

## 同じファイルが既に片側の規律を持っている

`frame_alloc_test.go:frameAllocBytes` は `r.N < minIters` を **Fatal** にする
（= 測れなかったときに緑を返さない）。欠けているのは
**「何も描かなかったときに緑を返さない」側だけ**。

これは `issues/done/072` が `tests/nvim/folds_timer_check.lua` で確定させ
「下界を追加」で直した形と同型。

## 発火条件

- 描画経路が壊れて出力が空・極端に短くなる退行が入ったとき、このゲートは**赤くならない**
  （むしろ数字が良くなる）
- **silent に壊れる**: 他のテスト（View の内容を見るもの）が拾う可能性はあるが、
  **確保ゲート自身は退行を観測できない**。上の変異では 8 ケース全部が緑

## 推奨対応

ケースごとに**下界**を足す:

- 確保回数の下限（実測値の 1/2 程度など、桁が変わったら落ちる水準）
- または `View().Content` の行数・非空を assert する（より直接的）

🚨 これは issue 269（bench の**時間**予算が緩い / `rel` の丸め）とは別物。
あちらは上限値の緩さ、こちらは**下限の不在**。

### 副次: CI 側にも同じ形がある

`tests/check_bench_budgets.sh` は ms が数値でない場合（計測失敗）を fail にする一方、
**0 近傍の値は素通しする**。同じ「何も測れていないのに緑」の形。

## 反証の試み

`frame_alloc_test.go` の長い doc（上限の実測値・`-race` の揺れ・issue 047/048/051/062 の経緯）/
`tests/glogx/bench_budgets.ci` / `issues/done/` の 047・051・062 を読んだが、
**下界を意図的に置かない旨の記述は無い**。

## 関連

- `issues/done/072`（同型を「下界を追加」で直した前例）
- 269（bench の時間予算。別問題）
