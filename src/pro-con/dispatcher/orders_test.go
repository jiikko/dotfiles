package dispatcher

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

// order は受付の箱に追加オーダーを置く。
func order(t *testing.T, dir, id string, k card.OrderKind, text string) {
	t.Helper()
	if _, err := store.Submit(dir, store.Request{Kind: "order", CardID: id, Order: k, Text: text}); err != nil {
		t.Fatal(err)
	}
}

// 作業中 (busy) の PG へは追記を届けない (止めない)。turn を終えて idle になったら同じ session を再開して届け、届いた印を付ける (issue 438)。
func TestAppendWaitsForIdleThenResumes(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = "busy"
	order(t, r.dir, "C-001", card.OrderAppend, "README も直して")
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.resumes) != 0 || len(r.l.stops) != 0 || c.State != card.Running || len(c.Pending()) != 1 {
		t.Fatalf("busy の PG を止めた / オーダーを失った: resumes=%v stops=%v %v %+v", r.l.resumes, r.l.stops, c.State, c.Orders)
	}
	r.ss[0].Status = agents.StatusIdle
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], "id-pc-c-001:") || !strings.Contains(r.l.resumes[0], "README も直して") {
		t.Fatalf("idle になった PG を、オーダーを渡して同じ session で再開していない: %v", r.l.resumes)
	}
	if c.State != card.Running || len(c.Pending()) != 0 || !c.Orders[0].Delivered {
		t.Fatalf("再開の後に届いた印が無い: %v %+v", c.State, c.Orders)
	}
}

// AskUserQuestion / 権限の確認で止まっている PG (status: waiting) には追記を届けない (止めると問いが消える)。落ちている PG も待つ。
func TestAppendNotDeliveredToWaitingOrDeadPG(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status, r.ss[0].WaitingFor = "waiting", "input needed"
	order(t, r.dir, "C-001", card.OrderAppend, "x")
	r.tick(t)
	r.ss[0].Status, r.ss[0].PID = agents.StatusIdle, 0 // 落ちて自動の再開を待っている
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.resumes) != 0 || c.State != card.Running {
		t.Fatalf("問いで止まった / 落ちた PG へ届けた: %v %v", r.l.resumes, c.State)
	}
}

// 方針変更は待たない: busy の PG でも止めて、指示を差し替えて同じ session を再開する。
func TestRedirectStopsBusyPG(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = "busy"
	order(t, r.dir, "C-001", card.OrderRedirect, "やっぱり青")
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], "id-pc-c-001:") || !strings.Contains(r.l.resumes[0], "方針変更") || !strings.Contains(r.l.resumes[0], "やっぱり青") {
		t.Fatalf("方針変更で止めて再開していない: %v", r.l.resumes)
	}
	if c := states(t, r.dir)["C-001"]; !c.Orders[0].Delivered {
		t.Fatalf("届いた印が無い: %+v", c.Orders)
	}
}

// PG が質問で turn を終えたら、追記は回答の再開に添えて届く (回答と別に 2 回再開しない)。
func TestAppendRidesOnAnswer(t *testing.T) {
	r := newCrashRig(t)
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "赤か青か"}, {Kind: "order", CardID: "C-001", Text: "README も"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	r.ss[0].Status = agents.StatusIdle
	r.tick(t)
	if len(r.l.resumes) != 0 {
		t.Fatalf("質問待ちの PG を回答より先に再開した: %v", r.l.resumes)
	}
	if _, err := store.Submit(r.dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "青"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], "id-pc-c-001:青") || !strings.Contains(r.l.resumes[0], "README も") {
		t.Fatalf("回答にオーダーを添えて 1 回で再開していない: %v", r.l.resumes)
	}
	if c := states(t, r.dir)["C-001"]; len(c.Pending()) != 0 {
		t.Fatalf("届いた印が無い: %+v", c.Orders)
	}
}

// 起動より前に積まれたオーダーは、起動の指示に入れて届ける。
func TestOrderBeforeStartGoesInPrompt(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	order(t, dir, "C-001", card.OrderAppend, "ついでに typo も")
	l := &fakeLauncher{}
	if _, err := newDispatcher(t, dir, l, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.prompts) != 1 || !strings.Contains(l.prompts[0], "ついでに typo も") {
		t.Fatalf("起動の指示にオーダーが無い: %v", l.prompts)
	}
	if c := states(t, dir)["C-001"]; len(c.Pending()) != 0 {
		t.Fatalf("届いた印が無い: %+v", c.Orders)
	}
}

