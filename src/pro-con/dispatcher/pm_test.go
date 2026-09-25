package dispatcher

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// pmRig は PM を起こす dispatcher (偽の launcher と一覧。本物の claude と state dir には触らない)。
type pmRig struct {
	dir string
	l   *fakeLauncher
	ss  []agents.Session
	d   *Dispatcher
	now time.Time
}

const pmName = "pc-pm-20260925-010000" // t0 に起動した PM の名前

func newPMRig(t *testing.T) *pmRig {
	t.Helper()
	r := &pmRig{dir: t.TempDir(), l: &fakeLauncher{}, now: t0}
	r.d = newDispatcher(t, r.dir, r.l, nil)
	r.d.List = func(context.Context) ([]agents.Session, error) { return r.ss, nil }
	r.d.Now = func() time.Time { return r.now }
	r.d.PMRepo, r.d.PMGuide = "/w/dotfiles", "GUIDE-BODY"
	r.d.Exists = func(string) bool { return true }
	return r
}

func (r *pmRig) tick(t *testing.T) []eventlog.Event {
	t.Helper()
	notes, err := r.d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

// request は依頼の列にカードを 1 枚足す (箱に置くだけ。適用は次の Tick)。
func request(t *testing.T, dir, title string) {
	t.Helper()
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: title, Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
}

func pmSession(id, sid string, pid int, status string, started time.Time) agents.Session {
	return agents.Session{ID: id, SessionID: sid, PID: pid, Kind: "background", Status: status, State: "working", Name: pmName,
		Cwd: "/w/dotfiles/.claude/worktrees/" + pmName, StartedAt: started.UnixMilli()}
}

func loadPM(t *testing.T, dir string) store.PMState {
	t.Helper()
	pm, err := store.LoadPM(dir)
	if err != nil {
		t.Fatal(err)
	}
	return pm
}

func pmRegRow(t *testing.T, dir string) (live.Owned, bool) {
	t.Helper()
	reg, err := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if err != nil {
		t.Fatal(err)
	}
	return roleRow(reg, PMCardID)
}

// startedPM は C-001 を依頼して PM を起動し、一覧に出た PM を記録に載せたところまで進める。
func startedPM(t *testing.T) *pmRig {
	t.Helper()
	r := newPMRig(t)
	request(t, r.dir, "一つ目")
	r.tick(t)
	r.ss = []agents.Session{pmSession("id-"+pmName, "P1", 60, "idle", t0.Add(time.Second))}
	r.tick(t)
	if _, ok := pmRegRow(t, r.dir); !ok {
		t.Fatal("起動した PM を記録に載せていない")
	}
	return r
}

// 依頼の列にカードが入ったら、PM の repo で PM を 1 本起動し、指示書と新しいカードを渡す。起動した PM は PM の行で記録に載る。
func TestPMStartsOnRequest(t *testing.T) {
	r := newPMRig(t)
	request(t, r.dir, "一つ目")
	r.tick(t)
	if !slices.Equal(r.l.starts, []string{pmName}) || !slices.Equal(r.l.repos, []string{"/w/dotfiles"}) {
		t.Fatalf("PM を PM の repo で 1 本起動していない: starts=%v repos=%v", r.l.starts, r.l.repos)
	}
	if p := r.l.prompts[0]; !strings.Contains(p, "GUIDE-BODY") || !strings.Contains(p, "新しい依頼 C-001「一つ目」") {
		t.Fatalf("起動の指示に指示書と新しいカードが無い: %q", p)
	}
	if pm := loadPM(t, r.dir); pm.Session != "id-"+pmName || !slices.Equal(pm.Told, []string{"C-001"}) || pm.Launching != "" {
		t.Fatalf("起動した PM の様子が違う: %+v", pm)
	}
	if c := states(t, r.dir)["C-001"]; c.State != card.Requested || c.Session != "" {
		t.Fatalf("依頼のカードに PG を割り当てた: %v %q", c.State, c.Session)
	}
	r.ss = []agents.Session{pmSession("id-"+pmName, "P1", 60, "idle", t0.Add(time.Second))}
	r.tick(t)
	row, ok := pmRegRow(t, r.dir)
	if !ok || row.SessionID != "P1" || row.PID != 60 || row.Cwd == "" {
		t.Fatalf("PM を記録に載せていない: %+v %v", row, ok)
	}
	r.tick(t)
	if r.l.startTries != 1 || len(r.l.resumes) != 0 {
		t.Fatalf("知らせ済みのカードで PM を起こし直した: starts=%d resumes=%v", r.l.startTries, r.l.resumes)
	}
}

// PM が居れば、新しいカードは同じ session を (止めてから) 再開して知らせる。まだ依頼の列に残っているカードも添える。
func TestPMResumedForNewCard(t *testing.T) {
	r := startedPM(t)
	request(t, r.dir, "二つ目")
	r.tick(t)
	if len(r.l.resumes) != 1 || r.l.startTries != 1 {
		t.Fatalf("居る PM を 1 回再開していない: resumes=%v starts=%d", r.l.resumes, r.l.startTries)
	}
	got := r.l.resumes[0]
	if !strings.HasPrefix(got, "id-"+pmName+":") || !strings.Contains(got, "新しい依頼 C-002") || !strings.Contains(got, "まだ依頼の列に残っている: C-001") {
		t.Fatalf("再開の知らせが違う (止める id / 新しいカード / 残っているカード): %q", got)
	}
	if strings.Contains(got, "GUIDE-BODY") {
		t.Fatalf("再開の知らせに指示書を書き直した (指示は起動のときだけ): %q", got)
	}
	if r.l.cwds[0] != "/w/dotfiles/.claude/worktrees/"+pmName {
		t.Fatalf("PM の worktree で再開していない: %q", r.l.cwds[0])
	}
	if pm := loadPM(t, r.dir); !slices.Equal(pm.Told, []string{"C-001", "C-002"}) {
		t.Fatalf("知らせ済みのカードが違う: %v", pm.Told)
	}
}

// 列を離れたカード (分けてキューに積んだ) は知らせ済みから外れ、また知らせない。
func TestPMToldPrunedWhenCardLeavesRequested(t *testing.T) {
	r := startedPM(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1}}}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if pm := loadPM(t, r.dir); len(pm.Told) != 0 || len(r.l.resumes) != 0 {
		t.Fatalf("列を離れたカードを知らせ済みに残した / PM を起こした: %v %v", pm.Told, r.l.resumes)
	}
}

