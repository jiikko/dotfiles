# 522 (chore): remote に溜まった `worktree-pc-c-*` のうち、master に取り込み済みのものだけを消す

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-080) に、ユーザーが「取り込み済みだけ消す」と答えた。PG が push した topic ブランチ (521) が remote に溜まっている
(2026-09-26 に 61 本。C-080 の起票の時点では取り込み済み 38 本・未取り込み 33 本)。

## やること

- 🚨 **remote のブランチを消すのは外に出る破壊的な操作**。消すのは「master に取り込み済み」と確かめられたものだけ。未取り込みのものは消さない
- 取り込み済みの判定は、**492 の判定 (`src/pro-con/wtclean/judge.go` の `inBase`) をそのまま使う** (同じ判定を 2 つ作らない)。今の判定は:
  先端が `origin/master` の祖先なら取り込み済み。祖先でなければ、`origin/master` に無い merge commit が 1 本でもあれば残す (git cherry は merge commit を比べない) /
  `git cherry` が全部 `-` でも、空白まで比べる `git patch-id --verbatim` で同じ patch が `origin/master` に無いものがあれば残す (git cherry の patch-id は空白の違いを無視する)
- 消す直前に `git fetch --prune` して判定を取り直し、1 本ずつ消す (`git push origin --delete <branch>`)。消した一覧と残した一覧 (理由つき) をこの本文に書き戻す
- 作業中・レビュー中のカードのブランチ (`pro-con card list` の完了していないカード) は、取り込み済みに見えても消さない
- 一度きりの片付けとして手で行う (自動化はしない。自動化するなら 492 の `pro-con worktree clean` に remote を足す別の issue にする)

## 関連

- 521 (PG に push させない。先に入れないとまた溜まる) / 492 (ローカルの worktree とブランチの片付け)

## 進捗 (2026-09-26, C-080)

- ユーザーの回答は「PG が消してよい」。判定は `inBase` (`src/pro-con/wtclean/judge.go`) を同じ package の一時のテストから呼んで取った (`git fetch --prune` の直後。一時のファイルは消した・commit していない)
- **取り込み済み 52 本** (先端が origin/master の祖先 33 / git cherry が全部 - で空白まで同じ 19): 以下は `worktree-pc-c-` の後ろ
  `002 003 005 006 007 008 010 011 013 014 016 017 018 019 021 022 024 025 025-r1 025-r2 026 027 029 030 031 032 033 034 035 036 037 038 039 041 042 044 045 046 047 048 049 051 052 053 054 055 057 058 059 060 061 062`
- **残す 9 本** (理由は inBase の出力):
  - 004 / 009: origin/master に無い commit が 1 本
  - 012: origin/master に無い merge commit が 2 本 (git cherry で比べられない)
  - 020 / 023: 無い commit が 2 本。020-rebased: 3 本
  - 070 / 078: 無い commit が 3 本 (カードがレビュー中)。072: 無い merge commit が 1 本 (カードが作業中)
- 完了していないカード (C-070〜C-075・C-078〜C-080) のブランチは、取り込み済みの 52 本に入っていない
- 🚨 **まだ消していない**。PG の session から消そうとしたところ、Claude Code の auto mode の分類器に止められた (Unverifiable Deletion Scope)。remote は 61 本のまま
- 人が消すなら、`~/dotfiles` で `git fetch --prune` の後、1 本ずつ判定した先端を lease に指定して消す (判定の後に動いたブランチは消えずに失敗する):
  `git push --force-with-lease=refs/heads/<branch>:<sha> origin :refs/heads/<branch>`。sha は `git rev-parse origin/<branch>` で取り直し、
  判定は上の一覧を使う (取り直すなら inBase をもう一度回す)。消したら、消した一覧をここへ書き足す
