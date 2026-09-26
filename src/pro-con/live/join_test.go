package live

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/presence"
	"pro-con/store"
)

// openAs は be に画面の印を置く (Start の代わり。読み直しを回さずに StopAll / Apply だけを見る)。
func openAs(t *testing.T, be *Backend, mode presence.Mode) {
	t.Helper()
	sc, err := presence.OpenAs(be.dir, presence.Info{Mode: mode, Label: be.label})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sc.Close)
	be.screen = sc
}

// join の画面 (issue 481) は quit で閉じても止めない。持ち主の画面は、join が残っていても最後の持ち主なら止める
// (join を数えると、加わった人の画面が開いているだけで持ち主の quit が PG を止めなくなる)。
func TestJoinQuitStopsNothingAndOwnerIgnoresJoins(t *testing.T) {
	owner, j := shortState(t), shortState(t)
	j.dir, j.registry = owner.dir, owner.registry
	var stops atomic.Int32
	stop := func(context.Context) error { stops.Add(1); return nil }
	owner.SetStopper(stop)
	j.SetStopper(stop) // つないでいても join は止めない
	jb := j.Join()
	openAs(t, owner, presence.Owner)
	openAs(t, j, presence.Join)
	if err := owner.StopAll(context.Background()); err != nil || stops.Load() != 1 {
		t.Fatalf("join が残っていても最後の持ち主は止める: err=%v stops=%d", err, stops.Load())
	}
	if got := inboxEvents(t, owner.dir); len(got) != 1 || !strings.Contains(got[0], "join の画面 1 は残り") {
		t.Fatalf("join が残ることを出来事に書かない: %q", got)
	}
	var kept backend.KeptRunning
	if err := jb.(backend.Stopper).StopAll(context.Background()); !errors.As(err, &kept) || !kept.Join || stops.Load() != 1 {
		t.Fatalf("join の画面の quit が止めた / 止めなかったと返さない: err=%v stops=%d", err, stops.Load())
	}
}

// join の画面は dispatcher を起こさない: keeper を呼ばず、c (ResumeDispatcher) も受けない。ほかの書く操作は受ける。
func TestJoinNeverWakesDispatcher(t *testing.T) {
	b := shortState(t)
	var keeps atomic.Int32
	b.SetKeeper(func() error { keeps.Add(1); return nil })
	jb := b.Join()
	openAs(t, b, presence.Join)
	b.refresh(context.Background(), false) // dispatcher の様子を読む (1 度も回っていない = 持ち主なら起こす)
	b.keep()
	if keeps.Load() != 0 {
		t.Fatal("join の画面が dispatcher を起こした")
	}
	if a, ok := jb.(backend.Accepter); !ok || a.Accepts(backend.OpResume) || !a.Accepts(backend.OpAnswer) {
		t.Fatal("join の画面が c を受ける / 回答を受けない")
	}
	if _, err := jb.Apply(backend.ResumeDispatcher{}); !errors.Is(err, ErrJoinNoResume) || keeps.Load() != 0 {
		t.Fatalf("join の画面の c が dispatcher を起こした: %v", err)
	}
	msg, err := jb.Apply(backend.NewRequest{Text: "調べて"})
	if err != nil || !strings.Contains(msg, "受付の箱で待っている") {
		t.Fatalf("dispatcher が居ないのに、箱で待っていると知らせない: %q %v", msg, err)
	}
}

// 除けた理由は、その依頼を置いた画面にだけ出す (画面 ID と依頼 ID を結ぶ。issue 481)。理由は dispatcher が書いたもの。
// 適用された依頼は知らせない。依頼の履歴には置いた画面が残る。
func TestRejectedReasonGoesOnlyToSubmittingScreen(t *testing.T) {
	a, b := shortState(t), shortState(t)
	b.dir, b.registry = a.dir, a.registry
	b.label = "review"
	openAs(t, a, presence.Owner)
	openAs(t, b, presence.Join)
	jb := b.Join()
	if _, err := jb.Apply(backend.NewRequest{Text: "調べて"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(a.dir, time.Now()); err != nil { // dispatcher の代わり
		t.Fatal(err)
	}
	// 同じカードへ 2 つの画面が回答を重ねた形: 先に来た a の回答で質問待ちでなくなり、後の b の回答は除けられる
	if err := store.Update(a.dir, func(st *store.State) error {
		st.Cards[0].State, st.Cards[0].Wait = card.Waiting, card.Wait{Kind: card.WaitQuestion, Question: "どちら?"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(backend.Answer{CardID: "C-001", Text: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := jb.Apply(backend.Answer{CardID: "C-001", Text: "B"}); err != nil {
		t.Fatal(err)
	}
	res, err := store.Apply(a.dir, time.Now())
	if err != nil || len(res) != 2 || res[0].Err != "" || res[1].Err == "" {
		t.Fatalf("後から来た回答を除けない: %+v %v", res, err)
	}
	for _, be := range []*Backend{a, b} {
		be.refresh(context.Background(), false)
	}
	if got := a.TakeRejected(); len(got) != 0 {
		t.Fatalf("ほかの画面が置いた依頼の除けた理由を出した: %+v", got)
	}
	got := b.TakeRejected()
	if len(got) != 1 || got[0].CardID != "C-001" || got[0].Kind != "answer" || got[0].Why != res[1].Err {
		t.Fatalf("置いた画面に、dispatcher が書いた除けた理由を渡さない: %+v (理由 %q)", got, res[1].Err)
	}
	if again := b.TakeRejected(); len(again) != 0 {
		t.Fatalf("同じ知らせを 2 度渡した: %+v", again)
	}
	st, err := store.Load(a.dir)
	if err != nil {
		t.Fatal(err)
	}
	var fromA, fromB bool
	for _, e := range st.Cards[0].History {
		fromA = fromA || e.Screen == a.screenName()
		fromB = fromB || e.Screen == b.screenName()
	}
	if !fromA || !fromB || !strings.Contains(b.screenName(), "join review") {
		t.Fatalf("依頼の履歴に置いた画面を残さない: %+v (a=%q b=%q)", st.Cards[0].History, a.screenName(), b.screenName())
	}
}
