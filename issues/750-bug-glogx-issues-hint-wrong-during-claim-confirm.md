# 750 (bug): glogx の issues viewer で n の確認を出している間も、hint 行が一覧の案内 (Enter: 本文 / q: 終了) のまま (P3)

> 🚨 **担当中: Claude (issues viewer の監査の対応セッション)**（2026-10-14〜）

起票日: 2026-10-13

## 概要

`src/glogx/issues_view.go` の `issuesView.hint` は、`urlPick` / `linkJump` / `numFilter` / 本文 / 一覧を切り替えるが、`markNext.active` (n で出す claim の確認の板) を見ていない。
`handleKey` は確認中に全キーを先に飲むので、案内と実際の動きが食い違う。

## 詳細

- 発火条件: 一覧で `n` を押して確認を出す。hint 行は一覧の案内 (`Enter: 本文` / `q: 終了` …) を出したまま
- 実際の動き (main が `handleKey` の分岐で確認): 確認中の Enter は `IsYesStrict` で**実行** (ファイルの移動とバナーの書き込み) になり、q は終了せずに確認を取り消すだけ。
  板の中の案内 (`confirm.HintYesOther`) と hint 行が逆のことを言う
- 方針は 2 つの先例が逆を向いていて、どちらへ揃えるかが先に決まっていない (反証レビュー):
  - 切り替える側: `docs/issues-viewer-spec.md` の URL ピッカーの節が「hint 行もピッカー用に切り替える」を既知の事故として挙げる。status viewer の `hint` は確認中 (`discarding`) を最優先で切り替える
  - 切り替えない側: `src/glogx/tui.go` の `hintLine` の doc は、status viewer から b / p で push / pull の確認を出したとき「中央の確認モーダルが y/N を案内するので、狭い hint 行を奪ってまで二重に出さない」とする。
    n の確認も板の中で `confirm.HintYesOther` を出すので、こちらの形に当たる
  - ただし切り替えない側でも、hint 行が**確認中に効かないキー** (Enter: 本文 / q: 終了) を出し続けるのは「二重に出さない」とは別の問題。最低限、効かない案内を消すか確認の語彙にする
- 既存テスト `TestIssuesViewHintChangesByMode` は確認のモードを含まない (監査体が複製で再現)

## 対応方針

- `hint` の先頭に `markNext.active` の分岐を足し、確認の板と同じ語彙 (y / Enter: 実行、その他: 取り消し) を出す
- `TestIssuesViewHintChangesByMode` に確認のモードを足す

## 関連ファイル

- `src/glogx/issues_view.go` (`hint` / `handleKey`)・`docs/issues-viewer-spec.md`
- 監査の記録: 754

## 進捗

- [ ] 未着手
