# 540 (chore): `pro-con worktree clean --remote` を外す (remote へ push しない方針に揃える)

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-094) に、ユーザーが「そもそも remote に push しないって話になっているので push しないで」と答えた。

- PG は 521 (「pro-con: PG は push しない」の commit) から remote へ push しない。521 で本物の PG の push が 0 回であることを確かめた
- remote の PG のブランチは 522 で 56 本を手で消し、残りは判定を取り直す 7 本だけ
- 533 で入れた `pro-con worktree clean --remote` (「pro-con: worktree clean --remote で origin の取り込み済みの PG のブランチも片付ける (533)」の commit) は、
  remote のブランチを `push --delete` で消す = remote への push。PG が push しないので消すものが出ず、口だけが残っている

## やること

- `worktree clean` から `--remote` の口と、その処理・テストを外す (`src/pro-con/worktreecmd.go` / `worktreecmd_test.go`)。ローカルの片付け (492) はそのまま
- `README.md` と `pro-con help usage` (`help/usage.md`) から `--remote` の記述を外す
- remote に残っている 7 本は消さない (消すのも remote への push になる。ユーザーの決定)
- 判定の部品 (`wtclean/judge.go` の `inBase`) は 492 のローカルの片付けが使うので残す。`--remote` だけが使っていた部品があれば一緒に外す

## 関連

- 533 (`--remote` を入れた) / 521 (PG は push しない) / 522 (remote の手での片付け) / 492 (ローカルの worktree の片付け)
