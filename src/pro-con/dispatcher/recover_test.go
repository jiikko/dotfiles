package dispatcher

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// restartAfter は crashRig の状態の置き場で dispatcher を起動し直す (前の dispatcher は止め処理を経ずに消えた形)。
// 一覧 (--all なし) は r.ss、--all は all、マシンの起動時刻は boot (zero なら読めない)。
func restartAfter(t *testing.T, r *crashRig, now, boot time.Time, all []agents.Session) {
	t.Helper()
	r.d = newDispatcher(t, r.dir, r.l, nil)
	r.d.List = func(context.Context) ([]agents.Session, error) { return r.ss, nil }
	r.d.ListAll = func(context.Context) ([]agents.Session, error) { return all, nil }
	r.d.Now = func() time.Time { return now }
	if !boot.IsZero() {
		r.d.BootTime = func() (time.Time, error) { return boot, nil }
	}
}

// failedS1 は再起動の後に --all に残る C-001 の PG (pid 無し・failed。482 の実測)。
func failedS1() []agents.Session {
	return []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", Kind: "background", State: "failed", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}
}

func recoverNote(t *testing.T, notes []eventlog.Event) string {
	t.Helper()
	var got []string
	for _, e := range notes {
		if e.Kind == eventlog.KindRecover {
			got = append(got, e.Reason)
		}
	}
	if len(got) != 1 {
		t.Fatalf("起動時の確かめの出来事が 1 件でない: %v", got)
	}
	return got[0]
}

// マシンの起動時刻より前に始まり、--all に pid 無し・failed で残る PG は、待たずに (最初の Tick で) 同じ session を再開する。
func TestStartupRecoversSessionLostInReboot(t *testing.T) {
	r := newCrashRig(t)
	r.ss = nil // failed は --all なしの一覧に出ない
	now := t0.Add(30 * time.Minute)
	restartAfter(t, r, now, t0.Add(10*time.Minute), failedS1())
	notes := r.tick(t)
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "マシンの再起動") {
		t.Fatalf("再起動で消えた PG を待たずに再開しない: resumes=%v notes=%v", r.l.resumes, notes)
	}
	if n := recoverNote(t, notes); !strings.Contains(n, "待たずに復旧した: C-001") {
		t.Fatalf("復旧したカードを出さない: %s", n)
	}
	if c := states(t, r.dir)["C-001"]; len(c.Crashes) != 1 || !c.CrashesFrom.Before(t0.Add(10*time.Minute)) || c.Revived {
		t.Fatalf("再起動で消えたのを落ちた回数に数えない / 再開で数え直した: crashes=%v from=%v revived=%v", c.Crashes, c.CrashesFrom, c.Revived)
	}
	ds, _, _ := store.LoadDispatcherState(r.dir)
	if ds.Startup != "起動時: 復旧 1 / 判定できない 0 (pro-con log)" || !ds.StartupAlert {
		t.Fatalf("ヘッダーの要約: %q alert=%v", ds.Startup, ds.StartupAlert)
	}
	if n := r.tick(t); hasEvent(n, eventlog.KindRecover, "") {
		t.Fatal("起動時の確かめを 2 度した")
	}
}

// 再起動で消えたと示せない形は復旧せず (その Tick で再開しない)、理由を出すだけにする。
func TestStartupDoesNotRecoverWhenUnsure(t *testing.T) {
	cases := []struct {
		name string
		boot time.Time
		all  []agents.Session
		why  string
		zero bool // 起動の記録の開始時刻を消す (古い行・startedAt を出さない版)
	}{
		{"再起動より後に始まった session (dispatcher だけが落ちた)", t0.Add(-time.Hour), failedS1(), "マシンの再起動より後に始まった", false},
		{"起動時刻を読めない", time.Time{}, failedS1(), "起動時刻を読めない", false},
		{"--all にも居ない", t0.Add(10 * time.Minute), nil, "--all の一覧にも居ない", false},
		{"pid 無しの working (自動の再開の途中)", t0.Add(10 * time.Minute), func() []agents.Session {
			ss := failedS1()
			ss[0].State = "working"
			return ss
		}(), `state "working"`, false},
		{"起動の記録に開始時刻が無い", t0.Add(10 * time.Minute), failedS1(), "開始時刻が無い", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newCrashRig(t)
			r.ss = nil
			if tc.zero {
				regPath := filepath.Join(r.dir, live.RegistryFile)
				reg, _ := live.LoadRegistry(regPath)
				reg[0].StartedAt = time.Time{}
				if err := live.ReplaceCard(regPath, reg[0]); err != nil {
					t.Fatal(err)
				}
			}
			restartAfter(t, r, t0.Add(30*time.Minute), tc.boot, tc.all)
			notes := r.tick(t)
			if len(r.l.resumes) != 0 {
				t.Fatalf("判定できないのに再開した: %v", r.l.resumes)
			}
			if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Stopped {
				t.Fatalf("判定できないのに記録を変えた: %v stopped=%v", c.State, c.Stopped)
			}
			if n := recoverNote(t, notes); !strings.Contains(n, "判定できないので待つ: C-001 (") || !strings.Contains(n, tc.why) {
				t.Fatalf("判定できない理由を出さない: %s", n)
			}
		})
	}
}

