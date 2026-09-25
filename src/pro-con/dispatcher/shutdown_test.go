package dispatcher

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
	"pro-con/live"
	"pro-con/store"
	"pro-con/wake"
	"slices"
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

// 終了で止めた作業中のカードは、次の dispatcher が待たずに (止めずに) 続きから再開する。
func TestResumeAfterShutdownDoesNotWait(t *testing.T) {
	r := newCrashRig(t)
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.ss = nil // 止めたので一覧に出ない
	d2 := newDispatcher(t, r.dir, r.l, nil)
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

// 動いている dispatcher には止める印を置いて、止まる (ロックが外れる) まで待ち、dispatcher が書いた結果を返す。動いていなければ false を返す。
func TestRequestStop(t *testing.T) {
	dir := t.TempDir()
	if running, err := RequestStop(context.Background(), dir, time.Second); running || err != nil {
		t.Fatalf("dispatcher が居ないのに居る扱い: %v %v", running, err)
	}
	for _, tc := range []struct {
		name   string
		result func() // dispatcher が抜ける前にすること
		ok     bool
	}{
		{"止め終えた", func() { WriteStopResult(dir, nil) }, true},
		{"止めきれなかった", func() { WriteStopResult(dir, errors.New("1 本の PG を止められなかった")) }, false},
		{"結果を書かずに抜けた (SIGTERM 等)", func() {}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unlock, err := Lock(dir) // 動いている dispatcher の形
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := RequestStop(context.Background(), dir, 10*time.Second); done <- err }()
			for i := 0; !StopRequested(dir); i++ { // dispatcher の側: 印を見つけたら止めて抜ける
				if i > 250 {
					t.Fatal("止める印が 5 秒たっても置かれない")
				}
				time.Sleep(20 * time.Millisecond)
			}
			// 否定の確認 (起きないことに待つ条件は無いので時間で見る): dispatcher がロックを持っている間は待ちを抜けない
			select {
			case err := <-done:
				t.Fatalf("dispatcher が止まる前に待ちを抜けた: %v", err)
			case <-time.After(300 * time.Millisecond):
			}
			tc.result()
			unlock()
			if err := <-done; (err == nil) != tc.ok {
				t.Fatalf("dispatcher の結果を読み違えた: %v", err)
			}
		})
	}
}

// 時間内に止まらなければ ErrStopTimeout。止める印は取り下げる (画面が閉じた後で dispatcher が止めに入らないように)。
func TestRequestStopTimeout(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := RequestStop(context.Background(), dir, 300*time.Millisecond); !errors.Is(err, ErrStopTimeout) {
		t.Fatalf("止まらない dispatcher を待ち切らない: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, StopRequestFile)); !os.IsNotExist(err) {
		t.Fatalf("止める印を取り下げていない: %v", err)
	}
}

// 起動した直後でまだ記録に無い PG も、一覧に出たら記録に載せてから止める (止め漏らして、dispatcher の居ないところで走らせない)。
func TestShutdownStopsJustLaunchedPG(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
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

// 落ちて Claude Code の自動の再開を待っている PG (一覧で pid 0) は、再開を待たずにそのまま止める (claude stop が再開を抑える。
// 2.1.282 で実測: kill -9 の直後に stop → stopped になり、35 秒後も再開しない)。再開して戻ってきた後に止める形も同じ。
func TestShutdownStopsPGWaitingForAutoRestart(t *testing.T) {
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
		if err != nil || !slices.Contains(r.l.stops, "id-pc-c-001") || !c.Stopped || c.State != card.Planned {
			t.Fatalf("戻ってくる=%v: 落ちて再開を待っている PG を止めない / 再開できる形にしない: err=%v stops=%v %v Stopped=%v",
				comesBack, err, r.l.stops, c.State, c.Stopped)
		}
		if calls > 2 {
			t.Fatalf("戻ってくる=%v: 自動の再開を待った (一覧を %d 回取った)", comesBack, calls)
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
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	n := 0
	d.Now = func() time.Time { n++; return t0.Add(time.Duration(n) * 20 * time.Second) }
	if _, err := d.Shutdown(context.Background()); err != nil {
		t.Fatalf("launchGrace が過ぎても待ち続けて失敗にした: %v", err)
	}
}

// 止める頼みは dispatcher を起こす (3 秒の待ちを切り上げさせる)。
func TestRequestStopPokes(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pcrs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	unlock, err := Lock(dir) // dispatcher が動いている形
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	srv, err := wake.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = RequestStop(ctx, dir, time.Minute); close(done) }()
	select {
	case <-srv.Wakes():
	case <-time.After(10 * time.Second):
		t.Fatal("止める頼みで dispatcher を起こさない")
	}
	cancel()
	<-done
}

// カードが完了した後も生きている PG (pro-con の記録にある session) も、終了のときに止める (カードから辿るだけでは漏れる)。
func TestShutdownStopsOwnedSessionOfDoneCard(t *testing.T) {
	r := newCrashRig(t) // C-001 の PG (id-pc-c-001) が作業中で記録にある
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.State, c.Session = card.Done, "" })
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "id-pc-c-001") {
		t.Fatalf("完了したカードの生きている PG を止めない: stops=%v", r.l.stops)
	}
}

