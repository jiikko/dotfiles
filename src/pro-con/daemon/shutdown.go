package daemon

// pro-con を終了するときの停止 (2026-09-25 にユーザーが決めた形: daemon と、pro-con が起動した PG を全部止め、次に daemon を起動したら続きから再開する)。
// 止めるのは daemon だけ (PG の session に触るのは daemon の役)。画面は `pro-con daemon --stop` で頼む。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// StopRequestFile は、動いている daemon に止めるよう頼む印 (状態の置き場の下)。daemon は Tick の間に見つけたら Shutdown して抜ける。
const StopRequestFile = "stop-request"

// resumeAfterStop は、終了で止めた作業中の PG を次に再開するときに渡す文。
const resumeAfterStop = "pro-con の終了で作業の途中で止めた。止まる前の続きから作業を再開して (規律は最初の指示のとおり)"

// ErrStopTimeout は、動いている daemon が時間内に止まらなかったとき。
var ErrStopTimeout = errors.New("pro-con daemon が時間内に止まらない")

// Shutdown は pro-con が起動した PG の session を全部止め、カードを次の起動で続きから再開できる形にする:
//   - 作業中 → 分解済みへ戻し、再開の文 (resumeAfterStop) を持たせる (次の daemon が --resume する)
//   - それ以外 (質問待ち・レビュー待ち等) → 列はそのまま
//
// 止めたカードには Stopped の印を付ける (再開のとき、落ちた PG の自動の再開を待つ restartWait を飛ばす)。
// 止める相手は、記録と session id・pid が一致する一覧の session だけ (外の session に触らない)。止められなかった本数をエラーで返す。
func (d *Daemon) Shutdown(ctx context.Context) ([]string, error) {
	now := d.Now()
	ss, err := d.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("session の一覧を取れないので止められない: %w", err)
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil, err
	}
	var notes []string
	failed := 0
	for _, c := range st.Cards {
		if c.State == card.Done || c.Session == "" {
			continue
		}
		o, ok := owned(c, reg)
		if !ok {
			continue
		}
		target := ""
		for _, s := range ss {
			if s.ID == c.Session && s.Kind == "background" && s.SessionID == o.SessionID && s.PID != 0 && s.PID == o.PID {
				target = s.ID
			}
		}
		if target != "" {
			if err := d.Launch.Stop(ctx, target); err != nil {
				failed++
				notes = append(notes, fmt.Sprintf("%s の PG (%s) を止められない: %v", c.ID, target, err))
				continue
			}
		}
		if err := d.update(c.ID, func(cc *card.Card) {
			cc.Stopped, cc.Launching = true, ""
			text := "pro-con の終了で PG を止めた"
			if target == "" {
				text = "pro-con の終了: PG の session は既に止まっていた"
			}
			if cc.State == card.Running {
				cc.State, cc.Since, cc.Resume = card.Planned, now, resumeAfterStop
				text += " (次に daemon を起動したら続きから再開する)"
			}
			cc.History = append(cc.History, card.Event{At: now, Text: text})
		}); err != nil {
			return notes, err
		}
		if target != "" {
			notes = append(notes, fmt.Sprintf("%s の PG (%s) を止めた", c.ID, target))
		}
	}
	if d.Publish != nil {
		_ = d.Publish("") // 件数を消す
	}
	if failed > 0 {
		return notes, fmt.Errorf("%d 本の PG を止められなかった", failed)
	}
	return notes, nil
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
			return true, nil
		}
	}
	return true, ErrStopTimeout
}
