# 620 (feat): issue 規約の注入を、mod でシステムプロンプトの節へ上げる

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。epic で最初に移す 1 本。

## 概要

`_claude/hooks/issue-rules-inject.sh` は、`issues/` を持つ repo のセッションで、SessionStart の `additionalContext` として
`_claude/issue-rules.md` と `_claude/issue-rules.d/*.md` (3 本) を注入している (settings に 4 行。issue 401)。
この注入は `<system-reminder>` として届き、CLAUDE.md と同じ拘束力を持たない。そのため `_claude/CLAUDE.md` に
「注入された規約に従う」義務を 1 行残している。

mod の `prompt.compose` なら、同じ文をシステムプロンプトの節 (`scope: 'session'`) として足せる。

## 対応方針

- `_claude/mods/issue-rules/`: `prompt.compose` で `next(e)` の `sections` の最後に 1 節を足す。
  `issues/` か `issue/` が在るかを `$.session.root()` と `$.fs.exists` で判定する (`issue-rules-inject.sh` と同じ条件にする)
- 中身は今と同じファイルを `$.fs.read` で読む (正本は `_claude/issue-rules.md` のまま。mod に写さない)
- 切り替えは 1 回で行う: mod を入れる commit で、settings の `issue-rules-inject.sh` の 4 行を外す (二重に注入しない)

## 確かめること

- [ ] headless の新規セッションで A-B を取る (`issues/` の在る cwd / 無い cwd)。手順は `.claude/rules/worktree-per-session.md` の
      `claude -p --model haiku --output-format stream-json ...` の形。答えは stream-json の先頭から読む (Stop hook が最後の返答を差し替えるため)
- [ ] subagent に規約が入るかを見る (今は入らない。入るなら挙動が変わるので、本文に書く)
- [ ] prompt cache への影響。節はセッションの間変わらないので、キャッシュは壊れない見込み (未実測)。`$.session.usage()` か `/context` で確かめる
- [ ] `_claude/CLAUDE.md` の「注入された規約に従う」の 1 行と、`tests/claude/test_issue_rules_inject.sh` を、移行後の形に合わせて直す / 消す

## 進捗

- 2026-10-02: 起票
