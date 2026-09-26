package dispatcher

import (
	"context"
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

// intRig は PG のカード C-001 が作業中の dispatcher に、PM と取り込みの係の repo・指示書を渡したもの (時計を進められる)。
type intRig struct {
	*crashRig
	now time.Time
}

const intName = "pc-int-20260925-010000" // t0 に起動した取り込みの係の名前

func newIntRig(t *testing.T) *intRig {
	t.Helper()
	r := &intRig{crashRig: newCrashRig(t), now: t0}
	r.d.Now = func() time.Time { return r.now }
	r.d.PMRepo, r.d.PMGuide, r.d.IntegratorGuide = "/w/dotfiles", "PM-GUIDE", "INT-GUIDE"
	r.d.Exists = func(string) bool { return true }
	return r
}

func submit(t *testing.T, dir string, reqs ...store.Request) {
	t.Helper()
	for _, q := range reqs {
		if _, err := store.Submit(dir, q); err != nil {
			t.Fatal(err)
		}
	}
}

func intSession(pid int, status string, started time.Time) agents.Session {
	return agents.Session{ID: "id-" + intName, SessionID: "I1", PID: pid, Kind: "background", Status: status, State: "working", Name: intName,
		Cwd: "/w/dotfiles/.claude/worktrees/" + intName, StartedAt: started.UnixMilli()}
}

func regRows(t *testing.T, dir string) []live.Owned {
	t.Helper()
	reg, err := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// reviewedAndStarted は C-001 をレビューの列に出し、取り込みの係を起動して記録に載せたところまで進める。
func reviewedAndStarted(t *testing.T) *intRig {
	t.Helper()
	r := newIntRig(t)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t)
	r.ss = append(r.ss, intSession(70, "idle", t0.Add(time.Second)))
	r.tick(t)
	if _, ok := roleRow(regRows(t, r.dir), IntegratorCardID); !ok {
		t.Fatal("前提: 起動した取り込みの係を記録に載せていない")
	}
	return r
}

// レビューの列にカードが来たら、PM の repo で取り込みの係を 1 本起動し、指示書とカードを渡す。PM は起こさない (依頼も質問も無い)。
// 起動した係は INT の行で記録に載り、PM の行とは混ざらない。
func TestIntegratorStartsOnReview(t *testing.T) {
	r := newIntRig(t)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t)
	if !slices.Equal(r.l.starts[1:], []string{intName}) || r.l.repos[len(r.l.repos)-1] != "/w/dotfiles" { // starts[0] は C-001 の PG
		t.Fatalf("取り込みの係を PM の repo で 1 本起動していない: starts=%v repos=%v", r.l.starts, r.l.repos)
	}
	p := r.l.prompts[len(r.l.prompts)-1]
	for _, want := range []string{"取り込みの係です", "INT-GUIDE", "レビュー待ち C-001", "/w/dotfiles/.claude/worktrees/pc-c-001"} {
		if !strings.Contains(p, want) {
			t.Fatalf("起動の指示に %q が無い: %q", want, p)
		}
	}
	if strings.Contains(p, "PM-GUIDE") {
		t.Fatalf("取り込みの係に PM の指示書を渡した: %q", p)
	}
	st, err := store.LoadRole(r.dir, store.IntegratorStateFile, "取り込みの係")
	if err != nil || st.Session != "id-"+intName || len(st.Told) != 1 || !strings.HasPrefix(st.Told[0], "C-001@") {
		t.Fatalf("起動した係の様子が違う: %+v %v", st, err)
	}
	if pm := loadPM(t, r.dir); pm.Session != "" || pm.Launching != "" {
		t.Fatalf("依頼も質問も無いのに PM を起こした: %+v", pm)
	}
	r.ss = append(r.ss, intSession(70, "idle", t0.Add(time.Second)))
	r.tick(t)
	reg := regRows(t, r.dir)
	row, ok := roleRow(reg, IntegratorCardID)
	if !ok || row.SessionID != "I1" || row.PID != 70 {
		t.Fatalf("取り込みの係を INT の行で記録に載せていない: %+v", reg)
	}
	if _, ok := roleRow(reg, PMCardID); ok {
		t.Fatalf("取り込みの係を PM の行に載せた: %+v", reg)
	}
	r.tick(t)
	if r.l.startTries != 2 || len(r.l.resumes) != 0 {
		t.Fatalf("知らせ済みのカードで係を起こし直した: starts=%d resumes=%v", r.l.startTries, r.l.resumes)
	}
}

// 係を off にしている間にカードがレビューの列を離れて戻っても (知らせ済みの掃除が回らない)、on に戻したら知らせ直す。
// 鍵がカード ID だけだと、前の知らせ済みのまま黙って取りこぼす (鍵にレビューの列に入った時刻を入れる理由)。
func TestIntegratorRetoldAfterReworkWhileOff(t *testing.T) {
	r := reviewedAndStarted(t)
	r.d.IntegratorOff = true
	r.now = t0.Add(time.Minute)
	submit(t, r.dir, store.Request{Kind: "rework", CardID: "C-001", Rework: "テストを足す"})
	r.tick(t)
	r.now = t0.Add(2 * time.Minute)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Review {
		t.Fatalf("前提: またレビューの列に来ていない: %v", c.State)
	}
	resumed := len(r.l.resumes)
	r.d.IntegratorOff = false
	r.tick(t)
	got := r.l.resumes[resumed:]
	if len(got) != 1 || !strings.HasPrefix(got[0], "id-"+intName+":") {
		t.Fatalf("off の間に戻ってきたカードを、on に戻しても知らせない: %v", got)
	}
}

