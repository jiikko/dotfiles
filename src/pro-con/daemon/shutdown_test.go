package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

// 終了で止めるとき: 作業中のカードは PG を止めて分解済みへ戻し、続きから再開する文を持たせる。質問待ちは列をそのまま残して止める。
func TestShutdownStopsOwnPGsAndMakesThemResumable(t *testing.T) {
	r := newCrashRig(t) // C-001 が作業中・登録済み
	planned(t, r.dir, 1)
	if _, err := store.Submit(r.dir, store.Request{Kind: "ask", CardID: "C-001", Question: "q"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t) // C-001 は質問待ち、C-002 を起動
	r.ss = append(r.ss, agents.Session{ID: "id-pc-c-002", SessionID: "S2", PID: 52, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-002", StartedAt: t0.Add(time.Second).UnixMilli()})
	r.tick(t) // C-002 を登録
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	cs := states(t, r.dir)
	if len(r.l.stops) != 2 {
		t.Fatalf("pro-con が起動した PG を全部止めていない: %v", r.l.stops)
	}
	if c := cs["C-002"]; c.State != card.Planned || c.Resume == "" || !c.Stopped {
		t.Fatalf("作業中のカードを続きから再開できる形にしていない: %v Resume=%q Stopped=%v", c.State, c.Resume, c.Stopped)
	}
	if c := cs["C-001"]; c.State != card.Waiting || !c.Stopped {
		t.Fatalf("質問待ちのカードの列を変えた / 止めた印が無い: %v %v", c.State, c.Stopped)
	}
}

// 終了で止めるのは、pro-con が起動した bg の session だけ (同じ短い id でも対話の session / 別の session id には触らない)。
// 同じ session id で pid だけ違うもの (Claude Code の自動の再開で戻った自分の PG) は止める。
func TestShutdownTouchesOnlyOwnSessions(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].PID = 99 // 自動の再開で戻った (再開の文はまだ無い)
	r.ss = append(r.ss, agents.Session{ID: "hum1", SessionID: "H", PID: 70, Kind: "interactive"})
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.l.stops) != 1 || r.l.stops[0] != "id-pc-c-001" {
		t.Fatalf("自分の PG だけを止めていない: %v", r.l.stops)
	}
	r2 := newCrashRig(t)
	r2.ss[0].SessionID = "OTHER" // 短い id を別の session が得た
	if _, err := r2.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r2.l.stops) != 0 {
		t.Fatalf("別の session id の session を止めた: %v", r2.l.stops)
	}
}

// 終了で止めた作業中のカードは、次の daemon が待たずに (止めずに) 続きから再開する。
func TestResumeAfterShutdownDoesNotWait(t *testing.T) {
	r := newCrashRig(t)
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.ss = nil // 止めたので一覧に出ない
	d2 := newDaemon(t, r.dir, r.l, nil)
	if _, err := d2.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], ":") || !strings.Contains(r.l.resumes[0], "続きから") {
		t.Fatalf("終了で止めたカードを次の起動ですぐ続きから再開しない: %v", r.l.resumes)
	}
	if c := states(t, r.dir)["C-001"]; c.Stopped {
		t.Fatal("再開の後も止めた印が残っている")
	}
}

// 止められなかった PG があればエラーで返す (画面は閉じた後に知らせる)。止められたカードの列は変えない。
func TestShutdownReportsStopFailure(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	if _, err := r.d.Shutdown(context.Background()); err == nil {
		t.Fatal("止められなかったのにエラーにしない")
	}
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Stopped {
		t.Fatalf("止められなかったカードを止めた扱いにした: %v %v", c.State, c.Stopped)
	}
}

// 動いている daemon には止める印を置いて、止まる (ロックが外れる) まで待ち、daemon が書いた結果を返す。動いていなければ false を返す。
func TestRequestStop(t *testing.T) {
	dir := t.TempDir()
	if running, err := RequestStop(context.Background(), dir, time.Second); running || err != nil {
		t.Fatalf("daemon が居ないのに居る扱い: %v %v", running, err)
	}
	for _, tc := range []struct {
		name   string
		result func() // daemon が抜ける前にすること
		ok     bool
	}{
		{"止め終えた", func() { WriteStopResult(dir, nil) }, true},
		{"止めきれなかった", func() { WriteStopResult(dir, errors.New("1 本の PG を止められなかった")) }, false},
		{"結果を書かずに抜けた (SIGTERM 等)", func() {}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unlock, err := Lock(dir) // 動いている daemon の形
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := RequestStop(context.Background(), dir, 10*time.Second); done <- err }()
			for i := 0; !StopRequested(dir); i++ { // daemon の側: 印を見つけたら止めて抜ける
				if i > 250 {
					t.Fatal("止める印が 5 秒たっても置かれない")
				}
				time.Sleep(20 * time.Millisecond)
			}
			// 否定の確認 (起きないことに待つ条件は無いので時間で見る): daemon がロックを持っている間は待ちを抜けない
			select {
			case err := <-done:
				t.Fatalf("daemon が止まる前に待ちを抜けた: %v", err)
			case <-time.After(300 * time.Millisecond):
			}
			tc.result()
			unlock()
			if err := <-done; (err == nil) != tc.ok {
				t.Fatalf("daemon の結果を読み違えた: %v", err)
			}
		})
	}
}

