# 782 (risk): WaitDelay の値が subproc (2 秒) と自前 (1 秒) に割れている・process_supervisor の見張りの子に WaitDelay が無い

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

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

## 進捗

### 値の割れ (寄せた)

- 1 秒を選んだ理由は 9 ファイルのどこにも書かれていなかった (usage.go の「launcher と同じ」だけ。Allow の理由も「1 秒を自分で張る」の写し)。
  WaitDelay が効くのは ctx の取り消し後・子が抜けた後に孫が pipe を握っている異常の経路だけで、各呼び出しの上限は 10 秒 (agents) 〜 3 分 (summarize)。2 秒にして困る画面は無い
- [x] 9 本を `subproc.CommandContext` (git の 2 本は `subproc.GitCommand`) へ寄せ、自前の `cmd.WaitDelay = time.Second` を消した:
  dispupgrade.go / upgrade/upgrade.go / dispatcher/{launcher,triage,usage,claude,progress}.go / agents/agents.go / mac-lan-manager/discover/exec.go
- [x] 起動口の検査: pro-con・mac-lan-manager とも Allow を空にした (Cmd を自分で作ってよいファイルが無い = 今より厳しい)。os/exec は型・ExitError・LookPath に要るので NoOsExec にはしない。
  Cmd を作る行が 0 になったので canary の下限も 0 (走査の空振りは boundarycheck が go list との突き合わせで止める)。usage_test の WaitDelay の期待値は `subproc.WaitDelay`
- 変異で red を確認 (`mutate-verify`): agents.go を旧形 (`exec.CommandContext` + `cmd.WaitDelay = time.Second`) に戻す → `TestExecBoundary` が「*exec.Cmd を作っているが Allow に無い」で red
- 対象外のまま: doctor/runner の `waitDelay` (773 で寄せないと決めた) / dispatcher/runner.go の `killGrace` (別の意味)

### process_supervisor の子 (今は穴が無い)

- `supervisor.Spec.Command` を書くのは pro-con の 2 か所だけ (2026-10-16 に `grep -rn 'supervisor.Spec'` で数えた): 見張り (`monitorCommand`) と dispatcher (`supervise.go` の `dispatcherCmd`)。
  どちらも `subproc.Command` を通す (WaitDelay 2 秒)。出力の渡し先も dispatcher は os.Stdout / os.Stderr (dispatcher.log の file)、見張りは runDispatcher の stdout / stderr (main が渡す os.Stdout / os.Stderr。旧版から引き継いで起動したときだけ stderr が altGuard = io.Writer になり exec がパイプを作るが、その場合も WaitDelay は張られている)
- [x] Spec.Command の doc に「Stdout / Stderr を *os.File 以外にするなら WaitDelay を張る」を足した (process_supervisor 側では強制しない: 呼び出し側が Cmd を作るので)

### 敵対的レビュー (opus 1 本、2026-10-16。779/781/783 と同じ 1 本)

- P1・P2 なし。壊せなかった観点: GitCommand に変えた 2 本の argv が旧形と一致 / Dir・Env・Stdout の設定順は不変 / 非テストに素の exec.Command( は 0 件
- P3 (採用・コメントのみ): store/seen.go の `SeenFresh` (15 秒 = tick 3 + agents.Timeout 10) は WaitDelay を数えていない。効くのは上限を過ぎて子孫が出力を握ったときだけで、そのときは Err つきで今の様子に使われないので挙動は変わらない。その旨をコメントに書いた
- P3 (記録): Allow 空・floor 0 で module ごとの canary は無くなったが、Cmd を作る行が 1 つでもあれば「Allow に無い」で落ちるので前より厳しい。走査の空振りは go list との突き合わせ、走査器の canary は boundarycheck_test と glogx の floor が持つ
