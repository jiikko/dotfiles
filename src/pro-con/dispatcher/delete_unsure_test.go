package dispatcher

// 削除で「PG の状態が分からないのに止まったと見なす」形 (451 の敵対的レビュー)。どれも消さずに待つか、上限で諦めてカードを残す。

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/live"
	"pro-con/store"
)

// 落ちた PG (一覧から消えた) を初めて見た Tick に削除を適用しても、自動の再開を待ってから消す (再開した PG をカードなしで残さない)。
func TestDeleteWaitsForAutoRestartOfCrashedPG(t *testing.T) {
	r := newCrashRig(t)
	now := t0
	r.d.Now = func() time.Time { return now }
	r.ss = nil // 落ちて自動の再開を待っている (一覧に出ない)
	deleteCard(t, r.dir, "C-001")
	r.tick(t)
	if c, ok := states(t, r.dir)["C-001"]; !ok || !c.Deleting() {
		t.Fatal("落ちたのを初めて見た Tick で、自動の再開を待たずに消した")
	}
	now = t0.Add(restartWait)
	r.tick(t)
	if _, ok := states(t, r.dir)["C-001"]; ok {
		t.Fatal("自動の再開を待った後も戻らない PG のカードを消さない")
	}
}

// 起動して作業中にしたが記録に載っていない session (落ちて pid 無し) が一覧に出ていれば、launchGrace を過ぎても止めてから消す。
func TestDeleteStopsUnregisteredSession(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil { // 起動 (id-pc-c-001)
		t.Fatal(err)
	}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", Kind: "background", State: "working", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
	deleteCard(t, dir, "C-001")
	d.Now = func() time.Time { return t0.Add(launchGrace + time.Second) }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := states(t, dir)["C-001"]; ok || !slices.Equal(l.stops, []string{"id-pc-c-001"}) {
		t.Fatalf("記録に無い session を止めずに消した / 消さない: stops=%v", l.stops)
	}
}

// dispatcher が落ちていて、削除を受けてから 1 分以上たって起動し直しても、最初の Tick で諦めない (この dispatcher が止めに入ってから数える)。
func TestDeleteDoesNotGiveUpOnFirstTickAfterRestart(t *testing.T) {
	r := newCrashRig(t)
	r.d.ListAll = func(ctx context.Context) ([]agents.Session, error) { return r.d.List(ctx) } // 止めても一覧がすぐには変わらない
	deleteCard(t, r.dir, "C-001")
	if _, err := store.Apply(r.dir, t0); err != nil { // 前の dispatcher が適用して落ちた
		t.Fatal(err)
	}
	now := t0.Add(5 * time.Minute)
	r.d.Now = func() time.Time { return now }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.Deleting() {
		t.Fatalf("起動し直した最初の Tick で諦めた: %q", lastHistory(c))
	}
	now = now.Add(closeStopWait)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.Deleting() || !strings.Contains(lastHistory(c), "削除できない") {
		t.Fatalf("止めに入ってから closeStopWait を過ぎても諦めない: %q", lastHistory(c))
	}
}

// 再開で入れ替わった前の session の記録が読めなければ、そちらが止まったかを確かめられないので消さない (上限で諦めてカードを残す)。
func TestDeleteKeepsCardWhenRetiredUnreadable(t *testing.T) {
	r := newCrashRig(t)
	if err := os.WriteFile(filepath.Join(r.dir, live.RetiredFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := t0
	r.d.Now = func() time.Time { return now }
	deleteCard(t, r.dir, "C-001")
	r.tick(t)
	if c, ok := states(t, r.dir)["C-001"]; !ok || !c.Deleting() {
		t.Fatal("前の session を確かめられないのに消した")
	}
	now = t0.Add(closeStopWait)
	r.tick(t)
	if c, ok := states(t, r.dir)["C-001"]; !ok || c.Deleting() || !strings.Contains(lastHistory(c), "前の session の記録を読めない") {
		t.Fatalf("諦めてカードを残し、理由を書く形になっていない: ok=%v %q", ok, lastHistory(c))
	}
}
