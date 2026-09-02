# rules/ — dotfiles 固有の規範 (必要なときだけ読む)

**この repo でしか成立しない規範**の置き場。zsh の実装依存、この repo の CI の見方など、
他プロジェクトへ持っていっても意味がないもの。

⚠️ **ここは自動では読まれない**。参照元から辿って初めて読まれるので、
**新しく足したら「その罠を踏む場所」から必ずリンクする** (下の表の「参照元」列がそれ)。
リンクの無いルールは、存在を知っている人にしか届かない。

## 置き場の使い分け

| 置き場 | 読まれ方 | 何を置くか |
|---|---|---|
| [`_claude/rules/`](../_claude/rules/) | **毎セッション全文** (`~/.claude/rules/` へリンクされ、**全プロジェクト**で読まれる) | どのプロジェクトでも成立する作業規範。**36 本 2,256 行 / 174 KB** あるので、ここへ足すと全セッションのコンテキストを食う |
| **`rules/`** (ここ) | 参照されたときだけ | **dotfiles 固有**。zsh / tmux / この repo の CI に閉じた規範 |
| そのディレクトリの `CLAUDE.md` | そのディレクトリを触るとき | ディレクトリ固有の規約 (`scripts/` / `tests/` / `src/glogx/` / `_claude/`) |
| [`docs/`](../docs/) | 索引から選んで | 設計判断・仕様・調査記録 (規範ではなく「なぜこうなっているか」) |

判断の順: **他プロジェクトでも成立するか** → する なら `_claude/rules/`、しない なら ここ。
迷ったらここに置く方が安い (毎セッションのコンテキストを増やさない)。

## 一覧

| ルール | 何を禁じる / 要求するか | 参照元 (踏む場所) |
|---|---|---|
| [`zsh-hook-return-via-reply.md`](zsh-hook-return-via-reply.md) | precmd / preexec / zle から呼ぶ関数は `$(...)` でなく `REPLY` で返す。fork がそのまま体感レイテンシになる (実測 0.42ms/回 vs 0.03ms/回)。hook 本体に `local REPLY` を置く | `scripts/tmux_agent_panel.sh` / `scripts/tmux_periodic_save.sh` / `_claude/CLAUDE.md` の設計方針 |
| [`zsh-trap-not-inherited.md`](zsh-trap-not-inherited.md) | `trap '' SIG` はサブシェルとバックグラウンドジョブに**継承されない** (zsh の実装依存。**bash では動くので bash の常識で書くと踏む**)。`cd` したいならサブシェルを掘らず `-C` オプションで済ませる | `bin/lib/go_autobuild.zsh` |
| [`bench-watch-after-push.md`](bench-watch-after-push.md) | nvim / tmux / zsh / glogx 系を push したら、その commit の CI (Bench を含む全 run) の完了を watch してデグレを確認するまでがタスク。「そのうち通るはず」で終わらない | `tests/run_bench.sh` / `tests/bench_stats.sh` |

## 書き方

- **本文は規範だけ**にする。「なぜ / 起源 / 実例」は長くなるので、必要なら別ファイルへ分ける
  (`_claude/rules/` 側は `_claude/rules-rationale/` に分けている)
- **実測を数字で残す**。zsh の 2 本はどちらも実測が根拠 (fork の所要時間 / どの経路で trap が落ちるか)。
  数字が無い規範は、次に疑われたときに再測定のコストを丸ごと払う
- **足したらこの索引と、踏む場所の両方からリンクする**
  ([`new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md))
