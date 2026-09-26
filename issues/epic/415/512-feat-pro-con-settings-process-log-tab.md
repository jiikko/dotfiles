# 512 (feat): 設定画面に「ログ」のタブ — プロセスが起動した・落ちた・止めた・入れ替わったを syslog のように時刻順に見る

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼: 「設定の画面タブに syslog 相当のログが欲しいね。プロセスが起動したとか、kill されたとかね」。
2026-09-26 に supervisor の起こし直しを確かめたとき (506)、何が起きたかは `pro-con ps` と `pro-con log` を端末で打って追うしかなかった
(dispatcher が落ちた → 10 秒後に起こし直した / dispatcher が新版に入れ替わった / 見張りが起き直した)。画面の中で追えるようにする。

## 今あるもの (2026-09-26 に確かめた)

- 出来事は `events.jsonl` に dispatcher が書く (`src/pro-con/eventlog/eventlog.go`。書き手は dispatcher 1 つ)。`pro-con log` が読む
- 種類 (`Kind*`) のうちプロセスの出来事に当たるもの: `launch` (PG を起動・再開した / できなかった)・`stop`・`crash` (PG が落ちた)・
  `register`・`suspect`・`upgrade` (dispatcher が新版に入れ替わった)・`supervisor` (dispatcher が落ちた・起こし直した)・`recover`・`screens`・`watchdog`・`error`
- 設定画面 (`ui/settings.go`) のタブは「設定 / プロセス / ディスク」(456)。プロセスのタブは今の様子 (`pro-con ps` と同じ) で、履歴は出さない
- 実測の件数 (2026-09-26 の `events.jsonl`): launch 1255・suspect 1003・stop 718・apply 450・run 206・register 182 …。
  `launch` の多くは同じ失敗の繰り返し (例: 3 秒ごとの「repo の場所が設定に無い」) なので、そのまま並べると埋もれる

## 期待する動作

- 設定画面に「ログ」のタブを足す。新しい出来事ほど下 (または上。見本で決める) に、時刻・役 (dispatcher / supervisor / 見張り / PM / PG / 係)・何が起きたか・カードを 1 行ずつ
- 対象はプロセスの一生の出来事 (起動・落ちた・止めた・起こし直した・入れ替わった・取り込んだ)。カードの状態の変化 (`apply` 等) は出さないか、切り替えで出す
- 同じ出来事の繰り返しは 1 行に畳む (「… ×N 回、最後 21:01」)
- 開いている間に増えた出来事も出る (画面が既に見ている更新の合図に乗せる。描くたびにファイルを読み直さない)
- `pro-con log` と同じ出どころを使う (別の集め方を作らない)

## 対応方針

- 見た目 (列・色・並ぶ向き・畳み方) は本体へ入れる前に見本を並べて人に選んでもらう (`decide-layout-in-sample-renderer-first.md`。456 の設定画面と同じ進め方)
- 🚨 今は記録に残らない出来事がある: dispatcher 自身が起きた / 抜けた・見張りが起きた / 落ちたは `events.jsonl` に載っているか確かめ、
  載っていなければ書く側を足す (ログに出ないものは、タブを作っても見えない)。supervisor の出来事は受付の箱を経て次の dispatcher が書く (506) ので、
  dispatcher が居ない間の出来事は後から並ぶ

## 受け入れ条件

- [ ] 設定画面に「ログ」のタブがあり、プロセスの出来事が時刻順に出る。繰り返しは畳まれる
- [ ] dispatcher を kill -9 したとき、「落ちた」「起こし直した」がタブに出る (506 と同じ手順で確かめる)
- [ ] 見た目は人が見本から選んだもの
- [ ] 集め方は `pro-con log` と共通

## 関連ファイル

- `src/pro-con/ui/settings.go` (タブ) / `src/pro-con/eventlog/eventlog.go` (種類) / `src/pro-con/logcmd.go` (`pro-con log`)
- `src/pro-con/supervise.go` (supervisor の出来事) / `src/pro-con/monitorsup.go` (見張りの起動)

## 進捗

(まだ無い)
