# 880 (bug): serialrun のシグナルと子の扱い

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-23〜）

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ C の 4 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### C1 起動時に SIGINT / SIGQUIT が無視されていると、serial-run が無視を解いて子を「INT で死ぬ」状態にする (HUP だけ配慮されている)
- 重要度: P3 (反証レビューで「バグ」から「設計判断の見直し」へ下げた) / 確認済み (`trap "" INT QUIT` の内側の `kill -INT $$` が、serial-run 越しだと survived が出ず rc=130。HUP は survived)
- 根拠: main.go `runQueued` (`signal.Notify(sigs, SIGINT, SIGQUIT, SIGTERM)` を無条件に張る。HUP だけ `signal.Ignored` を見る)。発火: 非対話 bash の `cmd &`・`trap '' INT` のスクリプト。hook は `make test &` も包む。同型が src/runtimeout/main.go:60・restartable/internal/runner/actor.go にある (lint-from-done の grep。restartable は未精査の推測)
- 直し方の案: INT/QUIT も HUP と同じく `!signal.Ignored(sig)` のときだけ Notify する。テストは `trap '' INT` の親から起こして子の `kill -INT $$` が生き残ること。runtimeout も同じ commit で直す (G1 の共有化と同時なら 1 箇所)。検査は「語の有無」だけでは serialrun 自身を捕まえられない (HUP にだけ Ignored があるため) ので、script にするなら「INT/QUIT の Notify より前に Ignored(SIGINT)」まで詰める。本命は共有 helper (lockman の `notifiableSignals` / `installSignalHandler` を proctree へ出す) + forbidigo。script は helper 化までのつなぎ
- 出典: broken-code P2-1 / lint-from-done P2-1

### C2 SIGINT / SIGQUIT を子へ伝えない設計が、pid 宛て (非端末) の SIGINT で「止められない serial-run」を作る
- 重要度: P2 / 一部確認済み (`serial-run -- sleep 20` の pid に SIGINT → 2 秒後も生きている。待ち中は INT で抜けるのに走り出すと抜けられない非対称)。Claude Code の中断・TaskStop がどのシグナルをどこへ送るかは未実測 (869:239 も未実測と書く)。P2 は、その実測次第で通常の中断経路が壊れるため
- 根拠: main.go `runChild` の select (INT/QUIT は「端末から同じグループの子にも届いている」前提で何もしない)
- 直し方の案: INT/QUIT は「直接の子が同じプロセスグループにいて、自分が前景グループにいる (tcgetpgrp)」ときだけ伝え直さない。それ以外は TERM/HUP と同じく子孫ごと止める。または INT を 1 度受けたら一定時間後に子へ INT を送る。先に 869:239 の未実測項目 (Claude Code の中断のシグナル) を測る
- 出典: design P2-2

### C3 呼び出し元の殺され方次第で「幽霊の待ち手」が残り、誰も待っていないのに重いコマンドを走らせる
- 重要度: 未確認 / 一部確認済み (待ちの stderr が壊れた pipe なら 30 秒の報告で SIGPIPE で死ぬが、stderr が file / /dev/null なら死なず実際に実行した)。Claude Code の中断・timeout の送り先は未実測
- 根拠: main.go `reportPos` (stderr 書き込みに依存した偶然の終了) / 待ち手が親 shell の消失を見ない
- 直し方の案: 待っている serial-run が ppid の変化 (親の消失) を見て抜ける。入れる判断の材料。C2 の実測と同時に決める
- 出典: design U-1

### C4 入れ子の直接実行 (execDirect) の rc とメッセージが通常経路 (runChild) と食い違い、存在しないコマンドは列を待ち切ってから 127 になる
- 重要度: P3 / 確認済み (同じ `bad.sh` が直列化されると rc=127 + path 付き、入れ子だと rc=126 + path 無し。`serial-run --max-wait 1 -- nonexistent_cmd` は 127 でなく待ちの上限で rc=125)
- 根拠: main.go `execDirect` (`syscall.Exec` の失敗を種類によらず rcCannotExec=126) / `runChild` (`startErrorRC` で ENOENT → 127)。typo の発覚が自分の番まで (最大 1 時間後)。bash の bad interpreter が 126 の点は手元の記憶で未実測
- 直し方の案: `execDirect` も `startErrorRC(err)` を通し path 付きで出す。enqueue の前に `exec.LookPath` して、無ければ即 127 (hook 経由は既知の重いコマンドだけなので手で打つ場合の話)
- 出典: error-handling P3-3 / ux P3-3

---

## 反証レビュー (2026-10-23、Opus 1 体・読み取りのみ。実験はしていない)

本文の重要度と案は下の訂正を反映済み。訂正の根拠を残す。

- **C1 は設計判断の見直し** (P2 の指摘): `src/runtimeout/main.go` の Notify の注記は、INT / QUIT の無視を**意図して**外すと書いている (「非対話の bash の `&` は INT / QUIT を
  無視して子を起こすので、戻さないと検証コマンドの Ctrl-C や trap … INT が効かない」。bin/mutate-verify の run_in_wt の注記)。HUP だけ `Ignored` を見るのは意図した非対称で、
  serialrun はそれを写した。**runtimeout は対象から外す**。serialrun で変えるかは、mutate-verify の要件 (hook が mutate-verify を包む) との衝突を見て 869 で決める
- **C2 の補強**: 869 の「子の起動」節は「SIGINT / SIGTERM / SIGHUP を受けたら子へ転送し」と書いているのに、`runChild` は INT / QUIT を転送しない (869 との乖離)。直すときは 869 か実装のどちらかに揃える
- 反証できなかった: C3・C4

## 進捗

- [ ] 未着手 (項目ごとに、直した commit の subject・当てた変異・実測を書く)
