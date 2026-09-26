package dispatcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// 入れ替え (505) の区切り: このプロセスのメモリにしか無いもの (裏の子・結果の分からない起動) が残っている間は区切りでない。
func TestBusyUntilSafePoint(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	if why, err := d.Busy(); err != nil || why != "" {
		t.Fatalf("何も走っていないのに区切りでない: %q %v", why, err)
	}
	cases := []struct {
		name string
		set  func()
		undo func()
		want string
	}{
		{"テストの係", func() { d.active = &runJob{cardID: "C-001"} }, func() { d.active = nil }, "テストの係が C-001 を実行中"},
		{"btw", func() { d.btw = &btwJob{cardID: "C-001"} }, func() { d.btw = nil }, "btw の答えを C-001 に作っている"},
		{"進捗", func() { d.progressBusy.Store(true) }, func() { d.progressBusy.Store(false) }, "進捗を集めている"},
		{"repo の lock 待ち", func() { d.blocked = &runBlock{cardID: "C-001"} }, func() { d.blocked = nil }, "C-001 のテストの係が repo の lock を待っている"},
		{"止めている", func() { d.stopFrom = map[string]time.Time{"C-001": t0} }, func() { d.stopFrom = nil }, "PG を止めている"},
		{"PG の起動", func() { setCard(t, dir, "C-001", func(c *card.Card) { c.Launching = "起動" }) },
			func() { setCard(t, dir, "C-001", func(c *card.Card) { c.Launching = "" }) }, "C-001 の PG の起動を確かめている"},
		{"PM の再開", func() {
			if err := store.SavePM(dir, store.PMState{Launching: "再開"}); err != nil {
				t.Fatal(err)
			}
		}, func() { _ = os.Remove(filepath.Join(dir, store.PMStateFile)) }, "PMの再開を確かめている"},
	}
	for _, c := range cases {
		c.set()
		why, err := d.Busy()
		if err != nil || !strings.Contains(why, c.want) {
			t.Errorf("%s: 区切りと判じた / 理由が違う: %q %v", c.name, why, err)
		}
		c.undo()
		if why, err := d.Busy(); err != nil || why != "" {
			t.Errorf("%s を外した後も区切りでない: %q %v", c.name, why, err)
		}
	}
}

// 記録を読めなければ区切りか分からない (エラー)。新版の確かめ (Preflight) も読めない記録で落ちる。
func TestBusyAndPreflightFailOnBrokenRecords(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	if err := Preflight(dir); err != nil {
		t.Fatalf("読める記録で確かめが落ちた: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, store.StateFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	if _, err := d.Busy(); err == nil {
		t.Fatal("読めない記録で区切りと判じた")
	}
	if err := Preflight(dir); err == nil {
		t.Fatal("読めない記録で確かめが通った")
	}
}

// 人の番を知らせ済みの鍵は入れ替えで渡す (切り替えのたびに人の番を全件知らせ直さない)。
func TestNotifiedSurvivesUpgrade(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	var old recorder
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	old.rig(d)
	for range 2 { // 起動してから質問待ちにする (TestAnnouncePublishesOnChangeAndNotifiesOnce と同じ形)
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Submit(dir, store.Request{Kind: "ask", CardID: "C-001", Question: "赤か青か"}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(old.notified) != 1 {
		t.Fatalf("人の番を知らせない: %v", old.notified)
	}
	var next recorder
	d2 := newDispatcher(t, dir, &fakeLauncher{}, nil)
	next.rig(d2)
	d2.SeedNotified(d.Notified())
	if _, err := d2.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(next.notified) != 0 {
		t.Fatalf("入れ替えの後に同じ人の番を知らせ直した: %v", next.notified)
	}
}
