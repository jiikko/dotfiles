## 対象プラットフォーム

- **macOS のみ。Linux はサポート対象外** (2026-08-28 決定 / issue 133)。CI も macOS runner で回す
- したがって **BSD 側の書き方で構わない**。「GNU でも動くように」という理由だけで分岐を足さない
- ⚠️ ただし**移行の途中**。`scripts/check_platform_dialect.sh` / `make test-gnu` / 各所の BSD-GNU
  コメントは Linux 前提の名残で、外す順序と「それが副次的に守っていたもの」の精査は issue 133 の
  手順 4 に残してある。**先走って消さないこと** (bench はまだ ubuntu で回っている)

## `_claude/` (Claude Code 設定の正本) を触るとき

- `_claude/rules/*.md` は `~/.claude/rules/` に link され、**毎セッション起動時に全文読まれる**。本文は規範だけにし、なぜ・起源・実例は同名の `_claude/rules-rationale/<name>.md` に置く (起動時には読まれない。ルールを疑う・改訂するときに読む)
- 特定のファイル種別を**読んだ**ときだけ発火してよいルールは frontmatter `paths:` で条件ロードにする (例: `no-comment-line-starting-with-shellcheck.md`)。⚠️ `paths` は Read でしか発火せず **Write / Edit では発火しない** (2026-08-27 に `InstructionsLoaded` hook で実測: `tmp/**` を宣言したルールは tmp/ 配下への Write では読み込まれず、同じファイルの Read で読み込まれた)。「ファイルを書いた瞬間に発火させたい」ルール (issue 作成 / レポート出力) と、行動で発火するルール (commit / tmux / テスト作法) は無条件のまま置く
- rule / hook / skill / agent / command を**足したら `./setup.sh` を再実行する** (per-file link なので、足しただけでは Claude Code から見えない)。漏れは `make test` (tests/claude/test_claude_links_complete.sh) が検出する