// 止めたと返っても止まっていなければ止め直し、それでも止まらなければ、残った PG をカードと短い id で名指しして失敗を返す。
func TestShutdownReportsSessionsThatDoNotStop(t *testing.T) {
	r := newCrashRig(t)
	r.d.ListAll = func(ctx context.Context) ([]agents.Session, error) { return r.d.List(ctx) } // 止めても一覧が変わらない (止まらない PG)
	_, err := r.d.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "C-001 (id-pc-c-001)") {
		t.Fatalf("止まらない PG を名指ししない: %v", err)
	}
	if n := len(r.l.stops); n < 1+ensurePolls { // カードの側で 1 回 + 確かめの周ごとに 1 回
		t.Fatalf("止まっていない PG を期限まで止め直さない: %d 回", n)
	}
}

// 止まったかを確かめるのは pro-con の記録にある session だけ (外の session は、生きていても止めない・残りに数えない)。
func TestEnsureStoppedIgnoresForeignSessions(t *testing.T) {
	r := newCrashRig(t)
	r.ss = append(r.ss, agents.Session{ID: "other01", SessionID: "X", PID: 77, Kind: "background", State: "working"})
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatalf("外の session を残りに数えた: %v", err)
	}
	if slices.Contains(r.l.stops, "other01") {
		t.Fatalf("外の session を止めた: %v", r.l.stops)
	}
}

// 止める頼みは、止め終えた結果を読んだら戻る。結果を書いて lock を外した直後に、開いている画面が次の dispatcher を起こして
// lock を取っても、時間切れと読まない。
func TestRequestStopReadsResultWhileLockTakenAgain(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir) // 次の dispatcher がもう lock を持っている形
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	done := make(chan error, 1)
	go func() { _, err := RequestStop(context.Background(), dir, 30*time.Second); done <- err }()
	for range 400 { // 頼みが置かれたら、止め終えた結果を書く (lock は持ったまま)
		if _, err := os.Stat(filepath.Join(dir, StopRequestFile)); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	WriteStopResult(dir, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("止め終えたのに誤りを返した: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("結果を書いても戻らない (lock が外れるのを待っている)")
	}
}

// 一覧を 1 回取れなくても、止めるのをやめない (取り直して止める。1 回の失敗で 1 本も止めずに抜けない)。
func TestShutdownStopsEvenIfListFailsOnce(t *testing.T) {
	r := newCrashRig(t)
	calls := 0
	r.d.List = func(context.Context) ([]agents.Session, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("timeout")
		}
		return r.ss, nil
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatalf("一覧を 1 回取れなかっただけで止めるのをやめた: %v", err)
	}
	if !slices.Contains(r.l.stops, "id-pc-c-001") {
		t.Fatalf("一覧を 1 回取れなかった後に PG を止めない: %v", r.l.stops)
	}
	// カードの側も取り直して続けた (記録からの確かめだけで止めたのではない): 作業中のカードは続きから再開できる形になっている
	if c := states(t, r.dir)["C-001"]; c.State != card.Planned || !c.Stopped || c.Resume == "" {
		t.Fatalf("一覧を 1 回取れなかった後にカードを再開できる形にしない: %v Stopped=%v Resume=%q", c.State, c.Stopped, c.Resume)
	}
}

// 再開で入れ替わった前の session (記録から外した行) が生きていれば、それも止める。
func TestShutdownStopsRetiredSession(t *testing.T) {
	r := newCrashRig(t)
	reg := filepath.Join(r.dir, live.RegistryFile)
	if err := live.ReplaceCard(reg, live.Owned{SessionID: "S2", ID: "id-new", PID: 43, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	r.ss = append(r.ss, agents.Session{ID: "id-new", SessionID: "S2", PID: 43, Kind: "background"})
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.Session = "id-new" }) // 再開の後の本物の形: カードは新しい session を指す
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "id-pc-c-001") {
		t.Fatalf("入れ替わった前の session を止めない: %v", r.l.stops)
	}
}

// 止め直しの周ごとにカードの履歴を足さない (前の Shutdown で止めたと書いたカードは書き直さない)。
func TestRepeatedShutdownDoesNotGrowHistory(t *testing.T) {
	r := newCrashRig(t)
	for _, alive := range []bool{false, true} { // 止まった (一覧から消えた) / 止めても一覧に生きて残る
		repeatShutdown(t, r, alive)
	}
}

