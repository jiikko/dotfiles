package main

// pro-con dispatcher — 本物のモードの dispatcher を常駐させる (issue 427 の段階 3c-3)。3 秒ごとに dispatcher.Tick し、何をしたかを時刻つきで出す。
// 2 つ起動しない (dispatcher.Lock)。🚨 本物の claude で PG を起動するので、週の利用枠を使う。

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"pro-con/agents"
	"pro-con/dispatcher"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/presence"
	"pro-con/store"
	"pro-con/wake"
)

// fromScreenFlag は画面が起こす / 止める dispatcher に付けるフラグの名前 (main の dispatcherCmd / stopInChild と e2e の後始末が付ける)。
const fromScreenFlag = "from-screen"

var dispatcherInterval = 3 * time.Second // テストが延ばす (Poke でだけ起きることを見る)

// projects は transcript の置き場 (~/.claude/projects。PG が落ちて自動で再開したかを読む)。
// pm は PM の設定 (issue 437)。
func runDispatcher(args []string, dir, projects string, repos map[string]string, pm pmConfig, stdout, stderr io.Writer) int {
	upgradedFrom, notified := takeUpgradedFrom() // 入れ替え (505) で起きたなら前の版の名前と知らせ済みの鍵。lock は下の AdoptLock で受け取る
	selfStart, selfErr := selfBinary()           // 動いている版 (shim が差し替える前に取る)
	fs := flag.NewFlagSet("pro-con dispatcher", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", 2, "同時に動かす PG の上限 (415 の決定事項: 2 から始める)。pro-con config set limit があればそちらが勝つ (これは設定が無いときの値)")
	once := fs.Bool("once", false, "1 回だけ回して終わる")
	stopAll := fs.Bool("stop", false, "動いている dispatcher と、pro-con が起動した PG を止める (次に dispatcher を起動したら続きから再開する)")
	e2eRoot := fs.String("e2e", "", "e2e モードの置き場 (PG は台本どおりに動く偽物。claude を起動しない。pro-con e2e が使う)")
	pmFlag := fs.String("pm", "", `"off" なら PM を起動も再開もしない (依頼の列のカードはそのまま置く)。"on" / "off" を書けば設定の pm より勝つ`)
	intFlag := fs.String("integrator", "", `"off" なら取り込みの係を起動も再開もしない (レビューの列のカードはそのまま置く)。"on" / "off" を書けば設定の integrator より勝つ`)
	alone := fs.Duration("exit-without-screens", 0, "開いている画面が 1 つも無い状態がこの長さ続いたら、PG を止めて抜ける (画面が起こすときに付ける。0 なら抜けない)")
	fromScreen := fs.Bool(fromScreenFlag, false, "画面が起こす / 止める (内部用。人が止めた印を --stop で置かず、起動で外さない。印があれば起動せずに抜ける)")
	preflight := fs.Bool(preflightFlag, false, "記録を読めるかだけ確かめて抜ける (内部用。動いている dispatcher が新版へ入れ替える前に、新版のバイナリに走らせる。issue 505)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *limit < 1 {
		_, _ = fmt.Fprintln(stderr, "pro-con dispatcher: --limit は 1 以上")
		return 2
	}
	pmOff, pmWhy, err := resolvePM(*pmFlag, pm.Mode)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
		return 2
	}
	intOff, intWhy, err := resolveOnOff("integrator", *intFlag, pm.IntegratorMode)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
		return 2
	}
	var e2e *dispatcher.E2E
	if *e2eRoot != "" {
		e := dispatcher.E2E{Root: *e2eRoot}
		e2e, dir, repos = &e, e.StateDir(), map[string]string{dispatcher.E2ERepo: e.RepoDir()}
		if err := os.MkdirAll(e.RepoDir(), 0o700); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
			return 1
		}
	}
	if *preflight { // 書かない・lock を取らない
		if err := dispatcher.Preflight(dir); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con dispatcher --preflight:", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, preflightOK, binaryLabel(selfStart))
		return 0
	}
	if *stopAll {
		if !*fromScreen { // 人が止めた: 印を置く (画面の keeper が起こし直さない。issue 459)。止める前に置く (止め終えた直後の keeper に先を越されない)
			if err := store.Hold(dir, time.Now()); err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con dispatcher --stop: 止めた印を置けない (開いている画面が dispatcher を起こし直しうる):", err)
			} else {
				_, _ = fmt.Fprintln(stdout, "止めた印を置いた: 開いている画面は dispatcher を起こさない (画面の c か、手で pro-con dispatcher を起動すると外れる)")
			}
		}
		// 止めている間に SIGTERM / SIGHUP / SIGINT が来ても (画面の終了・ログアウトと重なる)、1 回目は止めるのをもう 1 度だけ試してから抜ける。
		// 2 回目ですぐ抜ける (止まらない形でも kill -9 無しで止められる)。
		// 🚨 signal.Ignore にしない: 無視は exec した子 (claude stop / claude agents) に引き継がれ、子も止められなくなる (Notify で受けた分は引き継がれない)
		ctx, cancel := context.WithCancel(context.Background())
		sigs := make(chan os.Signal, 2)
		signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
		go func() {
			<-sigs
			cancel()
			<-sigs
			os.Exit(1)
		}()
		defer signal.Stop(sigs)
		if err := stopDispatcher(ctx, dir, projects, repos, pm.Repo, e2e, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con dispatcher --stop:", err)
			return 1
		}
		return 0
	}
	// SIGHUP: 端末・tmux のペインを閉じた (既定の動作で死ぬと実行を残す)。
	// 🚨 claude を引くより先に受け口を作る: 入れ替え (505) の直後に来た信号で、PG を止める処理を通らずに死ぬ窓を狭める
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	// claude を引けなければ lock も取らない (状態の置き場に何も書かない)。入れ替えで引き継いだ lock の fd は main の頭
	// (dispatcher.GuardInheritedLock) で exec で閉じる形に戻してあるので、ここで起こす子 (claude --version) へは渡らない
	cl, err := resolveClaude(context.Background(), e2e)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
		return 1
	}
	// 入れ替え (505) で引き継いだ lock は、受け取れなければ取り直す (別の dispatcher が取っていれば ErrRunning で抜ける = 2 つ立たない)
	lock, adoptErr := dispatcher.AdoptLock(dir)
	if lock == nil {
		if lock, err = dispatcher.TakeLock(dir); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", err)
			if errors.Is(err, dispatcher.ErrRunning) {
				return exitLockHeld // 起こした supervisor は落ちたと数えず、間を空けて起こし直す (issue 506)
			}
			return 1
		}
	}
	defer func() { lock.Release() }()
	// 入れ替えで起きた dispatcher は、止める印・人が止めた印を起動のときの形で扱わない (前のプロセス像の続き。
	// 入れ替えの隙に --stop が置いた印を捨てると、頼んだ --stop が時間切れまで待つ・人が止めた印を外すと画面が起こし直す)
	resumed := upgradedFrom != ""
	if !resumed {
		_ = dispatcher.StopRequested(dir) // 前の --stop が dispatcher の居ない間に置いた印は捨てる (起動した途端に止まらないように)
	}
	d := newDispatcherFor(dir, projects, repos, pm.Repo, pmOff, *limit, cl, e2e)
	d.Record = eventSink(dir, stdout, stderr)
	d.SeedNotified(notified)
	if resumed {
		text := fmt.Sprintf("dispatcher が新版に切り替わった (%s → %s。PID %d のまま)", upgradedFrom, binaryLabel(selfStart), os.Getpid())
		if adoptErr != nil {
			text += "。lock は引き継げず取り直した: " + adoptErr.Error()
		}
		say(d, eventlog.KindUpgrade, text)
	}
	if !resumed && !*once { // 起きた・抜けたは設定画面のログのタブが出す (issue 512。新版への入れ替えは上の KindUpgrade)
		how := "手で起動した"
		if *fromScreen {
			how = "画面か supervisor が起こした"
		}
		say(d, eventlog.KindDispatcher, fmt.Sprintf("dispatcher が起きた (pid %d・%s)", os.Getpid(), how))
	}
	// 🚨 lock を取ってから見る: 画面が印を見てから起こすまでの間に --stop が印を置いても、起こされた側が回らずに抜ける
	if *fromScreen && !resumed && store.Held(dir) {
		say(d, eventlog.KindStop, "人が止めた印 (dispatcher --stop) があるので、画面が起こした dispatcher は回らずに抜ける")
		return 0
	}
	if !*fromScreen && !resumed {
		if released, err := store.Release(dir); err != nil {
			say(d, eventlog.KindError, "人が止めた印を外せない (開いている画面は、この dispatcher が抜けた後に起こし直さない): "+err.Error())
		} else if released {
			say(d, eventlog.KindLaunch, "手で起動したので、人が止めた印 (dispatcher --stop) を外した")
		}
	}
	sayClaude(d, cl)
	if d.PMOff { // 依頼の列にカードが溜まっても PM が来ないのは、この設定のせいだと後から分かるように (dispatcher の値から出す = 渡し忘れも見える)
		say(d, eventlog.KindHold, "PM を起こさない ("+pmWhy+")。依頼の列のカードはそのまま置く (PM は人か外の Claude が行う)")
	}
	d.IntegratorOff = intOff
	if d.IntegratorOff {
		say(d, eventlog.KindHold, "取り込みの係を起こさない ("+intWhy+")。レビューの列のカードはそのまま置く (取り込みは人か外の Claude が行う)")
	}
	defer d.CancelRun() // どの出口 (Tick のエラー・SIGTERM) でも、テストの係の実行を残して抜けない
	if e2e == nil {     // e2e モードは本物の tmux の件数・macOS の通知に触らない
		d.Publish, d.Notify = dispatcher.TmuxPublish(ctx), dispatcher.MacNotify(ctx)
		defer func() { _ = dispatcher.TmuxPublish(context.Background())("") }() // 止まるときに件数を消す (古い件数を出し続けない)
	}
	var wakes <-chan struct{} // 開けなければ nil (ポーリングだけで動く)
	if srv, err := wake.Listen(dir); err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con dispatcher: 即時に起こす口を開けない (3 秒ごとのポーリングだけで動く):", err)
	} else {
		defer func() { _ = srv.Close() }()
		wakes, d.Changed = srv.Wakes(), srv.Broadcast
	}
	// 見張り (issue 475)。e2e モードの偽の worktree と、1 回だけの dispatcher では起こさない。
	// 🚨 d の欄 (Record / Changed) を書き終えてから起こす (起こし直しの goroutine が say で読む)
	stopMonitor := func() {}
	startMonitor := func() {
		if e2e == nil && !*once {
			say(d, eventlog.KindMonitor, "見張りを起こす")
			stopMonitor = superviseMonitor(ctx, monitorSpec(func(text string) { say(d, eventlog.KindMonitor, text) }, stdout, stderr))
		}
	}
	startMonitor()
	defer func() { stopMonitor() }() // unlock より先に走る (見張りを止めてから dispatcher の lock を外す)
	o := serveOpts{interval: dispatcherInterval, once: *once, alone: *alone}
	// 新版への入れ替え (505)。e2e モード (偽の PG の e2e がソースの編集で入れ替わらない) と 1 回だけの dispatcher では行わない
	if e2e == nil && !*once {
		u, err := newDispUpgrade(d, dir, args, selfStart, selfErr, lock, func(switchTo func() error) error {
			stopMonitor() // 見張りは子なので、入れ替えの前に止める (exec で置き去りにしない)。新版が起こし直す
			err := switchTo()
			startMonitor() // 戻ってきた = 失敗。旧版のまま見張りも起こし直す
			return err
		})
		if err != nil {
			say(d, eventlog.KindUpgrade, "dispatcher の新版への入れ替えは無効 ("+err.Error()+")。新版は手で起動し直すまで効かない")
		} else {
			if resumed {
				u.switched, u.switchedAt = upgradedFrom+" → "+u.from, time.Now()
			}
			d.UpgradeNote, o.upgrade = u.note, u.step
		}
	}
	rc := serve(ctx, d, dir, wakes, o, stderr)
	if !*once {
		say(d, eventlog.KindDispatcher, exitNote(ctx, rc))
	}
	return rc
}

