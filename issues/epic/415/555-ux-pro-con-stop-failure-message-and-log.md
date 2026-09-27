# 555 (ux): 止めきれなかったときに同じ行が 14 本並び、案内の `claude stop <id>` では直らない

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

dogfooding (2026-09-27) で、終了のときに C-089 の PG を止めきれなかった (原因は 551 で直した)。そのとき見えたもの:

- dispatcher.log に「C-089 の PG (4bbb588c) がまだ動いていたので止め直した」が同じ時刻 (14:39:20) で 14 行並んだ。
  確かめの周 (`dispatcher/shutdown.go` の `ensurePolls` = 15) ごとに 1 行ずつ積み、まとめて書くので同じ時刻になる
- 画面の終わりに出た案内は「止める: pro-con dispatcher --stop / claude stop <id>」。`claude stop 4bbb588c` は `stopped` と rc=0 を返したが、
  一覧は working のままで、案内どおりにしても直らなかった

## 対応方針 (案)

- 同じ PG の止め直しは 1 行にまとめる (「C-089 の PG (4bbb588c) を 14 回止め直したが、一覧で止まったと確かめられない」)
- 止めきれなかったときの案内に、そのとき見えている様子 (pid の有無・一覧の state・Claude Code の記録の state) を添える。
  次に読む人が「プロセスは居ない」と分かれば、kill や再起動に進まずに済む
- 案内する操作は、その状態で効くものだけにする (効かないと分かっている `claude stop` を並べない)

## 受け入れ条件

- [x] 止め直しの記録が PG ごとに 1 行になる (`TestShutdownRestopNoteIsOneLinePerPG` / 途中で止める要求が失敗した形も `TestShutdownRestopNoteMergesPartialFailure`)
- [x] 止めきれなかったときの案内に、pid の有無と一覧・記録の state が出る (`TestShutdownAdviceFollowsSessionState`)

## 関連

- 551 / `dispatcher/shutdown.go` の `ensureStopped`

## 進捗

- 実装 (2026-09-27、pro-con C-003):
  - 「pro-con: 止め直しの記録を PG ごとに 1 行にし、止めきれなかった案内に pid・一覧・記録の state と効く操作だけを出す (issue 555)」
  - 「pro-con: 止め直しの途中で止める要求が失敗しても同じ PG の記録を 1 行にし、失敗の案内を確かめる段の結果どおりに書く (issue 555。codex の敵対的レビュー)」
- 形:
  - `ensureStopped` は止め直しを周ごとに出来事へ積まず、session ごとに数えて最後に 1 行出す
    (例: 「C-089 の PG (4bbb588c) を 16 回止め直したが、一覧で止まったと確かめられない」。止める要求の失敗は同じ行に添える)
  - 止まらなかった名指しに様子を添える: `C-089 (4bbb588c) [pid 無し・一覧 working・記録 無し: プロセスは居ない]` /
    `[pid 42・一覧 working: プロセスが居る]` / 記録が crashed・resuming なら「自動の再開の途中」
  - 終了の案内 (`Shutdown` のエラー) は様子ごとに効く操作だけ: プロセスの居ないもの = 「一覧の表示だけが残っている (claude stop を送っても変わらなかった。kill・再起動は要らない)」/
    自動の再開の途中 = 「再開を待ってから pro-con dispatcher --stop をもう一度」/ 止め直しの claude stop が失敗したもの = 「claude stop <id> を手で打って失敗の中身を見る」/
    プロセスの居るもの = 「claude stop を送っても止まらなかった (ps -p <pid> で様子を確かめる)」。前の一律の「pro-con dispatcher --stop / claude stop <id>」は外した
  - 記録の state を案内に出すため、`agents.List` が `~/.claude/jobs/<id>/state.json` を読む条件を「pid 無し・working」から「pid 無しで止まった形でない (working と知らない state)」へ広げた。
    `Stopped` が記録を見るのは working のときだけなので、判定は変わらない (知らない state に記録 done があっても止まったと読まない = `TestListReadsJobStateForPidlessWorking` の aaaa0007)
- 検証:
  - 変異 (bin/mutate-verify): 集約を外す / 部分失敗の添え書きを外す / 案内の 3 分岐それぞれを外す / List の読む条件を元に戻す・全部読む → すべて想定のテストが red
  - codex の敵対的レビュー (カードに添付): 指摘 2 件 (部分失敗で同じ PG が 2 行 / 確かめる段の送信だけを数えて「通らなかった」と言う) はどちらも実コードで成立したので 2 つ目の commit で直した。
    未確認リスク (別の版の state と記録の組み合わせ / 巨大な state.json) は発火条件が示されず、直していない
  - `CGO_ENABLED=0 make lint` 0 issues / `CGO_ENABLED=0 go test -count=1 ./...` (src/pro-con) rc=0 (21 パッケージ ok)。
    `-race` は回せていない: cgo のリンクで、今の ld が macOS SDK 27.0 の `arm64e.x1` を読めずに落ちる (golangci-lint のビルドも同じ。コードと無関係)
- 残り:
  - [ ] close.go (閉じた・削除・レビュー待ちで止めきれなかった) の案内は一律の「claude stop <id>」のまま。名指しには様子が付くようになったので、C-089 の形ではその案内と食い違う
    (Shutdown の案内をそこへ使い回すかは close の段で別に決める)
