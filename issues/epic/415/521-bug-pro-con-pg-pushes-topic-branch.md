# 521 (bug): PG が自分のブランチ (`worktree-pc-c-NNN`) を remote へ push し、remote に溜まる

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

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

- **出どころは PG への指示そのものだった** (上の「詳細」の 1 項目と見込みは誤り)。86dd3ca3 より前の `Prompt` は
  「commit は自分のブランチまで push する (master へは push しない)」と書いていた (415 の旧い規律)。
  bg の session の既定の指示 (「worktree で変更したら commit し、remote があれば push する — task・CLAUDE.md が git を留保していればそちらに従う」) も同じ向きに押すが、留保の但し書きがあるので指示で止まる
- **修正は 86dd3ca3** で入った: 「commit までにする。push しない (master にも自分のブランチにも)。~/.claude/CLAUDE.md の push するまでが担当より優先する」。
  `TestPromptCarriesDiscipline` が旧い文へ戻すと red
- **本物の PG で 1 回確かめた**: C-072 (session 159835b1) は修正の後に aa459113 (merge) を commit したが、`origin/worktree-pc-c-072` は 4ab063e9 (22:45) のまま載っていない。
  C-080 (この session、修正後の指示で起動) も push していない。`--settings` で push を止める案は、指示で止まったので採らない
- 残り: 修正の前に起動して動いている PG (C-070 / C-074 / C-078 など) は旧い指示のまま走るので、その分は 522 と同じ手で後から片付ける
