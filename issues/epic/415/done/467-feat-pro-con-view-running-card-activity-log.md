# 467 (feat): 作業中のカードの PG が何をしているか (出力と道具の呼び出し) を、画面と CLI で読むだけで追える

起票日: 2026-09-25

親: [415](../415-design-claude-pm-worker-orchestration.md)

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

## 実装 (2026-09-26 / カード C-034)

- 読む部品は `src/pro-con/live/activity.go` の `CardLog`: 起動の記録 (sessions.json) と sessions-retired.json から
  **CardID がそのカードの行** (CardID の無い古い行は短い id がカードの今の session と同じものだけ) を拾い、
  各 transcript を読んだ位置の続きから読む (書きかけの最後の行は書き終わってから)。全 session の活動を時刻で並べる
- 出すのは assistant の応答の文と道具の呼び出し (`Bash: <コマンド>` / `Edit: <worktree からの相対パス>` / Grep・Glob は型 /
  Agent は説明 …)。**道具の結果は出さない** (見本で決める、とした点。何をしたかは呼び出しで分かり、結果は長い)。
  道具の要点は 300 字で切り、`termsafe.PlainLine` で制御文字を落とす
- 画面: 詳細の引き出しの末尾の節が「活動」になる (`backend.ActivityReader` を持つ本物・`--view`。模擬は今までの「出力」)。
  開いたとき・1 秒の tick・dispatcher の知らせで裏で読み直し (同時に 1 本)、本文の末尾を見ていれば足された分を追う。
  session の境目に `── 再開: session <id> ──`。画面が持つのは末尾 1000 件
- CLI: `pro-con card log <カード> [--follow] [--json]`。session ごとに `── session <id> ──` を名乗り、
  `01-02 15:04:05  Bash: go test ./...` の形。`--json` は 1 行 1 活動。`--follow` は読み直しの間隔 (1 秒) で追い、ctrl+c で rc=0
- 読むだけの担保: `TestViewCommandsDoNotWrite` に `card log` を足した (状態の置き場・transcript の置き場・socket)

### 敵対的レビュー (2026-09-26) で直したもの

- 🚨 **再開した session の transcript は、前の session の assistant のレコードを同じ uuid・時刻のまま写して始まる**
  (実データで確認: C-003 の再開後の session に、退いた session の 107 件が uuid ごと全部あった)。
  uuid で 1 度だけ出し、起動の古い順に読む (元の session の名で出る)。直す前は `card log C-003` が同じ活動を 3 回・session の見出しを 254 回出した
- 後から見つかった前の session の分は画面の側で時刻の位置へ差し込む / transcript が消えたら探し直して頭から、短く書き直されたら頭から (uuid で重ねない)
- `--follow` は 1 本の transcript が読めなくても止めず、理由を 1 度だけ stderr に出して追い続ける
- 画面: 開き直したら前の活動を出さずに読み直す / 途中を読んでいる間に上限 (1000 件) で頭が捨てられても位置を保つ
