package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

var t0 = time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)

// fakeLauncher は起動・再開を記録するだけ。fail が真なら起動に失敗する。
type fakeLauncher struct {
	starts  []string // name
	resumes []string // stopID + ":" + text
	fail    bool
	// resumeFail は再開に「失敗」と返す (実際には立っている形を作るのは一覧の側)
	resumeFail bool
}

func (f *fakeLauncher) Start(_ context.Context, _, name, _ string) (string, error) {
	if f.fail {
		return "", errors.New("起動できない")
	}
	f.starts = append(f.starts, name)
	return "id-" + name, nil
}

func (f *fakeLauncher) Resume(_ context.Context, stopID, _, text string) (string, error) {
	f.resumes = append(f.resumes, stopID+":"+text)
	if f.resumeFail {
		return "", errors.New("再開できない")
	}
	if stopID == "" {
		return "id-resumed", nil
	}
	return stopID, nil
}

// planned は分解済みのカードを n 枚積んだ記録を作る (箱から add → plan を通す)。
func planned(t *testing.T, dir string, n int) {
	t.Helper()
	for i := range n {
		if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "t", Repo: "dotfiles", At: t0.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Apply(dir, t0); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load(dir)
	for _, c := range st.Cards {
		if _, err := store.Submit(dir, store.Request{Kind: "plan", CardID: c.ID, Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1, Status: "open"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Apply(dir, t0); err != nil {
		t.Fatal(err)
	}
}

func newDaemon(t *testing.T, dir string, l Launcher, ss []agents.Session) *Daemon {
	t.Helper()
	return &Daemon{Dir: dir, Limit: 2, Repos: map[string]string{"dotfiles": "/w/dotfiles"}, Launch: l,
		List: func(context.Context) ([]agents.Session, error) { return ss, nil }, Now: func() time.Time { return t0 }}
}

func states(t *testing.T, dir string) map[string]card.Card {
	t.Helper()
	st, err := store.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]card.Card{}
	for _, c := range st.Cards {
		out[c.ID] = c
	}
	return out
}

// 分解済みのカードに上限まで PG を起動し、古い順に割り当てる。
func TestDispatchRespectsLimit(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 3)
	l := &fakeLauncher{}
	if _, err := newDaemon(t, dir, l, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	cs := states(t, dir)
	if len(l.starts) != 2 || cs["C-001"].State != card.Running || cs["C-002"].State != card.Running || cs["C-003"].State != card.Planned {
		t.Fatalf("上限 2 で古い順に起動するはず: starts=%v %v %v %v", l.starts, cs["C-001"].State, cs["C-002"].State, cs["C-003"].State)
	}
	if cs["C-001"].Session != "id-pc-c-001" {
		t.Fatalf("カードに session の id が入っていない: %q", cs["C-001"].Session)
	}
}

// 起動した PG の session は、一覧に出てから pro-con の記録に登録する (session id と pid が要る)。
func TestRegistersOnceListed(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDaemon(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg, _ := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 0 {
		t.Fatal("一覧に出る前に登録した (session id と pid が無い)")
	}
	d.List = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}}, nil
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg, _ = live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 1 || reg[0].SessionID != "S1" || reg[0].PID != 42 || reg[0].CardID != "C-001" {
		t.Fatalf("一覧に出た session を登録していない: %+v", reg)
	}
}

// 回答を受けたカードは、新しく起動せず同じ session を再開し、回答を渡す。渡したら Resume を空にする。
func TestAnsweredCardResumesSameSession(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDaemon(t, dir, l, []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}})
	for range 2 { // 起動 → 登録
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "赤か青か"}, {Kind: "answer", CardID: "C-001", Answer: "青"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.starts) != 1 || len(l.resumes) != 1 || l.resumes[0] != "id-pc-c-001:青" {
		t.Fatalf("同じ session を回答つきで再開するはず: starts=%v resumes=%v", l.starts, l.resumes)
	}
	if c := states(t, dir)["C-001"]; c.State != card.Running || c.Resume != "" {
		t.Fatalf("再開の後: %v Resume=%q", c.State, c.Resume)
	}
}

// 起動に失敗したカードは分解済みのまま残し、失敗を履歴に書く (作業中にしない)。
func TestLaunchFailureKeepsCardPlanned(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	notes, err := newDaemon(t, dir, &fakeLauncher{fail: true}, nil).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := states(t, dir)["C-001"]
	if c.State != card.Planned || !strings.Contains(c.History[len(c.History)-1].Text, "起動に失敗") || len(notes) == 0 {
		t.Fatalf("失敗したのに作業中にした / 履歴に無い: %v %+v %v", c.State, c.History, notes)
	}
}

// PG への指示には規律 (AskUserQuestion を使わず pro-con card ask / 終えたら review / master へ push しない) とカードの ID が入る。
func TestPromptCarriesDiscipline(t *testing.T) {
	p := Prompt(card.Card{ID: "C-007", Title: "直す", Request: "色を直して"})
	for _, want := range []string{"C-007", "AskUserQuestion", "pro-con card ask C-007", "pro-con card review C-007", "master へは push しない", "色を直して"} {
		if !strings.Contains(p, want) {
			t.Fatalf("PG への指示に %q が無い:\n%s", want, p)
		}
	}
}

