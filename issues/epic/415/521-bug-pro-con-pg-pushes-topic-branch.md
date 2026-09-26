# 521 (bug): PG が自分のブランチ (`worktree-pc-c-NNN`) を remote へ push し、remote に溜まる

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-080) に、ユーザーが「直す」と答えた。PG が `git push -u origin HEAD:worktree-pc-c-NNN` で自分のブランチを push している
(C-074 / C-077 / C-078 / C-002 の transcript)。取り込みの係はローカルの worktree から merge する (`integrator-guide.md` の役目 3) ので、remote のブランチは使われない。
remote の `worktree-pc-c-*` は 2026-09-26 に 61 本 (C-080 の起票の時点では取り込み済み 38 本・未取り込み 33 本)。

## 詳細 (2026-09-26 に読んだ)

- PG への指示 (`src/pro-con/dispatcher/dispatcher.go` の `Prompt`) にも、pm-guide・integrator-guide にも、PG に push させる文は無い
- 出どころの見込み (**未確認**): Claude Code の bg の session の既定の指示 (「worktree で変更したら commit し、remote があれば push する」)。PG は `claude --bg -w <name>` で起動する
- 残すと、remote に topic ブランチが増え続け、492 (`pro-con worktree clean`) で片付けても remote には残る

## 期待する動作

- PG は自分のブランチを remote へ push しない (commit はローカルの worktree に残し、取り込みの係がそこから取り込む)
- 実装の選択は PG が決めてよい。候補: PG への指示 (`Prompt`) に「push しない。取り込みの係がローカルの worktree から取り込む」を足す /
  起動の設定 (`--settings`) で push を止められるか確かめる。指示で止めるなら、止まったかを偽の claude では確かめられないので、本物の PG の transcript で 1 回確かめる

## 関連

- 522 (取り込み済みの remote のブランチを消す) / 492 (ローカルの worktree の片付け) / 487 (取り込みの係)
