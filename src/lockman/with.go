package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// renewDivisor は自動更新の間隔 = TTL / renewDivisor。TTL 30 分なら 10 分ごと。
const renewDivisor = 3

// renewOutcome は自動更新の結果。**「lease を失った」と「判定できない」は別物**で、
// 091:398-399 が終了コードを分けている (122 = 走行中に lease を失った / 125 = 判定不能)。
// 単一の bool に畳むと、一過性の I/O ヒカップが「引き継がれた」として報告される。
type renewOutcome int

const (
	renewOK            renewOutcome = iota
	renewLost                       // errNotOwner = 本当に持ち主でなくなった
	renewIndeterminate              // I/O タイムアウト・probe 失敗・打刻ずれ
)

// classifyRenewErr は Renew の失敗を終了コードの分類へ落とす。
// 🚨 errNotOwner は %w でラップされて返る経路がある (lock.go の「lease が切れている」)
// ので、== ではなく errors.Is で見る。
func classifyRenewErr(err error) renewOutcome {
	if errors.Is(err, errNotOwner) {
		return renewLost
	}
	return renewIndeterminate
}

// runWith は「取得 → 実行 → 確実に解放」。長い処理でも lease を失わないよう自動更新する。
//
// 終了コードは呼び出し側の API なので、子プロセスの終了コードをそのまま透過し、
// ロック側の失敗は子と衝突しない上位番号 (121/122/125) へ逃がす。
func runWith(l *Locker, ttl time.Duration, label string, onLostKill bool, argv []string) (rc int) {
	// 🚨 **打刻より前**の時刻を控える (issue 385)。lease の期限はサーバが打刻した時刻 + TTL だが、
	// こちらから見えるのは「呼ぶ前」と「返った後」だけ。打刻は必ずその間にあるので、
	// **呼ぶ前**を使えば「最も早い期限」= 保守側の見積もりになる (遅く見積もると、
	// 他者が正当に引き継げる時刻を過ぎてから昇格することになる)。
	acquireStartedAt := time.Now()
	meta, err := l.AcquireTimed(ttl, label)
	if err != nil {
		if errors.Is(err, errBusy) {
			// 🚨 **「人が動くまで解けない busy」のときだけ理由を足す** (issue 383)。
			// 定型文だけだと、中身を読めない lock による永久 skip が正常な保持中と区別できない。
			// 逆に正常な busy まで理由を出すと、定期ジョブの stderr が毎回汚れる (敵対レビュー P2)。
			if errors.Is(err, errUnreadableLock) {
				warnf("他が保持中のため実行しない (%v)", err)
			} else {
				warnf("他が保持中のため実行しない")
			}
			return exitWithBusy
		}
		// I/O タイムアウトもここへ落ちる (判定不能 = 125。091:418)。
		warnf("%v", err)
		return exitWithInvalid
	}
	defer func() {
		// 🚨 解放に失敗したら、**子が成功していたときだけ** rc を 125 へ上げる (issue 364 の 1)。
		//
		// `with` は 2 つの契約を持つ: ①子の終了コードを透過する ②確実に解放する。
		// 解放に失敗した時点で②は破れているのに、以前は warn だけで rc は子のものを透過して
		// いたので、**rc だけを見る呼び出し側からは成功に見えた** (実測: 解放直前に probe/ が
		// 消えると rc=0 のまま ttl_ms=1800000 の lock が 30 分残る)。
		//
		// 上書きを「子が rc=0 のときだけ」に絞るのは、非 0 の rc は既に「何かが失敗した」を
		// 伝えており、そこを潰すと情報が減るだけだから。**真の穴は子が成功した場合だけ** で、
		// そこは呼び出し側から成功と区別できない。
		//
		// 🚨 `errNotOwner` は上書きしない: 既に他者が引き継いでいるので、②で守りたい害
		// (次の実行が TTL 切れまで待たされる) が出ない。
		// 🚨 ただし **`errLeaseExpired` も `errNotOwner` を包む**ので同じ枝で除外される
		// (`lock.go` の `errLeaseExpired = fmt.Errorf("%w: lease が切れている", errNotOwner)`)。
		// こちらは**自分の lock がディスクに残ったまま**なので、「解放すべきものが無い」は
		// 事実ではない。それでも上げないのは、期限切れの lock は次の acquire が即座に
		// 引き継げるため**待たされる害が出ない**から (残るのは掃除されるまでの実体だけ)。
		// 🚨 122 (lease 喪失) / 125 (判定不能) を塗り潰さない: どちらも rc != exitOK なので
		// 上の条件で自然に除かれる。lease 喪失のほうが具体的な情報なので残す。
		if err := l.ReleaseTimed(meta.Token); err != nil && !errors.Is(err, errNotOwner) {
			warnf("解放に失敗: %v", err)
			if rc == exitOK {
				// 文面は「子は成功したがロックが残っている」= 呼び出し側が次に何を見ればよいかまで書く。
				warnf("子は成功したがロックを解放できていない: 次の実行は TTL が切れるまで待たされる (%s)", l.dir)
				rc = exitWithInvalid
			}
		}
	}()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// 子を独立したプロセスグループに置き、**lease を失ったときに**まとめて止められるように
	// する (この 1 点のためだけ。091:282 の `--on-lost=kill`)。
	//
	// 🚨 **「子が孫を作ったまま残るのを防ぐ」ではない** (issue 356 でコメントを訂正した)。
	// グループへ撃つのは (a) シグナルを受けたとき (b) lease を失ったとき の 2 経路だけで、
	// **子が正常終了した経路には無い**。`sh -c 'cmd & exit 0'` のように孫を置いて親だけ
	// 終わる形では、孫が走ったままロックが解放される (実測: 解放後の孫と次の保持者が
	// 同じ資源へ交互に書いた)。091 は孫の封じ込めを約束していないので、ここは
	// **直さないと決めた** — 正常終了時にグループを薙ぐと、意図的に起こす background の子
	// (`start-server &`) まで殺すことになり、opt-out の新設が要る。
	//
	// 🚨 さらに、**`setsid()` した子孫にはどの経路でも届かない** (Darwin 24.6.0 で実測:
	// グループへ SIGTERM を撃つと非 setsid の孫は死ぬが、setsid した孫は新しい pgid へ
	// 移っており生存する)。プロセスグループで回収できる範囲が上限。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 🚨 **ハンドラは子を起こす"前"に立てる** (issue 363)。旧版は `cmd.Start()` の**後**で
	// `signal.Notify` していたので、その窓で届いた INT / TERM / HUP は**既定処理**で lockman を
	// 即死させる。子は `Setpgid: true` で別のプロセスグループに居て端末のシグナルを受けないので、
	// **孤児として走り続ける** — lease を更新する者が居ないまま TTL が切れ、他ホストが引き継ぐと
	// 同じ排他区間に子が 2 つ並ぶ。出典の A-B 実測 (40 試行 ×3、遅延 0..10ms でランダムに SIGTERM、
	// 子は TERM を無視): 漏れ 20/120 のうち **9 件が「孤児の子 + lock 漏れ」**で、
	// **全 20 件が stdout 0B / stderr 0B の完全な無言**だった。
	//
	// 🚨 **`Acquire` 中の窓はこれでも残る** (2026-09-21 の判断で**受容**)。そこは子がまだ
	// 居ないので二重実行にはならず、残るのは `lock` (TTL で解ける) / 調停の目印 (猶予で解ける) /
	// mark (**掃除まで ~1h10m = 既定 TTL 30m を超える唯一の契約違反**)。
	// 消すには「取得中も生きて片付ける」形が要り、それは**取得中の Ctrl-C を即死でなくする**
	// ことと引き換えになる。即死を保つ方を選んだ (mark を消したいなら 366 の猶予側が筋)。
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	if err := cmd.Start(); err != nil {
		warnf("実行できない: %v", err)
		return exitWithInvalid
	}
	pgid := cmd.Process.Pid
	afterChildStartHook()

	ticker := time.NewTicker(ttl / renewDivisor)
	defer ticker.Stop()

	done := make(chan error, 1)
	// exited は「子が回収された」を昇格ゴルーチンへ知らせるためだけの合図。
	// done は select ループが 1 回だけ受け取る (受け手を 2 つにしない)。
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		// 🚨 **close が先**。done を先に送ると、main が受け取って return した後も
		// 昇格ゴルーチンが猶予待ちで残る窓ができる。その間に pgid が再利用されると
		// **無関係なプロセスグループへ SIGKILL を撃つ** (issue 340 項目 2 と同クラス)。
		close(exited)
		done <- err
	}()
	var escalate sync.Once

	outcome := renewOK
	// 更新は **同時に 1 本まで**。詰まっている間は新しく起こさない (tick ごとに積むと
	// 見捨てた goroutine が溜まる。issue 359 の項目 4)。
	//
	// 🚨 **詰まっても更新をやめない。** 以前ここで `ticker.Stop()` していたが、それは
	// 一過性のヒカップを**本物の lease 喪失**に変える: 更新が二度と走らないので lease は
	// 実際に期限切れになり、他マシンが正当に引き継ぐ — 子はまだ走っているので二重実行。
	// 敵対レビュー 2026-09-15 が A-B で実測した (止めた版は他マシンの Acquire が成功、
	// 止めない版は拒否)。しかも既定値 (ttl 30m / tick 10m) のほうが猶予が短い。
	//
	// 🚨 **「やめない」は「期限が来たら見捨てる」まで含む。** 以前は期限切れのあとも
	// renewCh を握ったままにしていたので、`Renew` の syscall が返らないと以後の tick が
	// すべて `continue` になり、`ticker.Stop()` で作ったのと**同じ穴**が残っていた
	// (issue 381。恒久ラッチ = 掴んだ fd が死んだまま、新しい open なら通る stale handle 形)。
	// 期限が来たら参照を捨て、次の tick で新しい更新を積む。
	var renewCh <-chan error
	var renewExpired <-chan time.Time // nil = 更新が走っていない
	// inFlightRenews は「まだ返っていない Renew の本数」(見捨てた分を含む)。
	// goroutine 自身が減らすので、マウントが復旧して溜まった分が返れば枠は戻る。
	var inFlightRenews atomic.Int64
	// renewStartedAt は「いま飛んでいる更新を始めた時刻」(打刻より前)。成功したら期限の起点になる。
	var renewStartedAt time.Time
	// 🚨 上限は**ここで 1 度だけ読む**。毎 tick 読むと、テストが差し替えた値を戻すのが
	// runWith の走行中に重なる経路 (boundedInt の安全網が先に落ちたとき) で data race になる。
	// すぐ下の onLostGracePeriod が「引数で渡して直読みを避けた」のと同じ理由。
	maxInFlight := maxInFlightRenews
	cappedReported := false
	// lease は「自分の lease が生きていると言い切れる限界」を持つ (issue 385)。
	// 期限の計算・境界・保留の状態は `leaseTracker` が 1 箇所で持つ (`lease_tracker.go` の doc)。
	lease := newLeaseTracker(acquireStartedAt, ttl)
	escalateNow := func() {
		// 🚨 **昇格は 1 回だけ**。tick ごとに撃ち直すと猶予が毎回振り出しに戻り、
		// SIGKILL へ永久に到達しない (上限の無い再試行は上限が無いのと同じ)。
		escalate.Do(func() { go escalateGroupKill(pgid, exited, onLostGracePeriod) })
	}
	reportRenewErr := func(err error) {
		cur := classifyRenewErr(err)
		if outcome == renewOK {
			outcome = cur
		}
		// 🚨 **文言は今回の err から決める** (sticky な outcome からではない)。判定不能の
		// あとに確実な喪失 (errNotOwner) を知る経路は、更新を張り直すようになって毎 tick
		// 起こりうる。そこで「判定不能」と出すと、**他所で走っている**と運用者が知る唯一の
		// 1 行が永久に出ない。終了コードだけは sticky にする — 判定不能だった窓があった事実は
		// 後から消えないので 125 に留める (091:398-399。issue 381 で意図的に据え置いた)。
		if cur == renewLost {
			warnf("lease を失った: %v", err)
		} else {
			warnf("lease を確認できない (判定不能): %v", err)
		}
		if !onLostKill {
			return
		}
		// 🚨 **「確実な喪失」と「判定不能」で昇格の扱いを分ける** (issue 385)。
		//
		// - `renewLost` (errNotOwner): **他者が既に引き継いでいる**ので、待つほど二重実行が
		//   延びる。即座に昇格する (従来どおり)
		// - `renewIndeterminate` (I/O タイムアウト等): **lease が生きているかは分からない**が、
		//   他者が**正当な手順で**引き継げるのは自分の lease が期限切れになってからで、その時刻は
		//   推測ではなく**計算できる** (最後に成功した更新の開始時刻 + TTL。同一レートで進む
		//   時計を仮定している。サーバ時計が前方へステップすると保守側の保証は崩れる = 未確認)。
		//
		// 🚨 **例外は `lockman break`** (敵対レビュー 385 が実測)。`Break` は期限も token も見ない
		// 無条件の rename なので、「期限までは誰も引き継げない」は break に対して**成立しない**。
		// 実測 2026-09-20 (ttl=1800ms / 恒久的な詰まり / t=1000ms で break → 別ホストが acquire):
		// 旧挙動は他者の取得より前に子が回収されていた (重なり −7〜−16ms) が、新挙動では
		// **+185〜+193ms 重なる**。窓の上限は「break の後に**完了する**更新が errNotOwner を
		// 返すまで」= 最大 1 tick + io-timeout (既定 ttl 30m なら約 10 分)。
		// 🚨 **さらに悪い形は未確認**: 判定不能の原因そのもの (毎回 io-timeout を超えるマウント) が
		// 続くと errNotOwner を観測できないので、重なりは期限まで (既定 30 分) 伸びうる。
		// それでもこの設計を採るのは、break が「人が force で撃つ操作」で、`lock.go` の
		// `tryTakeover` の注記が既に「**最も現実的に二重取得を作る操作**」として受容しているため。
		//
		// 旧版は最初の判定不能で即昇格していたため、**一過性の詰まりで殺さなくてよい子を
		// 殺していた** (実測 2026-09-20: 900ms の詰まりで warn は子が完走 3.04s / lease 保持、
		// kill は 710ms で子が死ぬ。lease は生きていた)。381 がループ側を「判定不能でも
		// 更新を続ける」へ変えたのに、昇格側が即殺すままで**便益が回収できていなかった**。
		//
		// 🚨 これは fail-open ではない。**期限を過ぎたら必ず昇格する** (下の tick の判定)。
		// 残る窓 (TERM → SIGKILL の猶予) は、確実な喪失の経路が既に受容しているものと同じ。
		if cur == renewLost {
			escalateNow()
			return
		}
		// 🚨 **報告の時点でも期限を見る** (敵対レビュー 385 の P3-1)。期限は構造的に tick 境界と
		// 重なる (`renewStartedAt` が tick の瞬間で、周期は ttl/renewDivisor) ので、tick の枝でしか
		// 見ないと配送ジッタで「今回の tick では未到達」に落ちて**次の tick まで = ttl/3 遅れる**
		// (実測 92 回中 1 回。既定 ttl 30m ならその 1 回が 10 分の窓になる)。
		if lease.indeterminate(time.Now()) {
			escalateNow()
		}
	}

	for {
		select {
		case sig := <-sigCh:
			// 受けたシグナルは子のプロセスグループへ転送する (自分だけ死なない)。
			//
			// 🚨 ここは**昇格しない**。撃ったのは人 (Ctrl-C / kill) で、`with` は子の
			// 終了コードを透過する薄い包みなので、子が TERM を無視するなら素で実行した
			// ときと同じ振る舞いにする。昇格するのは lease を失ったとき —
			// **他者が既に引き継いでいて、止めないと二重実行になる**ときだけ。
			//
			// 🚨 **エラーを捨てているのは意図的** (issue 364 の 3)。`sigCh` と `done` が同時に
			// ready なとき Go の select は一様ランダムに選ぶので、**回収済みの pgid へ撃つ**窓がある。
			// 実測 2026-09-20 (issue 384。darwin 24.6、3 回とも同じ) でその窓の失敗はすべて空振りだと
			// 分かっている: 回収済み (グループが空) → **ESRCH** / 回収前 (ゾンビだけ) → **EPERM** /
			// **生きたメンバーが 1 つでもあれば成功**。つまり実害には
			// 「窓の中で pid が再利用され、**かつ再利用した側がそのグループのリーダー**」が要る
			// (未確認。手元では再現できていない)。報告する価値のある失敗が無いので黙って捨てる。
			// 再評価の trigger: `with` に実利用者が現れ、シグナル転送の取りこぼしが報告されたとき。
			_ = killGroup(pgid, sig.(syscall.Signal))
		case <-ticker.C:
			// 🚨 **保留した昇格は、lease の期限を過ぎたらここで必ず撃つ** (issue 385)。
			// tick は更新が詰まっていても鳴り続けるので、これが「判定不能のまま期限を
			// 迎えた」を拾う唯一の経路になる (粒度は tick = ttl/renewDivisor)。
			if lease.dueForEscalation(time.Now()) {
				escalateNow()
			}
			if renewCh != nil {
				continue // 前回の更新がまだ返っていない。新しく積まない
			}
			if n := inFlightRenews.Load(); n >= maxInFlight {
				// 上限に達した = ここから先は更新を起こさないので、lease は次の TTL で
				// **必ず**切れる。黙って tick を捨てずに理由を出す。
				//
				// 🚨 これは**新しい保証ではなく、診断と多重防御**。枠を埋めるには 1 本ごとに
				// 期限切れ (下の renewExpired) を通る必要があるので、ここへ来た時点で
				// outcome は既に判定不能。**ただし昇格は済んでいるとは限らない** — issue 385 以降、
				// 判定不能の昇格は lease の期限まで保留される (敵対レビュー 385 の P2-2 で訂正)。
				// それでも reportRenewErr に通すのは、「更新が止まった理由」を出す経路を 1 本に保つため。
				//
				// 🚨 「上限に達した = もう手遅れ」とは限らない。返らない更新と成功する更新が
				// 交互に来る (半死のマウント) と、lease が生きているうちに枠だけが埋まり、
				// **成功するはずの更新まで起こさなくなる**。`--on-lost=warn` ではこの形で
				// 二重実行が残る (kill なら lease の期限で子を止めにいく。issue 385 以降は
				// 「最初の期限切れで」ではない。敵対レビュー 385 の P2-2 で訂正)。
				//
				// 🚨 報告は 1 回だけ (tick ごとに出すと warn が溢れる)。復旧して再び
				// 上限に達しても出し直さない — 既に outcome は判定不能で確定している。
				if !cappedReported {
					cappedReported = true
					reportRenewErr(fmt.Errorf("%w: 返らない更新が %d 本溜まったので更新を起こせない", errIOTimeout, n))
				}
				continue
			}
			// 🚨 **打刻より前**の時刻 (Acquire と同じ理由)。成功したらこれを期限の起点にする。
			renewStartedAt = time.Now()
			renewCh, renewExpired = l.renewAsync(meta.Token, &inFlightRenews)
		case err := <-renewCh:
			renewCh, renewExpired = nil, nil
			if err != nil {
				reportRenewErr(err)
				break
			}
			// 🚨 更新が成功した = サーバが打刻し直した。期限を進め、**保留していた昇格を解く**
			// (issue 385。一過性の詰まりから復旧した形がここ)。
			// `outcome` は sticky のままにする — 判定不能だった窓があった事実は消えないので
			// 終了コードは 125 に留める (091:398-399 / issue 381 の判断を据え置く)。
			lease.renewed(renewStartedAt)
		case <-renewExpired:
			// 期限切れ = 判定不能。報告して**この 1 本は見捨てる** — 参照を捨てるだけで
			// goroutine は止められない (ブロック中の syscall は中断できない)。次の tick が
			// 新しい更新を積み、マウントが復旧していれば lease はそこで生き返る。
			// 溜めてよい本数は maxInFlightRenews が抑える。
			renewCh, renewExpired = nil, nil
			reportRenewErr(l.ioTimeoutErr())
		case err := <-done:
			switch outcome {
			case renewLost:
				return exitWithLost
			case renewIndeterminate:
				return exitWithInvalid
			}
			return childExitCode(err)
		}
	}
}

