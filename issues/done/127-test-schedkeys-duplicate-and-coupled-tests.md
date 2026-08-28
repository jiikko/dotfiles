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

## 結果 (2026-08-28 実施)

全 5 項目を実施した。**削除の前後で変異バッテリ (11 本) を回し、どの変異にも守り手が残ることを
確認してから消した**。

- 1・2: `TestFormFitsInPopup` / `TestPickRowsFitWidth` / `TestPickEmptyDoesNotOpen` /
  `TestEditorIgnoresControlInput` を削除。消す前に `TestFrameFitsEverySize` の
  カーソル検査を 1 段強く (`> w` → `>= w`) し、空文字の拒否を `TestInvisibleRunesRejected` へ移した
  (これらが削除するテストの固有の主張だった)
- 3: `focusSpecOfKind(t, m, kind)` を入れ、位置依存の 27 箇所を種類ベースへ。候補を足しても
  無関係なテストが落ちなくなり、「意図した欄に入れたか」の検査も付いた
- 4: `newTestModelAt(label, w, h, jobs...)` に集約。実時計で走っていた 3 本を固定時計へ。
  さらに **`TestTestsUseSharedConstructor`** を足し、テストが `newModel` を直に呼んだら落ちるようにした
  (この回帰が再発しないよう構造で止める。ガード自身が空振りしないことも検査する)
- 5: `keyMod`/`keyCode` に修飾キーの接頭辞を解釈させ、`ctrlKey` を削除。`press(m, "ctrl+n", "")` と
  本番と同じ表記で書けるようになった (以前は `c` の打鍵になっていた)

**副産物**: バッテリで **`chipRange` を全範囲にしても誰も捕まえない**穴が見つかった (行の幅は
守られるのに選択中の候補が画面外へ出る)。`TestSelectedChipStaysVisible` を追加。
あわせて、幅 9 のような極端に狭い端末でカーソルが最終列の外 (X == width) に置かれていたのを
`width-1` に clamp した。
