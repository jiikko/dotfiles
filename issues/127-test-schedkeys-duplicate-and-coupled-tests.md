# 127 test: schedkeys のテストに重複と位置依存が残っている

起票日: 2026-08-28
種別: test
関連: 監査 2026-08-28 (test-cleanup / test-helpers)。修正済みの分は commit 79e2453

監査で挙がったテスト品質の指摘のうち、**今回直さなかった**もの。いずれも「今すぐ壊れる」類ではなく、
消す・括るの判断が要るので分けた。証拠は監査時に ~30 個の変異を当てて得た「どのテストが落ちたか」。

## 1. 変異を 1 つも捕まえないテストが 2 本ある

- `regression_test.go:TestFormFitsInPopup` — 30 変異のうち **0 件**で発火。70x14 の格子は
  `render_test.go:TestFrameFitsEverySize` の真部分集合で、幅の検査も `frame` 側に吸収された
  (per-screen の切り詰めを 4 箇所外しても緑だった)
- `regression_test.go:TestPickRowsFitWidth` — 同じく 0 件。`render_test.go:TestPickAndMenuFitWithNastyInput`
  の w=70 に包含される

判断が要る点: `TestFormFitsInPopup` の `Cursor.X >= w` は `TestFrameFitsEverySize` の `> w` より
1 だけ強い。消すなら強い側を残す方へ寄せる。

## 2. 同じ理由でしか落ちないテストの対 (どちらか一方でよい)

- `model_test.go:TestPickEmptyDoesNotOpen` ⊂ `render_test.go:TestMenuItemsAreTheSingleSource`
  (`keyMenu` の `it.enabled` を外すと **この 2 本だけ**が落ちる)
- `editor_test.go:TestEditorIgnoresControlInput` と `render_test.go:TestInvisibleRunesRejected` は
  大部分が重なる。ただし **前者だけ**が `acceptable` の `text == ""` を守っているので、丸ごと消せない
  (空文字の行だけ後者へ移す、が最小)

## 3. 候補の並びに位置で依存しているテストが 27 箇所

`press(m,"tab"); press(m,"left"); press(m,"left")` が「時刻は presets の末尾から 2 番目」を
暗黙に符号化している (model 9 / render 7 / regression 11)。候補を 1 つ足すだけで **11 本が同時に落ちる**
(意図した変更なのに)。`presets` を探して目的の kind まで進むヘルパーにすると、この結合が消え、
かつ「意図した欄に入れたか」の検査も付く。

## 4. テストモデルの構築が 8 通りに散っている

`newTestModel` が label / 幅 / 高さ / 時計を指定できないため、8 箇所が `newModel(...)` + 手直しで
組んでいる。実際に **3 箇所 (`TestPickRowsFitWidth` / `TestEmacsKeysMoveMenuAndPick` /
`TestNoToastForCancel`) が固定時計を入れ忘れて実時刻で走っている** (ファイルの規律から外れている)。
`newTestModelAt(label, w, h, jobs...)` を足して寄せる。

## 5. キー入力の経路が 2 本ある

`keyMod` が常に 0 を返すため `press` が修飾キーを表現できず、`ctrlKey` が `press` を迂回している
(そのため `Text` を運べない)。`press(m, "ctrl+n", "")` と書くと今は **`c` の打鍵**になり、名前と
無関係な理由で通る/落ちる。`keyMod`/`keyCode` に接頭辞を解釈させて 1 本にする。

## 対応方針

1〜2 は「消す」判断がユーザー確認向き (テストを消す変更なので)。3〜5 は機械的に寄せられる。
着手するときは、寄せた後に **元の変異が今も red か** を確認してから commit すること。
