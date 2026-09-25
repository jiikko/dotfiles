# 444 (feat): 出来事の記録 — dispatcher の判断を構造化して残し、外から読む

起票日: 2026-09-25

親: [441](441-design-pro-con-viewer.md)

## 概要

dispatcher が何を判断したか (箱の依頼を適用した / PG を起動・再開・停止した / 取り込んだ / 止め直した / 枠で絞った / watchdog / 画面の数)
を、あとから・外から読める形で残す。今は Tick の notes を標準出力に出すだけで、画面が起こした dispatcher なら dispatcher.log、
手で起動したら nohup の先に自由文で残る (2026-09-25 の dogfooding で、止め直しが続く理由を log を tail して推理した)。

## 対応方針 (候補)

- 状態の置き場に `events.jsonl` (1 行 1 出来事: 時刻・種類・カード・session・理由)。大きさの上限で回す
- `pro-con log [--card <カード>] [--follow] [--since <時刻>]` が読む。`--follow` は socket の購読で即時
- 画面の側の出来事 (開いた・閉じた・quit で止めた / 止めなかった) も同じ記録へ (受付の箱を通さず追記でよいかは 445 で決める)

## 決めたこと (2026-09-25。カード C-006)

- **書くのは dispatcher だけ** (lock を持つプロセス。カードの記録と同じ = 426 の決定 1)。`dispatcher --stop` は、動いている dispatcher に
  頼むだけのときは書かず、居なくて自分が dispatcher の役 (lock) を取ったときだけ書く。書き手が 1 つなので、追記と回しが競らない
- **置き場は状態の置き場の `events.jsonl`**。1 行 1 出来事の JSON: `at` (時刻)・`kind` (種類)・`card`・`session`・`reason` (人が読む 1 文。
  dispatcher のログの行と同じ文)。種類は `apply` / `reject` / `register` / `suspect` / `crash` / `watchdog` / `run` / `launch` / `hold` / `stop` /
  `screens` / `error` (正本は `src/pro-con/eventlog/eventlog.go`)。成功した適用 (`apply`) も今回から出来事にした (前は除けたものだけがログに出た)
- **上限は 1 MiB で、超えそうになったら `events.1.jsonl` へ回す (1 つ前は捨てる)**。理由: 1 行はおよそ 150〜400 バイトで、書くのは何かしたとき
  だけ (Tick ごとではない) なので 1 MiB で数千件 = 数日ぶんの判断が残る。2 ファイルで最大 2 MiB に収まり、`pro-con log` が毎回全部読んでも待たない。
  日付で回す形にしなかったのは、止め直しが続く日だけ膨らむ形 (2026-09-25 の dogfooding) でも上限が効くようにするため
- **権限はファイル 0600 / ディレクトリ 0700** (前から緩い権限で在ったファイルも追記のときに 0600 へ直す)
- **出来事は画面への知らせ (Broadcast) より先に書く** (`Dispatcher.Record` を Changed の前に呼ぶ)。知らせで読みに来た `--follow` が、その出来事をもう読める
- 画面の側の出来事 (開いた・閉じた・quit で止めた / 止めなかった) は**今回は入れない**。受付の箱を通さず画面から追記してよいか
  (= 書き手を dispatcher 1 つに保つか) は 445 の判断待ち。入れるなら、画面は受付の箱へ「出来事」を置き dispatcher が書く形なら書き手は 1 つのまま

## 進捗

- [x] 2026-09-25 (カード C-006): 実装。`src/pro-con/eventlog/` (追記・回し・回されても取りこぼさずに読み進める Follower) /
  dispatcher の notes を `[]string` から出来事 (種類・カード・session・理由) に替え、Tick と Shutdown が `Record` へ渡す /
  `dispatchercmd.go` の `eventSink` が標準出力 (dispatcher.log・nohup の先。前と同じ `HH:MM:SS 文` の行) と events.jsonl の両方へ書く /
  `pro-con log [--card] [--follow] [--since] [--json]` (`logcmd.go`)。`--follow` は package wake の購読 (sub) で即時、届かなくても 1 秒ごとに読み直す。
  README の行と `pm-guide.md` の役目 6 に足し、`TestPMGuideCommandsParse` が guide の `pro-con log` も使い方の誤りにならないかを見る
- [x] 確かめたこと: `TestLogDoesNotWrite` (`TestViewCommandsDoNotWrite` と同じ形。log の全 variant と `--follow` の前後で置き場の中身・権限・mtime・数が
  変わらず、socket に届いた行が `sub` だけ) / `TestRunDispatcherWiresSocket` (本物の配線の runDispatcher が、適用した出来事を 0600 の events.jsonl に書く) /
  回し・権限・Follower の回しまたぎ・書きかけの行 / Record が Changed より先 / Shutdown の出来事。`bin/mutate-verify` で変異 13 本がすべて red
- [x] 敵対的レビュー (Opus・読むだけ): P0〜P2 は無し。P3 のうち 4 件を直した — 末尾が行の途中で切れた記録に足すと最初の 1 件が壊れた行に
  つながって消える (再現して直し、テストと変異で固定) / serve の出来事と Shutdown の後に知らせが無く `--follow` が 1 秒遅れる / Tick の失敗・止めきれないが
  stdout と stderr の両方に出て dispatcher.log に 2 行入る / Follower の最初の読みの直後に回されると旧ファイルを 2 度出す
- 記録のみ (直していない):
  - socket の置き場のパスが長くて `/tmp/pro-con-<uid>/` へ逃がす場合、購読の前に `ensureFallbackDir` がそのディレクトリの緩い権限を 0700 に直す
    (状態の置き場の外の metadata を書く。`card wait` と同じ経路で元から在る。`TestLogDoesNotWrite` は見ていない) → 445 の範囲
  - 1 回に渡す出来事が MaxBytes を超えると 1 ファイルが上限を超える (1 Tick の出来事はその大きさにならないので実害は無い)
  - 読み進める間に 2 回回ると、1 つ前の回した分を失う (1 MiB を 1 秒で書くことは無い)
- [x] 2026-09-25: `make test` rc=0 (テストの係。lint 込み・6m30s)
- [x] 2026-09-25 差し戻し (レビュー): origin/master (447 の閉じたら PG を止める・443 の `pro-con screen`) に rebase。main.go の振り分けは
  `screen` と `log` を両方残し、shutdown.go の `ensureStopped` は 447 の引数 (cards / polls / sent) のまま返す notes を出来事にした。
  close.go の `stopClosed` も出来事 (種類 `stop`・カード・session) に揃え、「閉じたので止めた / 既に止まっていた / 止められない」が events.jsonl に出る
  (`TestCloseStopRecordsEvents`)。テストの重複 (`syncBuf` が 443 の screencmd_test.go と同名) は 443 の方を使う。
  rebase 後に変異を当て直して 16 本 (前の 13 本 + close.go の 3 本) がすべて red。`go test -race ./...` は 447・443 のテストを含めて緑
- [x] 2026-09-25: 差し戻しの後の `make test` rc=0 (テストの係。4m16s)
- [ ] 残り: 画面の側の出来事 (開いた・閉じた・quit で止めた / 止めなかった) は 445 の判断待ち (上の「決めたこと」)
