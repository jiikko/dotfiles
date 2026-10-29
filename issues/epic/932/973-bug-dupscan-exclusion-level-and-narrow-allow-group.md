# 973 (bug): パスの除外が L1m に効かない・起点を絞ると allow-group が粗い段の別の組に効く

> 🚨 **担当中: dotfiles-4c**（2026-10-29〜）

起票日: 2026-10-29

## 概要

issue 966 の敵対的レビューで記録した P2 の 2 件。1 件 (P2-a) を直し、1 件 (P2-b) は直し方を取り下げて設計の判断を残した。

## P2-a (修正済み): levels を省いたパスの除外が L1m の組を消さない

- 原因: `internal/exclude/exclude.go` の `check()` で、levels を省いたときの既定が `norm.Levels` (L1・L2・L3)。L1m (`norm.LM`) は除外の設定を入れた後に足された段で、既定に入っていなかった。
  コメントは「Levels が無ければ全部」と書いていたので、意図ではなく追従漏れと読んだ
- 影響: 生成コードの dir を除外しても、メンバー名だけ違う組が `-levels L1m` と sweep の節 2 に出続けた (gate の既定の段は L1・L2 なので CI は影響しない)
- 対応: commit「fix(dupscan): levels を省いたパスの除外を L1m にも効かせる (issue 973)」。省略は全部の段 (L1・L2・L3・L1m)。levels を書いたときは書いた段だけ (README に書いた)
- 確かめた方法: 変異 (mutate-verify・狙ったテストだけが赤): 既定を `norm.Levels` に戻す → `TestSweepPathExclusionWithoutLevelsCoversL1m`。
  `make -C src/dupscan test lint`・`make test-lint`・`make test-dupscan-gate` が rc=0 (他のセッションの dupscan の変更を取り込んだ rebase の後も dupscan の test・lint を回し直した)

## P2-b (直し方を取り下げ・未対応): 起点を絞ると allow-group が粗い段の別の組に効く

- 形: allow-group の人数の照合 (`groupCommentRule`) は起点の中の単位だけで同じ本体を数える。起点を絞ると、L1 で人数に足りない印 (相手が起点の外) が、
  粗い段 (L2・L3・L1m) でちょうど人数になった起点の中の別の組に効き、その組を黙って消す (sweep・report。gate は base に無い釘を弾くので止まる)
- 取り下げた直し方: 「起点が repo 全体でなければ L1 の組にだけ効かせる」。敵対的レビュー (opus) の P1 で崩れた: make test と CI の gate は起点 `src` で打つ (Makefile の `test-dupscan-gate`)。
  そのため起点はいつも「repo の一部」になり、全員が `src` の中にいる正しい L2 の allow-group まで効かなくなって gate が rc=1 になる。
  既存の印 (`src/dupscan/callers_cmd_test.go` の `allow-group 8`。L2 で 8 人の見込み・未確認) も、`dupscan sweep src` で組として出直す
- 直し方の候補 (どちらも設計の判断。ユーザーに委ねる):
  1. 判定を「起点の中に、その言語の単位が repo の全部入っているか」にする (dotfiles の Go は全部 `src/` の下なので、起点 `src` は全部入っている扱いになる)
  2. Makefile と CI の gate を起点の指定なし (repo 全体) で打つ
- 理由と候補は `groupCommentRule` のコメントにも書いた。再開の trigger: 起点を絞った sweep / report で、印を書いていない組が消えている報告が出たとき、または上の候補をユーザーが選んだとき

## 反証レビュー (敵対的、opus、読み取りのみ、1 周)

- P1 (採用・取り下げ): 上の P2-b の直し方が CI の gate を壊す
- P2 (採用): 起点を絞った走査で、L1 で人数が合う印が効くことを確かめる肯定のテストが無い → P2-b の取り下げでテストごと外した
- P3 (記録・未対応): L1 だけに絞っても、起点の中で L1 がちょうど人数・起点の外にも写しがある形は区別できない (この変更より前から)
- P3 (記録・未対応): `Rule.elements()` は段を `norm.Levels` でしか文字列にしないので、パスの除外が L1m に広がっても gate の「新しく足された除外」の要素は変わらない。
  影響は gate を `-levels` に L1m を入れて回したときだけ (CI は L1・L2)。既存の設定が新しく見える誤検知は無い (省略の除外の要素の文字列は前後で同じ)
- P3 (採用): `sweep_cmd.go` のコメントが「levels を省くと L1m に効かない」のままだった → 「levels を L3 などに絞ると」に直した
- レビュー 2 周目は回していない: P2-a の実装は 1 周目で見た版から変えておらず、直したのは P2-b の取り下げとコメントだけ

## 進捗

- [x] P2-a の修正と push
- [x] P2-b の直し方の取り下げと、理由・候補の記録
- 未対応: P2-b の本当の直し方 (候補 1・2 から選ぶ)。P3 2 件 (記録)
