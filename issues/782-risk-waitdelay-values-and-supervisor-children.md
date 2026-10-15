# 782 (risk): WaitDelay の値が subproc (2 秒) と自前 (1 秒) に割れている・process_supervisor の見張りの子に WaitDelay が無い

起票日: 2026-10-16

## 概要

issue 777 で外部コマンドの起動口の検査を `subproc/boundarycheck` に寄せたときに残ったもの。どちらも不具合は観測していない (未確認のリスク)。

- **値の割れ**: pro-con の 8 ファイル (dispupgrade / upgrade / launcher / claude.go の --version / triage / usage / progress / agents) と mac-lan-manager の `discover/exec.go` は
  `cmd.WaitDelay = time.Second` を自分で張り、`subproc.WaitDelay` (2 秒) と値が違う。検査は Allow (理由つき) で通している。
  対象外: `doctor/runner/runner.go` の `waitDelay = 2 * time.Second` (値は同じだが subproc を通らない別の写し。Setpgid + SIGKILL を自分で持つので 773 で寄せないと決めた) と、
  `pro-con/dispatcher/runner.go` の `killGrace` (止めるときの猶予で、WaitDelay とは別の意味)
- **process_supervisor**: 見張りの子の起動に WaitDelay を張らない。pro-con の monitorsup.go は 777 で `subproc.Command` を通すようになったが、`supervisor.Spec.Command` を書くほかの側も
  同じ穴を持ちうる (stdout が pipe で、子が孫を残すと Wait が戻らない)。実際に孫を残す見張りがあるかは未確認

## 対応方針 (案)

- 値をそろえるなら、1 秒を選んだ理由 (各ファイルのコメント) を読み、2 秒にして困るか (待ちが延びる画面) を確かめてから subproc の口へ寄せる
- process_supervisor が起動する子の stdout / stderr の渡し方を数え、pipe になる経路があれば WaitDelay を張る (または subproc を通す)

## 再評価の trigger

- pro-con の画面・CLI が「子が終わったのに戻らない」で止まった報告が出たとき

## 関連ファイル

- `src/subproc/subproc.go` (`WaitDelay`) / `src/process_supervisor/` / `src/pro-con/exec_boundary_test.go` (Allow) / issue 777 (done)