func repeatShutdown(t *testing.T, r *crashRig, alive bool) {
	t.Helper()
	r.d.List = func(context.Context) ([]agents.Session, error) { // 本物と同じく、止めた session は --all なしの一覧に出ない
		var out []agents.Session
		for _, s := range r.ss {
			if alive || !slices.Contains(r.l.stops, s.ID) {
				out = append(out, s)
			}
		}
		return out, nil
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := len(states(t, r.dir)["C-001"].History)
	for range 3 {
		if _, err := r.d.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if m := len(states(t, r.dir)["C-001"].History); m != n {
		t.Fatalf("止め直すたびに履歴が増えた (一覧に生きて残る=%v): %d → %d", alive, n, m)
	}
}

// 入れ替わった前の session の記録が壊れていても、記録にある session は止める。
func TestShutdownStopsDespiteBrokenRetired(t *testing.T) {
	r := newCrashRig(t)
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.State, c.Session = card.Done, "" }) // カードからは辿れない
	if err := os.WriteFile(filepath.Join(r.dir, live.RetiredFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	notes, err := r.d.Shutdown(context.Background())
	if err != nil || !slices.Contains(r.l.stops, "id-pc-c-001") {
		t.Fatalf("退いた記録が壊れていると記録の session を止めない: %v stops=%v", err, r.l.stops)
	}
	if !strings.Contains(joinNotes(notes), "読めない") {
		t.Fatalf("退いた記録を読めないことを知らせない: %v", notes)
	}
}

// カードから辿る段が上限を使い切っても (一覧が固まった)、止まったかを確かめる段は別の上限で最後まで回り、記録の PG を止める。
func TestEnsureStoppedRunsAfterCardStageTimesOut(t *testing.T) {
	old := shutdownBudget
	shutdownBudget = 50 * time.Millisecond
	t.Cleanup(func() { shutdownBudget = old })
	r := newCrashRig(t)
	r.d.List = func(ctx context.Context) ([]agents.Session, error) { <-ctx.Done(); return nil, ctx.Err() } // 固まった一覧
	r.d.ListAll = func(ctx context.Context) ([]agents.Session, error) {
		if err := ctx.Err(); err != nil { // 本物と同じく、上限を過ぎた ctx では呼び出しが失敗する
			return nil, err
		}
		s := r.ss[0]
		if slices.Contains(r.l.stops, s.ID) {
			s.State, s.PID = agents.StateStopped, 0 // 本物と同じく、止めると pid が無くなる
		}
		return []agents.Session{s}, nil
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil || !slices.Contains(r.l.stops, "id-pc-c-001") {
		t.Fatalf("前の段が上限を使い切ると、記録の PG を止めない: %v stops=%v", err, r.l.stops)
	}
}

// 止めたと書いたカードでも、その session がまだ生きていれば止め直す (記録に無い新しい session はこの経路でしか止まらない)。
// 起動の途中の印は外す (履歴は足さない)。
func TestRecordedCardStillStopsLiveSessionAndClearsMark(t *testing.T) {
	r := newCrashRig(t)
	// 前の終了で止めたカード (Stopped) を、開き直した画面の dispatcher が再開している最中の形: カードは前の session を指したまま、
	// 新しい session が同じ worktree で立っている (記録にはまだ載っていない)
	setCard(t, r.dir, "C-001", func(c *card.Card) { // 再開の印は前の session が始まった後 (t0+1s) に書いた
		c.State, c.Stopped, c.Launching, c.LaunchedAt = card.Planned, true, "再開", t0.Add(1500*time.Millisecond)
	})
	r.ss[0].PID = 0 // 前の session は止まっている (一覧で pid 無し)
	r.ss = append(r.ss, agents.Session{ID: "id-fresh", SessionID: "SF", PID: 77, Kind: "background",
		Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(2 * time.Second).UnixMilli()})
	n := len(states(t, r.dir)["C-001"].History)
	notes, err := r.d.Shutdown(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := states(t, r.dir)["C-001"]
	if !slices.Contains(r.l.stops, "id-fresh") {
		t.Fatalf("止めたと書いたカードの生きている session を止めない: %v\n%s", r.l.stops, joinNotes(notes))
	}
	if c.Launching != "" {
		t.Fatalf("起動の途中の印が残った: %q", c.Launching)
	}
	if len(c.History) > n+1 { // register の知らせ等で 1 つ増えることはあるが、止めた履歴を重ねない
		t.Fatalf("止めたと書いたカードの履歴を重ねた: %d → %d", n, len(c.History))
	}
}

// 起動・再開の途中で取り込んだ session (記録にまだ無い) の停止が失敗したら、確かめる段でも止まっていないと数え、止め直して名指しする
// (記録だけを確かめると、止めていないのに ok になる)。
func TestShutdownVerifiesAdoptedSession(t *testing.T) {
	r := newCrashRig(t)
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.Launching, c.LaunchedAt = "再開", t0.Add(1500*time.Millisecond) })
	r.ss[0].PID = 0
	r.ss = append(r.ss, agents.Session{ID: "id-fresh", SessionID: "SF", PID: 77, Kind: "background",
		Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(2 * time.Second).UnixMilli()})
	r.l.stopFail = true // 止めると失敗が返る (claude stop の時間切れ等)
	_, err := r.d.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "C-001 (id-fresh)") {
		t.Fatalf("取り込んだ session を止められなかったのに ok にした / 名指ししない: %v", err)
	}
	tries := 0
	for _, id := range r.l.stopTries {
		if id == "id-fresh" {
			tries++
		}
	}
	if tries < 2 { // カードの側で 1 回 + 確かめる段で止め直す
		t.Fatalf("取り込んだ session を確かめる段で止め直さない: %d 回", tries)
	}
}
