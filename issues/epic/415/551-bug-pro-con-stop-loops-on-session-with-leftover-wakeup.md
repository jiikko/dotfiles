# 551 (bug): 予約した起床が残った PG の session を、止め終えたと読めずに止め直し続ける (終了で「止めきれなかった」)

> 🚨 **担当中: dotfiles の issue 551 を実装しているセッション (worktree wt-551)**（2026-09-27〜）

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの報告 (2026-09-27): pro-con を止めたら次が出た。

```
pro-con: 終了のときに止めきれなかった: exit status 1: pro-con dispatcher --stop: pro-con が起動した PG のうち 1 本が止まっていない: C-089 (4bbb588c) (止める: pro-con dispatcher --stop / claude stop <id>)
```

## 事実 (2026-09-27 14:40 頃に観測。Claude Code 2.1.283)

- C-089 の PG `4bbb588c` のプロセスは無い (`ps` に出ない)。作業は 09-27 01:54 (JST) に終わり、カードは完了
- `~/.claude/jobs/4bbb588c/state.json` は `state: done`。`inFlight` に `session_cron` が 1 つ (`wake.at` = 02:08、`fires: 0`)。
  終わる前に入れた「起床の予約」が、1 度も発火しないまま残っている
- `claude agents --json` / `--all` はどちらも `state: working`・pid 無しを返し続ける
- `claude stop 4bbb588c` は stdout `stopped 4bbb588c`・rc=0 を返すが、直後の `claude agents --all` も `working` のまま (`state.json` は `done` のまま)
- dispatcher.log: 14:39:20 に「C-089 の PG (4bbb588c) がまだ動いていたので止め直した」が 14 行、最後に「止めきれない」

## 原因 (推定)

`agents.Session.Stopped` (src/pro-con/agents/agents.go) は「pid 無し・working」を、kill -9 の直後に Claude Code が自動で再開する途中と読む
(425 の実測)。予約が残った session もこの形で報告されるので、pro-con は止まったと確かめられず、止め直しを繰り返して諦める。

## 対応方針 (実測してから決める)

- 「pid 無し・working」のときだけ、Claude Code の記録 (`~/.claude/jobs/<id>/state.json`) も見て、そちらが止まった形なら止まったと読む案
- 🚨 この判定は kill -9 直後の自動の再開を守るためのもの。先に次を実測する (判定を足すと、再開の途中の PG を止まったと読んで放置する逆向きの事故になりうる):
  1. 作業中の session を kill -9 した直後: agents と state.json の state
  2. 作業を終えた (done) session を kill -9 した直後: 自動で再開するか・agents と state.json の state
  3. 起床を予約して終えた session を claude stop した後: agents と state.json (本件の再現)

## 実測 (2026-09-27 15:00〜15:15。Claude Code 2.1.283・haiku・`claude --bg --setting-sources ""`・cwd は ~/dotfiles/tmp/m551)

| 場合 | agents (`--all`) | `~/.claude/jobs/<id>/state.json` の state |
|---|---|---|
| 作業中に kill -9 | pid 無し・working → 約 10 秒後に新しい pid で busy / working (自動で再開) | `crashed` → `resuming` → `working` |
| 作業を終えた (done) 後に kill -9 | pid 無し・done のまま (40 秒待っても再開しない) | `done` |
| ScheduleWakeup で起床を予約して終え、`claude stop` | pid 無し・done | `done` (`inFlight` は空のまま) |
| C-089 の実物 (予約が残った) | pid 無し・working (claude stop の後も) | `done`、`inFlight` に `session_cron` 1 件 |

- 3 行目は C-089 の形を再現しなかった (予約が `inFlight` に載らなかった。いつ載るのかは未確認)。それでも「agents は working・記録は done」は C-089 の形でしか出ず、
  自動の再開の途中は記録が `crashed` / `resuming` なので区別できる
- 予約がプロセスの無い後に発火して再開するか: C-089 の実物で、02:08 の起床は 13 時間過ぎても `fires: 0` のまま (発火していない)
- 測定の session 4 本は `claude rm` で消し、残ったプロセスが無いことを確認。空の `~/dotfiles/tmp/m551` だけ残った (消そうとしたら作業ディレクトリの削除として安全装置に止められた)

## 受け入れ条件

- [x] 上の 3 つを実測し、結果を本文に書く
- [x] 予約が残った session を止めたら、pro-con が止め終えたと読む (`TestShutdownAcceptsPidlessWorkingWithDoneJob`)
- [x] kill -9 の直後の自動の再開の途中は、これまでどおり止まったと読まない (記録が crashed / resuming・記録が無い・壊れているは止まっていない。`TestListReadsJobStateForPidlessWorking`)
- [ ] 取り込み後、本物の pro-con を止めて「止めきれなかった」が出ないことを確かめる (人の手。下の残り)

## 関連ファイル

- `src/pro-con/agents/agents.go` (`Session.Stopped` / `UnknownState`)
- `src/pro-con/dispatcher/shutdown.go` (`ensureStopped`)

## 進捗

- 起票 (2026-09-27)
- 実装 (2026-09-27): 「pro-con: 起床の予約が残った PG を止め終えたと読む (Claude Code の記録が done なら。issue 551)」
  - `agents.List(ctx, run, jobsDir)`: pid 無し・working の session だけ `<jobsDir>/<id>/state.json` の state を `Session.JobState` に読む。
    `Session.Stopped` は pid 無し・working でも JobState が done なら真。一覧を読む 4 か所 (dispatcher の List / ListAll・worktree clean・画面) は同じ置き場を渡す
    (引数にしたので、渡し忘れはコンパイルで止まる)
  - 短い id の形の確かめを `agents.ShortID` の 1 か所にし、wtclean もそれを使う
  - 敵対的レビュー (sonnet 1 体。週の枠が 99% のため opus ではなく sonnet): P1 なし。採用: 終了の段の回帰テスト・id の形を既存と揃える・同じ式の重複。
    「作業を終えた session の kill -9 を測っていない」は測定済みを本文に書いていなかっただけ (上の表)。「予約が後で発火して再開するのでは」は C-089 の実物 (fires: 0) を書いた
  - 変異 (bin/mutate-verify): working のとき記録を見る分岐を外す → TestListReadsJobStateForPidlessWorking と TestShutdownAcceptsPidlessWorkingWithDoneJob (報告と同じ文面の誤り) /
    pid のある session も読む → 前者 / id の形の確かめを外す → 前者 (初回は fixture が置き場の中に置いていて緑。../x が指す置き場の 1 つ上へ置き直して red)。すべて red
  - make lint 0 issues / go test -race ./... (src/pro-con) rc=0
- 残り:
  - [ ] 取り込み後、本物の pro-con を止めて「止めきれなかった」が出ないことを確かめる。今の dispatcher は C-089 (4bbb588c) を止め終えたと読めずにいるので、
    新版へ入れ替わった後にもう一度止めれば確かめられる
