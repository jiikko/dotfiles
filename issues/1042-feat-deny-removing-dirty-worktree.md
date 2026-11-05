# 1042 (feat): 未コミットの変更がある worktree の削除を PreToolUse の hook で止める

起票日: 2026-11-06

## 概要

`git worktree remove --force` は未コミットの変更ごと消し、object にも残らない。retro 1040 の提案 D。
