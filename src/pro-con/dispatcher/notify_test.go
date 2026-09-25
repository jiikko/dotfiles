package dispatcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// 件数の文: 回答待ち (質問・権限) / 停滞 / 落ちて止めた、を別に数える。何も無ければ空。
func TestStatusText(t *testing.T) {
	cards := []card.Card{
		{State: card.Waiting, Wait: card.Wait{Kind: card.WaitQuestion}},
		{State: card.Waiting, Wait: card.Wait{Kind: card.WaitPermission}},
		{State: card.Waiting, Wait: card.Wait{Kind: card.WaitCrashed}},
		{State: card.Running, Stalled: true},
		{State: card.Running},
		{State: card.Done},
	}
	if got := Status(cards); got != "pro-con ?2 停滞1 🚨落ちた1" {
		t.Fatalf("件数の文: %q", got)
	}
	if got := Status([]card.Card{{State: card.Running}}); got != "" {
		t.Fatalf("知らせることが無いのに文を出した: %q", got)
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
