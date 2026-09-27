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

## 受け入れ条件

- [ ] 上の 3 つを実測し、結果を本文に書く
- [ ] 予約が残った session を止めたら、pro-con が止め終えたと読む (終了で「止めきれなかった」が出ない)
- [ ] kill -9 の直後の自動の再開の途中は、これまでどおり止まったと読まない

## 関連ファイル

- `src/pro-con/agents/agents.go` (`Session.Stopped` / `UnknownState`)
- `src/pro-con/dispatcher/shutdown.go` (`ensureStopped`)

## 進捗

- 起票のみ
