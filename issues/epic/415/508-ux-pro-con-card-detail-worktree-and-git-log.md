# 508 (ux): カードの詳細に、PG の worktree のパスと git log (取り込む先より先の commit) を出す

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「カード詳細では worktree へのパス、git log も見れるようにしてほしい」。

## 今の形 (2026-09-26)

- 詳細 (enter) と `pro-con card show` の「進捗」の節 (469。`card/progress.go` の `ProgressLines`) は、数だけを出す:
  「commit: origin/master より N 本先 (最後の commit M 分前)」「未 commit の変更: K ファイル」「issue の本文の進捗の記録」
- PG の worktree の場所 (`<repo>/.claude/worktrees/pc-c-NNN`) とブランチの名前は、どこにも出ない。見本のコマンドや差分を見るには、人が場所を推測して打っている

## 期待する動作

- 詳細と `card show` に、PG の **worktree の絶対パス** と **ブランチの名前** を出す (コピーしてそのまま `cd` できる形。狭いときも切らずに折り返す)
- **git log**: 取り込む先 (origin/master) より先の commit を、新しい順に 1 行ずつ (短い hash・時刻・subject)。多ければ上限で切って「ほか N 本」
- 未 commit の変更は、数に加えてファイルの名前を出す (多ければ上限)
- worktree が無い (片付けた・まだ起動していない) カードは、そう出す
- 集め方は 469 の進捗と同じ裏の収集に載せる (画面が描くたびに git を叩かない)。`--view` でも見られる (読むだけ)
- 見た目 (節の位置・長いときの畳み方) は本体に入れる前に見本を出して人に選んでもらう

## 関連

- 469 (進捗の節) / 467 (活動) / 492 (worktree の片付け。片付けたカードは worktree が無い)
