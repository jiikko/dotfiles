# 750 (bug): glogx の issues viewer で n の確認を出している間も、hint 行が一覧の案内 (Enter: 本文 / q: 終了) のまま

起票日: 2026-10-13

## 概要

`src/glogx/issues_view.go` の `issuesView.hint` は、`urlPick` / `linkJump` / `numFilter` / 本文 / 一覧を切り替えるが、`markNext.active` (n で出す claim の確認の板) を見ていない。
`handleKey` は確認中に全キーを先に飲むので、案内と実際の動きが食い違う。

## 詳細

- 発火条件: 一覧で `n` を押して確認を出す。hint 行は一覧の案内 (`Enter: 本文` / `q: 終了` …) を出したまま
- 実際の動き (main が `handleKey` の分岐で確認): 確認中の Enter は `IsYesStrict` で**実行** (ファイルの移動とバナーの書き込み) になり、q は終了せずに確認を取り消すだけ。
  板の中の案内 (`confirm.HintYesOther`) と hint 行が逆のことを言う
- 意図的ではない根拠: `docs/issues-viewer-spec.md` の URL ピッカーの節が「hint 行もピッカー用に切り替える」と同じ形を既知の事故として挙げている。
  status viewer の `hint` は確認中 (`discarding`) を最優先で切り替えている
- 既存テスト `TestIssuesViewHintChangesByMode` は確認のモードを含まない (監査体が複製で再現)

## 対応方針

- `hint` の先頭に `markNext.active` の分岐を足し、確認の板と同じ語彙 (y / Enter: 実行、その他: 取り消し) を出す
- `TestIssuesViewHintChangesByMode` に確認のモードを足す

## 関連ファイル

- `src/glogx/issues_view.go` (`hint` / `handleKey`)・`docs/issues-viewer-spec.md`
- 監査の記録: 754

## 進捗

- [ ] 未着手
