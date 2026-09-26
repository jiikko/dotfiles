# 522 (chore): remote に溜まった `worktree-pc-c-*` のうち、master に取り込み済みのものだけを消す

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-080) に、ユーザーが「取り込み済みだけ消す」と答えた。PG が push した topic ブランチ (521) が remote に溜まっている
(2026-09-26 に 61 本。C-080 の起票の時点では取り込み済み 38 本・未取り込み 33 本)。

## やること

- 🚨 **remote のブランチを消すのは外に出る破壊的な操作**。消すのは「master に取り込み済み」と確かめられたものだけ。未取り込みのものは消さない
- 取り込み済みの判定は、492 と同じく 2 つで見る: そのブランチの先頭が `origin/master` の祖先か、`git cherry origin/master <branch>` が全部 `-` (squash・cherry-pick で入ったもの)。
  どちらでもなければ残す
- 消す直前に `git fetch --prune` して判定を取り直し、1 本ずつ消す (`git push origin --delete <branch>`)。消した一覧と残した一覧 (理由つき) をこの本文に書き戻す
- 作業中・レビュー中のカードのブランチ (`pro-con card list` の完了していないカード) は、取り込み済みに見えても消さない
- 一度きりの片付けとして手で行う (自動化はしない。自動化するなら 492 の `pro-con worktree clean` に remote を足す別の issue にする)

## 関連

- 521 (PG に push させない。先に入れないとまた溜まる) / 492 (ローカルの worktree とブランチの片付け)