// 失敗と返った再開では届いた印を付けない。後の Tick で一覧から取り込んだとき、印を書いた後に積まれたオーダーには付けない (まだ渡していない)。
func TestDeliveredOnlyOrdersSentWithResume(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = agents.StatusIdle
	r.l.resumeFail = true
	order(t, r.dir, "C-001", card.OrderAppend, "1 つめ")
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.resumes) != 1 || c.Launching != "再開" || len(c.Pending()) != 1 {
		t.Fatalf("失敗と返った再開で届いた印を付けた: %v %q %+v", r.l.resumes, c.Launching, c.Orders)
	}
	t1 := t0.Add(10 * time.Second)
	r.d.Now = func() time.Time { return t1 }
	order(t, r.dir, "C-001", card.OrderAppend, "2 つめ")
	r.ss[0].PID, r.ss[0].StartedAt = 43, t0.Add(time.Second).UnixMilli()+1 // 実は再開していた (印の後に始まった)
	r.ss[0].Status = "busy"
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.State != card.Running || !c.Orders[0].Delivered || c.Orders[1].Delivered {
		t.Fatalf("渡したオーダーだけに印を付けていない: %v %+v", c.State, c.Orders)
	}
}

// レビュー待ちに未達のオーダーが残っていたら、PG へ戻して届ける。PG が turn の最後に置いた依頼が箱に残っている間は再開しない
// (その依頼を再開で追い越して除けさせない)。
func TestReviewWithPendingOrderGoesBackToPG(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = agents.StatusIdle
	order(t, r.dir, "C-001", card.OrderAppend, "README も")
	list := r.d.List
	r.d.List = func(ctx context.Context) ([]agents.Session, error) { // 一覧を取る間に、PG が review を置いて turn を終えた
		if _, err := store.Submit(r.dir, store.Request{Kind: "review", CardID: "C-001"}); err != nil {
			t.Fatal(err)
		}
		r.d.List = list
		return list(ctx)
	}
	r.tick(t)
	if len(r.l.resumes) != 0 {
		t.Fatalf("箱に review が残っているのに再開した: %v", r.l.resumes)
	}
	r.tick(t) // review を適用 → レビュー待ちに未達が残る → PG へ戻す
	if rej, _ := filepath.Glob(filepath.Join(r.dir, store.InboxDir, store.RejectedDir, "*.json")); len(rej) != 0 {
		t.Fatalf("PG の review が除けられた: %v", rej)
	}
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "README も") || !strings.Contains(r.l.resumes[0], "pro-con card review C-001") {
		t.Fatalf("レビュー待ちの未達を PG へ届けていない: %v", r.l.resumes)
	}
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || len(c.Pending()) != 0 {
		t.Fatalf("%v %+v", c.State, c.Orders)
	}
}

// 削除の依頼を受けたカードは、未達のオーダーがあっても再開の列へ戻さない (PG を止めて消すのを待っている間は列を動かさない。issue 451)。
func TestNoDeliveryToDeletingCard(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = agents.StatusIdle
	r.l.stopFail = true // 止められずに削除を待ち続ける形
	order(t, r.dir, "C-001", card.OrderRedirect, "x")
	if _, err := store.Apply(r.dir, t0, nil); err != nil {
		t.Fatal(err)
	}
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.DeleteAt = t0 })
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.resumes) != 0 || c.State != card.Running {
		t.Fatalf("削除を待つカードを再開の列へ戻した: resumes=%v %v", r.l.resumes, c.State)
	}
}

// 方針変更も、PG が turn の最後に置いた依頼 (review / run) が箱に残っている間は再開しない (追い越して除けさせない)。
func TestRedirectWaitsForInbox(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = "busy"
	order(t, r.dir, "C-001", card.OrderRedirect, "青に")
	list := r.d.List
	r.d.List = func(ctx context.Context) ([]agents.Session, error) {
		if _, err := store.Submit(r.dir, store.Request{Kind: "review", CardID: "C-001"}); err != nil {
			t.Fatal(err)
		}
		r.d.List = list
		return list(ctx)
	}
	r.tick(t)
	if len(r.l.resumes) != 0 {
		t.Fatalf("箱に review が残っているのに方針変更で再開した: %v", r.l.resumes)
	}
	r.tick(t)
	if rej, _ := filepath.Glob(filepath.Join(r.dir, store.InboxDir, store.RejectedDir, "*.json")); len(rej) != 0 {
		t.Fatalf("PG の review が除けられた: %v", rej)
	}
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "青に") {
		t.Fatalf("方針変更を届けていない: %v", r.l.resumes)
	}
}

// 一覧から消えて戻らない PG (458) に積まれた追記は、分解済みへ戻した再開の文に添えて届く (消えた PG の再開と別に待たせない)。
func TestAppendRidesOnVanishedResume(t *testing.T) {
	r := newCrashRig(t)
	order(t, r.dir, "C-001", card.OrderAppend, "README も")
	r.ss = nil // 一覧から消えた
	r.d.Now = func() time.Time { return t0.Add(time.Minute) }
	r.tick(t)
	r.d.Now = func() time.Time { return t0.Add(time.Minute + restartWait + time.Second) }
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], ":"+resumeAfterVanish) || !strings.Contains(r.l.resumes[0], "README も") {
		t.Fatalf("消えた PG の再開に追記を添えていない: %v", r.l.resumes)
	}
	if c := states(t, r.dir)["C-001"]; len(c.Pending()) != 0 {
		t.Fatalf("届いた印が無い: %+v", c.Orders)
	}
}