// PM が作業中 (busy) なら再開しない (止めると作業中の turn を殺す)。turn が終わって idle になったら知らせる。
func TestPMNotInterruptedWhileBusy(t *testing.T) {
	for _, status := range []string{"busy", "waiting"} {
		t.Run(status, func(t *testing.T) {
			r := startedPM(t)
			r.ss[0].Status = status
			request(t, r.dir, "二つ目")
			r.tick(t)
			if len(r.l.resumes) != 0 {
				t.Fatalf("%s の PM を止めて再開した: %v", status, r.l.resumes)
			}
			r.ss[0].Status = "idle"
			r.tick(t)
			if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "C-002") {
				t.Fatalf("idle になった PM に知らせていない: %v", r.l.resumes)
			}
		})
	}
}

// 起動が「失敗」と返っても立っているかもしれない: launchGrace の間は起動し直さない。一覧に出たら取り込み、出なければ起動し直す。
func TestPMNotStartedTwiceWhileUnconfirmed(t *testing.T) {
	t.Run("一覧に出たら取り込む", func(t *testing.T) {
		r := newPMRig(t)
		request(t, r.dir, "一つ目")
		r.l.fail = true
		r.tick(t)
		if pm := loadPM(t, r.dir); pm.Launching != "起動" || !slices.Equal(pm.Telling, []string{"C-001"}) {
			t.Fatalf("起動の前に印を書いていない: %+v", pm)
		}
		r.l.fail = false
		r.now = t0.Add(30 * time.Second)
		r.tick(t)
		if r.l.startTries != 1 {
			t.Fatalf("結果を確かめる前に PM をもう 1 本起動した: %d", r.l.startTries)
		}
		r.ss = []agents.Session{pmSession("pm-real", "P1", 60, "idle", t0.Add(time.Second))}
		r.tick(t)
		if pm := loadPM(t, r.dir); r.l.startTries != 1 || pm.Session != "pm-real" || pm.Launching != "" || !slices.Equal(pm.Told, []string{"C-001"}) {
			t.Fatalf("立っていた PM を取り込んでいない: starts=%d %+v", r.l.startTries, pm)
		}
	})
	t.Run("待っても出なければ起動し直す", func(t *testing.T) {
		r := newPMRig(t)
		request(t, r.dir, "一つ目")
		r.l.fail = true
		r.tick(t)
		r.l.fail = false
		r.now = t0.Add(launchGrace + time.Second)
		r.tick(t)
		if r.l.startTries != 2 || !strings.Contains(r.l.prompts[0], "C-001") {
			t.Fatalf("待っても出なかった PM を起動し直していない / 知らせを落とした: %d %v", r.l.startTries, r.l.prompts)
		}
	})
}

