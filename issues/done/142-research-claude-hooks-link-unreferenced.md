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

## 調査結果 (2026-09-02)

**3 点とも決着した。`~/.claude/hooks/` を読む主体は存在しない。**

### (1) Claude Code 本体は `~/.claude/hooks/` を自動探索しない — 確定

- **公式ドキュメント** (https://code.claude.com/docs/en/hooks-guide.md の "Configure hook location"):
  hook の配置先として挙がるのは settings.json (user / project / local) の `hooks` キー、
  plugin の `hooks/hooks.json`、skill / subagent の frontmatter のみ。
  **`~/.claude/hooks/` というディレクトリ規約の記述は無い**
- **バイナリ実測** (`claude 2.1.257`,
  `~/.nodenv/versions/24.2.0/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`):
  文字列 `.claude/hooks` の出現が **0 件**。比較として `.claude/skills` 66 件 /
  `.claude/agents` 20 件 / `.claude/rules` 9 件 / `.claude/commands` 6 件 /
  `.claude/workflows` 4 件は存在する。**規約ディレクトリとして扱われているものは文字列が入っており、
  hooks だけ無い**
- `--hooks-dir` / `SELF_HOSTED_RUNNER_HOOKS_DIR` という文字列はあるが、これは同梱の
  self-hosted runner (orchestrator mode) 用で Claude Code の hook とは無関係

⚠️ 版依存の事実である。**上の grep が唯一の検出手段**なので、Claude Code の版が上がって
hooks の探索が入った場合は静かに前提が崩れる (再測は上の 1 コマンドで足りる)。

### (1b) 起動パスの直接実測 — `$0` は dotfiles 側だった

「`~/.claude/hooks/human-tasks-due.sh` は現に動いているのだから読まれているのでは」への回答。
**両パスは同一 inode の同一ファイル**なので (`stat -L -f '%i'` が一致)、
hook が動いていること自体はどちらの経路でも成立し、**中身では区別できない**。

区別できる観測は「起動時の `$0`」だけ。PostToolUse(Bash) hook の shebang 直後に
`printf 'PROBE argv0=%s\n' "$0"` を一時的に挿入して実測した:

```
PROBE argv0=/Users/koji/dotfiles/_claude/hooks/git-state-verify.sh
```

settings.json の `~/dotfiles/_claude/hooks/...` が展開された形そのもので、
`~/.claude/hooks/` は現れない。**link を全て削除しても hook の動作は変わらない。**
(probe は復元済み。逆に settings.json が link 側を指していれば、この観測はそちらのパスを出す =
機構の有無で結果が変わる観測になっている。)

### (2) この機体の settings で `~/.claude/hooks/` を参照するものは 0 件

- `~/.claude/settings.json` は dotfiles の `_claude/settings.json` への symlink。
  hook の `command` は全て `~/dotfiles/_claude/hooks/...` の直接パス
  (厳密な grep `(~|$HOME|/Users/koji)/\.claude/hooks` で **0 件**。
  素の `claude/hooks` は `dotfiles/_claude/hooks` に部分一致するので誤検出する)
- `~` 配下 (深さ 4) の `.claude/settings*.json` を全走査しても、`hooks` キーを持つのは
  上記 1 ファイルのみ。他 repo は hook を配線していない
- plugin は公式 marketplace の LSP 3 種のみで、`~/.claude/hooks` を参照しない
- `~/.claude/hooks/` の中身は 9 本すべて dotfiles への symlink (別由来の実体は無い)
- **未確認のまま残る**: 他マシンの settings。ただし settings.json 自体が dotfiles からの
  symlink である以上、この dotfiles を使う機体は全て直接パス配線になる

### (3) `lib` の link は「symlink 経由で起動されるとき」にだけ必要 — 現状は不要

`human-tasks-due.sh` / `retro-open.sh` は lib を `"$(dirname "$0")/lib/issue-hooks.sh"` で解決する。
**`dirname "$0"` は symlink を解決しない**ことを実測で確認した (別ディレクトリの symlink 経由で
起動すると、実体側ではなく link のあるディレクトリを返す)。つまり:

- `~/dotfiles/_claude/hooks/...` から起動 (= 現状) → lib は dotfiles 側で閉じる。**link 不要**
- `~/.claude/hooks/...` から起動 → `~/.claude/hooks/lib` link が**必須**

`lib` link は hooks link とセットでのみ意味を持つ。片方だけ残す選択肢は無い。

## マスクしていた failure mode の列挙

[`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md) に従う。

| この機構が防いでいたもの | 外した後は誰が防ぐか |
|---|---|
| **本来の目的**: `~/.claude/hooks/` 経由の hook 読み込み漏れ | **そんな経路は存在しない** (上記 1)。防ぐ対象が無い |
| **副次**: 「新しい hook を足したら `./setup.sh` 再実行」の導線 (root CLAUDE.md) | hooks に限れば**この導線は元から空振り**。hook は直接パス配線なので setup.sh 未実行でも動く。テストが赤くなっても「再実行しろ」は嘘の指示 |
| **副次**: setup.sh 冒頭の migrate ループ (dir symlink 化した `~/.claude/hooks` に `ln -sfn` が書き込んで実体を壊す footgun) | link をやめれば**この footgun 自体が消える** (防御が不要になる形) |
| **副次**: dangling 掃除ループが `~/.claude/hooks` を対象にしている | link をやめた後の既存 link は**リンク先が実在するので dangling ではなく、掃除では消えない**。撤去するなら別途の後始末が要る |

**結論: 外して失うものは無い。ただし「既に張られた 9 本 + lib をどう畳むか」だけが残る問題。**

## 決着 (2026-09-02): B = 残して理由を書く

ユーザー判断: **symlink なら害が無いのでこのまま残す**。あわせて「hook がどう動いているか」を
明文化する (今回の混乱そのものが、明文化されていなかったことの実証)。

やったこと:

- `setup.sh` の hooks link ループの直上に、**なぜ張るのか**を書いた
  (従来のコメントは `(N)` 修飾子の説明だけで、目的が無かった)。
  「読む側が現れたら lib の link も必須になる」条件も併記
- root `CLAUDE.md` の `_claude/` 節に「**hook だけは link されていなくても動く**」を追記。
  同節の「足したら `./setup.sh` を再実行する (足しただけでは Claude Code から見えない)」が
  hooks には当てはまらないため、その乖離の訂正も兼ねる
- `tests/claude/test_claude_links_complete.sh` の FAIL メッセージが
  「未リンクのファイルを読まない」と**hooks については嘘**を言っていたので注記を足した
  (hooks の FAIL は機能停止を意味しない)

**撤去 (選択肢 A) を採らなかった理由**: 既存 link の後始末が破壊的操作の新設になる一方、
残すコストは symlink 9 本とテスト 1 ループ分しかない
([`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) 節 0
「掃除機構を作る前に、そもそも発生させない構造を問う」の逆向きの適用 —
掃除する価値が掃除機構のリスクを上回らない)。

**選択肢 C (settings.json を `~/.claude/hooks/` 経由に変える)** も採らない。
link が生きて検査も意味を持つ代わりに、**setup.sh 実行前は hook が動かない**状態を新設する。
現状の実体パス配線はその点で堅い。

### 再評価の trigger

- Claude Code の版が上がって `~/.claude/hooks/` の自動探索が入ったとき
  (検出は上記 (1) の grep 1 本。**版依存の事実なので静かに崩れうる**)
- settings.json を `~/.claude/hooks/` 経由に変えたくなったとき (lib の link が必須になる)
