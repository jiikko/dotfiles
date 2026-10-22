# 878 (bug): serialrun の排他・順番が壊れる形 (2 本走る / 永久に待つ)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-23〜）

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ A の 11 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### A1 ps の lstart を LC_ALL / TZ を固定せずに札へ書き、読む環境が違うと孤児の子を「死んだ」と読んで札を消す (2 本同時に走る)
- 重要度: P3 (反証レビューで P2 から下げた。下の「反証レビュー」) / 確認済み (LC_ALL=C と ja_JP.UTF-8 で実験。次の serial-run が待たずに走り、sleep の子は生存)
- 根拠: queue.go `procStart` (`/bin/ps -o lstart=` の出力をそのまま保存) / `stateOf` (`procStart(ChildPid) == ChildStart` の文字列比較 → 偽なら `os.Remove` して stateGone)。main.go `procStat` が同じ ps の呼び方をもう 1 つ持つ。`grep LC_ALL|TZ src/serialrun` は 0 件
- 直し方の案: `procStart` で `cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")` を付ける。または lstart ではなく開始時刻を数値 (sysctl) で持つ。`procStart` と `procStat` を `psField(pid, field)` 1 つに寄せて 1 か所で固定する。テストは「別ロケールで読んでも orphan のまま」。検査 script は足さない (該当 1 行。I14)
- 出典: leaky-abstraction P2-1 / lint-from-done P3-1 / duplication P3-2 (ps を起こす所が 2 つ)

### A2 procStart が「子はもう居ない」と「ps を起こせない・失敗した」を同じ "" に畳む
- 重要度: P3 (反証レビューで P2 から下げた) / 推測 (構造は読んだ。ps の失敗は再現していない)
- 根拠: queue.go `procStart` (`Output()` の err を全部 "" へ) / `stateOf` (`procStart(child) == info.ChildStart` が偽なら札を消す) / main.go `runQueued` の started callback (`ChildStart = procStart(pid)`)。(a) 孤児の判定中に ps が fork できない (EAGAIN/EMFILE。重い負荷の最中) → 孤児を死んだと読んで札を消す = 2 本同時。(b) 記録時の失敗で ChildStart="" のまま残り、その子は一生孤児として扱われない
- 直し方の案: ps の終了を分ける (ExitError rc=1 かつ空 = 居ない / それ以外 = 判定不能)。判定不能は stateOrphan 側 (消さない、recheck へ)。記録時の失敗は 1 回再試行し、それでも "" なら ChildPid も書かない (A3 と同じ commit)
- 出典: error-handling P2-1

### A3 子の pid / 開始時刻を札へ書く手順に窓と、失敗の握りつぶしがあり、半端な記録が残る
- 重要度: P3 / 窓は推測 (SIGKILL を当てて 2 本走ることは未再現)、`_ =` は確認済み (queue.go を読んだ)
- 根拠: main.go `runChild` の started callback (`cmd.Start()` の後、`procStart` (= ps を exec) を挟んで `rewriteInfo`。その間に serial-run だけが SIGKILL されると ChildPid が無い札 → 死んだ札として消され、走り続ける子の後ろに次が並ばない)。queue.go `rewriteInfo` の `WriteAt` 後の `_ = f.Truncate(...)` (ENOSPC/EDQUOT で新旧の混ざった中身が残る)
- 直し方の案: pid だけ先に書き (ps は後で追記)、ChildStart が空の札は「子の生死が判定できない」(stateUnknown 相当) にする。WriteAt/Truncate の失敗は呼び出し側へ返す。golangci の `errcheck.check-blank` は serialrun の local にだけ入れる案があるが、全体には入れない (I14)
- 出典: broken-code P3-3 / error-handling U-1 / lint-from-done P3-2

