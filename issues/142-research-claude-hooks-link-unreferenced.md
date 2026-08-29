# 142 research: `~/.claude/hooks/` への symlink を参照しているものが無い

起票日: 2026-08-29
種別: research
関連: [119](119-retro-claude-md-layout-and-rules-split-2026-08-27.md) の項目 5 が出典

`setup.sh` は `_claude/hooks/` 配下を `~/.claude/hooks/` へ per-file link しており、
`tests/claude/test_claude_links_complete.sh` がその漏れを検査している。しかし
**そのリンクを読む側が repo 内に 1 つも見つからない**。死んだ配線なら、link と検査の両方が
「守る対象の無い機構」になっている。

## 実測 (2026-08-29)

**(a) hook の配線は全て dotfiles の実体パスを直接指している。**

```
$ grep -n 'hooks' _claude/settings.json
   ... "command": "~/dotfiles/_claude/hooks/tmux-pane-state.sh working"
   ... "command": "~/dotfiles/_claude/hooks/deny-bare-tmux-kill.sh"
   ... "command": "~/dotfiles/_claude/hooks/git-state-verify.sh"
   ... "command": "~/dotfiles/_claude/hooks/gofmt-on-edit.sh"
   ... "command": "~/dotfiles/_claude/hooks/human-tasks-due.sh"
   ... "command": "~/dotfiles/_claude/hooks/retro-open.sh"
```

`~/.claude/hooks/` を指す command は **0 件**。

**(b) repo 全体で `~/.claude/hooks` を書いているのは 2 ファイルだけ** (`tmp/` を除く)。

| 場所 | 役割 |
|---|---|
| `setup.sh` | link を張る側 (対象 dir の列挙 / `mkdir -p` / `ln -sfn` ループ / dangling 掃除) |
| `tests/claude/test_claude_links_complete.sh` | 張られたことを検査する側 (`for dir in agents commands rules hooks`) |

**張る側と検査する側しかいない。読む側がいない。**

(`Makefile` の `_claude/hooks/normalize-settings.sh` も dotfiles 側の直接パスで、
`~/.claude/hooks/` は経由しない。`.claude/settings.local.json` に hooks の記述は無い。)

## 未確認 (これがこの issue の本体)

- [ ] **Claude Code 本体が `~/.claude/hooks/` を自動探索するか**。settings.json での明示配線が
      唯一の経路なら link は不要だが、規約上の置き場として探索される可能性を潰していない
- [ ] **他 repo / 他マシンの settings が `~/.claude/hooks/` 経由で参照していないか**。
      dotfiles 外の settings.json は本 repo からは見えない
- [ ] `_claude/hooks/lib` (ディレクトリ) の link が要るか。hook 本体が `~/dotfiles/...` から
      起動される限り、`lib` の解決も dotfiles 側で閉じるはず

## 対応方針

上 3 点を確認したうえで、

- **不要と確定した場合**: `setup.sh` の `for f in ~/dotfiles/_claude/hooks/*(N)` ループと、
  `tests/claude/test_claude_links_complete.sh` の `for dir in agents commands rules hooks` から
  `hooks` を外す (行番号で pin しない — 1 行の増減で無言にドリフトする)。
  既に張られた `~/.claude/hooks/*` は setup.sh の dangling 掃除では消えない
  (リンク先が実在するため) ので、**撤去する場合は掃除の手当てを別に要する**
- **必要だと分かった場合**: 何が読んでいるかを `setup.sh` のコメントに書く
  (現状のコメントは「なぜ `(N)` を付けるか」だけで、**何のために張るか**が無い)

## 判断の材料 (先に読む)

⚠️ **「参照が無い = 消してよい」で直行しない**。
[`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md)
に従い、この link が**副次的に**防いでいたものを先に列挙すること。少なくとも
「新しい hook を足したとき `./setup.sh` の再実行を促す導線」が link 完全性テスト経由で
効いている (root `CLAUDE.md` が「足したら `./setup.sh` を再実行する。漏れは `make test` が
検出する」と書いている) ため、hooks を検査対象から外すと**その導線が hooks だけ抜ける**。

## trigger

急がない。`setup.sh` か `_claude/hooks/` を次に触るときに一緒に片付ける。
