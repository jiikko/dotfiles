# test-changed の写像が tests/claude を一切拾わず、「テスト対象なし」と誤って断定する

起票日: 2026-08-14
種別: bug
優先度: **P3** (`make test` と CI は無改変で当該テストを実行するため、ゲートは迂回されていない)

> 🚨 **これはデグレではない。** `scripts/test_changed.sh` は `11a36ed` で**新設**された
> opt-in の補助コマンドで、997d078 時点には存在しない。「以前は正しく動いていた経路の退行」
> ではなく、**新設ツールの被覆漏れ**。昨晩の敵対的レビューで 2 人の検証者が独立に
> 「デグレではないが起票が妥当」と結論したものを記録する。

## 何が起きるか

`make test-changed PATHS="..."` が、**対応するテストが実在するパスに対して
「テスト対象なし (ドキュメント等)」と報告して exit 0 する**。

実測 (`./scripts/test_changed.sh --dry-run <path>`):

| 渡したパス | test-changed の判定 | 実際に検証しているテスト |
|---|---|---|
| `_claude/skills/fable/SKILL.md` | テスト対象なし | `tests/claude/test_skill_trigger_table.sh` |
| `_claude/rules/commit-with-pathspec.md` | テスト対象なし | (同上 / `test_dangling_symlinks.sh`) |
| `_claude/CLAUDE.md` | テスト対象なし | `tests/claude/test_skill_trigger_table.sh` |
| `_claude/hooks/deny-bare-tmux-kill.sh` | `test-syntax test-shellcheck test-zsh-syntax test-zshrc` (shell lint 4 本のみ) | `tests/claude/test_deny_bare_tmux_kill.sh` |

`tests/claude/` には 4 本のテストがあるが、**写像のどの腕からも到達しない**:

```
tests/claude/test_dangling_symlinks.sh
tests/claude/test_deny_bare_tmux_kill.sh
tests/claude/test_skill_trigger_table.sh
tests/claude/test_statusline.sh
```

## 問題は「コメントの断定」

`scripts/test_changed.sh:136-138`:

```sh
# テスト対象なし (明示写像)。ドキュメント・vendor・データファイルは対応する
# テストが存在しないので何も回さないが、黙って落とすのではなく報告する
*.md|*.txt|LICENSE|issues/*|docs/*|vendor/*|kinesis*|_claude/agents/*|_claude/rules/*|_claude/skills/*|...)
```

**「対応するテストが存在しない」は `_claude/skills/*` / `_claude/rules/*` / `_claude/CLAUDE.md`
については事実に反する。** 「黙って落とすのではなく報告する」という設計思想は正しいが、
報告している内容が誤っているため、**利用者は「検証不要なパスだ」と読んでしまう**。

🚨 この設計は「写像に無いパスは fail する」という規律 (`*)` 腕) を持っているのに、
明示写像した腕の内容が誤っていると**その規律をすり抜ける**。写像漏れより検出が難しい。

## ゲートは迂回されていない (影響が限定される理由)

同じ HEAD で、`make test` が通る経路は今も当該テストを実行して赤になる:

```console
$ make test-dir DIR=tests/claude          # test-discovered と同じ run_tests
[run] tests/claude/test_deny_bare_tmux_kill.sh
✗ subcommand 略記 kill-serve (tmux は前方一致を受理) 期待=deny 実際=allow
make: *** [test-dir] Error 1
```

`make test` / CI (`tests.yml`) はもともと `test-changed` を経由しないため、
**ゲートそのものは無傷**。実害は「部分実行を信じて `make test` を省略したときに漏れる」点に限られる。

なお `test-changed` の出力 1 行目は回すターゲットを開示するので、
`_claude/hooks/*` のケースは「34 本通ったように見える緑」ではなく
「4 つのターゲットしか回していない」と読める形にはなっている。

## 対応方針 (案)

1. **notest 腕から `_claude/skills/*` / `_claude/rules/*` を外し、`_claude/CLAUDE.md` と併せて
   `tests/claude` 腕へ写像する** (`make test-dir DIR=tests/claude`)
2. **`_claude/hooks/*` に `tests/claude` を追加**する (shell lint に加えて実テストも回す)
3. **`_claude/statusline-command.sh` も同様** (`*.sh` 腕に落ちて `tests/claude/test_statusline.sh` が漏れる)
4. コメントの「対応するテストが存在しない」を、**実際に存在しないものだけ**の記述に直す
   (`docs/*` / `vendor/*` / `LICENSE` 等)
5. 写像の正しさ自体をテストする: `tests/` 配下の各ディレクトリについて
   「そのテストが検証している対象パスを test_changed へ渡すと、そのディレクトリが
   ターゲットに入る」ことを assert する (写像の腐りを構造的に検出する)

## 未確認

- `tests/tmux` / `tests/bin` にも同種の写像漏れがあるかは未網羅
  (`_claude/hooks/*` → `tests/tmux` が必要かは要確認)
- 5 の「写像の正しさのテスト」が現実的なコストで書けるかは未検討
  (テストが検証する対象パスを機械的に取り出す手段が要る)

## 関連

- issue 056 — 同じ夜に新設された lint の false green (あちらは報告する本数が嘘、
  こちらは報告する「対象なし」が嘘)
- `_claude/rules/adversarial-review-own-safeguards.md` — 「沈黙 = 成功になっていないか」。
  本件は沈黙ではなく**誤った報告**だが、利用者に与える誤解は同じ

## 対応 (2026-08-14)

- 対応方針 1-4 を実施: `_claude/CLAUDE.md` / `_claude/(agents|rules|skills|references|commands)/*`
  → tests/claude 腕へ、`_claude/hooks/*` と `_claude/**.sh` (statusline 含む) → shell lint +
  tests/claude へ写像。notest 腕から _claude 系を除去しコメントの断定を実態に合わせた
- dry-run で issue の表の全ケースが tests/claude に写像されること、実走で 4 テストが
  回ることを確認
- 方針 5 (写像の正しさ自体のテスト) は「テストが検証する対象パスの機械的抽出」が必要で
  今回は見送り (コストが未検討のため。必要になったら別 issue で)
