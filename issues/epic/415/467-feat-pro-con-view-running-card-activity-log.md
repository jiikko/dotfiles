# 467 (feat): 作業中のカードの PG が何をしているか (出力と道具の呼び出し) を、画面と CLI で読むだけで追える

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-25): 「作業中のレーンにあるカードを思考ログとか、アウトプットが見れないのだが、見るのは無理？」。
今見えるのは、カードの出力の末尾 3 行 (`live.go` の `cards[i].Log = tail(t.Outputs, 3)`) と `pro-con card show` の出力の末尾だけ。
`a` (attach) は PG の session に入る口で、打てば PG に届く (読むだけではない)。`--view` では断る。

## 分かっていること (2026-09-25 に PG の transcript で確かめた)

- transcript (`~/.claude/projects/<worktree>/<session>.jsonl`) には、応答の文 (text)・道具の呼び出し (tool_use。Bash のコマンド・読んだファイル・編集)・道具の結果が残る
- **思考 (thinking) はほぼ残らない**: C-010 の PG の transcript で thinking のブロック 25 個のうち 24 個が中身 0 文字、1 個が 179 文字。
  思考の中身を出すのは原理的に難しい (Claude Code が中身を保存していない)。代わりに「何をしたか」(道具の呼び出し) を出す

## 期待する動作

- 作業中のカードを選んで開くと、その PG の活動 (応答の文と道具の呼び出しを時刻の順に。例 `20:31 Bash: go test ./dispatcher/...` / `20:32 Edit: close.go`) を
  スクロールして読める。新しいものが来たら追う (socket の知らせか transcript の追記で)
- 🚨 読むだけ: `--view` でも使える。受付の箱・記録・PG の session に何も書かない (445 の `TestViewCommandsDoNotWrite` の形で固定)
- CLI にも同じ口 (`pro-con card log <カード> [--follow]` など)。外の Claude (441) からも読める
- 再開で session が入れ替わっても (sessions-retired.json)、同じカードの前の session の活動から続けて読める
- 道具の結果の本文は長いので、1 行に畳むか省く (出すかは見本で決める)

## 関連

- 415 の要件 8 (「カードを選ぶと、履歴とそのタスクのログが見られる」) を満たす部品 (467 = ログ / 469 = 進捗 / 473 = 今走っているもの)

- 441 (viewer) / 444 (dispatcher の出来事の記録。こちらは PG の側の活動) / 445 (読むだけの担保) / 431 の前提 (transcript の置き場の決め打ち)
