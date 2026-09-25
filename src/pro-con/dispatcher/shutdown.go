package dispatcher

// pro-con を終了するときの停止 (2026-09-25 にユーザーが決めた形: dispatcher と、pro-con が起動した PG を全部止め、次に dispatcher を起動したら続きから再開する)。
// 止めるのは dispatcher だけ (PG の session に触るのは dispatcher の役)。画面は `pro-con dispatcher --stop` で頼む。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
	"pro-con/wake"
)

// StopRequestFile は、動いている dispatcher に止めるよう頼む印 (状態の置き場の下)。dispatcher は Tick の間に見つけたら Shutdown して抜ける。
// StopResultFile は、止めた dispatcher が抜ける前に書く結果 ("ok" か、止めきれなかった理由)。頼んだ側はロックが外れた後にこれを読む
// (ロックが外れただけでは、止め終えたのか、止める前に落ちた / SIGTERM で抜けたのか区別できない)。
const (
	StopRequestFile = "stop-request"
	StopResultFile  = "stop-result"
)

// WriteStopResult は Shutdown の結果を書く (頼んだ側が読む)。
func WriteStopResult(dir string, err error) {
	text := "ok\n"
	if err != nil {
		text = err.Error() + "\n"
	}
	_ = os.WriteFile(filepath.Join(dir, StopResultFile), []byte(text), 0o600)
}

// resumeAfterStop は、終了で止めた作業中の PG を次に再開するときに渡す文。
const resumeAfterStop = "pro-con の終了で作業の途中で止めた。止まる前の続きから作業を再開して (規律は最初の指示のとおり)"

// ErrStopTimeout は、動いている dispatcher が時間内に止まらなかったとき (dispatcher は止め直しを続ける。残りは dispatcher.log)。
var ErrStopTimeout = errors.New("pro-con dispatcher が時間内に止まらない (dispatcher は止め直しを続ける。残りの PG は dispatcher.log に出る)")

// ErrStopperDied は、止めていた dispatcher が結果を書く前に終わったとき (落ちた)。頼んだ側が止める役を引き継ぐ。
var ErrStopperDied = errors.New("pro-con dispatcher が止め終える前に終わった (止めた結果が無い)")

// Shutdown は pro-con が起動した PG の session を全部止め、カードを次の起動で続きから再開できる形にする:
//   - 作業中 → 分解済みへ戻し、再開の文 (resumeAfterStop) を持たせる (次の dispatcher が --resume する)
//   - それ以外 (質問待ち・レビュー待ち等) → 列はそのまま
//
// 止めたカードには Stopped の印を付ける (再開のとき、落ちた PG の自動の再開を待つ restartWait を飛ばす)。
// 止める相手は、記録と session id・pid が一致する bg の session、または dispatcher 自身が起動・再開した直後でまだ記録に無い session だけ。
// すぐには止められない形 (落ちて自動の再開を待っている / 起動・再開の直後で一覧にまだ出ない) は、一覧を取り直しながら shutdownPolls 回待つ。
// 待っても止められなかったカードは列を変えず (次の dispatcher が普段どおり扱う)、止めきれなかった本数をエラーで返す。
func (d *Dispatcher) Shutdown(ctx context.Context) (notes []eventlog.Event, err error) {
	defer func() {
		d.record(notes)
		if len(notes) > 0 && d.Changed != nil { // 書いてから知らせる (pro-con log --follow がすぐ読める)
			d.Changed()
		}
	}()
	// 🚨 止めるのを途中で打ち切らない: SIGTERM で ctx が切られても、止める・一覧を取るのは続ける (1 回ずつに上限を付ける)。
	// どこかで失敗しても、止められる分は止めて最後の確かめ (ensureStopped) まで進む (1 回の一覧の失敗で 1 本も止めずに抜けない)
	base := context.WithoutCancel(ctx)
	ctx, cancel := context.WithTimeout(base, shutdownBudget) // カードから辿って止める段の上限 (claude が応答しなくても次の段へ進む)
	defer cancel()
	now := d.Now()
	d.cancelRun(10 * time.Second) // テストの係の実行中の 1 本を取り消す (再開した PG は続きから頼み直す)
	// 止める直前に PG が置いた質問・完了の依頼を先に適用する (作業中のまま分解済みへ戻すと、次の起動で除けられて失われる)
	if res, err := store.Apply(d.Dir, now); err != nil {
		notes = append(notes, ev(eventlog.KindError, "", "", "箱の依頼を適用できない (止めるのは続ける): "+err.Error()))
	} else {
		notes = append(notes, applied(res)...)
	}
	failed, tried := d.stopCards(ctx, &notes)
	if d.Publish != nil {
		_ = d.Publish("") // 件数を消す
	}
	// 🚨 カードから辿った停止だけで終えない。pro-con が起動した session (記録にあるもの・再開で入れ替わった前のもの) が本当に止まったかを、
	// 止めた session も出す一覧で確かめ、残っていれば止め直す (カードが完了した後も生きている PG も止める)
	ectx, ecancel := context.WithTimeout(base, ensureBudget) // 確かめる段は別の上限 (前の段が上限を使い切っても、最後の確かめまで届く)
	defer ecancel()
	more, remaining, _, err := d.ensureStopped(ectx, tried, nil, ensurePolls)
	notes = append(notes, more...)
	if err != nil {
		return notes, err
	}
	if len(remaining) > 0 {
		return notes, fmt.Errorf("pro-con が起動した PG のうち %d 本が止まっていない: %s (止める: pro-con dispatcher --stop / claude stop <id>)",
			len(remaining), strings.Join(remaining, ", "))
	}
	if failed > 0 { // カードの側で止めきれなかったと書いたものも、記録にある session は確かめた結果止まっている (列を変えなかっただけ)
		notes = append(notes, ev(eventlog.KindStop, "", "", fmt.Sprintf("%d 枚のカードは列を変えずに残した (記録にある session は止まっていることを確かめた)", failed)))
	}
	return notes, nil
}