// claude --bg の出力から短い id を読む。形が違えば読めないとエラーにする (空の id でカードを作業中にしない)。
func TestParseBackgrounded(t *testing.T) {
	if id, err := parseBackgrounded("backgrounded · 931e734d · m425-done\n  claude agents  list sessions\n"); err != nil || id != "931e734d" {
		t.Fatalf("実測の形を読めない: %q %v", id, err)
	}
	for _, bad := range []string{"", "error: Workspace not trusted", "backgrounded ·  · x"} {
		if _, err := parseBackgrounded(bad); err == nil {
			t.Fatalf("読めない出力 %q を読めたことにした", bad)
		}
	}
	env := withoutTmux([]string{"HOME=/h", "TMUX=/tmp/x,1,0", "TMUX_PANE=%3", "TMUXX=keep"})
	if strings.Join(env, " ") != "HOME=/h TMUXX=keep" {
		t.Fatalf("TMUX / TMUX_PANE だけを落とすはず: %v", env)
	}
}

// pro-con が再開していないのに pid が変わった session (外の shell で同じ session を再開した疑い) は、記録を書き直さずに知らせる。
// daemon 自身の再開の後なら、daemon が起動し直していても (メモリの印が無くても) 書き直す。
func TestRegisterRefusesPidChangeNotCausedByDaemon(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDaemon(t, dir, l, nil)
	d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
	for range 2 { // 起動 → 登録
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ss[0].PID, ss[0].StartedAt = 99, t0.Add(time.Minute).UnixMilli() // 外で再開された
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 1 || reg[0].PID != 42 || !strings.Contains(strings.Join(notes, "\n"), "外から操作された疑い") {
		t.Fatalf("外で pid が変わった session の記録を書き直した / 知らせない: %+v %v", reg, notes)
	}
	// daemon 自身が再開した後なら書き直す。再開の後に daemon が起動し直した形 (新しい Daemon) でも同じ
	for _, r := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	t1 := t0.Add(2 * time.Minute)
	d.Now = func() time.Time { return t1 }
	if _, err := d.Tick(context.Background()); err != nil { // 再開
		t.Fatal(err)
	}
	ss[0].PID, ss[0].StartedAt = 100, t1.Add(time.Second).UnixMilli()
	d2 := newDaemon(t, dir, l, ss)
	d2.Now = d.Now
	if _, err := d2.Tick(context.Background()); err != nil { // 登録
		t.Fatal(err)
	}
	reg, _ = live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 1 || reg[0].PID != 100 || len(l.resumes) != 1 {
		t.Fatalf("daemon が再開した後の pid で書き直していない: %+v resumes=%v", reg, l.resumes)
	}
}

// 最後の起動より前に始まっている session (同じ短い id の別の session の疑い) は、最初の登録でも取り込まない。
func TestRegisterRefusesOlderSession(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	d := newDaemon(t, dir, &fakeLauncher{}, nil)
	if _, err := d.Tick(context.Background()); err != nil { // 起動 (LaunchedAt = t0)
		t.Fatal(err)
	}
	d.List = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{ID: "id-pc-c-001", SessionID: "OLD", PID: 7, Kind: "background", StartedAt: t0.Add(-time.Hour).UnixMilli()}}, nil
	}
	notes, _ := d.Tick(context.Background())
	reg, _ := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 0 || !strings.Contains(strings.Join(notes, "\n"), "起動・再開より前") {
		t.Fatalf("起動より前の session を取り込んだ: %+v %v", reg, notes)
	}
}

// claude が起動に「失敗」と返っても session が立っていることがある。次の Tick は一覧で確かめて取り込み、2 本目を起動しない。
func TestFailedLaunchThatActuallyStartedIsAdopted(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{fail: true}
	d := newDaemon(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.fail = false
	d.List = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{ID: "ab12", SessionID: "S1", PID: 5, Name: "pc-c-001", Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}}, nil
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; len(l.starts) != 0 || c.State != card.Running || c.Session != "ab12" || c.Launching != "" {
		t.Fatalf("立っていた session を取り込まずに起動し直した: starts=%v %v %q %q", l.starts, c.State, c.Session, c.Launching)
	}
}

// 起動の結果が分からず、一覧にも出ないカードは、launchGrace の間は起動し直さない (その間も上限に数える)。過ぎたら起動し直す。
func TestUnconfirmedLaunchWaitsGraceThenRetries(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 2)
	l := &fakeLauncher{fail: true}
	d := newDaemon(t, dir, l, nil)
	d.Limit = 1
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.fail = false
	d.Now = func() time.Time { return t0.Add(launchGrace / 2) }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.starts) != 0 {
		t.Fatalf("結果を確かめる前に起動し直した / 上限を超えて次のカードを起動した: %v", l.starts)
	}
	d.Now = func() time.Time { return t0.Add(launchGrace + time.Second) }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.starts) != 1 || l.starts[0] != "pc-c-001" {
		t.Fatalf("launchGrace を過ぎても起動し直さない: %v", l.starts)
	}
}

