# test: hint 幅ゲートが issue 201 の設計（走査型）でなく 3 サイトの列挙表で、detailOv 節は恒真

起票日: 2026-09-06
カテゴリ: test
優先度: 中（201 が「列挙表は持たない」と理由まで書いた設計が、実装では列挙表になっている）

## 何が起きているか

### (1) 201 の設計と実装が食い違う

`issues/done/201` は hint 幅ゲートの機構として **AST 走査**を明示的に指定し、理由まで書いている:

> **列挙表は持たない**（117 の「列挙すると兄弟を足したときに追随を忘れる = この検査が
> 守りたい事故を検査自身が踏む」を適用）。走査 0 件は fail

201 の完了ログは `[x] 候補 1: 走査型の検査を足し…` と記録しているが、**実在するのは
doctor / job パネル / detailOv の 3 件を `t.Run` で並べた列挙表**
（`hint_width_test.go:TestHintsFitPopupWidth`）。hint を対象にした go/ast 走査も
ソース走査も repo に存在しない（grep 済み）。

結果、issue 279 の 3 本（基底一覧 / rlDash / prStatusOv）は**検査対象に入っていない**。

### (2) detailOv サブテストは恒真

```go
t.Run("detailOv の項目", func(t *testing.T) {
    got := fitHintItems(width, []hintItem{ ...production と同じ 8 項目を写経... })
    assertFits(t, "detailOv", got, width)
```

**テスト側で項目表を組み直して `fitHintItems` を呼び、その結果が width に収まるか**を見ている。
`fitHintItems` の出力が引数の幅に収まるのは構成上の**恒真**なので、幅の主張は何も守っていない。
production の呼び出し口（`tui.go:hintLine` の `m.detailOv.visible()` 分岐）は**一度も通らない**。

**変異検証**（issue 201 以前の姿 = 現実の退行形へ戻す）:

- `tui.go` の detailOv 分岐を固定文字列（110 桁、予算 89）に置換
- `go build` / `go vet` 通過。**マーカー文字列を仕込んでテスト実行「後」にも変異が残存すること**を
  確認（実行経路上にあることも確認: 描画された hint にマーカーが出て、末尾が `…` で切れて
  `Enter/h/q: 戻る` が消える）
- → 対象テストも **`go test .` パッケージ全体（36.6s）も緑のまま**

🚨 `src/glogx/CLAUDE.md` は `hint_width_test.go` を「最下行の hint が幅に収まること」の
強制手段として挙げているが、detailOv についてはその強制が**成立していない**。

## 発火条件

- hint を持つ**新しい画面を足したとき**、列挙表に追記し忘れると検査対象にならない
  （実際に 3 画面が漏れている → 279）
- detailOv の呼び出し口を固定文字列へ戻す退行は、**パッケージ全体が緑のまま通る**
- **silent に壊れる**

## 推奨対応

### 1. 列挙表を「レジストリ + 共通 sweep」へ

`hint_width_test.go:TestDiffHintUsesRenderBudget` / `status_view_test.go:TestStatusHintUsesRenderBudget` /
`issues_view_test.go:TestIssuesHintUsesRenderBudget` の 3 本は
「browseModel を作る → `showFrame`/width/height を立てる → 対象画面を開く →
`frameMinWidth`〜140 で `hintLine` を掃いて幅超過と `…` を見る」という
**同一の判定を 3 実装**持っている。

- `assertHintSweep(t, name string, open func(*browseModel))` を `tui_helpers_test.go` に置き、
  判定（幅超過 / `…` / 優先度 1 の残存）を 1 実装に寄せる
- 「hint を持つ画面」の**レジストリ**（名前 → open 関数 → 期待する出口の語）をテーブルで持つ
- 🚨 **レジストリは `activeFullScreen` の enum で駆動する**。`exhaustive` が既に enum 側を
  守っているので、画面を足したときの漏れが compile / test 時に出る
  （`fullscreen_test.go:fullScreenCases` が既に「ビューアを足したら 1 行足す」番兵と
  `hint` 列を持っているので、そこへ `assertFits(t, c.name, c.hint(m), m.hintWidth())` を
  1 行足すのが最小手。rlDash 行が `m.rlDash.hint()`（width 引数なし）である事実が、
  そのまま `hint(width int)` への変更を強制する）

### 2. detailOv 節を呼び出し口から駆動する形に置き換える

幅の assert を捨て、detailOv を開いた browseModel に対し `m.hintLine()` を
`frameMinWidth`〜140 で掃く（既存の `TestDiffHintUsesRenderBudget` と同型）。
**テスト側に `hintItem` 表を写経しない** — 表の二重管理は運用コストを生むだけで、
production 側の分岐消失を 1 mm も守らない。

### 3. 閾値の注意

201 が書いた `testPopupWidth-2`（82）ではなく **`frameMinWidth-2`（58）**にすること。
82 では rlDash（70 桁）が素通りする。

## 反証の試み

テスト内コメントは「自己言及にならないよう優先度と幅の主張だけを見る」と書いているが、
**幅の主張こそが恒真**であり、コメントの意図と実際の検知力が食い違っている。
`issues/done/264` の「決着」節は `TestHintsFitPopupWidth` を
「fitHintItems に移した時点で収まるが構造で保証されるので、残すなら優先度 1 が残る assert に
置き換える」と書いており、**置き換えが detailOv 節には適用されていない**。

## 攻めたが壊せなかった（0 件として記録）

- 「doctor」サブテストは **vacuous ではない**。201 以前の姿（`fitHintItems` をやめて全項目を
  素の join にする）を当てると `doctorView.hint が 118 桁で幅 89 に収まらない` で red になり、
  サブテスト単位でも doctor だけが FAIL・他 2 つは PASS と確認した
- 「job パネル（カーソルあり）」サブテストは production の `m.hintLine()` を通っており、
  呼び出し口を迂回していない
- 上記 sweep 3 本も実 `hintLine` 経路を幅で掃いており、恒真ではない

## 関連

- 279（この gate が守れていなかった実際の違反 3 本）
- `issues/done/201`（走査型の設計。完了ログと実装の食い違い。**done 本文への追記も要る**）