// shutdownBudget / ensureBudget は、カードから辿って止める段 / 止まったかを確かめる段の上限 (claude が応答しないと、呼び出しごとの
// 上限の合計が 20 分になる。止まらなければ呼び出し側 (stopUntilDone) が止め直す)。
var (
	shutdownBudget = 3 * time.Minute
	ensureBudget   = 2 * time.Minute
)

// stopCallTimeout は、止めるときの 1 回の claude の呼び出しの上限 (ctx の取り消しを外しているので、上限は呼び出しごとに付ける)。
const stopCallTimeout = 30 * time.Second

// stopCards はカードから辿って PG を止め、作業中のカードを次の起動で続きから再開できる形にする。止めきれなかったカードの数を返す。
// 一覧・記録を読めない周は待って取り直し、shutdownPolls を過ぎたら諦めて戻る (後の ensureStopped が記録から止める)。
// 止めようとした session (短い id → カード) も返す: 起動・再開の途中で取り込んだ session は記録にまだ無いので、確かめる段 (ensureStopped) に渡す。
func (d *Dispatcher) stopCards(ctx context.Context, notes *[]eventlog.Event) (int, map[string]string) {
	tried := map[string]string{}
	done := map[string]bool{}
	seen := map[string]bool{}
	note := func(n eventlog.Event) { // 周をまたいで同じ知らせを重ねない
		if !seen[n.Reason] {
			seen[n.Reason] = true
			*notes = append(*notes, n)
		}
	}
	failed := 0
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			d.sleep(shutdownPoll)
		}
		last := attempt >= shutdownPolls || ctx.Err() != nil // 上限を過ぎたら待たずに次の段へ
		now := d.Now()                                       // 待ちの判定 (launchGrace / restartWait) は周ごとに今の時刻で行う
		lctx, cancel := context.WithTimeout(ctx, stopCallTimeout)
		ss, err := d.List(lctx)
		cancel()
		if err != nil {
			note(ev(eventlog.KindError, "", "", "session の一覧を取れない (取り直す): "+err.Error()))
			if last {
				return failed, tried
			}
			continue
		}
		// 起動・再開の直後で記録にまだ無い PG を、止める前に記録へ載せる
		_, warn, err := d.register(now, ss)
		if err != nil {
			note(ev(eventlog.KindError, "", "", "起動した session を記録に載せられない (止めるのは続ける): "+err.Error()))
		}
		for _, w := range warn {
			note(w)
		}
		st, err := store.Load(d.Dir)
		if err != nil {
			note(ev(eventlog.KindError, "", "", "カードの記録を読めない (取り直す): "+err.Error()))
			if last {
				return failed, tried
			}
			continue
		}
		reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
		if err != nil {
			note(ev(eventlog.KindError, "", "", "pro-con が起動した session の記録を読めない (取り直す): "+err.Error()))
			if last {
				return failed, tried
			}
			continue
		}
		waiting := 0
		for _, c := range st.Cards {
			if done[c.ID] || c.State == card.Done || (c.Session == "" && c.Launching == "") {
				continue
			}
			target, wait := d.stopTarget(c, now, ss, reg)
			if wait && !last {
				waiting++
				continue
			}
			done[c.ID] = true
			if wait { // 待っても止められる形にならなかった。列は変えない
				failed++
				*notes = append(*notes, ev(eventlog.KindStop, c.ID, c.Session, c.ID+" の PG は落ちて戻らない / 一覧に出ないので止められない (列はそのまま。次の dispatcher が扱う)"))
				continue
			}
			recorded := c.Stopped && c.State != card.Running // 前の Shutdown で止めたと書いた (止め直しの周ごとに履歴を足さない)
			if target != "" {
				tried[target] = c.ID
				sctx, cancel := context.WithTimeout(ctx, stopCallTimeout)
				err := d.Launch.Stop(sctx, target)
				cancel()
				if err != nil {
					failed++
					*notes = append(*notes, ev(eventlog.KindStop, c.ID, target, fmt.Sprintf("%s の PG (%s) を止められない: %v", c.ID, target, err)))
					continue
				}
				*notes = append(*notes, ev(eventlog.KindStop, c.ID, target, fmt.Sprintf("%s の PG (%s) を止めた", c.ID, target)))
			}
			if recorded { // 止め直しても履歴は書き直さない (起動の途中の印だけは外す: 残すと次の dispatcher が二重に扱う)
				if c.Launching != "" {
					if err := d.update(c.ID, func(cc *card.Card) { cc.Launching = "" }); err != nil {
						*notes = append(*notes, ev(eventlog.KindError, c.ID, target, fmt.Sprintf("%s のカードを書き直せない (PG は止めた): %v", c.ID, err)))
					}
				}
				continue
			}
			if err := d.update(c.ID, func(cc *card.Card) {
				cc.Stopped, cc.Launching = true, "" // 取り込めた起動・再開は止めた。取り込めなかったものは stopTarget が wait を返している
				text := "pro-con の終了で PG を止めた"
				if target == "" {
					text = "pro-con の終了: PG の session は既に止まっていた"
				}
				if cc.State == card.Running {
					cc.State, cc.Since, cc.Resume = card.Planned, now, resumeAfterStop
					cc.DropRun() // テストの係への頼みも取り下げる (続きから頼み直す)
					text += " (次に dispatcher を起動したら続きから再開する)"
				}
				cc.History = append(cc.History, card.Event{At: now, Text: text})
			}); err != nil {
				*notes = append(*notes, ev(eventlog.KindError, c.ID, target, fmt.Sprintf("%s のカードを書き直せない (PG は止めた): %v", c.ID, err)))
			}
		}
		if waiting == 0 {
			return failed, tried
		}
	}
}