// 再開の前に、今の一覧で短い id が前の session を指しているか確かめる。別の session を指していたら止めない (claude stop を撃たない)。
func TestResumeRefusesWhenShortIDPointsElsewhere(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDaemon(t, dir, l, nil)
	d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
	for range 2 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	ss[0].SessionID = "OTHER" // 自分の PG は終わり、同じ短い id を外の session が得た
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.resumes) != 0 || !strings.Contains(strings.Join(notes, "\n"), "別の session") {
		t.Fatalf("別の session を指す短い id で再開した: %v %v", l.resumes, notes)
	}
}

// session の一覧を取れない Tick は起動も再開もしない (立っている session を見落として増やさない)。
func TestNoDispatchWithoutSessionList(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDaemon(t, dir, l, nil)
	d.List = func(context.Context) ([]agents.Session, error) { return nil, errors.New("claude agents が落ちた") }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.starts) != 0 {
		t.Fatalf("一覧を取れないのに起動した: %v", l.starts)
	}
}

// daemon は 2 つ起動しない (記録の書き手は 1 つだけ)。1 つ目が外したら取れる。
func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrRunning) {
		t.Fatalf("2 つ目の daemon がロックを取れた: %v", err)
	}
	unlock()
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("外した後にロックを取れない: %v", err)
	}
	again()
}

// setCard は記録のカードを直接書き換える (daemon の途中の状態を作る)。
func setCard(t *testing.T, dir, id string, f func(*card.Card)) {
	t.Helper()
	if err := store.Update(dir, func(s *store.State) error {
		for i := range s.Cards {
			if s.Cards[i].ID == id {
				f(&s.Cards[i])
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// 印の残ったカードは、古いカードの後ろにあっても上限の判定より先に一覧と照らす (実際に立っている PG を数えずに別のカードを起動しない)。
func TestLaunchingCardCountsBeforeLimit(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 2)
	setCard(t, dir, "C-002", func(c *card.Card) { c.Launching, c.LaunchedAt = "起動", t0 })
	l := &fakeLauncher{}
	d := newDaemon(t, dir, l, []agents.Session{{ID: "cd34", SessionID: "S2", PID: 6, Name: "pc-c-002", Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}})
	d.Limit = 1
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	cs := states(t, dir)
	if len(l.starts) != 0 || cs["C-002"].State != card.Running || cs["C-001"].State != card.Planned {
		t.Fatalf("立っている PG を数えずに上限を超えて起動した: starts=%v C-001=%v C-002=%v", l.starts, cs["C-001"].State, cs["C-002"].State)
	}
}

// 前の session が一覧に無ければ、止めずに再開する (無い id への stop で再開が永遠に届かない形を作らない)。
func TestResumeSkipsStopWhenSessionIsGone(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDaemon(t, dir, l, nil)
	d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
	for range 2 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	ss = nil // PG は質問して終わった
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.resumes) != 1 || l.resumes[0] != ":a" {
		t.Fatalf("一覧に無い session を止めようとした / 再開しない: %v", l.resumes)
	}
}

// 起動の結果を確かめるとき、印を書いた時刻より前に始まった同名の session は取り込まない (前の起動の残りの疑い)。
func TestAdoptIgnoresSessionOlderThanMark(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	setCard(t, dir, "C-001", func(c *card.Card) { c.Launching, c.LaunchedAt = "起動", t0 })
	d := newDaemon(t, dir, &fakeLauncher{}, []agents.Session{{ID: "old1", SessionID: "S0", PID: 3, Name: "pc-c-001", Kind: "background", StartedAt: t0.Add(-time.Minute).UnixMilli()}})
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; c.State != card.Planned || c.Session == "old1" {
		t.Fatalf("印より前に始まった同名の session を取り込んだ: %v %q", c.State, c.Session)
	}
}

// 再開に「失敗」と返っても立っていれば、次の Tick は同じ session id の session を取り込み、同じ回答を 2 回渡さない。
func TestFailedResumeThatActuallyResumedIsAdopted(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDaemon(t, dir, l, nil)
	d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
	for range 2 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	t1 := t0.Add(time.Minute)
	d.Now = func() time.Time { return t1 }
	l.resumeFail = true
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	ss[0].PID, ss[0].StartedAt = 43, t1.Add(time.Second).UnixMilli() // 実は再開していた
	d.Now = func() time.Time { return t1.Add(launchGrace + time.Second) }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; len(l.resumes) != 1 || c.State != card.Running || c.Resume != "" {
		t.Fatalf("実は再開していた session を取り込まずに再開し直した: resumes=%v %v Resume=%q", l.resumes, c.State, c.Resume)
	}
}