// exitNote は dispatcher が抜けるときの出来事の文 (抜けた理由の出来事 = 止めた・画面が無い・Tick の失敗は serve がその前に書く)。
func exitNote(ctx context.Context, rc int) string {
	why := fmt.Sprintf("rc=%d", rc)
	if ctx.Err() != nil {
		why += "・止める合図 (信号) を受けた"
	}
	return fmt.Sprintf("dispatcher が抜ける (pid %d・%s)", os.Getpid(), why)
}

// serveOpts は serve の回し方。
type serveOpts struct {
	interval time.Duration // Tick の間隔 (起こされたら待たずに回る)
	once     bool          // 1 回だけ回して抜ける
	// alone は、開いている画面 (package presence) が 1 つも無い状態がこの長さ続いたら PG を止めて抜ける (0 なら抜けない)。
	// 🚨 画面が起こした dispatcher に付ける: 最後の画面が quit を通らずに消えても (端末を閉じた・落ちた・kill -9)、pro-con が起動した
	// PG を残さない。猶予は、ctrl+r の入れ替え・開き直しの間に止めないため
	alone time.Duration
	// now は alone を測る時計 (テストが差し替える)。nil なら time.Now
	now func() time.Time
	// retries は stopUntilDone の止め直しの上限 (0 なら止まるまで)
	retries int
	// upgrade は Tick の後に呼ぶ新版への入れ替え (dispupgrade.go。成功すると戻らない)。nil なら入れ替えない
	upgrade func(ctx context.Context)
}