// ensurePolls は、止め直しと確かめを繰り返す回数 (shutdownPoll ごと。約 30 秒。落ちて自動の再開の途中の session は、再開してから止める)。
const ensurePolls = 15

// ensureStopped は、pro-con の記録にある session (pro-con が起動したもの) がすべて止まった (state: stopped か、一覧から消えた) かを
// 確かめ、生きているものを止め直す。止まらなかった session を「カード (短い id)」で返す。記録に無い session には触らない。
//
// 照合は session id だけで、記録の pid は見ない: 同じ session id で pid だけ違うのは Claude Code の自動の再開 (pro-con の PG そのもの)。
// 外の shell の `claude --resume` は別の session id を立てる (427 の 3f で実測) ので、外の session には当たらない
// extra はカードの側で止めようとした session (短い id → カード)。記録に無くても (起動・再開の途中で取り込んだもの) 同じく確かめる。
// cards が nil でなければ、そのカードの session だけを確かめる (閉じたカードの PG を止める = close.go)。
// polls は止め直しの周の数 (周の間は shutdownPoll 待つ)。0 なら待たずに 1 周だけ止めて、もう 1 度だけ見る。
// sent は止める要求が通った回数 (「pro-con が止めた」と「既に止まっていた」を区別する = close.go)。
func (d *Dispatcher) ensureStopped(ctx context.Context, extra map[string]string, cards map[string]bool, polls int) (notes []eventlog.Event, remaining []string, sent int, err error) {
	list := d.ListAll
	if list == nil {
		list = d.List
	}
	regPath := filepath.Join(d.Dir, live.RegistryFile)
	for attempt := 0; ; attempt++ {
		reg, err := live.LoadRegistry(regPath)
		if err != nil {
			return notes, nil, sent, fmt.Errorf("pro-con が起動した session の記録を読めないので、止まったかを確かめられない: %w", err)
		}
		// 入れ替わった前の session の記録が読めなくても、記録にある session は止める (読めない分は知らせる)
		if retired, err := live.LoadRetired(regPath); err != nil {
			if attempt == 0 {
				notes = append(notes, ev(eventlog.KindError, "", "", "再開で入れ替わった前の session の記録を読めない (その分は確かめられない): "+err.Error()))
			}
		} else {
			reg = append(reg, retired...)
		}
		if cards != nil {
			reg = slices.DeleteFunc(reg, func(o live.Owned) bool { return !cards[o.CardID] })
		}
		lctx, cancel := context.WithTimeout(ctx, stopCallTimeout)
		ss, err := list(lctx)
		cancel()
		if err != nil {
			if attempt >= polls || ctx.Err() != nil {
				return notes, nil, sent, fmt.Errorf("止まったかを確かめる一覧を取れない: %w", err)
			}
			d.sleep(shutdownPoll)
			continue
		}
		var remaining []string
		for _, o := range withExtra(reg, extra, ss) {
			for _, s := range ss {
				if s.SessionID != o.SessionID || s.Kind != "background" || s.Stopped() {
					continue
				}
				remaining = append(remaining, fmt.Sprintf("%s (%s)", o.CardID, s.ID))
				sctx, cancel := context.WithTimeout(ctx, stopCallTimeout)
				err := d.Launch.Stop(sctx, s.ID)
				cancel()
				if err != nil {
					notes = append(notes, ev(eventlog.KindStop, o.CardID, s.ID, fmt.Sprintf("%s の PG (%s) を止め直せない: %v", o.CardID, s.ID, err)))
				} else {
					sent++
					notes = append(notes, ev(eventlog.KindStop, o.CardID, s.ID, fmt.Sprintf("%s の PG (%s) がまだ動いていたので止め直した", o.CardID, s.ID)))
				}
			}
		}
		if len(remaining) == 0 || attempt >= polls || ctx.Err() != nil {
			if len(remaining) > 0 { // 最後の周で止め直したものがあるかもしれないので、もう 1 度だけ見る
				lctx, cancel := context.WithTimeout(ctx, stopCallTimeout)
				if ss, err := list(lctx); err == nil {
					remaining = stillAlive(withExtra(reg, extra, ss), ss)
				}
				cancel()
			}
			return notes, remaining, sent, nil
		}
		d.sleep(shutdownPoll)
	}
}

