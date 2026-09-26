package dispatcher

// 知らせ (426 の決定 10): 件数を tmux のユーザー option に書き (status が読む)、人の番 (card.Turn) になったカードは macOS の通知でも知らせる。
// 数える・知らせるのは人の番だけ (PM が先に受ける質問・取り込みの係のレビューは人に知らせない。452 で 2026-09-26 にユーザーが決めた)
// 🚨 status のどこにどう出すかは未配線 (見た目はユーザーと決める。issue 427 の 3c-2c)。option の名前は StatusOption

import (
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
)

// StatusOption は dispatcher が件数を書く tmux のユーザー option。tmux の status は #() で fork せず、これを format で読む (_tmux.conf の方針)。
const StatusOption = "@pro-con-status"

// republishEvery は件数の文が変わらなくても書き直す間隔。
const republishEvery = time.Minute

// Status は tmux の status に出す短い文 (?N は人の番のうち落ちて止めた PG 以外。r は起こさない役)。知らせることが無ければ空。
func Status(cards []card.Card, r card.Roles) string {
	var ask, crashed, stalled int
	for _, c := range cards {
		switch {
		case c.State == card.Running && c.Stalled:
			stalled++
		case c.Turn(r) != card.TurnHuman:
		case c.Wait.Kind == card.WaitCrashed:
			crashed++
		default:
			ask++
		}
	}
	var parts []string
	if ask > 0 {
		parts = append(parts, fmt.Sprintf("?%d", ask))
	}
	if stalled > 0 {
		parts = append(parts, fmt.Sprintf("停滞%d", stalled))
	}
	if crashed > 0 {
		parts = append(parts, fmt.Sprintf("🚨落ちた%d", crashed))
	}
	if len(parts) == 0 {
		return ""
	}
	return "pro-con " + strings.Join(parts, " ")
}

// announce は件数の文が変わったら Publish し、新しく人の番になったカードを Notify する。
// 同じ人の番を 2 度知らせない (抜けたら忘れるので、次に人の番になったらまた知らせる。dispatcher が起動し直すと、人の番のカードを 1 度ずつ知らせ直す)。
func (d *Dispatcher) announce() []eventlog.Event {
	st, err := store.Load(d.Dir)
	if err != nil {
		return []eventlog.Event{ev(eventlog.KindError, "", "", "知らせ: 記録を読めない: "+err.Error())}
	}
	var notes []eventlog.Event
	now := d.Now()
	// 変わったときに加えて、republishEvery ごとにも書き直す (tmux サーバが作り直されて option が消えても戻る)
	if s := Status(st.Cards, d.roles()); d.Publish != nil && (!d.published || s != d.lastStatus || now.Sub(d.publishedAt) >= republishEvery) {
		if err := d.Publish(s); err != nil { // 失敗しても次は文が変わるか republishEvery 後 (tmux の外で動かしたとき Tick ごとには言い続けない)
			notes = append(notes, ev(eventlog.KindError, "", "", "知らせ: tmux に件数を書けない: "+err.Error()))
		}
		d.lastStatus, d.published, d.publishedAt = s, true, now
	}
	if d.Notify == nil {
		return notes
	}
	if d.notified == nil {
		d.notified = map[string]bool{}
	}
	// 鍵はカード ID と今の列に入った時刻 (質問待ちから人に回したレビューへ移ったら、人の番のままでも知らせ直す)
	humans := map[string]bool{}
	roles := d.roles()
	for _, c := range st.Cards {
		if c.Turn(roles) != card.TurnHuman {
			continue
		}
		k := c.ID + "@" + c.Since.UTC().Format(time.RFC3339Nano)
		humans[k] = true
		if d.notified[k] {
			continue
		}
		body := c.ID + " " + c.Title + ": " + cmp.Or(c.Wait.Question, c.State.Label())
		if err := d.Notify("pro-con: 人の番", body); err != nil {
			notes = append(notes, ev(eventlog.KindError, c.ID, "", "知らせ: 通知を出せない: "+err.Error()))
			continue // 次の Tick でまた出す
		}
		d.notified[k] = true
	}
	for k := range d.notified {
		if !humans[k] {
			delete(d.notified, k)
		}
	}
	return notes
}

// TmuxPublish は tmux のユーザー option に件数の文を書く (空なら option を外す)。tmux の外で動いていれば失敗を返す。
func TmuxPublish(ctx context.Context) func(string) error {
	return func(s string) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		args := []string{"set-option", "-g", StatusOption, s}
		if s == "" {
			args = []string{"set-option", "-gu", StatusOption}
		}
		if out, err := exec.CommandContext(ctx, "tmux", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("tmux %s: %w: %s", strings.Join(args[:2], " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

// MacNotify は macOS の通知を出す。題と本文は引数で渡す (AppleScript の文字列に埋め込まない)。
func MacNotify(ctx context.Context) func(title, body string) error {
	return func(title, body string) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "osascript",
			"-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", title, body)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("osascript: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}
