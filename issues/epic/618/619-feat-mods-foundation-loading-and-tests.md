# 619 (feat): mods の土台 — 置き場所・読み込みの配線・テスト・入口の文書

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。620〜624 の前提。

## 概要

mod を dotfiles で持つための共通部分を作る。中身のある mod は 620 以降で足す。

## 対応方針

1. **置き場所**: `_claude/mods/<name>/` (plugin 1 つ = ディレクトリ 1 つ。`.claude-plugin/plugin.json` / `hooks/hooks.json` / `hooks/register.ts(x)`)
2. **読み込み**: `_claude/settings.json` の `env` に `CLAUDE_CODE_PLUGIN_DIRS` を書き、`~/dotfiles/_claude/mods/<name>` を並べる。
   hook と同じく `~/dotfiles` の実体パスから読まれる (worktree で編集しても、master へ push して `~/dotfiles` へ pull するまで効かない。
   `.claude/rules/worktree-per-session.md`)
3. **読み込まれる経路の実測** (618 の前提の 1 項目目): 何もしない mod (`session.start` で `$.ui.log` するだけ) を置き、
   対話 / `claude -p` / pro-con の PG で読まれるかを確かめる。読まれない経路があれば、その経路に要る settings の hook は残す前提で 618 の表を直す
4. **テスト**: `claude plugin validate` と `claude plugin test` (`*.test.ts`) を `make test` から回す。`tests/claude/` に、
   `_claude/mods/*/` を自動で見つけて両方を回す bash のテストを 1 本置く。mod が 0 件なら失敗にする
   (`verify-execution-not-just-exit-code.md`: 対象 0 件は失敗)
   - CI (macOS runner) に `claude` が無いなら、そのテストは skip せず「判定不能」と出す形にし、CI での扱いを決める
5. **入口の文書**: `_claude/CLAUDE.md` か `docs/` に「mod の置き場所・読み込み・テスト・settings の hook との分担 (618 の表)」を書き、
   `docs/README.md` の索引に 1 行足す (`new-tool-requires-entrypoint-docs.md`)

## 受け入れ条件

- [ ] `_claude/mods/` に何もしない mod が 1 つあり、`make test` で validate と test が走ったことを出力で確かめた
- [ ] 対話 / `claude -p` / PG の 3 経路で、読まれた・読まれなかったの実測を本文に書いた
- [ ] 入口の文書と索引を足した

## 進捗

- 2026-10-02: 起票
