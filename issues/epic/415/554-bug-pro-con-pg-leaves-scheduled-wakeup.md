# 554 (bug): PG が作業を終える前に入れた起床の予約 (ScheduleWakeup) が残る

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

dogfooding (2026-09-27) で分かったこと。C-089 の PG は作業の途中で ScheduleWakeup を 3 回使い、最後に
「背景の撮影とレビューの通知が来なかったときの保険」として 02:08 の起床を入れたまま作業を終えた。
その予約が `~/.claude/jobs/4bbb588c/state.json` の `inFlight` に `session_cron` として残り、Claude Code (2.1.283) は
プロセスが無くても `claude agents` で working を返し続けた。pro-con は止め終えたと読めず、終了で「止めきれなかった」になった
(pro-con 側の受け止めは 551 で直した)。予約は 13 時間過ぎても発火していない (`fires: 0`)。

## 今の形

- PG への指示 (`dispatcher.Prompt`) には、質問は `pro-con card ask`・時間のかかるコマンドは `pro-con card run` で頼んで turn を終える、とあるが、
  ScheduleWakeup / Monitor / CronCreate の扱いは書いていない
- dispatcher は、回答・run の結果・差し戻しで PG を再開する。PG が自分で起床を予約する必要は無い

## 対応方針 (案)

- PG への指示に「ScheduleWakeup / CronCreate で自分を起こさない (再開は dispatcher がする)。使ったなら turn を終える前に取り消す」を足す
- 足した後、PG の transcript で ScheduleWakeup の使用が減ったかを見る (C-089 の transcript: ScheduleWakeup 3 回・Monitor 2 回)
- Claude Code 側の挙動 (予約が残った session を、プロセスが無くても working と返す) は上流の挙動なので、ここでは記録だけにする

## 受け入れ条件

- [ ] PG への指示に起床の予約を使わないことが入る (`dispatcher.Prompt`)
- [ ] 足した後に起動した PG で、ScheduleWakeup が残らないことを 1 度確かめる

## 関連

- 551 (pro-con 側の受け止め) / `dispatcher/dispatcher.go` の `Prompt`
