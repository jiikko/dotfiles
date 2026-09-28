---
paths:
  - "_claude/skills/**"
  - "_claude/skill-evals/**"
---

# skill を変えたら、commit の前に `bin/skill-eval` で評価する

## ルール

- **`_claude/skills/<skill>/` を変えたら、commit の前に `bin/skill-eval <skill>` を回し、結果の行を commit message に書く**
  (`PASS fable score=1 delta=1 cases=1/1 cost=$0.15` の 1 行)。引数を省くと `origin/master` から変わった skill を全部回す
- **ケースが無い skill (`NO-EVAL`・rc 3) は「通った」と書かない**。その skill を変えたなら、まず `_claude/skill-evals/<skill>/<case>/` に
  ケースを 1 本足す。最初の 1 本は「発火するか」(grader `type: tool_used` / `tool: Skill` / `input_match: <skill>`) でよい。
  変えた振る舞いを見るケース (`regex` / `llm` grader) は、その変更が振る舞いを変えるときに足す
- **文面の手直しだけ (typo・リンク・言い回し) なら回さなくてよい**。回さなかったことを commit message に 1 行書く
- **CI では回さない** (2026-09-29 決定。実行のたびにモデルを呼び、費用と 5h 枠を使うため)。ローカルで手で回す
- **回す前に枠を見る**。`bin/skill-eval` は `ratelimit -source claude -check` が超過を返すと rc 4 で止まる。
  `--force` で上書きするのは、ユーザーが続行を指示したときだけ
- **rc 1 (FAIL) を見たら、`tmp/skill-eval/<日時>-<乱数>/<skill>/report.html` で grader の判定を読んでから直す**。
  結果は揺れるので、1 回の FAIL / PASS だけで skill の良し悪しを決めない (`--runs` を増やして割合で見る)

## 何を測っていないか

- 各 run は隔離された HOME で動き、**ユーザーの CLAUDE.md・rules・他の skill は読み込まれない**。
  「他の指示と組み合わさったときの振る舞い」はこの評価の外
- skill が呼ぶ外部コマンドは、既定では許可されない (`claude plugin eval` の `--allow-tools` を渡していない)。
  Bash を使う手順を持つ skill は、手順の途中までしか走らない

## なぜ

prompt-audit (2026-09-29) で「skill の書き方を変えても、効き目を測る手段が無い」と分かった。静的な検査
(パスの実在・リンク) は lint で見られるが、発火するか・書いたとおりに動くかは、モデルに読ませないと分からない。
`claude plugin eval` は `.claude-plugin/plugin.json` を持つディレクトリしか評価しないので、
`bin/skill-eval` が skill ごとに一時的な plugin を組み立てて渡す (仕組みと終了コードはスクリプト冒頭)。
