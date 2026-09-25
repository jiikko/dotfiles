package daemon

// pro-con を終了するときの停止 (2026-09-25 にユーザーが決めた形: daemon と、pro-con が起動した PG を全部止め、次に daemon を起動したら続きから再開する)。
// 止めるのは daemon だけ (PG の session に触るのは daemon の役)。画面は `pro-con daemon --stop` で頼む。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// StopRequestFile は、動いている daemon に止めるよう頼む印 (状態の置き場の下)。daemon は Tick の間に見つけたら Shutdown して抜ける。
// StopResultFile は、止めた daemon が抜ける前に書く結果 ("ok" か、止めきれなかった理由)。頼んだ側はロックが外れた後にこれを読む
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

// ErrStopTimeout は、動いている daemon が時間内に止まらなかったとき。
var ErrStopTimeout = errors.New("pro-con daemon が時間内に止まらない")

// Shutdown は pro-con が起動した PG の session を全部止め、カードを次の起動で続きから再開できる形にする:
//   - 作業中 → 分解済みへ戻し、再開の文 (resumeAfterStop) を持たせる (次の daemon が --resume する)
//   - それ以外 (質問待ち・レビュー待ち等) → 列はそのまま
//
// 止めたカードには Stopped の印を付ける (再開のとき、落ちた PG の自動の再開を待つ restartWait を飛ばす)。
// 止める相手は、記録と session id・pid が一致する bg の session、または daemon 自身が起動・再開した直後でまだ記録に無い session だけ。
// すぐには止められない形 (落ちて自動の再開を待っている / 起動・再開の直後で一覧にまだ出ない) は、一覧を取り直しながら shutdownPolls 回待つ。
// 待っても止められなかったカードは列を変えず (次の daemon が普段どおり扱う)、止めきれなかった本数をエラーで返す。
func (d *Daemon) Shutdown(ctx context.Context) ([]string, error) {
	now := d.Now()
	var notes []string
	d.cancelRun(10 * time.Second) // テストの係の実行中の 1 本を取り消す (再開した PG は続きから頼み直す)
	// 止める直前に PG が置いた質問・完了の依頼を先に適用する (作業中のまま分解済みへ戻すと、次の起動で除けられて失われる)
	res, err := store.Apply(d.Dir, now)
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if r.Err != "" {
			notes = append(notes, fmt.Sprintf("箱の依頼 %s (%s) を除けた: %s", r.ID, r.Kind, r.Err))
		}
	}
	done := map[string]bool{}
	seenWarn := map[string]bool{}
	failed := 0
	for attempt := 0; ; attempt++ {
		now := d.Now() // 待ちの判定 (launchGrace / restartWait) は周ごとに今の時刻で行う
		ss, err := d.List(ctx)
		if err != nil {
			return notes, fmt.Errorf("session の一覧を取れないので止められない: %w", err)
		}
		// 起動・再開の直後で記録にまだ無い PG を、止める前に記録へ載せる
		_, warn, err := d.register(now, ss)
		if err != nil {
			return notes, err
		}
		for _, w := range warn { // 周をまたいで同じ警告を重ねない
			if !seenWarn[w] {
				seenWarn[w] = true
				notes = append(notes, w)
			}
		}
		st, err := store.Load(d.Dir)
		if err != nil {
			return notes, err
		}
		reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
		if err != nil {
			return notes, err
		}
		waiting := 0
		for _, c := range st.Cards {
			if done[c.ID] || c.State == card.Done || (c.Session == "" && c.Launching == "") {
				continue
			}
			target, wait := d.stopTarget(c, now, ss, reg)
			if wait && attempt < shutdownPolls {
				waiting++
				continue
			}
			done[c.ID] = true
			if wait { // 待っても止められる形にならなかった。列は変えない
				failed++
				notes = append(notes, fmt.Sprintf("%s の PG は落ちて戻らない / 一覧に出ないので止められない (列はそのまま。次の daemon が扱う)", c.ID))
				continue
			}
			if target != "" {
				if err := d.Launch.Stop(ctx, target); err != nil {
					failed++
					notes = append(notes, fmt.Sprintf("%s の PG (%s) を止められない: %v", c.ID, target, err))
					continue
				}
				notes = append(notes, fmt.Sprintf("%s の PG (%s) を止めた", c.ID, target))
			}
			if err := d.update(c.ID, func(cc *card.Card) {
				cc.Stopped, cc.Launching = true, "" // 取り込めた起動・再開は止めた。取り込めなかったものは stopTarget が wait を返している
				text := "pro-con の終了で PG を止めた"
				if target == "" {
					text = "pro-con の終了: PG の session は既に止まっていた"
				}
				if cc.State == card.Running {
					cc.State, cc.Since, cc.Resume = card.Planned, now, resumeAfterStop
					cc.Run, cc.RunAt, cc.Exec, cc.Wait = "", time.Time{}, card.Exec{}, card.Wait{} // テストの係への頼みも取り下げる (続きから頼み直す)
					text += " (次に daemon を起動したら続きから再開する)"
				}
				cc.History = append(cc.History, card.Event{At: now, Text: text})
			}); err != nil {
				return notes, err
			}
		}
		if waiting == 0 {
			break
		}
		d.sleep(shutdownPoll)
	}
	if d.Publish != nil {
		_ = d.Publish("") // 件数を消す
	}
	if failed > 0 {
		return notes, fmt.Errorf("%d 本の PG を止められなかった", failed)
	}
	return notes, nil
}

