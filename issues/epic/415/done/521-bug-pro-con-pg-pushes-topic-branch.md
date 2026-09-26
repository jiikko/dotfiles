# 521 (bug): PG が自分のブランチ (`worktree-pc-c-NNN`) を remote へ push し、remote に溜まる

起票日: 2026-09-26

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-080) に、ユーザーが「直す」と答えた。PG が `git push -u origin HEAD:worktree-pc-c-NNN` で自分のブランチを push している
(C-074 / C-077 / C-078 / C-002 の transcript)。取り込みの係はローカルの worktree から merge する (`integrator-guide.md` の役目 3) ので、remote のブランチは使われない。
remote の `worktree-pc-c-*` は 2026-09-26 に 61 本 (C-080 の起票の時点では取り込み済み 38 本・未取り込み 33 本)。

## 詳細 (2026-09-26 に読んだ)

- PG への指示 (`src/pro-con/dispatcher/dispatcher.go` の `Prompt`) にも、pm-guide・integrator-guide にも、PG に push させる文は無い
- 🚨 **PG への指示に「push しない」は既に入っている**: 「pro-con: PG は push しない (自分のブランチにも。…)」の commit (この issue の起票の 9 分前)。
  PM は起票のときにこれを見落とした (反証レビューで判明)。起票の時点で remote に溜まっていたのは、それより前に起動した PG の分
- 出どころの見込み (**未確認**): Claude Code の bg の session の既定の指示 (「worktree で変更したら commit し、remote があれば push する」)。PG は `claude --bg -w <name>` で起動する
- 残すと、remote に topic ブランチが増え続け、492 (`pro-con worktree clean`) で片付けても remote には残る

## 期待する動作

- PG は自分のブランチを remote へ push しない (commit はローカルの worktree に残し、取り込みの係がそこから取り込む)
- 残りは**効いたかの確認**: 指示が入った後に起動・再開した本物の PG の transcript で、`git push` を打っていないことを確かめる (偽の claude では確かめられない)。
  再開した古い session は、起動のときの指示を持ったままなので、指示が効くのは新しく起動した PG から。
  指示だけでは止まらない実例が出たら、起動の設定 (`--settings` の permissions の deny など) で止められるかを確かめる

## 関連

- 522 (取り込み済みの remote のブランチを消す) / 492 (ローカルの worktree の片付け) / 487 (取り込みの係)

## 進捗 (2026-09-26, C-080)

- **起票の前に push させていたのは、旧い PG への指示そのものだった**。86dd3ca3 より前の `Prompt` は「commit は自分のブランチまで push する (master へは push しない)」と書いていた
  (上の「詳細」の 1 項目目「push させる文は無い」は、86dd3ca3 の後の状態のこと)
- **効いたかの確認 (transcript)**: 86dd3ca3 (2026-09-26 23:07:18 +0900 = 14:07:18Z) の後に更新された PG の transcript (`~/.claude/projects/*pc-c-0*/*.jsonl`) から、
  Bash の tool_use の command に `git push` があるものを jq で抜いた (本文への言及は数えない)
  - **修正の後に新しく起動した PG**は C-080 (14:16Z 起動) だけで、`git push` は **0 回**。起動の指示に「push しない」が届いていることも transcript で見えた
  - C-078 (12:48Z 起動の session を再開) は修正の後の 14:11Z に `git push --force-with-lease origin HEAD:worktree-pc-c-078` などを打った。
    **再開した古い session は旧い指示のまま**、という上の見込みどおりで、指示が効かない実例ではない
  - C-072 / C-073 / C-074 は修正の後に push していない (C-072 の最後の push は 12:44Z)
- 残り: 新しく起動した PG の実例がまだ 1 本 (C-080) だけ。次に起動する PG でも 0 回かを見る。指示だけで止まらない実例が出たら `--settings` の deny を試す

## 決着 (2026-09-27)

- ユーザーの判断で done (「remote に push しないので」)。直し (`86dd3ca3`、2026-09-26 23:07) の後に remote に上がった PG のブランチは 0 本 (2026-09-27 02:0x の `git fetch --prune` の後。その間に C-073〜C-097 の PG が動いた)。残る 7 本は直しの前のもので、522 の判定待ち