// withExtra は記録の行に、カードの側で止めようとした session (一覧で短い id から session id を引く) を足す。
func withExtra(reg []live.Owned, extra map[string]string, ss []agents.Session) []live.Owned {
	out := append([]live.Owned(nil), reg...)
	for _, s := range ss {
		if cardID, ok := extra[s.ID]; ok && s.SessionID != "" && !slices.ContainsFunc(out, func(o live.Owned) bool { return o.SessionID == s.SessionID }) {
			out = append(out, live.Owned{ID: s.ID, SessionID: s.SessionID, CardID: cardID})
		}
	}
	return out
}

// stillAlive は記録にある session のうち、一覧で止まっていないもの。
func stillAlive(reg []live.Owned, ss []agents.Session) []string {
	var out []string
	for _, o := range reg {
		for _, s := range ss {
			if s.SessionID == o.SessionID && s.Kind == "background" && !s.Stopped() {
				out = append(out, fmt.Sprintf("%s (%s)", o.CardID, s.ID))
			}
		}
	}
	return out
}

// shutdownPoll / shutdownPolls は、すぐには止められない形を待つ間隔と回数 (約 46 秒。自動の再開は 11〜18 秒 = 427 の 3f)。
const (
	shutdownPoll  = 2 * time.Second
	shutdownPolls = 23
)