// 差し戻して PG が直し、またレビューの列に来たら、同じ係の session を再開してもう一度知らせる (鍵はレビューの列に入った時刻)。
func TestIntegratorRetoldAfterRework(t *testing.T) {
	r := reviewedAndStarted(t)
	r.now = t0.Add(time.Minute)
	submit(t, r.dir, store.Request{Kind: "rework", CardID: "C-001", Rework: "テストを足す"})
	r.tick(t) // 分解済みへ戻り、同じ PG を再開する
	if c := states(t, r.dir)["C-001"]; c.State != card.Running {
		t.Fatalf("前提: 差し戻したカードが作業中に戻らない: %v", c.State)
	}
	resumed := len(r.l.resumes)
	r.now = t0.Add(2 * time.Minute)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t)
	got := r.l.resumes[resumed:]
	if len(got) != 1 || !strings.HasPrefix(got[0], "id-"+intName+":") || !strings.Contains(got[0], "レビュー待ち C-001") {
		t.Fatalf("またレビューの列に来たカードを係に知らせていない: %v", got)
	}
	if names := r.l.resumeNames[len(r.l.resumeNames)-1:]; names[0] != intName { // 起動のときの名前に付け直す (488)
		t.Fatalf("係の再開で session の名前を付け直していない: %v", names)
	}
}

// 係が人に回した (handoff) カードは、またレビューの列に入り直すまで知らせる物から外れる (起こし直す理由にならない)。
func TestIntegratorSkipsHandedOffCard(t *testing.T) {
	r := reviewedAndStarted(t)
	submit(t, r.dir, store.Request{Kind: "handoff", CardID: "C-001", Text: "人の判断が要る", From: "取り込みの係"})
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Review || !c.HandedOff() {
		t.Fatalf("前提: レビュー待ちのカードを人に回せていない: %v handedOff=%v", c.State, c.HandedOff())
	}
	r.ss = r.ss[:len(r.ss)-1] // 係が落ちた
	starts, resumes := r.l.startTries, len(r.l.resumes)
	for i := range 3 { // 落ちたのを見てから restartWait を過ぎるまで回す (過ぎなければ、人に回していなくても起こし直さない)
		r.now = t0.Add(time.Duration(i+1) * 10 * time.Minute)
		r.tick(t)
	}
	if r.l.startTries != starts || len(r.l.resumes) != resumes {
		t.Fatalf("人に回したカードのために係を起こし直した: starts=%d→%d resumes=%d→%d", starts, r.l.startTries, resumes, len(r.l.resumes))
	}
}

// --integrator=off なら、レビューの列にカードがあっても係を起こさない。前の dispatcher が起こした係は、終了では止める。
func TestIntegratorOffButStoppedOnShutdown(t *testing.T) {
	r := reviewedAndStarted(t)
	r.d.IntegratorOff = true
	r.now = t0.Add(time.Minute)
	submit(t, r.dir, store.Request{Kind: "add", Title: "別の依頼", Repo: "dotfiles"}, // PM は起こす (役ごとに止める設定が分かれている)
		store.Request{Kind: "rework", CardID: "C-001", Rework: "直す"})
	r.tick(t)
	r.now = t0.Add(2 * time.Minute)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"}) // off の間に、知らせる物が新しくできる
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Review {
		t.Fatalf("前提: またレビューの列に来ていない: %v", c.State)
	}
	if slices.ContainsFunc(r.l.resumes, func(s string) bool { return strings.HasPrefix(s, "id-"+intName) }) {
		t.Fatalf("off の係を再開した: %v", r.l.resumes)
	}
	if pm := loadPM(t, r.dir); pm.Session == "" {
		t.Fatalf("係を off にしただけで PM まで起こさない: %+v", pm)
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "id-"+intName) {
		t.Fatalf("off の係を終了で止めない: stops=%v", r.l.stops)
	}
}