// dispatcher が起動の途中で落ちた (印だけ残った) 後の dispatcher は、立っている PM を取り込み、2 本目を起動しない。
func TestPMAdoptedAfterDispatcherCrash(t *testing.T) {
	r := newPMRig(t)
	request(t, r.dir, "一つ目")
	if _, err := store.Apply(r.dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePM(r.dir, store.PMState{Name: pmName, Launching: "起動", LaunchedAt: t0, Telling: []string{"C-001"}}); err != nil {
		t.Fatal(err)
	}
	r.ss = []agents.Session{
		pmSession("old", "P0", 59, "idle", t0.Add(-time.Hour)), // 印より前に始まった同じ名前の session は取り込まない
		pmSession("pm-real", "P1", 60, "idle", t0.Add(time.Second)),
	}
	r.now = t0.Add(10 * time.Second)
	r.tick(t)
	if pm := loadPM(t, r.dir); r.l.startTries != 0 || pm.Session != "pm-real" || !slices.Equal(pm.Told, []string{"C-001"}) {
		t.Fatalf("落ちる前に起動した PM を取り込んでいない: starts=%d %+v", r.l.startTries, pm)
	}
}

// 知らせた後に PM が止まった / 消えたら、依頼の列に残るカードを知らせ直す。ただし消えた直後は自動の再開を待つ
// (待たずに再開すると 2 本立つ)。pid 無しで一覧に出ている間 (自動の再開の途中) も待つ。
func TestPMRenotifiedAfterDeath(t *testing.T) {
	t.Run("自動の再開の途中は待つ", func(t *testing.T) {
		r := startedPM(t)
		r.ss[0].PID = 0 // 落ちて自動の再開を待っている
		r.now = t0.Add(time.Hour)
		r.tick(t)
		r.now = r.now.Add(restartWait - time.Second)
		r.tick(t)
		if len(r.l.resumes) != 0 {
			t.Fatalf("自動の再開の途中の PM を待たずに再開した: %v", r.l.resumes)
		}
		r.now = r.now.Add(2 * time.Second) // 待っても戻らない (PG と同じく restartWait で見切る)
		r.tick(t)
		if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], ":") {
			t.Fatalf("戻らない PM を止めずに再開していない: %v", r.l.resumes)
		}
	})
	t.Run("消えたら待ってから知らせ直す", func(t *testing.T) {
		r := startedPM(t)
		r.ss = nil // 一覧から消えた
		r.now = t0.Add(time.Hour)
		r.tick(t)
		r.now = r.now.Add(30 * time.Second)
		r.tick(t)
		if len(r.l.resumes) != 0 {
			t.Fatalf("消えた直後の PM を待たずに再開した: %v", r.l.resumes)
		}
		r.now = r.now.Add(restartWait)
		r.tick(t)
		if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], ":") || !strings.Contains(r.l.resumes[0], "C-001") {
			t.Fatalf("消えた PM を止めずに再開して、残っているカードを知らせ直していない: %v", r.l.resumes)
		}
	})
}

// 利用枠が「新しく起動・再開しない」(95%) なら PM も起こさない。80% (PG は 1 本まで) では起こす。
func TestPMHeldOnlyAtStopThreshold(t *testing.T) {
	for _, tc := range []struct {
		pct   int
		start bool
	}{{96, false}, {85, true}} {
		r := newPMRig(t)
		r.d.Usage = func(context.Context) (Usage, error) { return Usage{Session: tc.pct}, nil }
		request(t, r.dir, "一つ目")
		r.tick(t)
		if got := len(r.l.starts) == 1; got != tc.start {
			t.Fatalf("枠 %d%% で PM を起動したか = %v (想定 %v)", tc.pct, got, tc.start)
		}
	}
}

// PM は --limit (同時に動かす PG の数) に数えない: PM が居ても limit 1 で PG を 1 本起動する。
func TestPMNotCountedInLimit(t *testing.T) {
	r := startedPM(t)
	r.d.Limit = 1
	planned(t, r.dir, 1)
	r.tick(t)
	if !slices.ContainsFunc(r.l.starts, func(n string) bool { return strings.HasPrefix(n, "pc-c-") }) {
		t.Fatalf("PM が PG の枠を占めた: starts=%v", r.l.starts)
	}
}

