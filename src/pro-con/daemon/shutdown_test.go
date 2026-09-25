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

// 終了で止めるのは、記録と session id・pid が一致する bg の session だけ (外の session に触らない)。
func TestShutdownTouchesOnlyOwnSessions(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].PID = 99 // 外から操作された疑い (pid が記録と違う)
	r.ss = append(r.ss, agents.Session{ID: "hum1", SessionID: "H", PID: 70, Kind: "interactive"})
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.l.stops) != 0 {
		t.Fatalf("記録と一致しない session を止めた: %v", r.l.stops)
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

// 動いている daemon には止める印を置いて、止まる (ロックが外れる) まで待つ。動いていなければ false を返す。
func TestRequestStop(t *testing.T) {
	dir := t.TempDir()
	if running, err := RequestStop(context.Background(), dir, time.Second); running || err != nil {
		t.Fatalf("daemon が居ないのに居る扱い: %v %v", running, err)
	}
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
	unlock()
	if err := <-done; err != nil {
		t.Fatalf("daemon が止まったのに待ちが終わらない: %v", err)
	}
}

// 時間内に止まらなければ ErrStopTimeout。印は残す (daemon が後で見つけて止める)。
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
	if _, err := os.Stat(filepath.Join(dir, StopRequestFile)); err != nil {
		t.Fatalf("止める印が残っていない: %v", err)
	}
}
