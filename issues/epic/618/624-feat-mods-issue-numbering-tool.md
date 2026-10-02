# 624 (feat): issue の採番を、push まで済ませるツールにする (mod の `$.tool.register`)

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。

## 概要

issue 番号の衝突が繰り返している (`.claude/rules/worktree-per-session.md` に 214 / 221 / 223 / 295 の 4 例)。
規約は「採番したら即 push」だが、本文を書いてから commit するあいだに他のセッションが同じ番号を取る。
番号の一意性は `tests/issues/test_issue_numbers_unique.sh` と `githooks/pre-push` が**事後に**検出するだけ。

mod の `$.tool.register` なら、モデルが呼ぶツールとして「fetch → 次の番号を数える → skeleton を置く → その 1 ファイルだけを commit → push」を
1 回の呼び出しにできる。番号が push されるまでの窓が、本文を書く時間からツール 1 回分に縮む。

## 既にある判断との関係

`scripts/issue_number_drafts.sh` (issue 530) の冒頭は「**番号を払い出す口を別に作ると、pro-con の外で採番する人・session からその予約が見えない**」として、
予約の仕組みを作らなかった。このツールは予約ではなく、**master へ push された skeleton そのものが番号の取得**になる
(他の人・session からは、今の規約どおり origin/master の `issues/` に見える)。530 の判断とはぶつからない。この節を実装時にもう一度確かめる。

## 対応方針

- `_claude/mods/issue-number/`: ツール `mcp__issue-number__take` (入力: 置き場 (`issues/` か `issues/epic/<name>/`) / type / slug / タイトル)
- 数え方は `issues/README.md` の採番手順と同じ母集合にする (working tree と origin/master の `issues/` 全体)。
  採番の式を mod に写さず、`scripts/` に「次の番号を出す」入口を 1 本置いて、README・`issue_number_drafts.sh`・このツールから共有する
- commit は pathspec で skeleton の 1 ファイルだけにする (`commit-with-pathspec.md`)。push の前に `git log origin/master..HEAD` が
  そのコミット 1 本だけであることを確かめ、他の未 push の commit があれば push せずに返す (他の commit を巻き込まない)
- push が non-fast-forward で弾かれたら、fetch して番号を取り直す (取り直した番号で skeleton を作り直す)
- 共有の working tree (`~/dotfiles`) では使わせない (worktree の中だけ。`.claude/rules/worktree-per-session.md`)
- PG (push しない書き手) は今の `new-*.md` の運用のまま。このツールを PG に見せない (`agent.offer` / PG の設定で外す)

## 進捗

- 2026-10-02: 起票
