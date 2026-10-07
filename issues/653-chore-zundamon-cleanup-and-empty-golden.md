# 653 (chore): zundamon-kaisetsu の掃除 (死んだコード・古いコメント) と、空でも通るテスト

> 🚨 **担当中: dotfiles-4d**（2026-10-07〜）

起票日: 2026-10-07

## 詳細

- `build.go:lookupOr`: 中身は `m[key]` だけで、コメントの「キーが無いと null を区別する」とは違って何も区別しない (呼び出し元の numOr が既定値に倒すので害は無い)。消して直接書く
- `build.go:parseWav` の末尾: `if !gotFmt { return …missing }` の後に同じ値の `return` がもう一度ある。分岐が意味を持たない
- `engine_auto.go` の冒頭のコメント・`samePort` のコメント・`startAutoEngine` の利用者向けの文言に「コンテナ名は 1 つ」「同時に 1 つしか動かせない」の理由が残っている。
  名前は既に `containerNameFor` でポートごと。同時に 1 つの制限が残る本当の理由は、印 (engine.auto) が URL を 1 つしか持たないこと
- `golden_test.go:TestSpokenTextMatchesPython` / `TestRoundMatchesPython`: golden が空 (`[]`) でも PASS する (監査で実測。今は 7 件と 20 件)。下限の件数を検査する
- `more_test.go:TestBuildWithDefaultAssetsMatchesPython`: `golden["script"]` があるかを確かめていない (無ければ比較 0 件で通る。コードを読んだ判断)
- `build.go:writeWav` のヘッダの byte rate / block align は自前の parser との往復でしか確かめていない (parser はこの 2 つを読まない。
  壊す変異でも 128 本すべて PASS した)。値をテストで直接見る。ffmpeg 9.0.2 は壊れたヘッダも受け入れた (afconvert は未確認)

## 関連ファイル

- `src/zundamon-kaisetsu/build.go` / `engine_auto.go` / `golden_test.go` / `more_test.go`。監査の記録: issue 656

## 進捗

- [x] 死んだコード・古いコメント — chore(zundamon-kaisetsu) の commit
- [x] 空でも通るテストと wav ヘッダの検査 — 同じ commit

## 結果 (2026-10-07)

- `lookupOr` を消して `query["…"]` を直接書く (null の扱いは `numOr` のコメントへ) / `parseWav` の重複した return を 1 つに
- engine_auto.go の「コンテナ名は 1 つ」系の 3 か所を「印はエンジンを 1 つしか書けない」に直す (利用者向けの文言を含む)
- `TestSpokenTextMatchesPython` / `TestRoundMatchesPython` に件数の下限、`TestBuildWithDefaultAssetsMatchesPython` に golden の script の有無の検査
- `TestWriteWavHeaderFields`: ヘッダの 9 欄とチャンク名を直接見る
- 変異で red: byte rate を壊す / spoken.json と round.json を空にする / build_data.json から script を消す