func (d *Dispatcher) sleep(t time.Duration) {
	if d.Sleep != nil {
		d.Sleep(t)
		return
	}
	time.Sleep(t)
}

// stopTarget は、終了のときにこのカードで止める session の短い id を返す。wait が真なら、今は止められないが待てば止められる形。
// 両方とも空 / 偽なら、止めるものが無い (既に止まっている)。
func (d *Dispatcher) stopTarget(c card.Card, now time.Time, ss []agents.Session, reg []live.Owned) (string, bool) {
	if c.Launching != "" { // 起動・再開の結果が分からない。立っていれば取り込んで止める (落ちて pid 0 でも止める。claude stop が再開を抑える)
		if id, ok := adopt(c, d.Repos[c.Repo], ss, reg); ok {
			return id, false
		}
		return "", now.Sub(c.LaunchedAt) < launchGrace // 印の直後ならまだ一覧に出ていないだけかもしれない
	}
	o, ok := owned(c, reg)
	if !ok { // dispatcher が起動・再開した直後で、記録にまだ無い (register が載せるのを待つ)
		return "", now.Sub(c.LaunchedAt) < launchGrace
	}
	for _, s := range ss {
		if s.ID != c.Session || s.Kind != "background" || s.SessionID != o.SessionID {
			continue
		}
		if s.PID == 0 { // 落ちて Claude Code の自動の再開を待っている: そのまま止める (claude stop が再開を抑える。2.1.282 で実測 2026-09-25:
			// kill -9 の直後 (pid 無し・working) に stop → rc=0 で stopped になり、35 秒後も再開しない)
			return s.ID, false
		}
		// pid が記録と違っても止める: 同じ session id で pid だけ違うのは Claude Code の自動の再開 (外の shell の --resume は
		// 別の session id を立てる = 427 の 3f)。再開の文がまだ書かれていないと register が記録を書き直さないので、ここで照らす
		return s.ID, false
	}
	// 一覧に無い: 止まっている。ただし落ちたのを見た直後なら、自動の再開を待っている途中かもしれない
	return "", !c.DeadSince.IsZero() && now.Sub(c.DeadSince) < restartWait
}

// StopRequested は止める印があるかを見て、あれば消す。
func StopRequested(dir string) bool {
	err := os.Remove(filepath.Join(dir, StopRequestFile))
	return err == nil
}

// RequestStop は動いている dispatcher に止めるよう頼み、止まる (ロックが外れる) まで待つ。
// dispatcher が動いていなければ false を返す (呼び出し側が自分で dispatcher の役を取って止める)。
func RequestStop(ctx context.Context, dir string, timeout time.Duration) (bool, error) {
	unlock, err := Lock(dir)
	if err == nil {
		unlock()
		return false, nil
	}
	if !errors.Is(err, ErrRunning) {
		return false, err
	}
	_ = os.Remove(filepath.Join(dir, StopResultFile)) // 前の結果を読まない
	if err := os.WriteFile(filepath.Join(dir, StopRequestFile), []byte("stop\n"), 0o600); err != nil {
		return true, err
	}
	_ = wake.Poke(dir) // 待ち (3 秒) を切り上げさせる。届かなくても dispatcher は次の Tick で印を読む
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		// 結果を先に見る: 止め終えた dispatcher が lock を外した直後に、開いている画面が次の dispatcher を起こして lock を取ることがある
		// (lock が外れるのを待つと、止め終えたのに時間切れと読む)
		if data, err := os.ReadFile(filepath.Join(dir, StopResultFile)); err == nil {
			if r := strings.TrimSpace(string(data)); r != "ok" {
				return true, errors.New(r)
			}
			return true, nil
		}
		if unlock, err := Lock(dir); err == nil {
			unlock()
			if _, err := os.Stat(filepath.Join(dir, StopResultFile)); err == nil {
				continue // lock を外す直前に書いた結果を、次の周で読む
			}
			return true, ErrStopperDied
		}
	}
	_ = os.Remove(filepath.Join(dir, StopRequestFile)) // 取り下げる (画面が閉じた後で dispatcher が止めに入らないように)
	return true, ErrStopTimeout
}
