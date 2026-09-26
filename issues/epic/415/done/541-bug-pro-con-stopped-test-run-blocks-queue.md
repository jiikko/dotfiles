# 541 (bug): テストの係の実行が止まった (STAT T) まま、最大 1 時間ほかのカードの実行を塞ぐ

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

2026-09-26 21:39、テストの係の実行 (C-071 の `make test`) のプロセスグループが SIGTTIN で丸ごと止まり (STAT `T`)、そのまま残った (518 の「巻き添え」)。
テストの係は 1 本ずつ順に走らせるので、C-070 / C-072 / C-074 の実行が待たされ続けた。止まったグループには SIGCONT が来ない (dispatcher は別 session の親)。

- 今の上限は `dispatcher/runner.go` の `runTimeout = time.Hour`。止まった実行は、1 時間たつまで順番を塞ぐ
- 518 の直し (`Setsid` で端末から切り離す) で、端末の前面を取られて止まる経路は塞いだ。ただ、ほかの理由で止まる形 (外からの SIGSTOP・SIGTSTP、テストが自分を止める) は残る

## 期待する動作

- テストの係が、実行のグループが止まっている (全部 STAT `T`) のを見つけたら、数十秒待っても続かなければ止め直し (グループへ SIGCONT → SIGTERM → SIGKILL)、「止まっていたので止めた」として失敗を PG に返す
- 出来事に残す (`pro-con log` の kind `run`)。順番を待っているカードは次へ進む
- 止まっているかの判定は、`kill(pid, 0)` ではなく実際の状態 (ps の STAT 相当) で見る

## 受け入れ条件

- [x] 実行のグループを SIGSTOP で止めると、上限 (1 時間) より十分前に止め直され、PG に失敗として返り、次の実行が始まる (偽の時計か短い待ちでテストする。実時間を待たない)
- [x] 止め直したことが出来事に残る

## 関連

- 518 (画面が SIGTTIN で止まった件。巻き添えでこの形が出た) / 471 (重い処理の直列) / `src/pro-con/dispatcher/runner.go`

## 進捗

- 2026-09-27 (C-099) commit「pro-con: テストの係は、実行のグループが止まったまま (全部 STAT T) 30 秒続かなければ止め直して失敗として返す (issue 541)」:
  `ExecRunner.Run` に見張り (`watchStopped`) を足した。5 秒ごとに `ps -A -o pgid=,stat=` で、頼まれたコマンドのグループ
  (lock を取るときは bash が書いた pgid。それが居なくなった後は lockman のグループ) を見て、ゾンビ以外が全部 STAT `T` のまま 30 秒続いたら
  `errRunStopped` を原因に取り消す。取り消しは SIGTERM の直後に SIGCONT を送る (止まったグループは続けないと SIGTERM を受け取らない。
  期待する動作の「SIGCONT → SIGTERM」とは順が逆だが、先に届いた SIGTERM を続けた瞬間に処理させる方が確実) → WaitDelay の後に SIGKILL。
  PG にはログの末尾つきの失敗 (「止まったまま (STAT T) 続かなかったので止め直した」) を返し、出来事 (kind `run`) にも同じ趣旨の文を残す
  - 確かめたこと (`src/pro-con/dispatcher/stopped_test.go`、待ちは 200ms に縮めて実時間を待たない): 外からグループへ SIGSTOP → 止め直して
    `errRunStopped`・子も残らない・bash の TERM の trap が走る (lockman あり / なしの両方。lockman ありは止め直した後に lock が空くことも) /
    一部だけ止まっているのは止め直さない / 判定表 (`stoppedIn`) / dispatcher 側で PG に返り・出来事に残り・順番待ちの次のカードが始まる。
    `-count=10` で安定。変異 (判定を偽・SIGCONT を外す・出来事の文を戻す・ゾンビを数える) で各テストが落ちるのを確かめた
  - 🚨 テストの中の `kill -STOP 0` で止めると、bash だけ止まって直前に起こした子 (`sleep`) が `SN` のまま残ることがある (macOS で実測)。
    グループの全部は止まっていないので正しく止め直さない。テストは外から `kill(-pgid, SIGSTOP)` する形 (541 の形) にした
  - codex の敵対的レビュー 2 回 (1 回目は表層で止まったので攻め口を 5 つ足して回し直した): 採ったのは 1 件。lock を取る実行で、前の実行が残した
    `<log>.pgid` を、この実行の bash が書く前に見張りが読み、残った値のグループが止まっていれば新しい実行を止め直して別のグループを撃つ。
    起動前に `.pgid` を消す形で直し、`TestExecRunnerIgnoresLeftoverPgidFile` で固定した (消す行を外すと落ちる)。
    log の名前は「カード-unix 秒-置き場の印」なので同じ名前の再実行はまず起きないが、取り消し (cmd.Cancel) が同じ値を撃つ既存の形も同時に塞がる
  - `make test` (テストの係、6m36s): rc=2。`pro-con/dispatcher` を含む go test はすべて ok。落ちた 3 件はどれもこの変更の外で、基点 (eeef7699) から在ったもの:
    `test_human_issues_have_deadline.sh` (545 の `期限:`。origin/master では 545 が done へ移って消えた) /
    `test_enumerations_are_derived.sh` (`src/pro-con/samples/537-picker-cards/gen.sh` が LINT_DIRS の外) /
    `test-unused-excluding-tests` (`src/pro-con/store/loadcache.go:loadDecodes` が production から到達できない。528 の 8282a8cc)。後の 2 件は origin/master でも残っている
  - 残る形 (範囲外): 子が setsid / setpgid で自分のグループを作って止まり、bash がそれを待つ (S) 形は検出しない (グループごと撃つ方式の既知の制限と同じ。
    `runScript` の注記)