// childExitCode は子の終了状態を終了コードへ変換する。
// シグナル死は shell の慣習に合わせて 128+signal にする。
func childExitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	warnf("子プロセスの終了を取得できない: %v", err)
	return exitWithInvalid
}

// maxInFlightRenews は「まだ返っていない Renew」の上限。超えたら新しい更新を起こさない。
//
// 見捨てた 1 本は goroutine 1 つと**ブロック中の syscall = OS スレッド 1 つ**を抱える
// (`withTimeout` の注記どおり回収できない)。上限が無いと、応答しないマウント + 短い TTL で
// tick ごとに 1 スレッド積み、Go の既定のスレッド上限 (10000) に当てて落ちうる。
//
// 🚨 上限が抑えているのは goroutine の数だけではない。見捨てた Renew は**解放されたときに
// `lockPath()` を名前で開き直して書く** (issue 380 の TOCTOU。**380 で解消**: いまは開いた
// fd に対してしか書かないので、後から書きに来ても他人の lock には当たらない)。つまりこの値は「後から
// lock へ書きに来るかもしれない本数」の上限でもある。上げるときは 380 を見ること。
//
// 🚨 **全部が詰まる形では、この値は lease の生死の近因にならない**。上限に達するには
// 8 サイクル ≈ 2.67 × TTL かかり、その時点で lease は 1.67 × TTL 前に死んでいる。
// 近因になりうるのは「返らない更新と成功する更新が交互に来る」形 (lease が生きたまま枠が
// 埋まる) だけで、そこは `--on-lost=warn` の既知の残存リスク (runWith の該当箇所)。
//
// 8 は「連続して 8 回詰まっても更新を再開できる」余裕。テストが縮めるので var。
var maxInFlightRenews int64 = 8

