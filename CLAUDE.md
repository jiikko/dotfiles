## 対象プラットフォーム

- **macOS のみ。Linux はサポート対象外** (2026-08-28 決定 / issue 133)。CI も macOS runner で回す
- したがって **BSD 側の書き方で構わない**。「GNU でも動くように」という理由だけで分岐を足さない
- 移行は完了済み (issue 133)。Linux 前提の道具 (`make test-gnu` / `scripts/check_platform_dialect.sh`)
  は、対象が macOS だけになった時点で「正しい macOS の書き方を弾く」側に回ったので外した
- ⚠️ **残るのは「版」の差**。CI runner の `/bin/bash` は 3.2、開発機は Homebrew の 5 系。
  workflow 側で brew の bash を PATH 先頭に出して揃えている。新しい workflow を足すときは同じ手当てが要る

## `_claude/` (Claude Code 設定の正本) を触るとき

- `_claude/rules/*.md` は `~/.claude/rules/` に link され、**毎セッション起動時に全文読まれる**。本文は規範だけにし、なぜ・起源・実例は同名の `_claude/rules-rationale/<name>.md` に置く (起動時には読まれない。ルールを疑う・改訂するときに読む)
- 特定のファイル種別を**読んだ**ときだけ発火してよいルールは frontmatter `paths:` で条件ロードにする (例: `no-comment-line-starting-with-shellcheck.md`)。⚠️ `paths` は Read でしか発火せず **Write / Edit では発火しない** (2026-08-27 に `InstructionsLoaded` hook で実測: `tmp/**` を宣言したルールは tmp/ 配下への Write では読み込まれず、同じファイルの Read で読み込まれた)。「ファイルを書いた瞬間に発火させたい」ルール (issue 作成 / レポート出力) と、行動で発火するルール (commit / tmux / テスト作法) は無条件のまま置く
- rule / hook / skill / agent / command を**足したら `./setup.sh` を再実行する** (per-file link なので、足しただけでは Claude Code から見えない)。漏れは `make test` (tests/claude/test_claude_links_complete.sh) が検出する
- ⚠️ **hook だけは例外で、link されていなくても動く**。hook の起動経路は `_claude/settings.json` の `command` **だけ**で、そこには dotfiles の実体パス (`~/dotfiles/_claude/hooks/...`) を書いている。`~/.claude/hooks/` への link は**どこからも読まれていない** (実測 2026-09-02 / issue 142: Claude Code 2.1.257 のバイナリに `.claude/hooks` の文字列が 0 件 — `skills` 66 / `agents` 20 等は在る。起動時の `$0` も dotfiles 側だった)。link は「置き場所に依存しない安定パス」として残してあるだけなので、**新しい hook を足して setup.sh を忘れても hook は動く** (テストは赤くなる)
