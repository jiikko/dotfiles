# 881 (ux): serialrun の状態の置き場・status・history

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ D の 9 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](../886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](../869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### D1 `printStatus` と hook の `queueNote` が札の列挙・状態判定を `stateOf` を通さず自前で持ち、孤児・読めない札を「無い」と出す (status は rc=0 + stdout にエラー)
- 重要度: P3 / 確認済み (design: SIGKILL 後の status が `(持ち主なし)` と「走っているものも待っているものも無い」を出す一方、待ち手側は「子 pid … がまだ走っている」と待つ。error-handling: chmod 000 で `status_rc=0`・文言は stdout)。ui の「書き換えの窓で読めない」と encapsulation U-1 は未確認
- 根拠: main.go `printStatus` (flock が取れた = dead とだけ見る / `os.Open`・`readInfo` の失敗を `continue` で捨てる / `q.tickets()` の err を stdout に出して rc=0)。hook.go `queueNote` (`queue{dir: ...}` を自分で組み、`os.Stat(q.qdir())` で置き場の有無を判定し、`predecessors(math.MaxInt64)` で「全員」を表す)
- 直し方の案: queue に `waiting()` (置き場が無ければ空・作らない) を足し、queueNote と printStatus が同じ入口を通る。孤児は「走っている (持ち主の serial-run は死亡。子 pid N)」と出して n に数える。読めない札は `(情報を読めない)` の行にして n に数える。失敗は stderr + rc=125。`rewriteInfo` の書き換え中の読み (表示が 1 回欠ける) もこの経路で `stateUnknown` に落ちる。F3 のテストと同じ commit
- 出典: design P3-1 / error-handling P3-2 / ui-components P3-6 / encapsulation P3-3・U-1

### D2 読むだけの `serial-run status` が置き場 (lock の dir) を作る
- 重要度: P3 / 確認済み (`status -l defualt` で `<state>/defualt/queue` ができ rc=0 で「無い」)
- 根拠: main.go `run` (status の分岐より前に `openQueue` → `MkdirAll`)。hook の `queueNote` は「無ければ作らない」設計で、同じ「見るだけ」の入口が違う規律
- 直し方の案: status は `os.Stat(q.qdir())` が無ければ「その lock は無い」と返す。置き場は消さない規約なので、typo の空 dir を掃除する経路は無い (resource-leaks P3-3 も言及)
- 出典: broken-code P3-2 / responsibility P3-2

### D3 history.log は「実行した run」だけを記録し、待ちを諦めた・上限で抜けた・中断された待ちが記録されない。869 の計測の母集合が偏る
- 重要度: P2 / 確認済み (実機の seq が 29 まで進んでいるのに history.log は 22 行。seq 23〜28 の 6 本が 1 行も残さず消えた。欠番の理由は推測)
- 根拠: main.go `appendHistory` の呼び出しが runQueued の実行後の 1 箇所だけ。869:332 の「5. 計測」(history.log の待ち・所要を読む) と、869:307 の「待ちの上限 1 時間は合計で尽きる」の頻度が、いちばん知りたい値なのに取れない。history.log に「hook が差し込んだか手で起こしたか」の列も無い (実機の最初の行は `/bin/echo hello` の手動実行)
- 直し方の案: 待ちを抜ける全経路 (上限・シグナル・EMFILE などの失敗) で rc と「待っただけで走らなかった」(ran=-) を 1 行足す。または札の作成時点で `enqueue` 行を足し終了行と対にする。hook 由来かの列を足す。B6 (execDirect の `direct` 行) と同じ commit にしてよい
- 出典: general P2-2 (+ false-green P2-2 の execDirect の件)

### D4 置き場の配置のうち history.log だけ queue.go の外 (main.go とテスト) が直書きで知っている
- 重要度: P3 / 確認済み (production 1 / test 1)
- 根拠: main.go `appendHistory` の `filepath.Join(q.dir, "history.log")`、main_test.go の同文字列
- 直し方の案: `q.historyPath()`。ついで `appendHistory` を queue.go 側へ (5 行)
- 出典: encapsulation P3-4

### D5 札の寿命 (消す → 閉じる → run.lock を放す) と「子の記録」を queue の外の `runQueued` が手で守っている
- 重要度: P3 / 出口の数と対の更新箇所は確認済み。壊れ方は推測 (短命 CLI なので fd は終了で解ける)
- 根拠: main.go `runQueued` の `leave` クロージャ呼び出し 5 か所と `rf.Close()` の併記 5 か所 (「札を消してから run.lock を放す」の順のコメントは最後の 1 か所にだけある) / `info.ChildPid, info.ChildStart = pid, procStart(pid)` + `rewriteInfo` (対の 2 値の更新を呼び出し側が書く。片方を忘れると孤児の子の裏で次が走る。silent)
- 直し方の案: `ticket` 型 (file + seq + path + info) に `setChild(pid)` / `release()` を持たせ、release が「消す → 閉じる → run.lock を放す」を 1 か所で行う。A2・A3 と同じ領域なので先に型を作るか、後でやるか順番を決める
- 出典: encapsulation P3-1

### D6 `ticketInfo.Seq` は書かれるだけで、誰も読まない
- 重要度: P3 / 確認済み (`grep '\.Seq\b'` は queue.go の代入 1 件だけ。番号は札のファイル名から取る)
- 根拠: queue.go `ticketInfo.Seq` と `enqueue` の `info.Seq = next`。札の中身に「番号」が 2 つあるように見える。未知のキーは json.Unmarshal が無視するので旧版の札との互換の問題は無い
- 直し方の案: フィールドと代入を消す。残すなら「札を cat したときの目印」とコメントで理由を書く
- 出典: dead-code P3-3

### D7 札の JSON を読む入口が 2 つ (`stateOf` の ReadFile + Unmarshal と `readInfo(seq)`)
- 重要度: P3 / 確認済み (読んで)。害は小さい
- 根拠: queue.go `stateOf` と `readInfo`。片方だけ緩めると孤児判定と表示が食い違う
- 直し方の案: `readInfoAt(path)` を 1 つ作り readInfo がそれを呼ぶ (5 行)
- 出典: duplication P3-1

### D8 `ticketInfo.Start` は「札を出した時刻」なのに、「長く持っている」判定と表示が「実行を始めてからの時間」として読む
- 重要度: P3 / 読みのみ (式と代入の突き合わせは確実。longHold 30 分に短縮用の差し込み口が無く実測していない)
- 根拠: main.go `runQueued` (`Start: enq`)・`describe` (`held := time.Since(info.Start)` → `held >= longHold` で 🚨)・`printStatus` (待ち手にも同じ式を当てる)・hook.go `queueNote`。前の持ち主を 30 分待って先頭に上がった札が、走り出した直後から 🚨 になる。LLM が正常なコマンドを止めうる
- 直し方の案: ticketInfo に `RunStart` を足し (rewriteInfo で子を起こした時に書く)、🚨 の判定と表示はそれを使う。未実行の札には 🚨 を付けない。E6 (表示の共通化) と同じ場所なので同時にやる (手戻りが無い)
- 出典: leaky-abstraction P3-1

### D9 `-l` の lock 名に `.` / `..` が通り、状態の置き場の外へ固定名のファイル (seq / run.lock / history.log / queue/) を作り、既存の seq を上書きする
- 重要度: P3 / 確認済み (`$S/proj/seq` に "precious" と書いた状態で `sr -l .. -- true` → seq が `000000000001` に上書き)
- 根拠: main.go `lockNameRe = ^[A-Za-z0-9._-]+$` (`.` `..` も一致)。影響は親 dir に 4 つの固定名を作る・同名の既存ファイルの先頭 13 byte を書き換えるまで
- 直し方の案: 先頭が英数字に限る形にし、`.` / `..` を拒否する。A5 (lock 名の大小文字) と同じ commit
- 出典: security P3-1

---

## 進捗

- 2026-10-23 (commit「fix(serialrun): 状態の置き場・status・history を直す (issue 881)」):
  - [x] D1 status と hook の案内 (`queueNote`) は待ち手と同じ入口 `lineup` (= `predecessors` を `stateOf` で) で数える。孤児の子は「持ち主の serial-run は死んだ。子 pid N がまだ走っている」、
    開けない札・読めない札も 1 行に出して数える。札の一覧を読めなければ stderr + rc=125。`lineup` は見るだけで、死んだ札を消さず止まった孤児の子も回収しない (回収は待ち手の `predecessors`)。
    `TestStatusShowsOrphanChildAsRunning` / `TestStatusFailsLoudWhenQueueUnreadable`。入れ子の保険 `heldByAncestorProcess` も同じ入口へ寄せた
  - [x] D2 status は `openExistingQueue` (作らずに開く) を通す。無い lock なら「一度も使われていない」と rc=0。hook.log の書き手も同じ入口。`TestStatusDoesNotCreateQueue`
  - [x] D3 待ちの上限・シグナル・待ちの失敗・待ち列を使えない経路 (札を出す前に抜けた分も番号 `-` で) も history.log に 1 行。結末の列 (ran / timeout / signal / error / unqueued) を末尾に足した (古い版の 7 列の行と混ざっても cwd・コマンドの位置が変わらない)。unqueued は exec した結果を見ないので rc=-。
    `TestHistoryRecordsRunsThatDidNotRun`。**hook 由来かの列は足していない**: 差し込む前置きは許可ルール (`Bash(serial-run -- make test:*)`) のために固定の `serial-run -- ` で、
    印を足すと前置きが変わる。hook.log が差し込んだ時刻とコマンドを残すので、計測ではそちらと時刻で突き合わせる
  - [x] D4 `historyPath` / `hookLogPath` を queue に置き、`appendHistory` を queue.go へ移した (テストも同じ関数でパスを作る)
  - [x] D5 自分の札を `ticket` 型にし、子の記録 (`setChild`: pid → 走り出した時刻 → 開始時刻の順で書く) と後始末 (`release`: 消す → 閉じる → run.lock を放す) をその上に置いた。
    `runQueued` の出口 5 か所は `tk.release(rf)` (run.lock を開く前は `release(nil)`)
  - [x] D6 `ticketInfo.Seq` を消した。878 で足した `TestTicketRecordsItsSeq` (子の情報を書き直したとき seq を 0 へ戻さない) も消した: 欄が無ければその退行は起こりえない。
    読み手は 0 件 (番号は札のファイル名から取る)。古い版の札の `seq` は json.Unmarshal が無視する
  - [x] D7 札を読む入口を `readInfoAt(path)` 1 つにした (`stateOf` と `readInfo` が通る)
  - [x] D8 札に `run_start` (子を起こした時刻) を足し、「長く持っている」の 🚨 は走り出してから 30 分で付ける。表示も「走り出してから N」「札を出してから N」を分けた (describe・status・hook の案内が同じ `age`)。
    まだ走っていない札は、先頭のときだけ札を出してから 30 分で 🚨 (持ち主が止まった・run_start を書かない古い版)。`TestLongHoldCountsFromRunStart` / `TestTicketRecordsRunStart`。札の版 (`ticketVersion`) は 3
  - [x] D9 878 で済んでいた (`lockNameRe` が `.` / `..` / 先頭の `.` / 大文字を拒否する。`TestLockNameRejectsCaseAndDots`)
- 変異 (全部 red): status が置き場を作る / 孤児を開けない札として出す / 読めない置き場で rc=0 / 上限で抜けた run を history に書かない / 長く持っているを札を出した時刻から数える /
  走り出した時刻を札に書かない / lineup が生きている札だけを返す (孤児を数えない)
- 敵対的レビュー 1 周目 (Opus): 2 本同時・順番抜け・永久待ちは壊せなかった。P2 1 / P3 5
  - P2 走り出す前に止まった先頭に 🚨 が付かない (直す前は付いた。再現済み) → 先頭の未実行の札は札を出してから 30 分で 🚨
  - P3 status と hook が止まった孤児の子を止め、札を消す (直す前の status は触らなかった。再現済み) → `lineup` を回収なしにした。`TestStatusDoesNotReclaimStoppedOrphan`
  - P3 unqueued の行が rc=0 → rc=- / P3 history の列が版で混ざる → 結末を末尾に / P3 札の版を上げていない → 3
  - P3 **記録**: status / hook が孤児の札を probe する間 flock を持つので、env を落とした入れ子の `heldByAncestorProcess` がその札を生きていると読み、死んだ持ち主の pid で祖先かを見て外す。
    待ち手どうしでも前からある競合 (推論のみ。再現していない)
- 変異 (レビュー対応、全部 red): lineup が回収する / 走らない先頭に 🚨 を付けない / 後ろの札にも付ける
- 敵対的レビュー 2 周目 (Opus): P1 / P2 なし。入れ子の判定 (回収なしの lineup) も先頭の 🚨 も壊せなかった
  - P3 status が死んだ札を消さないことに検査が無い → `TestStatusDoesNotRemoveDeadTicket` (変異: lineup が死んだ札を消す → red)
  - P3 lineup の注記に入れ子の保険 (heldByAncestorProcess) が抜けていた → 足した
  - P3 **受け入れ**: 止まった孤児の子を status は「がまだ走っている」と出し 🚨 を付けない (直す前は status が止めて消していた)。開始時刻で確かめられない札は待ち手も回収しないので、上限まで同じ表示
  - P3 **受け入れ**: 版 2 の走っている札は run_start が無いので、待ちを含めて札を出してから 30 分で 🚨 (移行期だけ)
