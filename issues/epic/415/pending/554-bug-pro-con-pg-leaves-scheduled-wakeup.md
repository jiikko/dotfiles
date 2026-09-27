# 554 (bug): PG が作業を終える前に入れた起床の予約 (ScheduleWakeup) が残る

起票日: 2026-09-27

> **待ち (2026-09-28〜)**: 受け入れ条件 2 の確かめを待っている (実装は済み)。待っているもの: 指示を変えた後 (09-27 18:33) に pro-con が
> 起動した PG が、作業を終えること。来たら「進捗」の 1 行で、その PG の session に起床の予約 (`session_cron`) が残っていないかが分かる。
> 残っていなければ done、残っていれば指示が効いていない (PG の transcript で ScheduleWakeup を使った理由を見る)。
> epic の中の `waiting/` は issues viewer が対応していないので `pending/` に置いている

親: [415](../415-design-claude-pm-worker-orchestration.md)

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

- [x] PG への指示に起床の予約を使わないことが入る (`dispatcher.Prompt`)
- [ ] 足した後に起動した PG で、ScheduleWakeup が残らないことを 1 度確かめる

## 進捗

- 2026-09-27 (pro-con C-004): `dispatcher.Prompt` に「ScheduleWakeup / CronCreate / Monitor で自分を起こさない。使ったなら turn を終える前に取り消す
  (ScheduleWakeup は stop / CronCreate は CronDelete / Monitor は TaskStop)」の 1 行を、`card run` の行の後に足した。
  `TestPromptCarriesDiscipline` で守る (行を消すと FAIL することを確かめた)
  - commit: `pro-con: PG に起床の予約 (ScheduleWakeup / CronCreate / Monitor) を残させない (554)`
- 残り: 受け入れ条件 2 (足した後に起動した PG で予約が残らないこと) は、master へ入って dispatcher が新しい指示で PG を起こしてからでないと確かめられない。
  `~/.claude/jobs/<id>/state.json` の `inFlight` に `session_cron` が無いことを見る
- 2026-09-28 (dotfiles-01): 受け入れ条件 2 を確かめようとしたが、確かめる相手がまだ居ない。`~/.claude/jobs` に残る pro-con の PG の session
  (cwd が `.claude/worktrees/pc-c-*`) は 74 本で、最後に起動したのは 09-27 11:27 (C-102)。指示を変えた commit (09-27 18:33) より後に起動した
  PG は 0 本。dispatcher.log も 09-27 18:41 の停止で終わっている
  - 確かめ方 (次に pro-con で PG を 1 本以上動かして完了した後): 下の 1 行で、指示を変えた後に作った PG の session の `inFlight` を並べる。
    `kinds` に `session_cron` が無ければ条件 2 を満たす

    ```sh
    python3 -c 'import json,glob,os;[print(p.split("/")[-2],d.get("createdAt"),d.get("state"),(d.get("inFlight") or {}).get("kinds")) for p in glob.glob(os.path.expanduser("~/.claude/jobs/*/state.json")) for d in [json.load(open(p))] if "/pc-c-" in d.get("cwd","") and d.get("createdAt","")>"2026-09-27T09:33"]'
    ```

## 関連

- 551 (pro-con 側の受け止め) / `dispatcher/dispatcher.go` の `Prompt`