// onLostGracePeriod は SIGTERM を撃ってから SIGKILL へ昇格するまでの猶予。
// 🚨 これは「待ち」ではなく**仕様値** — 子に後片付けの機会を与えるための窓で、
// 縮めると trap を書いた子が片付け切れない。テストが差し替えるので var。
// 🚨 **読むのは runWith の goroutine だけ** (昇格側へは引数で渡す)。ここから直接読むと
// テストの差し替えと data race になる (go test -race が実際に検出した)。
var onLostGracePeriod = 5 * time.Second

// escalateGroupKill は lease を失ったときに子のグループを**確実に**止める。
//
// 🚨 SIGTERM を 1 回撃つだけでは足りない。091:282 は「子プロセスを止められるようにする」と
// 書いているが、TERM を trap / 無視するプログラム (ffmpeg を含め普通にある) だと子は生き続け、
// `with` は子が終わるまで返らないので**他者が既に引き継いでいる状態が無期限に続く**
// (issue 356 の経路 2)。
//
// 🚨 届く範囲はプロセスグループに残った子孫まで。**`setsid()` した子孫には届かない**
// (runWith の Setpgid の注記を参照)。
func escalateGroupKill(pgid int, exited <-chan struct{}, grace time.Duration) {
	// 🚨 **撃つ前に「もう終わっている」かを見る。** `escalate.Do` から goroutine が実走する
	// までの間に子が回収されていると、TERM は空いた pgid へ飛び、**再利用されていれば
	// 無関係なプロセスグループに当たる** (issue 340 項目 2 と同クラス)。
	// 🚨 同じ guard が **SIGKILL の直前にも要る**。以前のコメントは「close(exited) を先にしたので
	// SIGKILL 側は塞がっている」と読める書き方だったが、塞がっていなかった (issue 384 項目 2)。
	select {
	case <-exited:
		return
	default:
	}
	if err := killGroup(pgid, syscall.SIGTERM); err != nil {
		// 🚨 **ここで返ると昇格は二度と走らない** (`escalate.Do` は sync.Once で、消費済み)。
		// 再試行しても届かないので Once は消費したままでよい: `kill(2)` の失敗は
		// EPERM / ESRCH / EINVAL しか無く、**一過性のものが無い** (EAGAIN も EINTR も返らない)。
		// 効くのは「届かなかった」を人に伝えることだけなので、そこを厚くする (issue 384 項目 1)。
		//
		// 🚨 **実測 2026-09-19 (darwin 24.6 / go 1.26。3 回とも同じ)**。この pgid に対して
		// `kill(-pgid, SIGTERM)` が失敗するのは、**グループに生きたメンバーが 1 つも無いとき**だけ:
		//   - 子が終了して回収済み (グループが空)        → **ESRCH (3)**
		//   - 子が終了したが未回収 (ゾンビだけが残る)      → **EPERM (1)**
		//   - ゾンビの子 + 同グループの生きた孫           → **成功** (生きたメンバーが 1 つでもあれば届く)
		// 384 の本文が挙げていた「子が `setsid()` すると EPERM」は**再現しない**:
		// `Setpgid` で起こした子は**自分がグループリーダー**なので `setsid()` 自体が EPERM で失敗する
		// (EPERM を受け取るのは親の kill ではなく**子の setsid**)。`setsid` した孫が居る形でも、
		// 子が生きていれば kill は成功する (孫に届かないのは別問題 = runWith の Setpgid の注記)。
		// つまり上のメッセージの「子は走り続けている可能性がある」は、**測れた範囲では偽**
		// (どちらの errno でも生きたメンバーは居ない)。それでも残すのは、
		// **別 uid が所有する生きたメンバー**が居る形を測れていないため (root 権限が要る。未確認)。
		// 再開の trigger: 実運用でこの warn が出たとき、`ps -o pid,pgid,user -g <pgid>` を採る。
		warnf("昇格できない (%v)。**子は走り続けている可能性がある**: lease は既に他者が持っているので、"+
			"二重実行になっていないか確認すること (pgid=%d)", err, pgid)
		return
	}
	select {
	case <-exited:
		return // 猶予の内に終わった
	case <-time.After(grace):
	}
	escalateBeforeKillHook()
	// 🚨 **撃つ直前にもう一度見る。** 両方 ready のとき Go は一様ランダムに選ぶので、
	// 子が終わっていても timer 枝を取ることがある (実測 50.1%)。その後 warnf の write が挟まる
	// あいだも窓で、`<-done` の後も deferred な解放が最大 --io-timeout 走るため到達機会がある。
	// 空いた pgid を撃つと、再利用した pid がその group leader だったときに無関係なグループへ
	// 当たる (issue 384 項目 2。関数先頭の guard と同じ理由で、SIGKILL 側にも要る)。
	select {
	case <-exited:
		return
	default:
	}
	warnf("子が %v 以内に終わらないので強制終了する (lease は既に他者が持っている)", grace)
	// 🚨 ここもエラーを捨てる (上の転送と同じ理由。issue 364 の 3)。直前の guard を通っている =
	// まだ回収されていないと見えた時点なので、ここでの失敗は「guard の後に回収された」形に限られ、
	// 実測ではその失敗はすべて空振り (ESRCH / EPERM)。
	_ = killGroup(pgid, syscall.SIGKILL)
}

