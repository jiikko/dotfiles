# 1031 (retro): 全 Go module に堅牢性の linter を足す作業 (2026-11-04)

起票日: 2026-11-04

## 概要

`src/.golangci.yml` (26 module の共通の設定) に errchkjson・exhaustive・fatcontext・makezero を足し、当たった所を直した。
commit は「lint(go): 全 module に errchkjson・exhaustive・fatcontext・makezero を足す」。踏んだ所だけを書く。

## どこで踏んだか

1. **候補の linter の件数を、採るときと違う設定で数えた**。`--enable-only` で既定の設定のまま当てたので、exhaustive は
   `default-signifies-exhaustive: false` で数えられ、20 件と出た。採る設定 (true) では 13 件で、`default` のある switch まで直す計画を
   立てかけた。採る設定を共通の設定に書いてから数え直して気づいた
2. **完了報告・途中経過を英語で書き、「日本語にして」と 4 回求められた**。道具の出力・commit message・rule の文書は日本語なのに、
   長い作業の後の要約だけが英語に崩れた
3. module の数を数えずに「25」と書き、ユーザーに聞かれて数えたら 26 だった
4. 書いたばかりのコードのコメントに、確かめていない到達性 (「先頭の札にしか使わないので、消えた札はここへ来ない」) を書きかけた。commit の前に自分で気づいて外した
5. 小さな手戻り: 除外の設定を `formatters` の下に書いた・makezero の nolint を報告の出る行と別の行へ付けた・`rm -f $d/*` が安全装置で止まった

## 次に効きそうな改善

- **A (1 から)**: 既製の検査を、当たる件数で選ぶなら、採るときと同じ設定で数える。→ 既存のチェックリスト
  `rules-checklists/adversarial-review-own-safeguards.md` の 1.5 節に 1 項足した (常時読み込みの予算の残りが少ないので、起動時に読まれない側に置く)
- **B (2 から)**: 報告は日本語で書き、送る前に冒頭の 1 文の言語を見る。→ auto memory (`reports-in-japanese`) に置いた。
  repo の規約ではなく、この利用者との作業の作法なので rule にはしない
- 却下 (3 から): 規範は既にある (`~/.claude/CLAUDE.md`「N 箇所すべてに対応したと書くなら N を機械で数えてから」)
- 却下 (4 から): commit の前に自分で気づいて外した。規範 (主張は証拠ではない) が働いている
- 却下 (5 から): どれも lint・安全装置がその場で止めた。足す規範が無い

## 進捗

- [x] A・B をユーザーの指示 (2026-11-05「retro を書いて対応やって」) で反映した
