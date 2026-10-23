# 883 (test): serialrun のテストの穴・後始末・隔離・ヘルパー

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ F の 17 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](../886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](../869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### F1 テストの Cleanup が serial-run の pid だけを kill し、fifo で止まった子 (sh) を残す。失敗・中断した run のたびに永久に残る
- 重要度: P2 / 確認済み (実機の `ps` に PPID=1 の `/bin/sh -c … read _ < …/Test<名前>…/001/<gate>.fifo` が 9 本。3 時間前から。fifo も一時 dir も既に無く kill 以外に終わらせる手段が無い。緑の run では残らない)。それらを作った run が失敗した run かは推測。報告者は 9 本を止めていない (自分が起こしたものではないので)
- 根拠: main_test.go `fixture.start` の `t.Cleanup` (`cmd.Process.Kill()` のみ。子も、`setpgid` で起こした run のグループも殺さない) と `fixture.held` (子は fifo の open 待ち)。テストが検出したい退行 (待ち手が起きない・順番が崩れる) が起きたときに必ず残る
- 直し方の案: `start` の Cleanup で子の pid (札の `child_pid` かテストが渡す pid ファイル) またはグループ (`setpgid` の run は `Kill(-pid)`) まで止める。または子を「親の pid が消えたら終わる」形に (`while kill -0 $PPID`)。proctree に「子ごと止める Cleanup 登録」を 1 本作る案があるが、forbidigo で `Process.Kill` を禁止するのは偽陽性が多い (9 箇所中 1 つしか実害を確認していない)。serialrun の test を helper 経由にするだけで足りる (I14)。実機に残った 9 本はユーザーが判断して pid だけ kill
- 出典: resource-leaks P2-1 / lint-from-done P2-3

### F2 「孤児・判定不能の札では blocking で眠らず recheck する」の防御が、片側 (`last.state != stateAlive` の枝) を外しても全テスト緑のまま CPU 空回りになる
- 重要度: P2 / 確認済み (変異 `last.state != stateAlive` → `false` で `go test -count=1` 全緑。同じ変異体で孤児の後ろの待ち手が 1.5 秒に wake 705 回 (正規版は 2 回))
- 根拠: main.go `waitPredecessors` の recheck と queue.go `blockUntilReleased` の 2 枚で 1 つの防御。`TestUnopenableTicketDoesNotSpin` は開けない札で blockUntilReleased が起こさないので recheck 無しでも緑、`TestOrphanedChildKeepsTheTurn` は機能だけ見て wake 回数を見ない。869 の「写しに両方を戻して red」は片側だけの退行を検出できない
- 直し方の案: `TestOrphanedChildKeepsTheTurn` にも `SERIAL_RUN_TEST_WAKE_LOG` を渡し、孤児の間の wake 回数に上限 (recheck 100ms で 0.5 秒なら ≤ 十数回) を置く。変異の記録に「recheck だけ外す」を足す。A10 を触る前にやる
- 出典: false-green P2-1

### F3 `serial-run status` を起こすテストが 1 本も無い
- 重要度: P3 / 確認済み (`printStatus` を「無い」だけ出して return に置き換える変異で全緑)
- 根拠: main_test.go に `status` サブコマンドを起こすテストが無く、`"serial-run status"` の案内文の検査だけ。hook の案内が指す道具が壊れても緑
- 直し方の案: A (走行中) + B (待ち) を作って出力に「先頭」「待ち」と各 pid が出ること、孤児を作って状態語が出ることを見る (fifo 方式の既存 fixture で足りる)。D1 と同じ commit
- 出典: false-green P3-1

### F4 シグナルまわりの機構がテストの射程の外: `exitAs` の自殺 (`signal.Reset` + `Kill(self)`)・`tgt.Stop` の子孫停止・待っている間のシグナル・`acquireRun` の deadline
- 重要度: P3 / 確認済み (4 本の変異がすべて全緑)
- 根拠: main.go `exitAs` / `runChild` の `tgt.Stop(stopGrace)` / `waitPredecessors` と `acquireRun` の `case s := <-sigs` / `case <-deadline`。`exitCode` ヘルパーが signaled を `128+n` に直すので `os.Exit(143)` と自殺が区別できず、`TestTermStopsChildAndLeavesNoTicket` は `_ = exitCode(...)` で rc を捨て、子は孫を持たない sh 1 本
- 直し方の案: 親が `Setpgid` で TERM を送るテストを 1 本 (孫を `sleep` で起こし、止まったことと `cmd.ProcessState.Sys().(syscall.WaitStatus).Signaled()` を `exitCode` ヘルパーを介さず直接見る)。待ち手に TERM を送って札が消え終わることを見るテストを 1 本。`--max-wait` を acquireRun の待ちでも見るテスト
- 出典: false-green P3-2

### F5 副産物の無検査: `sweepStaleTmp`・history.log の rc・`heldByAncestor` の「前に札が無い」条件
- 重要度: P3 / 確認済み (`sweepStaleTmp` 呼び出し削除 / history の rc を 0 固定 / `return len(q.predecessors(seq)) == 0` → `return true` の 3 変異が全緑)
- 根拠: queue.go `sweepStaleTmp` / main.go `appendHistory` / `heldByAncestor`。`TestStaleHeldEnvStillQueues` は seq 999 (札が無い) の場合だけ見ている
- 直し方の案: (a) 61 秒前の mtime の `.tmp-x` (flock なし) を `os.Chtimes` で作り、1 回待ちを回した後に消えること。(b) `exit 3` の run で history の最終行の rc を見る。(c) A 走行中に、B の札 (待ち) の seq を `SERIAL_RUN_HELD` に入れた C が並ぶこと
- 出典: false-green P3-3

### F6 hook のテスト表のうち、書かれた防御を守っていない入力がある (書き直し・足す)
- 重要度: P2 (test-cleanup P2-2) / 確認済み (変異で緑を確認。書き換え案が red になることは未実行 = 推測)
- 根拠 (hook_test.go): 負の表 `serial-run -- make test` は `strings.Contains(cmd, "serial-run")` を守っていない (`serial-run` が先頭語なら isHeavy が false。効くのは `serial-run -- make test && make lint` で、防御を消すと `… && serial-run -- make lint` に化ける) / `# make test`・`ls # make test` は `#` を読み飛ばす枝を守っていない (効くのは `ls # x; make test`) / `make test)`・`case $x in a) make test;; esac`・`f() { make test; }` は重ねた防御のどれか 1 層が生きていれば緑 (層ごとに見るなら `(make test` と `echo a) (echo b` を足す) / `TestHookIsSilentWhenNothingToWrap` の `{"tool_name":"Read","tool_input":{"file_path":"make test"}}` は `command` を持たず tool_name の検査を守っていない (`"command":"make test"` にする)。付記: `lint-` の接頭辞 (`HasPrefix(a, "lint-")`) を消しても全緑 = 守る入力が無い (足りない側)
- 直し方の案: 上の括弧のとおり差し替え・追加。B1 を直すときに `GOPATH=` / 引用内の serial-run の 2 件も足す。P3-2 の 3 件を消すかは好み (層を重ねる設計なら残す)
- 出典: test-cleanup P2-2・P3-1・P3-2・P3-4

### F7 シェルテスト (`tests/claude/test_serial_run_wrap.sh`) の末尾「7 件」が手書きで、`git status` の expect は fast path で落ちて hook.go に届かない
- 重要度: P3 / 確認済み (`wrapCommand` の `if !changed { return 0 }` を外す変異で赤になったのは `grep -rn "make test"` の 1 件だけ。「7 件」は expect を足し引きすると嘘になる)
- 根拠: 同ファイルの `expect "" "重くないコマンドは書き換えない" "git status"` と末尾の `echo "✓ ... 7 件すべて通過"`
- 直し方の案: `git status` の expect は消すか `make build` に差し替え (fast path を通って「変えない」枝に届く)。件数は `FAIL_COUNT` と別に数えて出す
- 出典: test-cleanup P3-3 (+ design・broken-code の「7 件」の言及)

### F8 シェルテストが本物の既定の状態 (実機の `~/.local/state/serial-run`) を読み書きする
- 重要度: P3 / 推測 (実機の列に誰かがいる状態でテストを回していない)
- 根拠: テストは SERIAL_RUN_STATE_DIR / HOME / XDG_STATE_HOME を差し替えない。`queueNote` が実機の default の queue を読み、死んだ札を回収する。置き場が壊れていると `rewritten` の rc≠0 で赤になり「手元で赤・CI で緑」の形になる
- 直し方の案: テスト冒頭で `export SERIAL_RUN_STATE_DIR="$TMP_DIR/state" HOME="$TMP_DIR/home" XDG_STATE_HOME="$TMP_DIR/xdg"` (jq の PATH 照合ケースは HOME を使わないので影響しない)。隔離した state に札を 1 枚置いて additionalContext の入口の検査も足せる
- 出典: general P3-4

### F9 シェルテストの「PATH に serial-run が無い」ケースが、hook が別の理由で空を返しても緑 (negative が positive の対照を持たない)
- 重要度: 未確認 / 推測 (発火の実例は示せない)
- 根拠: 最後の 2 件 (`PATH=$JQ_DIR:/usr/bin:/bin`)。同じ restricted PATH での positive 対照が無い。「PATH に無い」の方は `command -v serial-run || exit 0` を外しても次の `[ ... = "$bin" ] || exit 0` が止めるので、どちらの行が検査されているか区別できない (「別物」の方は照合行を削る変異で red 確認済み)
- 直し方の案: 同じ restricted PATH で positive の対照を 1 件足す
- 出典: false-green U-1

### F10 `TestMiddleWaiterDeathKeepsOrder` は、機械の変異で唯一の検出者になった変異が 0 本で、想定する退行の変異は等価
- 重要度: P3 / 確認済み (機械の変異 215 本中、このテストが赤になった 21 本は全部ほかのテストも赤にした。「1 つ前だけを見る」変異は全緑 = 設計上等価)
- 根拠: main_test.go。中間固有の退行を報告者は作れていない = 唯一かは未確認
- 直し方の案: `TestRunsOneAtATimeInTicketOrder` の 3 本の鎖の 1 本を SIGKILL で落とす形に畳む (削除より安全)。`TestRunLockExcludesEvenIfHolderTicketIsRemoved` は消さない (run.lock を取らない変異を red にするのはこれだけ)
- 出典: test-cleanup P3-5

### F11 `TestKilledHolderGroupLetsNextRun` の `childPid > 0 &&` が、pid ファイルが空だと子の生存検査を黙って飛ばす
- 重要度: P3 / 推測 (今は発火しない。コードを読んだだけ)
- 根拠: main_test.go の `childPid, _ := strconv.Atoi(...)` の失敗が 0 になり検査が飛ぶ
- 直し方の案: Atoi の失敗を `t.Fatal` にする (数行)
- 出典: test-cleanup P3-6

### F12 `zombie` / `alive` (ps の stat を読んで「居るか」を決める) が複数 module にコピーされ、定義が割れている
- 重要度: P2 / 確認済み (`grep -rn 'ps", "-o", "stat="' src` の _test.go = 4 件。ps 失敗時の倒れ先が serialrun は「居ない」、runtimeout / proctree は false。pro-con は `kill -0` だけ、doctor は EPERM を生きている扱い)
- 根拠: src/serialrun/main_test.go `zombie`・src/runtimeout/main_test.go `alive`・src/proctree/tree_test.go `alive`・restartable の integration_test.go。serialrun の「子が残っていない」検査は `syscall.Kill(pid,0)==nil && !zombie(pid)` を 2 箇所 (TestKilledHolderGroupLetsNextRun / TestTermStopsChildAndLeavesNoTicket) で手で組む
- 直し方の案: まず serialrun の 2 箇所を `gone(pid)` 1 関数 (kill -0 と ps の両方を中で見る) に寄せる。module をまたぐ共通化は go.mod が別で重いので、3 module 目に同じ修正が要るときまで凍結
- 出典: test-helpers P2 (zombie / alive)

### F13 `waitFor` が 6 module で同形にあり、上限・文言・刻みが各自の定数 (serialrun と runtimeout は逐語一致)
- 重要度: P2 / 確認済み (`grep -rn "^func waitFor(" src` = 6 module。400 回 × 25ms と文言「10 秒待っても成立しない」が別々に手書き)
- 根拠: src/serialrun/main_test.go と src/runtimeout/main_test.go ほか。shell 側は tests/lib/wait_until.sh が正本を持つのに Go 側に対応物が無い
- 直し方の案: serialrun 内では `const waitTicks, waitTick` から文言を作る。module 横断は F12 と同じ扱い (凍結)
- 出典: test-helpers P2 (waitFor)

### F14 「X が始まる」「N 枚の札が並ぶ」の待ちと rc 検査が多数のテストにインラインで、対応が呼び出し側の頭の中にある
- 重要度: P2 / 確認済み (`grep -c 'rc := exitCode'` = 16、`exitCode(t` 30 箇所)。N 枚目 = N 番目の待ち手の対応が崩れても条件は通り、競合の窓を作り損ねた緑になりうる (推測)
- 根拠: main_test.go の `waitFor(t, "X が始まる", ...)`・`waitFor(t, "B の札", func() bool { return f.ticketCount() == N })`
- 直し方の案: `f.waitStarted(name)` / `f.waitTickets(n)` / `f.mustExit0(name, cmd)`。文言は引数から作る。「起こす → 札を待つ」を `f.heldAt(n, ...)` 1 呼び出しにする
- 出典: test-helpers P2 (インライン)

### F15 最初の札のパス `"000000000001"+ticketSuffix` が 3 テストに手書き
- 重要度: P3 / 確認済み (`grep -c 000000000001` = 3)。Unopenable 側は別名の開けない札を作って意図と別の札を見たまま緑になりうる (未確認: 本体の列挙が別名の札も数えるなら赤)
- 根拠: main_test.go の TestUnopenableTicketDoesNotSpin / TestOrphanedChildKeepsTheTurn / TestRunLockExcludesEvenIfHolderTicketIsRemoved
- 直し方の案: `f.ticketPath(seq int)`。本体に名前を作る関数があればそれを呼ぶ (テストが形式を再実装しない)
- 出典: test-helpers P3 (札のパス)

### F16 否定の確認の `time.Sleep` (500ms / 1s) が、env の刻み (REPORT=50ms / RECHECK=100ms / FALLBACK=100ms) と別々の手書き値
- 重要度: P3 / 推測 (刻みだけ伸ばすと「見直しが来ても B が入らない」を見ない弱い検査になるが、変異で赤になるかは未確認)
- 根拠: main_test.go の TestLaterArrival... / TestWaitWakes... / TestOrphanedChildKeepsTheTurn / TestRunLockExcludes...。「刻み 100ms の 5 回分」は sleep-ok の理由コメントにしか無い。ci は、runner が極端に遅いと空振りの緑がありうるが根拠なしとしている
- 直し方の案: `const recheck = 100 * time.Millisecond` を env の文字列と sleep の両方に使い、`negWindow(tick, n)` で窓を作る
- 出典: test-helpers P3 (否定の確認)

### F17 hook_test.go の `hookContext` (`map[string]any`) と `TestHookOutputsUpdatedInputKeepingOtherFields` (型付き struct) が同じ「runHook → hookSpecificOutput を読む」を別の型で持つ
- 重要度: P3 / 確認済み (読んで。5 本中 2 本なので優先度は低い)
- 根拠: hook_test.go。出力のキー名が変わると 2 箇所が別々に直り、map 側は改名で「書き換えていない」の Fatalf に落ちて原因が読みにくい
- 直し方の案: `runHookJSON(t, in)` 1 つ
- 出典: test-helpers P3 (hookContext)

---

## 進捗

- 2026-10-23 (commit「test(serialrun): テストの後始末・射程・helper を直し、run.lock の待ちで止まらなくする (issue 883)」):
  - [x] F1 テストが起こす serial-run を全部専用のプロセスグループに置き (`fixture.track`)、後始末でそのグループの残りを止める。
    🚨 pgid の使い回しで無関係なプロセスを撃たないよう、止めるのはグループに居てコマンド行に test の一時 dir か test 用のバイナリを含むものだけ。
    `start` / `held` の `setpgid` の引数は消した (常に専用グループ)。テストを途中で落とす手で確かめた: 孫は残らなかった。
    負の対照 (グループの残りを止めない版) では孫が PPID の付いたまま残った。**残る穴**: テストのプロセスが panic・SIGKILL で落ちると後始末が走らない
    (2026-10-23 に gone の書き損じの無限再帰で 1 本残り、pid で止めた)
  - [x] F2 `TestOrphanedChildKeepsTheTurn` に起きた回数の上限 (0.5 秒で 20 回)。正しい版は 0.5 秒で 4 回 (3 回測って 3 回とも)。変異 (孤児でも blocking で眠る) → red
  - [x] F3 881 で済んだ (`TestStatus*`)
  - [x] F4 `TestTermStopsDescendantsAndDiesBySignal` (TERM で子孫ごと止め、自分も TERM で死ぬ。exitAs の死に直しを外す変異 → red) /
    `TestTermWhileWaitingRemovesTicket` / `TestMaxWaitAppliesWhileWaitingForRunLock` / `TestTermWhileWaitingForRunLock`。
    🚨 **このテストで本物の不具合が見つかった**: run.lock の待ち (acquireRun) で待ちの上限・シグナルを受けると、後始末の `rf.Close()` が、
    同じ fd で blocking の flock に入った goroutine を待って返らず、serial-run が終わらなかった (直す前からの形)。待ちで抜ける 3 つの出口は run.lock を閉じず、
    プロセスの終了に任せる。変異 (上限 / シグナルの出口で閉じる・acquireRun が deadline を見ない) → red。同じ形 (goroutine の blocking flock の fd を閉じる) は src に他に無い (grep)
  - [x] F5 `TestStaleTmpTicketIsSwept` / `TestHistoryRecordsChildRC` / `TestHeldTokenOfWaitingTicketDoesNotBypass`。変異 3 本 → red
  - [x] F6 hook の表に `make lint-go`・引用の中の serial-run (正)、`ls # x; make test`・`(make test`・`echo a) (make test` (負) を足し、tool_name の検査を `command` を持つ入力で見る。
    変異 (lint- の接頭辞を外す・tool_name を見ない) → red。`serial-run -- make test && make lint` は 879 で表に入っている
  - [x] F7 シェルテストの「重くない」の例を、入口の手がかりの語を通る `make -n test` に替え、通った件数を数えて出す (0 件なら落とす)
  - [x] F8 シェルテストは `SERIAL_RUN_STATE_DIR` / `XDG_STATE_HOME` を一時 dir に向ける (HOME は Go のキャッシュのため替えない)。テストの前後で本物の hook.log の大きさが変わらないのを見た
  - [x] F9 狭い PATH の対照 (同じ PATH でこの checkout の serial-run を先頭に置けば差し込む) を足した
  - [ ] F10 **消さない**: `TestMiddleWaiterDeathKeepsOrder` は唯一の検出者になる変異が無いが、テストを削るのは守るものを減らす変更で、残しても害は時間 (1 秒未満) だけ
  - [x] F11 pid のファイルは `readPid` で読み、読めなければ落とす
  - [x] F12 生存は `gone(pid)` 1 つに寄せた (serialrun の中だけ。module をまたぐ共通化は凍結のまま)
  - [x] F13 `waitFor` の上限と刻みを定数にし、文言を上限から作る (serialrun の中だけ)
  - [x] F14 `f.waitStarted(name)` (26 か所) / `f.waitTickets(n)` (16 か所) に寄せた。rc の検査は寄せていない (期待する rc がテストごとに違い、helper にすると読みにくくなる)
  - [x] F15 札のパスは `f.ticketPath(seq)` (本体の `queue.ticketPath` を通す)
  - [x] F16 刻みの定数 (`testTick` / `reportTick`) を env と否定の確認の窓 (`negWindow`) の両方に使う
  - [x] F17 hook の出力は `runHookJSON` 1 つで読む
- 変異 (全部 red): 孤児でも blocking で眠る / exitAs が死に直さない / acquireRun が deadline を見ない / 上限・シグナルの出口で run.lock を閉じる /
  作りかけの札を掃除しない / history の rc を 0 にする / SERIAL_RUN_HELD の札の前の札を見ない / lint- を外す / tool_name を見ない
- 敵対的レビュー 1 周目 (Opus): P1 なし。P2 1 / P3 2。テストと無関係なプロセスを撃つ経路は見つからなかった
  - P2 後始末が、コマンド行に一時 dir を含まない子孫 (`sleep 41 &`) を撃たない (再現済み。直す前の setpgid=true の 7 本より後退) → 先頭が未回収の間 (pgid が使い回されない間) はグループごと止める
  - P3 run.lock を閉じない修正は直す前より良い (直す前の版で 2 本のテストが「10 秒で終わらない」で赤を確認・レビュー)。残る窓: select が上限・シグナルを選んだ後に goroutine の flock が取れると、
    札の無いまま終了まで run.lock を持つ (普通は数 ms〜100ms。推論のみ)
  - P3 `gone` のコメントが用途と逆 → 直した
  - ユーザーの指示 (2026-10-23「ペースを下げて」) で、この直しには 2 周目を当てていない
