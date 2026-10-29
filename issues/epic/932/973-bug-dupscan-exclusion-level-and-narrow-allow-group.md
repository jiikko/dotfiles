# 973 (bug): パスの除外が L1m に効かない・起点を絞ると allow-group が粗い段の別の組に効く

> 🚨 **担当中: dotfiles-4c**（2026-10-29〜）

起票日: 2026-10-29

## 概要

issue 966 の敵対的レビューで記録した P2 の 2 件。どちらも直した (P2-b は 1 回目の直し方を取り下げ、ユーザーが選んだ候補 1 で直した)。

## P2-a (修正済み): levels を省いたパスの除外が L1m の組を消さない

- 原因: `internal/exclude/exclude.go` の `check()` で、levels を省いたときの既定が `norm.Levels` (L1・L2・L3)。L1m (`norm.LM`) は除外の設定を入れた後に足された段で、既定に入っていなかった。
  コメントは「Levels が無ければ全部」と書いていたので、意図ではなく追従漏れと読んだ
- 影響: 生成コードの dir を除外しても、メンバー名だけ違う組が `-levels L1m` と sweep の節 2 に出続けた (gate の既定の段は L1・L2 なので CI は影響しない)
- 対応: commit「fix(dupscan): levels を省いたパスの除外を L1m にも効かせる (issue 973)」。省略は全部の段 (L1・L2・L3・L1m)。levels を書いたときは書いた段だけ (README に書いた)
- 確かめた方法: 変異 (mutate-verify・狙ったテストだけが赤): 既定を `norm.Levels` に戻す → `TestSweepPathExclusionWithoutLevelsCoversL1m`。
  `make -C src/dupscan test lint`・`make test-lint`・`make test-dupscan-gate` が rc=0 (他のセッションの dupscan の変更を取り込んだ rebase の後も dupscan の test・lint を回し直した)

## P2-b (修正済み): 起点を絞ると allow-group が粗い段の別の組に効く

- 形: allow-group の人数の照合 (`groupCommentRule`) は起点の中の単位だけで同じ本体を数える。起点を絞ると、L1 で人数に足りない印 (相手が起点の外) が、
  粗い段 (L2・L3・L1m) でちょうど人数になった起点の中の別の組に効き、その組を黙って消す (sweep・report。gate は base に無い釘を弾くので止まる)
- 取り下げた直し方: 「起点が repo 全体でなければ L1 の組にだけ効かせる」。make test と CI の gate は起点 `src` で打つ (Makefile の `test-dupscan-gate`) ので、
  全員が `src` の中にいる正しい L2 の allow-group まで効かなくなり gate が rc=1 になる (1 周目の敵対的レビューの P1)
- 採った直し方 (ユーザーが候補 1 を選んだ。2026-10-29): 起点が repo の一部のときだけ repo の全ファイルを一覧にし (`repo.OutsideExts`)、
  印の単位の言語のファイルが起点の外にもあるときだけ L1 の組に限る (`scopeComplete`)。dotfiles の Go は `src/` の外に 0 本 (`git ls-files '*.go'` で数えた) なので、
  起点 `src` の gate では今までどおり効く。report・gate (base と head で同じ判定)・near・sweep に同じ判定を渡す。効かせなかった印は人数の合わない印として残る
- commit「fix(dupscan): 起点の外に同じ言語のファイルがあるときだけ、allow-group を L1 の組に限る (issue 973)」
- 確かめた方法: 変異 4 本 (mutate-verify・狙ったテストだけが赤): 判定をいつも「全部ある」/ いつも「全部ない」/ 起点の外かを見ずに全部を外に数える /
  一覧を untracked だけにする。実物: 起点 `src` と root の sweep で静かにした組がどちらも 10、起点 `src/dupscan` では起点の外と区別できない印 1 個を数だけ言う。
  `make -C src/dupscan test lint`・`make test-lint`・`make test-dupscan-gate` が rc=0
- 限界 (README に書いた): 判定は言語ごとで相手の有無を見ないので、起点の外に同じ言語のファイルを 1 本置くだけで、起点の中の粗い段の印も効かなくなる (組が出る側に倒れる)。
  `dupscan-gate.yml` の paths は `src/**` 等なので、`src` の外に Go を足した commit では gate が起動せず、振る舞いが変わるのは次に `src` を触った commit。
  再開の trigger: `src` の外に Go を置く変更が来たとき (相手の有無まで見る判定にするか、gate の起点を見直す)

## 反証レビュー (敵対的、opus、読み取りのみ、1 周)

- P1 (採用・取り下げ): 上の P2-b の直し方が CI の gate を壊す
- P2 (採用): 起点を絞った走査で、L1 で人数が合う印が効くことを確かめる肯定のテストが無い → P2-b の取り下げでテストごと外した
- P3 (記録・未対応): L1 だけに絞っても、起点の中で L1 がちょうど人数・起点の外にも写しがある形は区別できない (この変更より前から)
- P3 (記録・未対応): `Rule.elements()` は段を `norm.Levels` でしか文字列にしないので、パスの除外が L1m に広がっても gate の「新しく足された除外」の要素は変わらない。
  影響は gate を `-levels` に L1m を入れて回したときだけ (CI は L1・L2)。既存の設定が新しく見える誤検知は無い (省略の除外の要素の文字列は前後で同じ)
- P3 (採用): `sweep_cmd.go` のコメントが「levels を省くと L1m に効かない」のままだった → 「levels を L3 などに絞ると」に直した
- レビュー 2 周目は回していない: P2-a の実装は 1 周目で見た版から変えておらず、直したのは P2-b の取り下げとコメントだけ

## 反証レビュー (P2-b の採った直し方、opus、読み取りのみ、1 周)

P1 なし。
- P2 (記録・README): 上の「限界」。今の dotfiles では起点の外の Go は 0 本で発火しない
- P2 (採用): 追加したテストが untracked のファイルでしか回っておらず、一覧を untracked だけにする変異が緑になる → 起点の外のファイルを `git add` してから走らせる形にし、その変異で赤を確かめた
- P2 (記録・未対応): gate の配線 (base にも同じ判定)・near / report の配線・L3 と L1m の段・複数の起点・拡張子を広げるループ (Go も Swift も 1 つ) を見るテストが無い
- P3 (記録・未対応): `gate -head <commit>` でも判定は作業ツリーの一覧から作る / base と head で起点の外の Go の有無が違うと base の印の段が変わる /
  言語ごとに判定するが同じ本体の数は全言語で数える (Swift と Go が粗い段で衝突するかは未確認) / 起点に glob の文字があると pathspec と `Contains` がずれる (安全側。変更前から) /
  起点の外のファイルの `Lstat` の権限エラーで rc=2 になりうる / 起点の L1 でちょうど人数・repo 全体では多い印は今も組を消す (起点の外の写しは見えない。本質的な限界)
- 性能 (未実測): 起点を絞るたびに repo 全体の `ls-files` と `Lstat` を 1 回ずつ足す。dotfiles は対象 1,309 ファイルで問題にならない見込み。印があるときだけ計算する・`:(exclude)` で外だけを列挙する、が軽くする案

## 進捗

- [x] P2-a の修正と push
- [x] P2-b の直し方の取り下げと、理由・候補の記録
- [x] P2-b を候補 1 で修正・敵対的レビュー・push
- 記録のみ: 上の各レビューの P2・P3 (理由と再開の trigger は各節)