// 一覧に pid ありで居る PG は生きている (何もしない)。記録に動いているはずの session が無ければ「復旧は要らない」。
func TestStartupLeavesLiveAndCleanAlone(t *testing.T) {
	r := newCrashRig(t)
	restartAfter(t, r, t0.Add(30*time.Minute), t0.Add(10*time.Minute), failedS1())
	notes := r.tick(t)
	if n := recoverNote(t, notes); !strings.Contains(n, "生きている: C-001") || len(r.l.resumes) != 0 {
		t.Fatalf("生きている PG を扱った: %s resumes=%v", n, r.l.resumes)
	}

	clean := &crashRig{dir: t.TempDir(), l: &fakeLauncher{}}
	restartAfter(t, clean, t0, t0, nil)
	if n := recoverNote(t, clean.tick(t)); !strings.Contains(n, "復旧は要らない") {
		t.Fatalf("対象が無いときの文: %s", n)
	}
	if ds, _, _ := store.LoadDispatcherState(clean.dir); ds.Startup != "起動時: 復旧は要らない" || ds.StartupAlert {
		t.Fatalf("ヘッダーの要約: %q alert=%v", ds.Startup, ds.StartupAlert)
	}
}

// テストの係の実行の途中でマシンが落ちたカードは、戻さずに同じコマンドを頼み直し、結果が出たら待たずに PG を再開する。
// PG が一覧に居ないまま restartWait を過ぎても、458 の経路で戻して頼みを捨てない。
func TestStartupRerunsInterruptedRunThenResumes(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	old := killStaleFn
	killStaleFn = func(string) {}
	t.Cleanup(func() { killStaleFn = old })
	setCard(t, r.dir, "C-001", func(c *card.Card) {
		c.Run, c.RunAt, c.RunCwd = "make test", t0, "/w/dotfiles/.claude/worktrees/pc-c-001"
		c.Exec = card.Exec{Command: "make test", Since: t0, RunID: "C-001-777.log"}
	})
	r.ss = nil
	now := t0.Add(30 * time.Minute)
	restartAfter(t, r, now, t0.Add(10*time.Minute), failedS1())
	r.d.Runner = fr
	notes := r.tick(t)
	fr.waitStarted(t)
	if n := recoverNote(t, notes); !strings.Contains(n, "テストの係の結果を待って再開する: C-001") {
		t.Fatalf("頼み直すカードを出さない: %s", n)
	}
	later := now.Add(2 * restartWait)
	r.d.Now = func() time.Time { return later }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Run == "" || len(r.l.resumes) != 0 {
		t.Fatalf("結果を待つカードを戻した / 結果の前に再開した: %v run=%q resumes=%v", c.State, c.Run, r.l.resumes)
	}
	fr.release <- 0
	waitDone(t, r)
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "rc=0") {
		t.Fatalf("結果を渡して再開しない: %v", r.l.resumes)
	}
}

// PM が再起動で消えていたら、消えた時刻をマシンの起動時刻にする (知らせる物があれば待たずに起こす。終了の印は付けない =
// 起こし直しの回数に数え、PM がマシンを落としている形でも上限なく起こし直さない)。
func TestStartupMarksRoleLostInReboot(t *testing.T) {
	dir := t.TempDir()
	if err := store.SaveRole(dir, store.PMStateFile, store.PMState{Session: "pm1"}); err != nil {
		t.Fatal(err)
	}
	if err := live.Register(filepath.Join(dir, live.RegistryFile), live.Owned{SessionID: "SPM", ID: "pm1", PID: 7, CardID: PMCardID, StartedAt: t0, Cwd: "/w/dotfiles/.claude/worktrees/" + pmName}); err != nil {
		t.Fatal(err)
	}
	r := &crashRig{dir: dir, l: &fakeLauncher{}}
	restartAfter(t, r, t0.Add(time.Hour), t0.Add(10*time.Minute), []agents.Session{{ID: "pm1", SessionID: "SPM", Kind: "background", State: "failed"}})
	notes := r.tick(t)
	pm, err := store.LoadPM(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pm.Stopped || !pm.DeadSince.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("再起動で消えた PM の消えた時刻を起動時刻にしない / 終了の印を付けた: %+v", pm)
	}
	if n := recoverNote(t, notes); !strings.Contains(n, "待たずに復旧した: PM") {
		t.Fatalf("復旧した役を出さない: %s", n)
	}
}

// 窓の間に上限まで落ちた PG は、再起動の後でも再開せずに人の番へ送る (マシンを落とす PG を再起動のたびに再開し続けない)。
func TestStartupAsksHumanWhenCrashingRepeatedly(t *testing.T) {
	r := newCrashRig(t)
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.Crashes = []time.Time{t0.Add(5 * time.Minute)} })
	r.ss = nil
	restartAfter(t, r, t0.Add(20*time.Minute), t0.Add(10*time.Minute), failedS1())
	notes := r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed || len(r.l.resumes) != 0 {
		t.Fatalf("落ち続けた PG を人の番へ送らない: %v %v resumes=%v", c.State, c.Wait.Kind, r.l.resumes)
	}
	if n := recoverNote(t, notes); !strings.Contains(n, "落ち続けたので人の番へ: C-001") {
		t.Fatalf("人の番へ送ったカードを出さない: %s", n)
	}
}
