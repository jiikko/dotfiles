# 562 (ux): `pro-con card list` の「(N 分)」が所要と読み違えられる

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

`pro-con card list --all` の各行の末尾の「(N 分)」は、今の列に入ってからの経過 (完了のカードなら完了してからの経過)。
2026-09-27 に Claude が C-010 の「(17 分)」を依頼から完了までの所要と読み、文書に書きかけた。履歴で数え直すと所要は 20 分 (19:11 → 19:31) だった。

## 対応方針 (案)

- 何の経過かを添える (例「完了から 17 分」「作業中 6 分」)。所要 (依頼から完了まで) を出すなら別の欄にする
- 画面のカードの経過の表示とも語を揃える

## 受け入れ条件

- [x] `card list` の経過が何の経過かを、ヘルプを読まずに読める

## 進捗

2026-09-28 に対応 (commit「pro-con: card list / show / 詳細の経過に、何の経過かの語を添える (issue 562)」)。

- 経過の起点は `card.Card.Since` (今の State に入った時刻) で、`card list` の行末・`card show` の状態・画面の詳細 (drawer) の状態の 3 箇所が裸の長さを出していた。
  `card.State.SinceText` に寄せ、起点を語にした: 依頼と完了は「依頼から 5 分」「完了から 17 分」(列に入った瞬間の出来事から数える)、途中の列は「作業中 6 分」「質問待ち 3 分」
  - list: `C-003  完了  終わったもの  担当: -  (完了から 17 分)`
  - show / 詳細: `状態: 完了から 17 分  担当: -  …` (旧 `状態: 完了 (17 分)`。列名と語が重なるので括弧を外した)
- ボードのカードは列の下に並ぶので列が文脈になる。裸の長さのまま (完了のカードには元々出していない)。語を揃えたのは詳細の方
- 所要 (依頼から完了まで) の欄は足していない。`help/usage.md` に「経過は所要ではなく、所要は `card show` の履歴の時刻から数える」を 1 行足した
- テスト: `TestCardListAgeSaysWhatItMeasures` (list 3 状態 + show) / `TestDrawerStateAgeSaysWhatItMeasures`。
  変異 4 本 (`SinceText` を裸の長さに戻す / list・show・drawer の配線をそれぞれ旧実装に戻す) で、どれも想定のテストだけが red になるのを `bin/mutate-verify` で確認
- `make test`: 失敗は `tests/issues/test_issue_numbers_unique.sh` の 1 件だけ (番号 563 が epic/415 と pending で重複。origin/master に元からある他の作業の採番衝突で、この変更とは無関係)
- 敵対的レビューは省略: 表示の文言だけで、判断ロジック・状態遷移・外部 I/O は変えていない (`card list` の文字列を解析している箇所が repo に無いことは grep で確認)
