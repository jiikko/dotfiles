# 533 (feat): `pro-con worktree clean` で remote の取り込み済みのブランチも片付ける

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-083) に、ユーザーが issue にすると答えた。`pro-con worktree clean` (492) はローカルの worktree とブランチしか消さない。
remote の `worktree-pc-c-*` は 522 で一度だけ手で片付けた。PG は push しない (521) ので今は増えにくいが、521 より前の PG や、例外で push したブランチは残る。

## 期待する動作

- `pro-con worktree clean` に remote のブランチも対象にする口を足す (例: `--remote`)。既定は一覧を出すだけ、`--yes` で消す (492 と同じ)
- 🚨 remote のブランチを消すのは外に出る破壊的な操作。取り込み済みの判定は 492 の `wtclean/judge.go` の `inBase` をそのまま使う (同じ判定を 2 つ作らない)。
  消す直前に `git fetch --prune` して取り直し、1 本ずつ消す。完了していないカードのブランチは消さない
- テストは偽の remote (`git init --bare` を `mktemp -d` に) で行い、本物の remote には触らない。敵対的レビューを最終ゲートにする

## 関連

- 492 (worktree clean) / 521 (PG は push しない) / 522 (remote の手での片付け)