// e2e モード (PMRepo が空) では PM を起こさない。
func TestPMDisabledWithoutRepo(t *testing.T) {
	r := newPMRig(t)
	r.d.PMRepo = ""
	request(t, r.dir, "一つ目")
	r.tick(t)
	if r.l.startTries != 0 {
		t.Fatalf("PM の repo が無いのに起動した: %v", r.l.starts)
	}
}

// 終了で PM も止め、次の dispatcher は止めた PM を待たずに再開して、依頼の列に残るカードを知らせ直す (取りこぼさない)。
func TestShutdownStopsPMAndResumesItNext(t *testing.T) {
	r := startedPM(t)
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.l.stops, []string{"id-" + pmName}) {
		t.Fatalf("終了で PM を止めていない: %v", r.l.stops)
	}
	if pm := loadPM(t, r.dir); !pm.Stopped {
		t.Fatalf("止めた印が無い: %+v", pm)
	}
	r.ss = nil // 止めた PM は一覧から消える
	r.now = t0.Add(time.Minute)
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], ":") || !strings.Contains(r.l.resumes[0], "C-001") {
		t.Fatalf("終了で止めた PM を再開して残っているカードを知らせていない: %v", r.l.resumes)
	}
}

// 起動の直後で記録にまだ載っていない PM (起動を確かめる前の印だけ / 起動が返ったが pid がまだ無かった) も終了で止める。
func TestShutdownStopsPMNotYetRegistered(t *testing.T) {
	t.Run("印だけ", func(t *testing.T) {
		r := newPMRig(t)
		request(t, r.dir, "一つ目")
		r.l.fail = true
		r.tick(t)
		r.ss = []agents.Session{pmSession("pm-real", "P1", 60, "idle", t0.Add(time.Second))}
		if _, err := r.d.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(r.l.stops, []string{"pm-real"}) {
			t.Fatalf("起動を確かめる前の PM を止めていない: %v", r.l.stops)
		}
	})
	t.Run("記録の前", func(t *testing.T) {
		r := newPMRig(t)
		request(t, r.dir, "一つ目")
		r.tick(t)
		r.ss = []agents.Session{pmSession("id-"+pmName, "P1", 60, "idle", t0.Add(time.Second))}
		if _, err := r.d.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(r.l.stops, "id-"+pmName) {
			t.Fatalf("記録に載る前の PM を止めていない: %v", r.l.stops)
		}
	})
}

// 再開で入れ替わった前の PM (sessions-retired.json) も終了の確かめで止める。
func TestShutdownStopsRetiredPM(t *testing.T) {
	r := startedPM(t)
	r.l.resumeID = "pm2"
	request(t, r.dir, "二つ目")
	r.tick(t)
	r.ss = append(r.ss, agents.Session{ID: "pm2", SessionID: "P2", PID: 61, Kind: "background", State: "working", Status: "idle",
		Cwd: "/w/dotfiles/.claude/worktrees/" + pmName, StartedAt: t0.Add(2 * time.Second).UnixMilli()})
	r.tick(t)
	if row, _ := pmRegRow(t, r.dir); row.SessionID != "P2" {
		t.Fatalf("再開した PM で記録を置き換えていない: %+v", row)
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "pm2") || !slices.Contains(r.l.stops, "id-"+pmName) {
		t.Fatalf("今の PM と入れ替わった前の PM を両方止めていない: %v", r.l.stops)
	}
}