// 時間内に止まらなければ ErrStopTimeout。止める印は取り下げる (画面が閉じた後で daemon が止めに入らないように)。
func TestRequestStopTimeout(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := RequestStop(context.Background(), dir, 300*time.Millisecond); !errors.Is(err, ErrStopTimeout) {
		t.Fatalf("止まらない daemon を待ち切らない: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, StopRequestFile)); !os.IsNotExist(err) {
		t.Fatalf("止める印を取り下げていない: %v", err)
	}
}

// 起動した直後でまだ記録に無い PG も、一覧に出たら記録に載せてから止める (止め漏らして、daemon の居ないところで走らせない)。
func TestShutdownStopsJustLaunchedPG(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDaemon(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil { // 起動 (一覧にはまだ出ていない = 記録に無い)
		t.Fatal(err)
	}
	calls := 0
	d.List = func(context.Context) ([]agents.Session, error) {
		calls++
		if calls < 3 { // 止めに入ってからしばらく一覧に出ない
			return nil, nil
		}
		return []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}, nil
	}
	if _, err := d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; len(l.stops) != 1 || c.State != card.Planned || !c.Stopped {
		t.Fatalf("起動した直後の PG を止め漏らした: stops=%v %v %v", l.stops, c.State, c.Stopped)
	}
}

// 止める直前に PG が置いた質問は、止める前に適用する (作業中のまま分解済みへ戻して、次の起動で除けて失わない)。
func TestShutdownAppliesPendingRequests(t *testing.T) {
	r := newCrashRig(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "ask", CardID: "C-001", Question: "赤か青か"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, r.dir)["C-001"]; c.State != card.Waiting || c.Wait.Question != "赤か青か" {
		t.Fatalf("止める直前に置いた質問を失った: %v %q", c.State, c.Wait.Question)
	}
}

// 落ちて自動の再開を待っている PG (一覧に pid 無し) は、戻るのを待ってから止める。戻らなければ列を変えずに止めきれなかったと返す。
func TestShutdownWaitsForAutoRestart(t *testing.T) {
	for _, comesBack := range []bool{true, false} {
		r := newCrashRig(t)
		calls := 0
		r.d.List = func(context.Context) ([]agents.Session, error) {
			calls++
			s := r.ss[0]
			if calls < 3 || !comesBack {
				s.PID = 0
			}
			return []agents.Session{s}, nil
		}
		_, err := r.d.Shutdown(context.Background())
		c := states(t, r.dir)["C-001"]
		if comesBack && (err != nil || len(r.l.stops) != 1 || !c.Stopped) {
			t.Fatalf("戻った PG を止めない: err=%v stops=%v Stopped=%v", err, r.l.stops, c.Stopped)
		}
		if !comesBack && (err == nil || len(r.l.stops) != 0 || c.State != card.Running || c.Stopped) {
			t.Fatalf("戻らない PG を止めた扱いにした: err=%v stops=%v %v Stopped=%v", err, r.l.stops, c.State, c.Stopped)
		}
	}
}

// 再開の結果が分からないカード (印が残っている) は、立っていれば取り込んで止める。印を消して二重に再開させない。
func TestShutdownStopsUnconfirmedResume(t *testing.T) {
	r := newCrashRig(t)
	r.l.resumeFail = true
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	t1 := t0.Add(time.Minute)
	r.d.Now = func() time.Time { return t1 }
	r.tick(t) // 再開が失敗と返る (印が残る)
	r.ss = []agents.Session{{ID: "db1e", SessionID: "S2", PID: 60, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t1.Add(time.Second).UnixMilli()}}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, r.dir)["C-001"]; len(r.l.stops) != 1 || r.l.stops[0] != "db1e" || c.Launching != "" {
		t.Fatalf("結果の分からない再開で立った PG を止めない: stops=%v Launching=%q", r.l.stops, c.Launching)
	}
}

// 待ちの途中で、自動の再開により新しい pid で戻った PG も止める (再開の文がまだ無くても。「既に止まっていた」にしない)。
func TestShutdownStopsPGRestartedWithNewPid(t *testing.T) {
	r := newCrashRig(t)
	calls := 0
	r.d.List = func(context.Context) ([]agents.Session, error) {
		calls++
		s := r.ss[0]
		if calls < 3 {
			s.PID = 0
		} else {
			s.PID = 4242
		}
		return []agents.Session{s}, nil
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.l.stops) != 1 {
		t.Fatalf("新しい pid で戻った PG を止めない: %v", r.l.stops)
	}
}

// 待ちの判定は周ごとに今の時刻で行う (起動の結果が分からないカードは、launchGrace が過ぎたら待たずに先へ進む)。
func TestShutdownRechecksGraceEachPoll(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	setCard(t, dir, "C-001", func(c *card.Card) { c.Launching, c.LaunchedAt = "起動", t0 })
	d := newDaemon(t, dir, &fakeLauncher{}, nil)
	n := 0
	d.Now = func() time.Time { n++; return t0.Add(time.Duration(n) * 20 * time.Second) }
	if _, err := d.Shutdown(context.Background()); err != nil {
		t.Fatalf("launchGrace が過ぎても待ち続けて失敗にした: %v", err)
	}
}