### A4 「TERM を受けて子孫を止める」途中 (SIGSTOP の後) に serial-run が SIGKILL されると、子の木が停止のまま永久に残り、全員が `--max-wait` まで待つ
- 重要度: P2 / 確認済み (遅延 0.01〜0.07 秒の 10 回中 5 回で sh と sleep 6 本が TN で残る。2 本目が 12 秒待って rc=125)
- 根拠: src/proctree/tree.go `Target.Stop` (子孫を SIGSTOP で凍らせてから TERM → CONT。凍結と TERM の間が窓) / main.go `runChild` / queue.go `stateOf` (孤児判定が「子の pid が居て lstart が一致」だけで、停止 (T) を見ない)
- 直し方の案 (両方): (a) Stop は先に Root へ TERM を送ってから凍らせる、または凍結の直後に TERM と CONT を同じ呼び出しで送る (proctree のテストで、集め直して撃てば逃がさないことを確かめる)。(b) `stateOf` で、子が停止 (stat T) かつ持ち主が死んでいるなら「走っている」と数えず、子孫ごと CONT + KILL して札を消す。(a) は src/runtimeout (同じ Stop) の窓も直る。テストは「Stop 中に呼び出し元を KILL しても木が T で残らない」
- 出典: concurrency P2-1

### A5 状態の置き場・lock 名の「同じ」を文字列で比べるので、綴りが違うと別物になる (入れ子が親の札を待つ自己デッドロック / 別の待ち行列)
- 重要度: P3 / 確認済み (大小文字 `-l Default` で 3 秒待って rc=125、置き場を symlink 経由にしても rc=125。`XDG_STATE_HOME=rel` で cwd ごとに別の `a/rel` `b/rel` ができる)
- 根拠: main.go `heldByAncestor` (`v[:i] != q.dir` の文字列比較) / `stateBase` (`filepath.Abs` は SERIAL_RUN_STATE_DIR の枝だけ) / `lockNameRe` (大小文字を区別して通す。APFS は区別しない)。相対の SERIAL_RUN_STATE_DIR も、子が cd した先で呼ぶと照合に失敗 (broken-code U-2。未確認)
- 直し方の案: stateBase の全枝を最後に Abs (+ EvalSymlinks)。相対の XDG は仕様どおり無視して既定に落とす案もある (どちらかを 869 に書く)。HELD の照合は文字列でなく置き場の (dev, ino)。lock 名は小文字に畳む (または大小文字だけが違う名前を拒否)
- 出典: concurrency P3-1 / leaky-abstraction P3-2 / broken-code U-2。D9 (`.`/`..`) と同じ commit にしてよい

### A6 入れ子の素通し (`SERIAL_RUN_HELD`) が「環境変数の継承」と「番号が合う札があること」だけに依存する
- 重要度: P3 (反証レビューで P2 から下げ、発火条件を絞った) / design は確認済み (`env -i` で内側が親の札を待ち 2 秒で rc=125。`sh -c` 経由は素通し)。security P3-3 は確認済み (置き場を消して番号が 1 から再利用され、古い HELD を持つプロセスが素通し)。error-handling P3-4 の EMFILE は未再現・推測。encapsulation P3-2 の書式変更は推測
- 根拠: main.go `heldByAncestor` (祖先関係は環境変数だけ。`st != stateAlive && st != stateOrphan` で stateUnknown も false) / runChild の `Sprintf("%s=%s:%d", ...)` と `heldByAncestor` の `LastIndex(":")` + `ParseInt` とテスト 3 か所。現状は serial-run を明示で入れ子に呼ぶ道具が無く未顕在。**869:312-322「Makefile への組み込み」を入れた時点から顕在化する** (tests/ の `env -i` / isolate_env 系が `make` を呼ぶと 1 時間止まる)
- 直し方の案: (1) 札の Pid が自分の祖先 (ppid を辿る) にいることも見る。環境が落ちても祖先の札が生きていれば並ばない (または 125 で即失敗して「入れ子で並んだ」と出す)。(2) 札に起動ごとの nonce を持たせ、HELD に番号 + nonce を載せて両方一致を見る (番号の再利用に効く)。(3) stateUnknown は素通し側 (true) か 125 で明示失敗。(4) `heldToken(seq)` / `parseHeldToken(v)` を queue.go に置き、runChild とテストを通す。Makefile 組み込みより前にやる
- 出典: design P2-3 / security P3-3 / error-handling P3-4 / encapsulation P3-2

