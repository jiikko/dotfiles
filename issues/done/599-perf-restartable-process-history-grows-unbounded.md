# 599 (perf): restartable の終わったプロセスの記録 (allProcesses) が runner の生存期間中に増え続ける

起票日: 2026-10-01

出典: restartable の audit (codex のコードスキャン、2026-10-01。[601](601-research-restartable-audit-2026-10-01.md))。Claude がコードで裏を取った。

## 問題

`src/restartable/internal/runner/actor.go` の `actor.allProcesses` は、build / run / stop-cmd / ready-cmd の確認を起動するたびに追加され、actor が終わるまで消えない。
各記録は `exec.Cmd` と環境のスライスを持つ。ready-cmd の確認は 0.5 秒ごとに 1 本ずつ起動するので、起動の遅いアプリでは 1 回の再起動で最大約 240 本 (上限 120 秒) 増える。
終了時の `beginForceStop` / `drainOutputs` は全記録を走査する。

## 発火条件

runner を長時間動かし、R や control restart を繰り返す (特に --ready-cmd の確認が長く続く起動)。メモリと終了時の走査量が起動の回数に比例して増える。

## 推奨対応

出力を回収済みで、プロセスグループも消えた記録を捨てる (残ったグループを強制終了で撃つのに要る最小限の情報だけ残す)。数の上限を観測するテストを足す。

## 確認

コードで確認 (2026-10-01 Claude)。メモリの増え方は計測していない (未実測)。

## 進捗

2026-10-01 (dotfiles-58 の下で 599 を担当したサブエージェント)

### 実装

- 記録の追加を `actor.trackProcess` に一本化した (startBuild / startRun / beginStop の stop-cmd / startReadyProbe の 4 箇所)。追加の前に `process.settled()` を満たす記録を取り除く
- `settled()` = leader 回収済み (`finished`) かつ出力のコピー終了 (`outputEnd` close 済み) かつ `!groupExists(pid)`。強制終了 (`beginForceStop` は `!finished || groupExists` のものしか撃たない) と出力回収 (`drainOutputs` は `outputEnd` を待つだけ) のどちらにも、その記録でやることが無い
- 残すもの: グループが生きている記録 (leader が終わって子孫が残る形。強制終了で撃つ) と、グループを抜けた子孫が出力 pipe を握る記録 (drainOutputs が閉じる唯一の手がかり)
- 空になったグループに後から入ることはできないので、同じ番号のグループが現れたらそれは pid 再利用による無関係なプロセス。記録を残し続けると強制終了でそれを撃つ既存の危険があり、捨てるとその危険は減る

### テスト (`src/restartable/internal/runner/process_records_test.go`)

- `TestTrackProcessKeepsRecordCountBoundedAcrossManyFinishedProbes`: 片付いたプロセスを 50 回 track して常に 1 件
- `TestReadyProbeLoopDoesNotAccumulateProcessRecords`: 実際の ready-cmd のループ (`exit 1` を 60 回) を actor の event で回し、記録数の最大を見る
- `TestTrackProcessKeepsReapedLeaderWhoseGroupIsAliveForForcedShutdown`: leader が終わって TERM を無視する子孫が残るグループの記録が、後続の 20 回の track で捨てられず、`beginForceStop` がそのグループを殺す
- `TestTrackProcessKeepsRecordWhoseOutputIsStillHeld`: グループを抜けて pipe を握る子孫 (`/usr/bin/perl` の setpgrp) がいる記録が残り、`drainOutputs` が閉じる

### 変異 (`mutate-verify`、すべて rc=0 = 想定のテストだけが red)

- M1 prune を外す → `KeepsRecordCountBounded` と `ReadyProbeLoop` が red (記録数 60 / 60 probe)。他は緑
- M2 `settled` の groupExists 条件を外す (生きているグループの記録も捨てる) → `KeepsReapedLeaderWhoseGroupIsAlive` だけ red。既存の `TestForcedShutdownIncludesGroupsWhoseLeadersWereReaped` は後続の track が無いので緑のまま (このテストを足した理由)
- M3 outputEnd 条件を外す → `KeepsRecordWhoseOutputIsStillHeld` だけ red
- M4 startReadyProbe だけを旧来の append に戻す (配線の退行) → `ReadyProbeLoop` だけ red

### 検証

- `go test -race -count=1 ./...` (src/restartable) rc=0、4 package とも ok
- `../../scripts/golangci_lint.sh v2.5.0 run --max-same-issues 0 --max-issues-per-linter 0 ./...` rc=0、`0 issues.`
- テスト後にテストが起こしたプロセスが残っていないことを ps で確認

### 実測

- ready-cmd が 60 回失敗する間の allProcesses の最大件数: 変更前 (M1 の変異体) 60 件 → 変更後 1 件 (`TestReadyProbeLoop` の t.Logf、5 回とも 1)。バイト数は測っていない (記録 1 件 = `*process` + `exec.Cmd` + 環境のスライス。件数が probe 回数に比例しなくなったことまでが実測)

### codex の指摘と採否

- 設計 (P2): 旧記録の pid が新しいグループに再利用されていると、その間 prune されない → **採らない**。再利用されたグループが生きている間だけ残り、後続の track のたびに判定し直すので積み上がらない。再利用されたグループを強制終了で撃ちうるのは変更前からある危険で、prune はそれを狭める側
- 通常レビュー: 指摘なし
- 敵対的 1: ready-cmd が毎回「グループを抜けて pipe を握る子孫」を作ると記録が probe ごとに増える → **修正は採らず、コメントの「有界」を正確にした**。変更前より悪化しない (変更前は全 probe が残る)。残る記録はどれも実際に漏れている fd と出力回収の goroutine に 1 対 1 で、drainOutputs が閉じるための唯一の手がかり。ready cleanup で reader を先に閉じる案は、子孫の出力を落とし SIGPIPE を送る挙動の変更になるので 599 の範囲外
- 敵対的 2: テストが `/usr/bin/perl` を暗黙に要求する → **採った**。無ければ理由を書いて Fatal にした (macOS 専用の repo なので skip にはしない)
- 取り込み (dotfiles-58): 597・598 の後に cherry-pick し、衝突なし。捨てる条件を外す変異で TestTrackProcessKeepsRecordCountBoundedAcrossManyFinishedProbes と TestReadyProbeLoopDoesNotAccumulateProcessRecords が red になることを、取り込んだ木で確かめ直した