// 係が入力待ち (権限の確認か質問) で止まったら、知らせる物が無くても (手元のカードの push の確認で止まる形) 1 度だけ出来事にする。
// 抜けて (idle) またなったら、また出す。入力待ちの間は再開しない (turn を殺さない)。
func TestIntegratorWaitingIsReported(t *testing.T) {
	r := reviewedAndStarted(t)
	waitingNotes := func(evs []eventlog.Event) int {
		n := 0
		for _, e := range evs {
			if e.Card == IntegratorCardID && strings.Contains(e.Reason, "入力待ち") {
				n++
			}
		}
		return n
	}
	count := func(ticks int) int {
		var all []eventlog.Event
		for range ticks {
			r.now = r.now.Add(time.Minute)
			all = append(all, r.tick(t)...)
		}
		return waitingNotes(all)
	}
	r.ss[len(r.ss)-1].Status = "waiting" // 知らせる物は無い (C-001 は知らせ済み)
	if n := count(3); n != 1 {
		t.Fatalf("知らせる物が無いときの入力待ちの出来事が %d 回 (1 回のはず)", n)
	}
	r.ss[len(r.ss)-1].Status = "idle"
	if n := count(1); n != 0 {
		t.Fatalf("入力待ちを抜けたのに出来事を出した: %d", n)
	}
	r.ss[len(r.ss)-1].Status = "waiting"
	if n := count(2); n != 1 {
		t.Fatalf("また入力待ちになったのに出来事が %d 回 (1 回のはず)", n)
	}
	submit(t, r.dir, store.Request{Kind: "rework", CardID: "C-001", Rework: "直す"})
	r.tick(t)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	if n := count(2); n != 0 { // 知らせる物ができても、同じ session の入力待ちは出し直さない
		t.Fatalf("入力待ちが続いているだけなのに出来事が %d 回", n)
	}
	if slices.ContainsFunc(r.l.resumes, func(s string) bool { return strings.HasPrefix(s, "id-"+intName) }) {
		t.Fatalf("入力待ちの係を再開した (turn を殺す): %v", r.l.resumes)
	}
}

// 入力待ちの係が落ち、起こし直した別の session が busy を見せずにまた入力待ちになっても、出来事にする
// (同じ作業をやり直して同じ権限の確認で止まる形。印が前の session のまま残ると二度と出ない)。
func TestIntegratorWaitingReportedAgainAfterRevive(t *testing.T) {
	r := reviewedAndStarted(t)
	r.ss[len(r.ss)-1].Status = "waiting"
	r.now = t0.Add(time.Minute)
	r.tick(t)
	r.ss = r.ss[:len(r.ss)-1] // 落ちた
	for i := range 2 {        // restartWait を過ぎて起こし直す
		r.now = t0.Add(time.Duration(i+2) * time.Minute)
		r.tick(t)
	}
	if !slices.ContainsFunc(r.l.resumes, func(s string) bool { return strings.Contains(s, "C-001") }) {
		t.Fatalf("前提: 落ちた係を起こし直していない: %v", r.l.resumes)
	}
	again := intSession(71, "waiting", r.now)
	again.ID, again.SessionID = "id-int-2", "I2"
	r.l.resumeID = ""
	r.ss = append(r.ss, again)
	st, _ := store.LoadRole(r.dir, store.IntegratorStateFile, "取り込みの係")
	if st.Session != "" {
		r.ss[len(r.ss)-1].ID = st.Session // 再開が返した短い id の session として一覧に出る
	}
	var notes []eventlog.Event
	for i := range 2 {
		r.now = r.now.Add(time.Duration(i+1) * time.Minute)
		notes = append(notes, r.tick(t)...)
	}
	if !strings.Contains(joinNotes(notes), "入力待ち") {
		t.Fatalf("起こし直した係の入力待ちを出来事にしない: %s", joinNotes(notes))
	}
}

// 終了は、同時に起きている PM と取り込みの係の両方を止める。どちらも起動が返った直後で記録に載っていない形で見る
// (記録に載っていれば記録の段が止める。役ごとに止める段が 1 つ目の役で抜けると、2 つ目の役が残る)。
func TestShutdownStopsPMAndIntegrator(t *testing.T) {
	r := newIntRig(t)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"}, store.Request{Kind: "add", Title: "別の依頼", Repo: "dotfiles"})
	r.tick(t) // PM と係を起動した (記録にはまだ無い)
	r.ss = append(r.ss, intSession(70, "idle", t0.Add(time.Second)), pmSession("id-"+pmName, "P1", 60, "idle", t0.Add(time.Second)))
	reg := regRows(t, r.dir)
	if _, ok := roleRow(reg, PMCardID); ok {
		t.Fatal("前提: PM がもう記録に載っている")
	}
	if _, ok := roleRow(reg, IntegratorCardID); ok {
		t.Fatal("前提: 係がもう記録に載っている")
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "id-"+intName) || !slices.Contains(r.l.stops, "id-"+pmName) {
		t.Fatalf("終了で PM と係の両方を止めない: stops=%v", r.l.stops)
	}
}

// 起動が返った直後で、まだ起動の記録に載っていない係も、終了で止める (記録を見て止める段では拾えない。役ごとに止める段が拾う)。
func TestIntegratorStoppedOnShutdownBeforeRegistered(t *testing.T) {
	r := newIntRig(t)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t)                                                        // 起動した (記録にはまだ無い)
	r.ss = append(r.ss, intSession(70, "idle", t0.Add(time.Second))) // 一覧には出た
	if _, ok := roleRow(regRows(t, r.dir), IntegratorCardID); ok {
		t.Fatal("前提: 係がもう記録に載っている")
	}
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.l.stops, "id-"+intName) {
		t.Fatalf("記録に載る前の係を終了で止めない: stops=%v", r.l.stops)
	}
}