// serve は dispatcher を回す。Tick の後は interval か、依頼を置いた側に起こされる (wakes) まで待つ。
func serve(ctx context.Context, d *dispatcher.Dispatcher, dir string, wakes <-chan struct{}, o serveOpts, stderr io.Writer) int {
	now := o.now
	if now == nil {
		now = time.Now
	}
	aloneSince := now() // 起こした画面がすぐ落ちた場合も数える
	for {
		stop := dispatcher.StopRequested(dir)
		if o.alone > 0 && !stop {
			if screensOpen(dir) {
				aloneSince = now()
			} else if now().Sub(aloneSince) >= o.alone {
				say(d, eventlog.KindScreens, fmt.Sprintf("開いている画面が %s 無いので、PG を止めて抜ける", o.alone))
				stop = true
			}
		}
		if stop {
			if stopUntilDone(ctx, d, dir, wakes, o, stderr) {
				return 0
			}
			aloneSince = now() // 止めている間に画面が開いた。止めるのをやめて続ける
			continue
		}
		_, err := d.Tick(ctx) // 出来事は Tick が d.Record (eventSink) へ渡す
		if err != nil {
			sayOr(d, stderr, eventlog.KindError, "Tick が失敗したので抜ける: "+err.Error())
			// 画面が起こした dispatcher は、抜ける前に PG を止める (持ち主の画面が無ければ見張る者が居なくなる。join は起こし直さない = issue 481)
			if o.alone > 0 && !ownersOpen(dir) {
				stopUntilDone(ctx, d, dir, nil, serveOpts{}, stderr)
			}
			return 1
		}
		if o.once {
			return 0
		}
		if o.upgrade != nil && ctx.Err() == nil {
			o.upgrade(ctx)
		}
		select {
		case <-ctx.Done():
			// 画面が起こした dispatcher は、画面が無ければ PG を止めてから抜ける (SIGTERM / SIGHUP でも pro-con が起動した PG を残さない)。
			// 持ち主の画面が開いていれば止めない (持ち主の画面が dispatcher を起こし直し、PG はそのまま続く。止めて再開すると枠を使う)。
			// 🚨 join の画面しか無ければ止める: join は dispatcher を起こさないので、止めずに抜けると PG を見張る者が居なくなる (issue 481)
			// 🚨 画面にも同時に SIGTERM が届いている (ログアウト・pkill) かもしれないので、画面が消えるのを少し待ってから決める
			// (待たずに見ると、閉じる途中の画面を「開いている」と数えて止めずに抜け、画面も quit を通らずに抜けて PG が残る)
			if o.alone > 0 && !ownersStayOpen(dir, signalGrace) {
				stopUntilDone(ctx, d, dir, nil, serveOpts{interval: o.interval, retries: 1}, stderr)
				if b, _ := os.ReadFile(filepath.Join(dir, dispatcher.StopResultFile)); strings.TrimSpace(string(b)) != "ok" {
					return 1 // 止めきれないまま抜ける (残りは dispatcher.log。画面を開けば keeper が次の dispatcher を起こして続きを扱う)
				}
			}
			return 0
		case <-time.After(o.interval):
		case <-wakes:
		}
	}
}