// afterChildStartHook は「子を起こした直後」に割り込む seam。既定は何もしない。
// 🚨 **`signal.Notify` より後・select ループより前**という窓を、テストから決定論で作るために要る
// (issue 363)。ここで自分へシグナルを撃つと、ハンドラが立っていれば `sigCh` へ、
// 立っていなければ**既定処理**へ流れる = 修正の有無がそのまま差になる。
var afterChildStartHook = func() {}

// escalateBeforeKillHook は「猶予が切れてから SIGKILL を撃つまで」に割り込む seam。
// 既定は何もしない。**テストが「timer 枝を取った後に子が終わった」状態を決定論で作る**ために使う
// (その窓は本番では 50% のランダム選択に依存していて、テストから作れない)。
var escalateBeforeKillHook = func() {}

// killGroup はプロセスグループへシグナルを送る。
//
// 🚨 **pgid が 0 や 1 なら撃たない。** `kill(0, sig)` は「呼び出し側のプロセスグループ」を
// 撃つので、pgid の計算を誤ると本番では呼び出し元シェルを、テストでは `go test` 自身を
// 殺す。撃つ前に弾く (issue 356 の実装時の注意)。
func killGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 1 {
		return fmt.Errorf("プロセスグループ %d には撃たない (自分のグループを撃つ危険)", pgid)
	}
	return syscall.Kill(-pgid, sig)
}
