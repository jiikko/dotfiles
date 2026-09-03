# 031 refactor: issues 一覧の半ページ glide を削除する (実質アニメしない機構)

issues viewer の**一覧**に付けた半ページスクロールの glide は、幾何的に視覚効果が出ない。
使われないアニメ機構が残っていると「アニメするつもりで書いたのに効かない」状態が次の読者を
迷わせるので削除する。挙動は変わらない (今も瞬時に飛んでいる)。

方針は A(現状維持) / B(削除) / C(カーソル中央寄せでアニメを効かせる) の 3 案から **B をユーザーが選択**
(2026-07-31)。**スクロールのカーブは ease-in のまま変えない** (同日の決定)。
codex レビュー未通過 (codex 不使用の運用指示による)。

## なぜアニメしないのか (実測)

一覧の半ページ移動は cursor と窓を同時に動かし、`windowOffset` が「カーソルを含む**最小**の窓」を
導出するため、カーソルは必ず新しい窓の端に来る。窓を 1 行でも遅らせるとカーソルが画面から出るので、
遅らせる余地がゼロ。

```
下スクロール: 移動前 offset=0 → 着地点 9 / cursor=26 (rows=18)
  frame 0: 描画 offset=9 (glide の生の値=0)   ← 最初から着地点に張り付く
  frame 1: 描画 offset=9 (生の値=0)
```

カーソルが窓の途中にあるケースも測ったが、そのときは**窓自体が動かない** (`prev=0 target=0`) ので
やはりアニメは起きない。つまり「窓が動くのはカーソルが端に達したときだけ = 遅らせられないとき
だけ」で、条件が排他になっている。

glide の途中位置をそのまま使うと、アニメ中 (~200ms) だけカーソル行が 1 本も描かれない状態になる
(= 見えない行が Enter・v・y の対象になる)。これは実害があるので `issues_view.go` の `listLines` で
「カーソルを含む範囲へ寄せる」clamp を入れて塞いだ (2026-07-31 の敵対的レビュー P2)。B を入れると
**この clamp も不要になる** — glide が無ければ窓は `windowOffset` の導出値そのままで、カーソルは
定義上必ず含まれる。

## やること

`issues_view.go`:

- `issuesView.listGlide` フィールドを削除
- `refresh` / `close` の `listGlide.stop()` を削除
- `handleKey` の半ページ 2 箇所 (`ctrl+d`/`pgdown`/`Space`/`f` と `ctrl+u`/`pgup`/`b`/`shift+space`) の
  `listGlide.start(prev, v.offset)` を削除 (`prev` の捕捉も不要になる)
- `advanceGlide` の list 側と `animating()` の list 判定を削除
- `listLines` の描画を 1 行へ:

```go
// 現在: glide の途中位置 → カーソルを含む範囲へ寄せる → 範囲内へ clamp
offset := clampScrollOffset(v.offset, len(v.rows), rows)
```

`tui.go`:

- `WindowSizeMsg` の `m.issuesOv.listGlide.stop()` を削除

`scroll_glide_test.go`:

- `listGlide` を参照する 4 箇所を整理。🚨 **「一覧の glide 中もカーソル行が描かれる」テストは
  消さずに残す** — glide が無くなっても「一覧の窓は必ずカーソルを含む」という不変条件は生き続ける
  ので、glide への依存だけ外して不変条件のテストとして残す

**本文 pager (`bodyGlide`) と diff pager・コミット一覧の glide は残す**。あちらはカーソルを持たない
(または cursor が動かない) ので、遅らせる余地があり実際にアニメする。共有型 `scrollGlide`
(`scroll_glide.go`) もそのまま。

## 着手の順序 (重要)

**並行セッションが `clampScrollOffset` を新設するリファクタを進めている間は着手しない**
(2026-07-31 時点で `issues_view.go` / `tui.go` / `scroll_glide.go` が未コミットで dirty)。B が消す
描画行は、そのリファクタが書き換えている行と同一。

```diff
- offset := max(min(v.bodyGlide.offset(v.bodyOff), max(len(lines)-rows, 0)), 0)
+ offset := clampScrollOffset(v.bodyGlide.offset(v.bodyOff), len(lines), rows)
```

**そのコミットが入ってから着手する方が差分も小さい** — B は `clampScrollOffset` の呼び出し箇所を
1 つ減らす方向の変更になる。

## 完了条件

- `listGlide` の参照が production から消えている
- 一覧の半ページ移動の挙動は変わらない (今も瞬時)
- 「一覧の窓は必ずカーソルを含む」不変条件のテストが残っている
- `go test ./...` green / `make lint` 0 issues
