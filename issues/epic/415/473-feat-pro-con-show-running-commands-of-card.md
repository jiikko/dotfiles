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

- 415 の要件 8 (「カードを選ぶと、履歴とそのタスクのログが見られる」) を満たす部品 (467 = ログ / 469 = 進捗 / 473 = 今走っているもの)

- 467 (活動の履歴) / 469 (カードを開いたら進捗) / 471 (重い処理の直列化。今走っているものが見えると、並んでいる負荷も見える)

## 進捗 (C-037 の PG, 2026-09-26)

- [x] 実装 (branch `worktree-pc-c-037`): dispatcher が 10 秒ごとに集めて `…/live/doing.json` に書く (`dispatcher/doing.go`)。画面の詳細・`card show` (`--json` は `doing` / `doingAt`)・ボードのバッジ (`▸ mut.sh 13分`) は読むだけ
  - 集めるもの: 一覧と記録で pid が一致した PG の session の**子孫のプロセス** (Claude Code の Bash の包み `zsh -c … eval '<コマンド>'` は中のコマンドで出す。caffeinate は除く)・transcript の末尾の**裏のサブエージェント** (起こした結果の `toolUseResult.isAsync/agentId` から、終わりの知らせ `<task-notification>` の status が running 以外で消す。`~/.claude/jobs/<短い id>/state.json` の `inFlight.kinds` に `local_agent` が無ければ出さない)・**結果待ちの道具の呼び出し** (Bash はプロセスとして出るので ps が読めたときは重ねない)。テストの係の順番待ち / 実行中はカードの記録から同じ節に足す
  - 1 分集め直されていなければ、詳細は「N 分前に集めたまま。dispatcher が止まっている?」と添え、ボードには出さない
- [x] 実測 (Claude Code 2.1.282): 自分の session (pid = 記録の worker `claude bg-spare`) の子孫を実物の ps で読ませ、包みの取り出し・子孫の木・jobs の種類が読めることを確かめた。ps は 1 回 0.04 秒
- [x] make test rc=0。変異 5 本すべて想定のテストが red (pid の一致の検査を外す / assistant の行でも知らせを探す / ボードの古さの判定を外す / 包みの重複の畳みを外す / jobs の種類の判定を外す)

### 分かっている穴 (やっていないこと)

- PG の外へ抜けたプロセス (`nohup … &` / setsid で親が launchd に移ったもの) は出ない。issue の案の「cwd で拾う」は、人が worktree で開いた shell まで PG のものとして出すので採らなかった
- 裏のサブエージェントは transcript の末尾 512KB で起こした記録を探す。それより前に起こしたものは名前なしで出す (jobs の state.json が走っていると言うとき)
- 知らせの形 (queue-operation / attachment に載る) と state.json の形は 2.1.282 の実測。版で変わると、サブエージェントは state.json の判定だけに倒れる
- 敵対的レビュー (読むだけのサブエージェント, 2026-09-26) で直したもの: ps を読む前に PG が落ちて pid が使い回されたら外のプロセスの木を出す (同じ ps の中で pid が claude かを確かめる) / 一覧と pid が合わない session の transcript の残り (道具・サブエージェント) を出していた / 完了した直後のカードに前の様子を足していた / ps に上限の時間が無かった (5 秒) / ps の行の区切りの数え方の食い違いで panic しうる
- 記録だけにしたもの: サブエージェントが終わった直後、jobs の state.json が追いつくまでの最大 10 秒、「transcript の末尾に起こした記録が無い」と出る (次に集めたとき消える) / Bash の包みの `'\''` を文字列ごと置き換えるので、引数にその 4 文字を含むコマンドは表示が崩れる (表示だけ)