// shutdownPoll / shutdownPolls は、すぐには止められない形を待つ間隔と回数 (約 46 秒。自動の再開は 11〜18 秒 = 427 の 3f)。
const (
	shutdownPoll  = 2 * time.Second
	shutdownPolls = 23
)

func (d *Daemon) sleep(t time.Duration) {
	if d.Sleep != nil {
		d.Sleep(t)
		return
	}
	time.Sleep(t)
}

// stopTarget は、終了のときにこのカードで止める session の短い id を返す。wait が真なら、今は止められないが待てば止められる形。
// 両方とも空 / 偽なら、止めるものが無い (既に止まっている)。
func (d *Daemon) stopTarget(c card.Card, now time.Time, ss []agents.Session, reg []live.Owned) (string, bool) {
	if c.Launching != "" { // 起動・再開の結果が分からない。立っていれば取り込んで止める
		if id, ok := adopt(c, d.Repos[c.Repo], ss, reg); ok {
			for _, s := range ss {
				if s.ID == id && s.PID != 0 {
					return id, false
				}
			}
			return "", true // 立っているが落ちている
		}
		return "", now.Sub(c.LaunchedAt) < launchGrace // 印の直後ならまだ一覧に出ていないだけかもしれない
	}
	o, ok := owned(c, reg)
	if !ok { // daemon が起動・再開した直後で、記録にまだ無い (register が載せるのを待つ)
		return "", now.Sub(c.LaunchedAt) < launchGrace
	}
	for _, s := range ss {
		if s.ID != c.Session || s.Kind != "background" || s.SessionID != o.SessionID {
			continue
		}
		if s.PID == 0 {
			return "", true // 落ちて Claude Code の自動の再開を待っている
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

// RequestStop は動いている daemon に止めるよう頼み、止まる (ロックが外れる) まで待つ。
// daemon が動いていなければ false を返す (呼び出し側が自分で daemon の役を取って止める)。
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
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		if unlock, err := Lock(dir); err == nil {
			unlock()
			data, err := os.ReadFile(filepath.Join(dir, StopResultFile))
			if err != nil {
				return true, errors.New("pro-con daemon が止め終える前に終わった (止めた結果が無い)")
			}
			if r := strings.TrimSpace(string(data)); r != "ok" {
				return true, errors.New(r)
			}
			return true, nil
		}
	}
	_ = os.Remove(filepath.Join(dir, StopRequestFile)) // 取り下げる (画面が閉じた後で daemon が止めに入らないように)
	return true, ErrStopTimeout
}