### A7 「マシン全体」の排他が、置き場の解決に使う環境 (HOME / XDG_STATE_HOME / SERIAL_RUN_STATE_DIR) が同じ session 同士にしか効かない
- 重要度: P3 / 確認済み (`XDG_STATE_HOME=a` と `=b` の 2 本の `sleep 3` が 1 秒後に同時に走っていた)
- 根拠: main.go `stateBase`。tests/ の複数が HOME / XDG_STATE_HOME を使い捨てへ差し替える。Makefile 組み込み (869:312-) で isolate_env 系が入れ子を呼ぶと、素通しにも待ちにもならず別の行列で走る
- 直し方の案: 置き場を HOME の外の固定の場所 (`/var/tmp/serial-run-<uid>` など。$TMPDIR は使わない) に決め打ちする。または README に「環境が同じ session 同士だけ。他ユーザーとの共有は対象外」と書く (E8 に寄せてよい)
- 出典: concurrency P3-2

### A8 enqueue (seq の flock の中) にデッドラインもシグナルの経路も無く、テスト用の差し込み口 (`SERIAL_RUN_TEST_PAUSE_BEFORE_RENAME`) が本番の同じ経路にある
- 重要度: P3 / 推測 (環境変数が漏れる具体的経路は無い。構造の指摘)
- 根拠: queue.go `enqueue` / `createTicket` (環境変数が立っていると fifo に書かれるまで seq の flock を持ったまま blocking read) / main.go `runQueued` (Notify 済みだが sigs を読むのは enqueue の後)。seq の flock を持った側が SIGSTOP / FS 停止で止まっても、全 session の enqueue が止まり SIGKILL でしか抜けられない
- 直し方の案: テスト seam は `-tags` か `testing` 側に逃がす。enqueue の flock は LOCK_NB + 短い再試行にして deadline と sigs を見る
- 出典: design P3-3

### A9 `tickets()` の ReadDir 失敗を「札が 0 枚」と同じに読み、待ち手が順番を飛ばして run.lock の奪い合いに入る
- 重要度: P3 / 推測 (排他自体は run.lock が守るので 2 本同時にはならない。順番の保証が崩れる)
- 根拠: queue.go `predecessors` / `maxTicket` の `seqs, _ := q.tickets()` と、`waitPredecessors` が preds 空を waitTurn と読む所。`maxTicket` 側は seq が壊れ、かつ ReadDir も失敗したときだけ番号が 1 から振り直される (二重故障)
- 直し方の案: 列挙の失敗は「判定不能」として次の recheck まで待ち直す (preds が取れたときだけ waitTurn)
- 出典: error-handling P3-5

### A10 `blockUntilReleased` の失敗の扱いが両方向に偏っている (開けない → 起こさない: 最大 2 分の遅れ / flock 失敗 → 起こす: 空回り)
- 重要度: P3 / 推測 (どちらも EMFILE・ENOLCK を作る実験はしていない。害は遅れと CPU。排他は崩れない)
- 根拠: queue.go `blockUntilReleased` (open に失敗しても IsNotExist 以外は woke を閉じない → `fallbackPoll` 2 分ごとの見直しだけで進む。concurrency P3-3) / 同関数の `_ = flock(f, LOCK_EX)` の後で無条件に `close(woke)` (EINTR 以外の失敗でも起こす → 列挙し直し → 同じ札 → 張り直しの空回り。error-handling P3-6)。probe の flock も EWOULDBLOCK 以外を「生きている」に倒す
- 直し方の案: open 失敗・flock 失敗のどちらも「判定不能」として呼び出し側の recheck に任せる (woke は閉じない、または recheck を短くして stateUnknown 経路へ)。F2 のテストを先に直してから触る
- 出典: concurrency P3-3 / error-handling P3-6