// 🚨 閉じたカードの PG を止めるとき (447)、PM は止めない。終了では止める。
func TestCloseDoesNotStopPM(t *testing.T) {
	r := newCrashRig(t) // C-001 が作業中・登録済み
	r.d.PMRepo = "/w/dotfiles"
	reg := filepath.Join(r.dir, live.RegistryFile)
	if err := live.Register(reg, live.Owned{SessionID: "P1", ID: "pm1", PID: 60, CardID: PMCardID, Cwd: "/w/dotfiles/.claude/worktrees/" + pmName, StartedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePM(r.dir, store.PMState{Session: "pm1", Name: pmName, LaunchedAt: t0}); err != nil {
		t.Fatal(err)
	}
	r.ss = append(r.ss, agents.Session{ID: "pm1", SessionID: "P1", PID: 60, Kind: "background", State: "working", Status: "idle", StartedAt: t0.UnixMilli()})
	closeCard(t, r.dir, "C-001")
	r.tick(t)
	r.tick(t)
	if !slices.Equal(r.l.stops, []string{"id-pc-c-001"}) {
		t.Fatalf("閉じたカードの PG だけを止めていない (PM に触った?): %v", r.l.stops)
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "pm1") {
		t.Fatalf("終了で PM を止めていない: %v", r.l.stops)
	}
}

// 🚨 再開が返った直後 (記録の行はまだ前の PM) に終了しても、今の PM を止める (敵対的レビュー P1-1)。
func TestShutdownRightAfterPMResume(t *testing.T) {
	r := startedPM(t)
	r.l.resumeID = "pm2"
	r.now = t0.Add(time.Minute)
	request(t, r.dir, "二つ目")
	r.tick(t)
	r.ss = []agents.Session{{ID: "pm2", SessionID: "P2", PID: 61, Kind: "background", State: "working", Status: "busy",
		Cwd: "/w/dotfiles/.claude/worktrees/" + pmName, StartedAt: r.now.Add(time.Second).UnixMilli()}}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "pm2") {
		t.Fatalf("記録に載る前の再開した PM を止めていない: %v", r.l.stops)
	}
}

// 落ち続ける PM は、crash の窓の間に pmReviveLimit 回までしか起こし直さない (枠を焼き続けない。敵対的レビュー P2-1)。
func TestPMReviveLimited(t *testing.T) {
	r := startedPM(t)
	r.ss = nil // 再開しても一覧に出ない (すぐ落ちる)
	for i := range 40 {
		r.now = t0.Add(time.Hour + time.Duration(i)*30*time.Second)
		r.tick(t)
	}
	if len(r.l.resumes) != pmReviveLimit {
		t.Fatalf("20 分の間に起こし直した回数 = %d (上限 %d)", len(r.l.resumes), pmReviveLimit)
	}
	r.now = r.now.Add(defaultCrashWindow)
	r.tick(t)
	if len(r.l.resumes) != pmReviveLimit+1 {
		t.Fatalf("窓が過ぎても起こし直さない: %d", len(r.l.resumes))
	}
}

// 前の PM の worktree が消えたら、再開 (消えた cwd へは再開できない) ではなく新しい worktree で起動する (敵対的レビュー P2-2)。
func TestPMStartsFreshWhenWorktreeGone(t *testing.T) {
	r := startedPM(t)
	r.d.Exists = func(string) bool { return false }
	r.ss = nil
	r.now = t0.Add(time.Hour)
	if _, err := r.d.Shutdown(context.Background()); err != nil { // 止めた PM (待たずに起こす)
		t.Fatal(err)
	}
	r.tick(t)
	if len(r.l.resumes) != 0 || r.l.startTries != 2 || r.l.starts[1] != "pc-pm-20260925-020000" {
		t.Fatalf("worktree の消えた PM を再開しようとした / 新しく起動していない: resumes=%v starts=%v", r.l.resumes, r.l.starts)
	}
}

// 知らない status の PM は作業中とみなして止めない (敵対的レビュー P3-1)。
func TestPMUnknownStatusNotInterrupted(t *testing.T) {
	r := startedPM(t)
	r.ss[0].Status = "compacting"
	request(t, r.dir, "二つ目")
	var reasons []string
	for _, e := range r.tick(t) {
		reasons = append(reasons, e.Reason)
	}
	if len(r.l.resumes) != 0 || !strings.Contains(strings.Join(reasons, "\n"), "compacting") {
		t.Fatalf("知らない status の PM を止めて再開した / 知らせていない: %v %v", r.l.resumes, reasons)
	}
}

// pm.json が壊れても、PG の割り当ては続ける (敵対的レビュー P3-2)。
func TestBrokenPMStateDoesNotStopDispatch(t *testing.T) {
	r := newPMRig(t)
	planned(t, r.dir, 1)
	if err := os.WriteFile(filepath.Join(r.dir, store.PMStateFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if !slices.Contains(r.l.starts, "pc-c-001") {
		t.Fatalf("PM の壊れで PG を起動しなかった: %v", r.l.starts)
	}
}

// PMOff (--pm=off / 設定 pm = "off") なら PM を起動も再開もせず、依頼の列のカードはそのまま置く。前から居る PM も終了では止める。
func TestPMOffLeavesRequestedCards(t *testing.T) {
	t.Run("起動しない", func(t *testing.T) {
		r := newPMRig(t)
		r.d.PMOff = true
		request(t, r.dir, "一つ目")
		r.tick(t)
		if r.l.startTries != 0 || states(t, r.dir)["C-001"].State != card.Requested {
			t.Fatalf("off なのに PM を起動した / カードを動かした: %v", r.l.starts)
		}
		if pm := loadPM(t, r.dir); pm.Launching != "" || len(pm.Told) != 0 {
			t.Fatalf("off なのに PM の様子を書いた: %+v", pm)
		}
	})
	t.Run("再開しないが終了では止める", func(t *testing.T) {
		r := startedPM(t)
		r.d.PMOff = true
		request(t, r.dir, "二つ目")
		r.tick(t)
		if len(r.l.resumes) != 0 {
			t.Fatalf("off なのに PM を再開した: %v", r.l.resumes)
		}
		if _, err := r.d.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(r.l.stops, "id-"+pmName) {
			t.Fatalf("off のとき、前から居る PM を終了で止めていない: %v", r.l.stops)
		}
	})
}

// claude が PM の起動・再開を受け付けない (rc≠0 がすぐ返る) のが launchRejectLimit 回続いたら、起こし直さない (462 の PM 版)。
// 起こし直しの上限 (pmReviveLimit) は生きていない PM を起こし直すときにしか効かず、1 度も起動できていない PM と終了で止めた PM には当たらない。
func TestPMRejectedLaunchLimited(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) *pmRig
		tries func(r *pmRig) int
	}{
		{"起動", func(t *testing.T) *pmRig {
			r := newPMRig(t)
			request(t, r.dir, "一つ目")
			return r
		}, func(r *pmRig) int { return r.l.startTries }},
		{"終了で止めた後の再開", func(t *testing.T) *pmRig {
			r := startedPM(t)
			if _, err := r.d.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			r.ss = nil
			return r
		}, func(r *pmRig) int { return len(r.l.resumes) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.setup(t)
			r.l.reject = true
			var notes []eventlog.Event
			for i := range 40 {
				r.now = t0.Add(time.Hour + time.Duration(i)*30*time.Second)
				before := tc.tries(r)
				notes = append(notes, r.tick(t)...)
				// 上限に達した拒否の直後に見る (印は launchGrace の後の Tick でも外れるので、最後に見ても差が出ない)
				if pm := loadPM(t, r.dir); tc.tries(r) == launchRejectLimit && before < launchRejectLimit && pm.Launching != "" {
					t.Fatalf("起こし直さないのに印を残した (終了が一覧に出るのを待つ): %+v", pm)
				}
			}
			if n := tc.tries(r); n != launchRejectLimit {
				t.Fatalf("受け付けられない%sを %d 回試した (上限 %d)", tc.name, n, launchRejectLimit)
			}
			held := 0
			for _, n := range notes {
				if n.Kind == eventlog.KindHold && strings.Contains(n.Reason, "受け付けなかった") {
					held++
				}
			}
			if held != 1 {
				t.Fatalf("起こし直さない理由の出来事 = %d 件 (1 件のはず): %v", held, notes)
			}
		})
	}
}

// PM の拒否の回数は「続いた」回数: 立っているかもしれない失敗・起動できたら 0 に戻る (飛び飛びの拒否で起こさなくならない)。
func TestPMRejectCountResetsOnOtherOutcomes(t *testing.T) {
	r := newPMRig(t)
	request(t, r.dir, "一つ目")
	tickAt := func(i int) {
		r.now = t0.Add(time.Hour + time.Duration(i)*2*launchGrace)
		r.tick(t)
	}
	r.l.reject = true
	tickAt(0)
	tickAt(1)
	r.l.reject, r.l.fail = false, true // 立っているかもしれない失敗
	tickAt(2)
	if r.d.roleRun(pmRole).rejects != 0 {
		t.Fatalf("拒否でない失敗で回数を 0 に戻していない: %d", r.d.roleRun(pmRole).rejects)
	}
	r.l.reject, r.l.fail = true, false
	tickAt(3)
	tickAt(4)
	r.l.reject = false
	tickAt(5) // 起動できた
	if r.d.roleRun(pmRole).rejects != 0 || loadPM(t, r.dir).Session == "" {
		t.Fatalf("起動できたのに回数を 0 に戻していない: %d %+v", r.d.roleRun(pmRole).rejects, loadPM(t, r.dir))
	}
}
