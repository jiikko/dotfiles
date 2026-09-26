package dispatcher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// 件数の文: 人の番 (質問・権限・人に回したレビュー) / 停滞 / 落ちて止めた、を別に数える。PM が先に受ける質問は数えない。何も無ければ空。
func TestStatusText(t *testing.T) {
	cards := []card.Card{
		{State: card.Waiting, Wait: card.Wait{Kind: card.WaitQuestion}},
		{State: card.Waiting, Wait: card.Wait{Kind: card.WaitPermission}},
		{State: card.Waiting, Wait: card.Wait{Kind: card.WaitCrashed}},
		{State: card.Review, History: []card.Event{{Text: card.HandoffText("取り込みの係", "衝突")}}},
		{State: card.Review},
		{State: card.Running, Stalled: true},
		{State: card.Running},
		{State: card.Done},
	}
	if got := Status(cards, card.Roles{}); got != "pro-con ?2 停滞1 🚨落ちた1" {
		t.Fatalf("件数の文: %q", got)
	}
	if got := Status(cards, card.Roles{PMOff: true, IntegratorOff: true}); got != "pro-con ?4 停滞1 🚨落ちた1" {
		t.Fatalf("起こさない役の質問・レビューを人の番に数えない: %q", got)
	}
	if got := Status([]card.Card{{State: card.Running}, cards[0]}, card.Roles{}); got != "" {
		t.Fatalf("知らせることが無いのに文を出した: %q", got)
	}
}

// PM が居れば、PG の質問は PM が先に受けるので人に知らせない。PM が人に回したら知らせ、件数にも数える。
func TestAnnounceOnlyHumansTurn(t *testing.T) {
	r := startedPM(t)
	var rec recorder
	rec.rig(r.d)
	last := func() string {
		if len(rec.published) == 0 {
			return ""
		}
		return rec.published[len(rec.published)-1]
	}
	asked(t, r.dir, "C-009", "赤か青か", t0)
	r.tick(t)
	if len(rec.notified) != 0 || strings.Contains(last(), "?") {
		t.Fatalf("PM が先に受ける質問を人に知らせた: pub=%q notif=%v", rec.published, rec.notified)
	}
	if _, err := store.Submit(r.dir, store.Request{Kind: "handoff", CardID: "C-009", Text: "好みは人が決める", From: "PM"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if len(rec.notified) != 1 || !strings.Contains(rec.notified[0], "赤か青か") || last() != "pro-con ?1" {
		t.Fatalf("人に回した質問を知らせない: pub=%q notif=%v", rec.published, rec.notified)
	}
}

type recorder struct {
	published []string
	notified  []string
	failPub   bool
}

func (r *recorder) rig(d *Dispatcher) {
	d.Publish = func(s string) error {
		r.published = append(r.published, s)
		if r.failPub {
			return errors.New("tmux の外")
		}
		return nil
	}
	d.Notify = func(_, body string) error { r.notified = append(r.notified, body); return nil }
}

// 件数の文は起動の直後に 1 度 (空でも) 書き、以後は変わったときだけ書く。回答待ちになったカードは 1 度だけ通知する。
func TestAnnouncePublishesOnChangeAndNotifiesOnce(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	var r recorder
	r.rig(d)
	for range 2 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.published) != 1 || r.published[0] != "" || len(r.notified) != 0 {
		t.Fatalf("起動の直後に 1 度だけ書く / 待ちが無いのに通知した: pub=%q notif=%v", r.published, r.notified)
	}
	if _, err := store.Submit(dir, store.Request{Kind: "ask", CardID: "C-001", Question: "赤か青か"}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.published) != 2 || r.published[1] != "pro-con ?1" || len(r.notified) != 1 {
		t.Fatalf("回答待ちを知らせない / 2 度知らせた: pub=%q notif=%v", r.published, r.notified)
	}
}

// tmux に書けなくても、文が変わるまでは書き直さない (tmux の外で動かしたとき Tick ごとに言い続けない)。
func TestAnnounceDoesNotRetryFailedPublishUntilChange(t *testing.T) {
	dir := t.TempDir()
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	r := recorder{failPub: true}
	r.rig(d)
	for range 3 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.published) != 1 {
		t.Fatalf("書けなかった文を Tick ごとに書き直した: %d 回", len(r.published))
	}
}

// 回答で待ちを抜けて、また待ちに入ったカードは、もう一度通知する。
func TestAnnounceNotifiesAgainOnNewWait(t *testing.T) {
	cr := newCrashRig(t) // 作業中・登録済みの PG (再開できる形)
	var r recorder
	r.rig(cr.d)
	submit := func(req store.Request) {
		t.Helper()
		if _, err := store.Submit(cr.dir, req); err != nil {
			t.Fatal(err)
		}
	}
	submit(store.Request{Kind: "ask", CardID: "C-001", Question: "q1"})
	cr.tick(t)
	submit(store.Request{Kind: "answer", CardID: "C-001", Answer: "a"})
	cr.d.Now = func() time.Time { return t0.Add(time.Minute) }
	cr.tick(t) // 再開
	if c := states(t, cr.dir)["C-001"]; c.State != card.Running {
		t.Fatalf("前提: 再開して作業中に戻っていない: %v", c.State)
	}
	submit(store.Request{Kind: "ask", CardID: "C-001", Question: "q2"})
	cr.d.Now = func() time.Time { return t0.Add(2 * time.Minute) }
	cr.tick(t)
	if len(r.notified) != 2 {
		t.Fatalf("2 度目の質問を通知しない: %v", r.notified)
	}
}

// 件数の文が変わらなくても republishEvery ごとに書き直す (tmux サーバが作り直されて option が消えても戻る)。
func TestAnnounceRepublishesPeriodically(t *testing.T) {
	dir := t.TempDir()
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	var r recorder
	r.rig(d)
	for _, at := range []time.Time{t0, t0.Add(30 * time.Second), t0.Add(republishEvery + time.Second)} {
		d.Now = func() time.Time { return at }
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.published) != 2 {
		t.Fatalf("間隔ごとに書き直さない / 間隔の前に書き直した: %d 回", len(r.published))
	}
}