### A11 `ps` の起動 (`listProcs` / `aliveOf` / `procStart`) に時間の上限が無く、詰まると待ちの select に戻れない
- 重要度: 未確認 / 推測 (ps を詰まらせる手段が無く未実験)
- 根拠: src/proctree の `Stop` 内の `ps` と、queue.go `stateOf` の `procStart`。`runChild` の TERM 経路や `waitPredecessors` の列挙が戻らない
- 直し方の案: ps に context の上限を付け、超えたら「判定不能」(A2 の分類) にする。実験できていないので優先度は低い
- 出典: concurrency U-1

---

## 反証レビュー (2026-10-23、Opus 1 体・読み取りのみ。実験はしていない)

本文の重要度と案は下の訂正を反映済み。訂正の根拠を残す。

- **A4 の案 (a) を取り下げる** (P2 の指摘): 「先に Root へ TERM」「凍結の直後に TERM+CONT」はどちらも凍結の意味を外す。`src/proctree/tree.go` の `Target.Stop` の注記
  (「凍らせるのは、親が先に死んで子が init へ付け替わると木から外れて取りこぼすため」・setsid した子が 6 回中 13〜20 個逃げた実測) と衝突し、runtimeout も劣化する。
  **案は (b) だけ**: `stateOf` が「持ち主が死んでいて、子が停止 (stat T)」を見たら、子孫ごと CONT + KILL して札を消す。持ち主ではない待ち手が他人の木を止めるので、
  所有の判定 (子の開始時刻の一致。A1 を直した後の形) を前提にする
- **A1 / A2 を P3 へ**: 「2 本同時」は、孤児の状態 (serial-run だけが pid 宛ての SIGKILL で死ぬ) に入った上で、ロケールが食い違う (A1) / ps の fork が失敗する (A2) の
  二重条件でだけ起きる。2 本走ること自体は正しい (持ち主が死ねば run.lock は外れる)
- **A6 の発火条件を絞る**: 止まるのは「HELD だけが落ち、置き場の解決が同じ」場合 (`env -u SERIAL_RUN_HELD`・`env -i HOME=$HOME …`) だけ。`tests/tmux/lib/isolate_env.sh` は
  HOME を差し替えるので A7 (別の行列で走る) になり止まらない。素の `env -i` は HOME が無く `os.UserHomeDir` が失敗して即 125。Makefile の組み込み (869) の前提として残す
- **A3 を絞る**: 「Truncate の失敗で新旧の混ざった中身が残る」は起きない (書き直す本文は child_pid の分だけ常に長く、WriteAt が全体を上書きする)。残すのは
  「WriteAt の途中失敗で JSON が壊れると、子の情報が無い札として消される」だけ。**新しい指摘を足す**: `runQueued` の `info` は `enqueue` に値で渡すので `info.Seq = next` は
  写しにしか入らず、`rewriteInfo` は札へ `"seq":0` を書き戻す (今は `.Seq` を読む所が無いので無害。dead-code P3-3 と合わせて直す)
- **A5 の補強**: 869 の「入れ子」節は HELD を「置き場の絶対パス」と定めているのに、`stateBase` の XDG 枝は Abs を通さない (869 との乖離)
- **A10 の補強**: `blockUntilReleased` の注記「起こさなければ、呼び出し側の短い見直しが拾う」はコードと合わない (`waitPredecessors` は前の札が stateAlive のとき recheck を張らず、
  拾うのは 2 分の fallback だけ)。注記も直す
- 反証できなかった: A7・A8・A9・A11。A2・A5・A10 の根拠 (file:シンボル) は合っていた。既に直っているもの・869 で受け入れ済みのものは無かった

## 進捗

- [ ] 未着手 (項目ごとに、直した commit の subject・当てた変異・実測を書く)
