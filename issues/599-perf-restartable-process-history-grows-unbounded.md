# 599 (perf): restartable の終わったプロセスの記録 (allProcesses) が runner の生存期間中に増え続ける

> 🚨 **担当中: dotfiles-58**（2026-10-01〜）

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