// screensOpen は開いている画面 (持ち主と join) があるか (数えられなければ無いとみなす: 止める側に倒す)。
// 無画面で抜ける判定はこちら: 持ち主が落ちても、join の画面が開いている間は PG を止めない (issue 481)
func screensOpen(dir string) bool {
	n, err := presence.Count(dir)
	return err == nil && n > 0
}

// ownersOpen は開いている持ち主の画面があるか (数えられなければ無いとみなす: 止める側に倒す)。
// 「画面が dispatcher を起こし直してくれる」前提の判定はこちら (止めている途中でやめる・止めずに抜ける): join の画面は dispatcher を
// 起こさないので、join が開いている・残っているだけで止めずにおくと、止めたはずの PG・見張る者の居ない PG が動き続ける (issue 481)
func ownersOpen(dir string) bool {
	n, err := presence.Owners(dir)
	return err == nil && n > 0
}

// signalGrace は、取り消されたときに画面が消えるのを待つ長さ。
var signalGrace = 5 * time.Second

// ownersStayOpen は、grace の間ずっと持ち主の画面が開いているか (途中で 1 度でも 0 になれば偽)。
func ownersStayOpen(dir string, grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for {
		if !ownersOpen(dir) {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopRetryEvery は、止めきれなかったときに止め直す間隔。
var stopRetryEvery = 10 * time.Second

// stopUntilDone は PG を止める。止めきれなければ結果 (頼んだ画面が読む) を書いてから、止まるまで止め直す。止め終えたら真。
// 止めている間に持ち主の画面が開いたら (presence) 止めるのをやめて偽を返す (カードは続きから再開できる形になっている)。
// join の画面 (pro-con --join) では取り消さない (開いた持ち主は dispatcher を起こすが、join は起こさない。issue 481)。
// 人が止めた印 (store.Held) があれば、画面が開いても止めきる (印がある間、画面は dispatcher を起こさないので、続ける者が居ない)。
// 🚨 止めきれないまま抜けない: 抜けると、pro-con が起動した PG を見張る者が居なくなる。o.retries が正なら、その回数で諦める (取り消された ctx の中の最後の試み)
func stopUntilDone(ctx context.Context, d *dispatcher.Dispatcher, dir string, wakes <-chan struct{}, o serveOpts, stderr io.Writer) bool {
	for try := 1; ; try++ {
		_, err := d.Shutdown(ctx) // 出来事は Shutdown が d.Record へ渡す
		if try == 1 || err == nil {
			dispatcher.WriteStopResult(dir, err) // 頼んだ画面は最初の結果を読む。後から止め終えたら ok で書き直す
		}
		if err == nil {
			return true
		}
		if o.retries > 0 && try >= o.retries {
			sayOr(d, stderr, eventlog.KindStop, "止めきれないまま抜ける: "+err.Error())
			return true
		}
		sayOr(d, stderr, eventlog.KindStop, "止めきれない (止め直す): "+err.Error())
		select {
		case <-ctx.Done():
			o.retries = try + 1 // 取り消された: もう 1 度だけ試して抜ける
		case <-time.After(stopRetryEvery):
		case <-wakes:
		}
		if ownersOpen(dir) && ctx.Err() == nil && !store.Held(dir) { // 人が止めたなら、画面が開いても止めきる (issue 459)
			say(d, eventlog.KindScreens, "止めている間に持ち主の画面が開いたので、止めるのをやめて続ける")
			return false
		}
	}
}

// stopTimeout は、動いている dispatcher が止め終えるまで待つ上限。Shutdown の待ち (約 46 秒 + 一覧の取り直し) と実行中の 1 Tick より長く。
const stopTimeout = 120 * time.Second

// stopDispatcher は dispatcher と PG を止める。dispatcher が動いていれば止めるよう頼んで待ち、動いていなければ自分で dispatcher の役を取って止める。
func stopDispatcher(ctx context.Context, dir, projects string, repos map[string]string, pmRepo string, e2e *dispatcher.E2E, stdout io.Writer) error {
	running, err := dispatcher.RequestStop(ctx, dir, stopTimeout)
	if errors.Is(err, dispatcher.ErrStopperDied) { // 止めていた dispatcher が落ちた: 止める役を引き継ぐ
		_, _ = fmt.Fprintln(stdout, "止めていた dispatcher が落ちたので、止める役を引き継ぐ")
		running, err = false, nil
	}
	if running || err != nil {
		return err
	}
	unlock, err := dispatcher.Lock(dir)
	if err != nil {
		return err // 確かめた直後に別の dispatcher が起動した
	}
	defer unlock()
	_ = dispatcher.StopRequested(dir)
	cl, err := resolveClaude(ctx, e2e) // 止める役を取ったときだけ (動いている dispatcher に頼むだけなら claude は要らない)
	if err != nil {
		return err
	}
	d := newDispatcherFor(dir, projects, repos, pmRepo, false, 1, cl, e2e) // 止めるだけ (PM は PMOff でも止める)
	d.Record = eventSink(dir, stdout, stdout)                              // dispatcher の役を取った (lock を持つ) ので、出来事を書いてよい
	sayClaude(d, cl)
	// 自分で止めている間に次の --stop が来たら、その --stop は結果のファイルを読む。止めきれなければ止まるまで止め直す
	// (画面の待ちが切れて閉じても、このプロセスは別のプロセスグループで続ける)
	if !stopUntilDone(ctx, d, dir, nil, serveOpts{}, stdout) {
		return errors.New("止めている間に持ち主の画面が開いたので、止めるのをやめた (開いた画面の dispatcher が続きを扱う)")
	}
	data, _ := os.ReadFile(filepath.Join(dir, dispatcher.StopResultFile))
	if r := strings.TrimSpace(string(data)); r != "ok" {
		return errors.New(r)
	}
	return nil
}

// eventSink は dispatcher の出来事を、時刻つきの 1 行で out へ出し (dispatcher.log / nohup の先)、状態の置き場の events.jsonl へ足す
// (pro-con log が読む。issue 444)。🚨 呼んでよいのは dispatcher の lock を持つプロセスだけ (書き手を 1 つにする = 426 の決定 1)。
// 🚨 並行に呼ばれる (Tick と、見張りを起こし直す goroutine = monitorsup.go)。1 つずつ書く (追記と回しを競わせない)
func eventSink(dir string, out, errOut io.Writer) func([]eventlog.Event) {
	var mu sync.Mutex
	return func(evs []eventlog.Event) {
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		stamped := make([]eventlog.Event, len(evs))
		for i, e := range evs {
			if e.At.IsZero() {
				e.At = now
			}
			stamped[i] = e
			_, _ = fmt.Fprintf(out, "%s %s\n", e.At.Format("15:04:05"), e.Reason)
		}
		if err := eventlog.Append(dir, stamped); err != nil {
			_, _ = fmt.Fprintln(errOut, "pro-con dispatcher: 出来事を events.jsonl に書けない:", err)
		}
	}
}

// say は dispatcher の外側 (serve) で決めたことを出来事として渡し、購読している側 (画面・pro-con log --follow) へ知らせる。
func say(d *dispatcher.Dispatcher, kind, text string) {
	if d.Record != nil {
		d.Record([]eventlog.Event{{Kind: kind, Reason: text}})
		if d.Changed != nil {
			d.Changed()
		}
	}
}

// resolveClaude は本物の dispatcher が使う claude の実体を引く (e2e モードは claude を起動しないので引かない)。
// 解決は家の cwd で行う (起こした側の cwd の .node-version で版を決めない。dispatcher.ResolveClaude)
func resolveClaude(ctx context.Context, e2e *dispatcher.E2E) (dispatcher.Claude, error) {
	if e2e != nil {
		return dispatcher.Claude{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dispatcher.Claude{}, err
	}
	return dispatcher.ResolveClaude(ctx, home)
}

// sayClaude は使う claude の実体と版を出来事に残す (464。e2e モードは claude を起動しないので残さない)。
func sayClaude(d *dispatcher.Dispatcher, cl dispatcher.Claude) {
	if cl.Path != "" {
		say(d, eventlog.KindLaunch, fmt.Sprintf("claude は %s (%s) を使う", cl.Path, cl.Version))
	}
}

// sayOr は say と同じ。出来事の書き先が無い (Record が nil) ときだけ errOut へ出す
// (両方へ出すと、stdout と stderr を同じファイルへ向ける dispatcher.log / stop.log に同じ文が 2 行入る)。
func sayOr(d *dispatcher.Dispatcher, errOut io.Writer, kind, text string) {
	if d.Record == nil {
		_, _ = fmt.Fprintln(errOut, "pro-con dispatcher:", text)
		return
	}
	say(d, kind, text)
}

// pmConfig は設定から読んだ PM の値 (main が渡す)。
type pmConfig struct {
	Repo string // PM を起動する repo (空なら PM を起こさない)
	Mode string // 設定の pm ("on" / "off" / 空)
	// IntegratorMode は設定の integrator ("on" / "off" / 空。487)
	IntegratorMode string
}

// resolvePM は PM を起こさないかを決める (resolveOnOff の pm 版)。
func resolvePM(flagVal, cfgVal string) (off bool, why string, err error) {
	return resolveOnOff("pm", flagVal, cfgVal)
}

// resolveOnOff は役 (pm / integrator) を起こさないかを決める。dispatcher の --<name> が設定の <name> に勝つ (起動ごとに明示した方を優先する)。
// why は off のとき、どちらで決まったか。
func resolveOnOff(name, flagVal, cfgVal string) (off bool, why string, err error) {
	switch flagVal {
	case "off":
		return true, "--" + name + "=off", nil
	case "on":
		return false, "", nil
	case "":
	default:
		return false, "", fmt.Errorf(`--%s は "on" か "off" (%q)`, name, flagVal)
	}
	if cfgVal == "off" {
		return true, `設定 ` + name + ` = "off"`, nil
	}
	return false, "", nil
}

// userSettingsPath はユーザーの settings.json (家が分からなければ "" = 言語を渡さない)。
func userSettingsPath(home string) string {
	if home == "" {
		return ""
	}
	return dispatcher.UserSettingsPath(home)
}

// newDispatcherFor は dispatcher を組む。e2e が nil なら本物 (claude の実体 cl を起動する)、あれば偽の PG と偽の一覧 (claude を起動しない。PM は起こさず FakePM が役を持つ)。
func newDispatcherFor(dir, projects string, repos map[string]string, pmRepo string, pmOff bool, limit int, cl dispatcher.Claude, e2e *dispatcher.E2E) *dispatcher.Dispatcher {
	if e2e != nil {
		return &dispatcher.Dispatcher{Dir: dir, Limit: limit, Repos: repos, Launch: e2e.Launcher(), List: e2e.List, ListAll: e2e.ListAll, Now: time.Now,
			Runner: dispatcher.ExecRunner{}, FakePM: e2e.FakePM, PMOff: pmOff} // テストの係は本物のシェル (偽の worktree で走る)。失敗の要約 (haiku) はしない
	}
	home, _ := os.UserHomeDir() // 分からなければ "" (言語と ~/.claude/CLAUDE.md の除外を渡さないだけで、起動は止めない)
	haiku := dispatcher.HaikuSettings(home)
	return &dispatcher.Dispatcher{Dir: dir, Limit: limit, Repos: repos, Launch: dispatcher.ExecLauncher{Claude: cl.Path, UserSettings: userSettingsPath(home)}, PMRepo: pmRepo, PMGuide: pmGuide, PMOff: pmOff, IntegratorGuide: integratorGuide,
		Runner: dispatcher.ExecRunner{Lockman: "lockman"}, Summarize: dispatcher.HaikuSummarize(cl.Path, dir, haiku), Ask: dispatcher.HaikuAsk(cl.Path, dir, haiku), Usage: dispatcher.ReadUsage(cl.Path, dir),
		Procs: dispatcher.PSProcs, ProgressGit: dispatcher.ExecProgressGit{}, BootTime: dispatcher.KernBootTime, JobsDir: filepath.Join(filepath.Dir(projects), "jobs"),
		List: func(ctx context.Context) ([]agents.Session, error) {
			return agents.List(ctx, agents.ExecRunner(cl.Path))
		},
		ListAll: func(ctx context.Context) ([]agents.Session, error) {
			return agents.List(ctx, agents.ExecRunnerAll(cl.Path))
		}, Now: time.Now,
		Transcript:     transcriptReader(projects, &live.TranscriptCache{}),
		TranscriptPath: func(sessionID string) (string, error) { return live.FindTranscript(projects, sessionID) }}
}

// transcriptReader は dispatcher が session の transcript を読む口。1 回の tick で同じ session を何か所からも読む
// (watch・進捗・役・btw) ので、読んだ結果を使い回す (issue 503)。探すのは毎回 (別の cwd で再開したら新しい方へ移る)。
func transcriptReader(projects string, c *live.TranscriptCache) func(string) (live.Transcript, error) {
	return func(sessionID string) (live.Transcript, error) {
		p, err := live.FindTranscript(projects, sessionID)
		if err != nil {
			return live.Transcript{}, err
		}
		return c.Get(p)
	}
}
