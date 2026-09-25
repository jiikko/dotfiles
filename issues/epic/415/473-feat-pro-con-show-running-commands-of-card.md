# 473 (feat): 作業中のカードで、PG が今走らせているコマンド (裏の作業を含む) と経過を見る

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-25): C-009 (445) が 23 分作業中のとき「今何をやっているんだろうね？この claude のログを見たいなぁ。実行中のコマンドも見たいなぁ」→「issue を作成しといて」。
PM (人間の代わりの Claude) が手で調べると、C-009 の PG は変異の検証のスクリプト (`~/.claude/jobs/<session>/tmp/mut.sh`。13 分 54 秒経過) と、その中の
`bin/mutate-verify --name hints-ro --file src/pro-con/ui/view.go …` を裏で走らせ、読むだけの敵対的レビュー (サブエージェント) の結果も待っていた。
画面にもカードの出力の末尾にも出ていなかった (出力の末尾は「変異検証とレビューが裏で走っています」の 1 文だけ)。

## 期待する動作

- 作業中のカードで、その PG が今走らせているコマンドを経過つきで見られる: Bash のコマンド・裏で走らせた作業 (background の shell・サブエージェント)・テストの係に頼んだコマンド (順番待ち / 実行中)
- 出す場所: カードの詳細 (469 の進捗と同じ所) と `pro-con card show`。ボードのカードにも 1 行 (例 `mutate-verify 13 分`)
- 🚨 読むだけ: `--view` でも出す。PG の session にも受付の箱にも何も書かない
- 🚨 集め方は、pro-con が起動した PG の分だけに絞る (その PG の worktree を cwd に持つプロセス・その session の jobs の置き場)。pro-con の外の session のプロセスを出さない
  (README の s の行と同じ「pro-con の外の session は名前も本数も出さない」)
- 集めるのは dispatcher か読む口の側 (画面が数秒ごとに ps / lsof を重く回さない。441 の「人間の画面を遅くしない」)

## 手で調べたときのやり方 (2026-09-25。参考)

- `lsof -d cwd -Fpn` で cwd が `.claude/worktrees/pc-c-009` のプロセスを拾い、`ps -o pid,etime,command` で並べた
- transcript (`~/.claude/projects/<worktree>/<session>.jsonl`) の最後の道具の呼び出し (tool_use) からも、今走らせているコマンドが分かる (467 の活動ログと同じ出どころ)

## 関連

- 467 (活動の履歴) / 469 (カードを開いたら進捗) / 471 (重い処理の直列化。今走っているものが見えると、並んでいる負荷も見える)
