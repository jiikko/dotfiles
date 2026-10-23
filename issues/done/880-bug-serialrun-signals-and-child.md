# 880 (bug): serialrun のシグナルと子の扱い

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ C の 4 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](../869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
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

- 2026-10-23 (commit「fix(serialrun): シグナルと子の扱いを直す (issue 880)」):
  - [x] C1 起動時に無視されていた INT / QUIT / HUP は Notify しない (無視のまま子へ渡す)。**決め**: serial-run は包むだけの道具なので、挟んでも挟まなくても子の受け方を変えない
    (runtimeout は mutate-verify のために INT / QUIT の無視を意図して外すが、serial-run はその逆を選んだ。runtimeout は変えていない)。`TestIgnoredIntStaysIgnoredForChild`
  - [x] C2 子が走っている間の INT / QUIT は子へ伝え直さない。端末の Ctrl-C・グループ宛ての INT (runtimeout・CI) は子にも直接届くので、伝え直すと 2 回届く。
    **端末の前景に居るとき** (`/dev/tty` の TIOCGPGRP が自分のグループ) は猶予も張らない (Ctrl-C を自分で扱って走り続ける子を殺さない。直す前と同じ)。
    **端末の外** (制御端末が無い・前景でない) では猶予 30 秒 (`intGrace`) のうちに子が終わらなければ子孫ごと止める (TERM → KILL)。猶予が切れるのと子の終了が同時なら終了を採る。
    `TestIntToSerialRunPidStopsChildAfterGrace` / `TestGroupIntIsNotForwardedTwice` / `TestIntLetsChildFinishCleanupWithinGrace` / `TestIntGraceStopsGrandchildren`
    - 合図 × 置かれ方の表 (期待値):

      | 合図 | 端末の前景 | 端末の外 (Claude の Bash・CI・背景) |
      |---|---|---|
      | 端末の Ctrl-C | 子にも届く。serial-run は待つだけ (猶予なし) | — |
      | グループ宛ての INT / QUIT | 子にも届く。待つだけ | 子にも届く。子が 30 秒で終わらなければ子孫ごと止める |
      | pid 宛ての INT / QUIT | 子には届かない。**止まらない (受け入れ。前景で他の端末から pid 宛てに送る使い方は想定しない)** | 30 秒の後に子孫ごと止める |
      | TERM / HUP | すぐ子孫ごと止める | すぐ子孫ごと止める |
    - 端末の前景の経路は `TestIntInTerminalForegroundWaitsForChild` が pty (Setsid + Setctty で制御端末だけを pty にし、stdin は端末にしない) で固定する。
      先に python の pty driver でも手で確かめた: 猶予 1 秒の seam 版で Ctrl-C を送り、INT を trap して 3 秒走る子が rc=7 で最後まで走った。前景の判定を外した版では 1.1 秒で止められた (負の対照)
    - 猶予が切れるのと子の終了が同時の場合の覗き (`graceUp` の分岐の非 blocking の `done`) は競合の窓が狭く、テストで固定していない
  - [ ] C3 **直さない (取り消した)**: 待っている間に親が消えたら抜ける形 (kqueue の NOTE_EXIT) を一度入れたが、敵対的レビューで nohup / `&` で正当に背景に回した待ち手を
    黙って落とすと分かった (再現済み)。親の終了は「中断された」と「意図して切り離した」を見分けられない。待ち手が抜けるのは待ちの上限 (既定 1 時間) とシグナルだけ
  - [x] C4 札を出す前に LookPath し、無ければ待たずに 127。`/` を含む名前は前確認しない (前に並んでいる make build が作るものを指せるように)。入れ子の直接実行の Exec の失敗も
    通常の経路と同じ rc。`TestMissingCommandFailsBeforeWaiting` / `TestPathLikeCommandIsCheckedAtTurn` / `TestBadInterpreterRCMatchesNested`
- 変異 (全部 red): 無視された INT を Notify する / pid 宛ての INT を猶予の後に止めない / 伝え直す (2 回届く) / 前確認をしない / `/` を含む名前も前確認する / 入れ子の Exec の失敗を 126 に戻す
- 敵対的レビュー 1 周目 (Opus): P1 1 / P2 2 / P3 2。P1 親の見張りが nohup / `&` の待ち手を落とす (再現済み) → C3 を取り消した / P2 端末の前景で stdin をリダイレクトすると INT が 2 回 (再現済み) /
  P2 グループ宛ての INT が 2 回 (再現済み) → 伝え直しをやめ、猶予の後に止める / P3 `./bin/x` を前確認で落とす → `/` を含む名前は見ない / P3 親の見張りの pid の使い回し → 取り消しで消えた
- テストの後始末: 専用のグループで起こしたテストは、後始末でグループごと止める (ループで待つ子が孤児で残っていた)
- 敵対的レビュー 2 周目 (Opus): P2 1 / P3 2。P2 端末の Ctrl-C を扱う子 (REPL・長い後始末) を 5 秒で殺す (直す前より悪い) → 前景では猶予を張らず、猶予を 30 秒へ /
  P3 テストが猶予の存在と子孫の停止を守っていない → 2 本足した / P3 猶予と子の終了が同時のとき回収済みの pid を撃ちうる → 先に `done` を覗く
- 変異 (2 周目、全部 red): 猶予 0 / 猶予を張らない / 猶予が seam を読まない / 猶予の後に子だけ止める
- 敵対的レビュー 3 周目 (Opus): P1 0 / P3 2。直す前より悪くなる実用上のセルは見つからなかった
  - P3 前景の経路に自動のテストが無い → pty のテストを足した。変異 (前景の判定が常に false / /dev/tty でなく stdin を見る) はどちらも red
  - P3 **受け入れ**: 端末の master が閉じても、HUP を無視した session leader が slave を握って残ると、前景と判定され pid 宛ての INT で止まらない (再現済み)。
    leader の shell が HUP で死ぬ普段の形では端末が revoke されて open が失敗し、猶予が効く。直す前 (47508a73^) は stdin が /dev/null なら INT を子へ伝えていたので、この形に限ってはそちらが止まった
  - テストの穴 (記録): 「INT のたびに猶予を張り直す」に変えても赤にならない。30 秒という値そのものはテストが seam で上書きするので守っていない
  - 壊せなかった: carrier が落ちた後の /dev/tty の open は詰まらない / TIOCEXCL の端末も影響なし / Claude の Bash では /dev/tty が ENXIO で猶予が効く (実測)
