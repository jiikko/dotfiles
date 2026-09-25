package dispatcher

import (
	"context"
	"errors"
	"fmt"
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

var t0 = time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)

// fakeLauncher は起動・再開を記録するだけ。fail が真なら起動に失敗する。
type fakeLauncher struct {
	starts  []string // name
	resumes []string // stopID + ":" + text
	fail    bool
	// reject は起動・再開に「claude が受け付けなかった」(ErrRejected。rc≠0 がすぐ返った) と返す
	reject bool
	// resumeFail は再開に「失敗」と返す (実際には立っている形を作るのは一覧の側)
	resumeFail bool
	stops      []string
	cwds       []string // 再開した cwd
	resumeID   string   // 空でなければ、再開はこの短い id の新しい session を立てる (本物の claude の形)
	stopFail   bool
	stopTries  []string // 失敗も含めて止めようとした id
	startTries int      // 失敗も含めて起動しようとした回数
	repos      []string // 起動した repo
	prompts    []string // 起動の指示
}

func (f *fakeLauncher) Start(_ context.Context, repo, name, prompt string) (string, error) {
	f.startTries++
	if f.reject {
		return "", fmt.Errorf("claude --bg: %w: exit status 1: error: unknown option", ErrRejected)
	}
	if f.fail {
		return "", errors.New("起動できない")
	}
	f.starts = append(f.starts, name)
	f.repos, f.prompts = append(f.repos, repo), append(f.prompts, prompt)
	return "id-" + name, nil
}

func (f *fakeLauncher) Resume(_ context.Context, stopID, _, cwd, text string) (string, error) {
	f.cwds = append(f.cwds, cwd)
	f.resumes = append(f.resumes, stopID+":"+text)
	if f.reject {
		return "", fmt.Errorf("claude --bg: %w: exit status 1: error: unknown option", ErrRejected)
	}
	if f.resumeFail {
		return "", errors.New("再開できない")
	}
	if f.resumeID != "" {
		return f.resumeID, nil
	}
	if stopID == "" {
		return "id-resumed", nil
	}
	return stopID, nil
}

func (f *fakeLauncher) Stop(_ context.Context, id string) error {
	f.stopTries = append(f.stopTries, id)
	if f.stopFail {
		return errors.New("止められない")
	}
	f.stops = append(f.stops, id)
	return nil
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

func newDispatcher(t *testing.T, dir string, l Launcher, ss []agents.Session) *Dispatcher {
	t.Helper()
	d := &Dispatcher{Dir: dir, Limit: 2, Repos: map[string]string{"dotfiles": "/w/dotfiles"}, Launch: l,
		List: func(context.Context) ([]agents.Session, error) { return ss, nil }, Now: func() time.Time { return t0 }, Sleep: func(time.Duration) {}}
	if fl, ok := l.(*fakeLauncher); ok {
		d.ListAll = func(ctx context.Context) ([]agents.Session, error) { return fl.listAll(ctx, d.List) }
	}
	return d
}

// listAll は `claude agents --json --all` の偽物: 一覧 (d.List。テストが途中で差し替える) に、止めた session を stopped で重ねる
// (本物は止めた session を --all に state: stopped・pid 無しで残す。2.1.282 で実測)。
func (f *fakeLauncher) listAll(ctx context.Context, list func(context.Context) ([]agents.Session, error)) ([]agents.Session, error) {
	ss, err := list(ctx)
	if err != nil {
		return nil, err
	}
	out := append([]agents.Session(nil), ss...)
	for i := range out {
		if slices.Contains(f.stops, out[i].ID) {
			out[i].State, out[i].PID = agents.StateStopped, 0
		}
	}
	return out, nil
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
	if _, err := newDispatcher(t, dir, l, nil).Tick(context.Background()); err != nil {
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
	d := newDispatcher(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg, _ := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 0 {
		t.Fatal("一覧に出る前に登録した (session id と pid が無い)")
	}
	d.List = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}, nil
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
	d := newDispatcher(t, dir, l, []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}})
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

// 差し戻したカードは、回答と同じく新しく起動せず同じ session を再開し、直してほしい点を渡す (issue 446)。
func TestReworkedCardResumesSameSession(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}})
	for range 2 { // 起動 → 登録
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []store.Request{{Kind: "review", CardID: "C-001"}, {Kind: "rework", CardID: "C-001", Rework: "テストを足して"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.starts) != 1 || len(l.resumes) != 1 || !strings.HasPrefix(l.resumes[0], "id-pc-c-001:"+store.ReworkPrefix+"テストを足して") {
		t.Fatalf("同じ session を直してほしい点つきで再開するはず: starts=%v resumes=%v", l.starts, l.resumes)
	}
	if c := states(t, dir)["C-001"]; c.State != card.Running || c.Resume != "" {
		t.Fatalf("再開の後: %v Resume=%q", c.State, c.Resume)
	}
}

// 起動に失敗したカードは分解済みのまま残し、失敗を履歴に書く (作業中にしない)。
func TestLaunchFailureKeepsCardPlanned(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	notes, err := newDispatcher(t, dir, &fakeLauncher{fail: true}, nil).Tick(context.Background())
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
	for _, want := range []string{"C-007", "AskUserQuestion", "pro-con card ask C-007", "pro-con card review C-007", "pro-con card run C-007 --", "master へは push しない", "色を直して", "pro-con card attach C-007 ", "結果が届くまで ask しない"} {
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
// dispatcher 自身の再開の後なら、dispatcher が起動し直していても (メモリの印が無くても) 書き直す。
func TestRegisterRefusesPidChangeNotCausedByDispatcher(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDispatcher(t, dir, l, nil)
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
	if len(reg) != 1 || reg[0].PID != 42 || !strings.Contains(joinNotes(notes), "外から操作された疑い") {
		t.Fatalf("外で pid が変わった session の記録を書き直した / 知らせない: %+v %v", reg, notes)
	}
	// dispatcher 自身が再開した後なら書き直す。再開の後に dispatcher が起動し直した形 (新しい Dispatcher) でも同じ。
	// (外で変わった pid のままでは再開しない = prepare が拒むので、外の操作が終わって記録の pid に戻った形から始める)
	ss[0].PID = 42
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
	d2 := newDispatcher(t, dir, l, ss)
	d2.Now = d.Now
	if _, err := d2.Tick(context.Background()); err != nil { // 登録
		t.Fatal(err)
	}
	reg, _ = live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 1 || reg[0].PID != 100 || len(l.resumes) != 1 {
		t.Fatalf("dispatcher が再開した後の pid で書き直していない: %+v resumes=%v", reg, l.resumes)
	}
}

// 最後の起動より前に始まっている session (同じ短い id の別の session の疑い) は、最初の登録でも取り込まない。
func TestRegisterRefusesOlderSession(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	if _, err := d.Tick(context.Background()); err != nil { // 起動 (LaunchedAt = t0)
		t.Fatal(err)
	}
	d.List = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{ID: "id-pc-c-001", SessionID: "OLD", PID: 7, Kind: "background", StartedAt: t0.Add(-time.Hour).UnixMilli()}}, nil
	}
	notes, _ := d.Tick(context.Background())
	reg, _ := live.LoadRegistry(filepath.Join(dir, live.RegistryFile))
	if len(reg) != 0 || !strings.Contains(joinNotes(notes), "起動・再開より前") {
		t.Fatalf("起動より前の session を取り込んだ: %+v %v", reg, notes)
	}
}

// claude が起動に「失敗」と返っても session が立っていることがある。次の Tick は一覧で確かめて取り込み、2 本目を起動しない。
func TestFailedLaunchThatActuallyStartedIsAdopted(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{fail: true}
	d := newDispatcher(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.fail = false
	d.List = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{ID: "ab12", SessionID: "S1", PID: 5, Name: "pc-c-001", Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}, nil
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
	d := newDispatcher(t, dir, l, nil)
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

// claude が起動・再開を受け付けない (rc≠0 がすぐ返る) 失敗が launchRejectLimit 回続いたら、分解済みで回し続けず人の番へ回して理由を書く (462)。
// 回答すると同じカードをもう一度起動・再開する。立っているかもしれない失敗 (fail) は数えない (取り込む前に人の番へ回さない)。
func TestRepeatedRejectedLaunchGoesToHuman(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 2)
	l := &fakeLauncher{reject: true}
	d := newDispatcher(t, dir, l, nil)
	d.Limit = 1
	at := t0
	for i := range launchRejectLimit {
		d.Now = func() time.Time { return at }
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if c := states(t, dir)["C-001"]; i < launchRejectLimit-1 && c.State != card.Planned {
			t.Fatalf("%d 回目で人の番へ回した: %v", i+1, c.State)
		}
		at = at.Add(launchGrace + time.Second)
	}
	c := states(t, dir)["C-001"]
	if c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed || c.Launching != "" || !strings.Contains(c.Wait.Question, "unknown option") {
		t.Fatalf("%d 回すぐ失敗しても人の番へ回らない: %v %v Launching=%q %q", launchRejectLimit, c.State, c.Wait.Kind, c.Launching, c.Wait.Question)
	}
	tries := l.startTries
	d.Now = func() time.Time { return at }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := l.startTries - tries; got != 1 || states(t, dir)["C-002"].Launching == "" {
		t.Fatalf("人の番へ回したカードが枠を空けない (次のカードを起動しない): 起動の試行 %d", got)
	}
	l.reject = false
	if _, err := store.Submit(dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "- 直した"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(dir, at); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; c.State != card.Planned || c.Rejects != 0 {
		t.Fatalf("回答で分解済みへ戻らない / 回数が残った: %v Rejects=%d", c.State, c.Rejects)
	}

	dir = t.TempDir()
	planned(t, dir, 1)
	l = &fakeLauncher{fail: true}
	d = newDispatcher(t, dir, l, nil)
	for i := range launchRejectLimit + 1 {
		d.Now = func() time.Time { return t0.Add(time.Duration(i) * (launchGrace + time.Second)) }
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if c := states(t, dir)["C-001"]; c.State != card.Planned {
		t.Fatalf("立っているかもしれない失敗まで人の番へ回した: %v", c.State)
	}
}

// 状態の置き場を作り直すとカード ID は C-001 から振り直されるので、前の世代の PG の worktree (<repo>/.claude/worktrees/pc-c-001) が
// 残っていると claude -w はそれを黙って使う (465)。初回の起動の前に在れば起動せずに人の番へ回し、消して回答すると起動する。
func TestStartRefusesLeftoverWorktree(t *testing.T) {
	dir, repo := t.TempDir(), t.TempDir()
	wt := filepath.Join(repo, ".claude", "worktrees", "pc-c-001")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	d.Repos = map[string]string{"dotfiles": repo}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := states(t, dir)["C-001"]
	if l.startTries != 0 || c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed || c.Launching != "" || !strings.Contains(c.Wait.Question, wt) {
		t.Fatalf("前の世代の worktree の上で起動した / 人の番へ回らない: 起動の試行 %d %v %v Launching=%q %q", l.startTries, c.State, c.Wait.Kind, c.Launching, c.Wait.Question)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "消した"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(dir, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.starts) != 1 {
		t.Fatalf("worktree を消して回答しても起動しない: %v", l.starts)
	}
}

// 結果の分からない起動 (claude が worktree を作ってから失敗と返した) の起動し直しでは、在る worktree はこのカードが作ったものなので止めない (465)。
func TestRestartAfterUnknownLaunchKeepsOwnWorktree(t *testing.T) {
	dir, repo := t.TempDir(), t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{fail: true}
	d := newDispatcher(t, dir, l, nil)
	d.Repos = map[string]string{"dotfiles": repo}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "worktrees", "pc-c-001"), 0o755); err != nil {
		t.Fatal(err)
	}
	l.fail = false
	d.Now = func() time.Time { return t0.Add(launchGrace + time.Second) }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; len(l.starts) != 1 || c.State != card.Running {
		t.Fatalf("自分の起動が作った worktree で起動し直さない: %v %v", l.starts, c.State)
	}
}

// 再開を claude が受け付けずに人の番へ回ったカードに回答しても、まだ渡せていない文 (差し戻し) は消えず、回答と一緒に同じ session へ届く (462)。
func TestRejectedResumeKeepsUndeliveredTextThroughAnswer(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}})
	for range 2 { // 起動 → 登録
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []store.Request{{Kind: "review", CardID: "C-001"}, {Kind: "rework", CardID: "C-001", Rework: "テストを足す"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	l.reject = true
	at := t0.Add(time.Hour) // 一覧の session (t0 の 1 秒後に始まった) を、再開の印より後に立った session として取り込まないように
	for range launchRejectLimit {
		d.Now = func() time.Time { return at }
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		at = at.Add(launchGrace + time.Second)
	}
	if c := states(t, dir)["C-001"]; c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed {
		t.Fatalf("再開の拒否が続いても人の番へ回らない: %v %v", c.State, c.Wait.Kind)
	}
	l.reject = false
	if _, err := store.Submit(dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "- trust した"}); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return at }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := l.resumes[len(l.resumes)-1]
	if c := states(t, dir)["C-001"]; c.State != card.Running || !strings.Contains(last, "テストを足す") || !strings.Contains(last, "- trust した") {
		t.Fatalf("回答で差し戻しの文が消えた / 再開しない: %v %q", c.State, last)
	}
}

// 拒否の回数は「続いた」ものだけ: 拒否以外の失敗・起動の成功で 0 に戻る (前の拒否を持ち越して早く人の番へ回さない)。
func TestRejectCountResetsOnOtherOutcomes(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{reject: true}
	d := newDispatcher(t, dir, l, nil)
	at := t0
	tick := func() {
		t.Helper()
		d.Now = func() time.Time { return at }
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		at = at.Add(launchGrace + time.Second)
	}
	for range launchRejectLimit - 1 {
		tick()
	}
	l.reject, l.fail = false, true
	tick()
	if c := states(t, dir)["C-001"]; c.Rejects != 0 {
		t.Fatalf("拒否以外の失敗で回数が戻らない: %d", c.Rejects)
	}
	l.reject, l.fail = true, false
	tick()
	l.reject = false
	tick()
	if c := states(t, dir)["C-001"]; c.State != card.Running || c.Rejects != 0 {
		t.Fatalf("起動の成功で回数が戻らない: %v %d", c.State, c.Rejects)
	}
}

// 再開の前に、今の一覧で短い id が前の session を指しているか確かめる。別の session を指していたら止めない (claude stop を撃たない)。
func TestResumeRefusesWhenShortIDPointsElsewhere(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDispatcher(t, dir, l, nil)
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
	if len(l.resumes) != 0 || !strings.Contains(joinNotes(notes), "別の session") {
		t.Fatalf("別の session を指す短い id で再開した: %v %v", l.resumes, notes)
	}
}

// session の一覧を取れない Tick は起動も再開もしない (立っている session を見落として増やさない)。
func TestNoDispatchWithoutSessionList(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	d.List = func(context.Context) ([]agents.Session, error) { return nil, errors.New("claude agents が落ちた") }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.starts) != 0 {
		t.Fatalf("一覧を取れないのに起動した: %v", l.starts)
	}
}

// dispatcher は 2 つ起動しない (記録の書き手は 1 つだけ)。1 つ目が外したら取れる。
func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrRunning) {
		t.Fatalf("2 つ目の dispatcher がロックを取れた: %v", err)
	}
	unlock()
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("外した後にロックを取れない: %v", err)
	}
	again()
}

// setCard は記録のカードを直接書き換える (dispatcher の途中の状態を作る)。
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
	d := newDispatcher(t, dir, l, []agents.Session{{ID: "cd34", SessionID: "S2", PID: 6, Name: "pc-c-002", Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-002", StartedAt: t0.Add(time.Second).UnixMilli()}})
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
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDispatcher(t, dir, l, nil)
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
	if len(l.resumes) != 0 {
		t.Fatalf("一覧に無い session をすぐ再開した (Claude Code の自動の再開と重なる): %v", l.resumes)
	}
	d.Now = func() time.Time { return t0.Add(restartWait + time.Second) }
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
	d := newDispatcher(t, dir, &fakeLauncher{}, []agents.Session{{ID: "old1", SessionID: "S0", PID: 3, Name: "pc-c-001", Kind: "background", StartedAt: t0.Add(-time.Minute).UnixMilli()}})
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
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d := newDispatcher(t, dir, l, nil)
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

// crashRig は作業中・登録済みの PG を 1 本用意し、transcript の再開の文を差し替えられる形にする。
type crashRig struct {
	dir      string
	l        *fakeLauncher
	d        *Dispatcher
	ss       []agents.Session
	restarts []time.Time
}

func newCrashRig(t *testing.T) *crashRig {
	t.Helper()
	r := &crashRig{dir: t.TempDir(), l: &fakeLauncher{}}
	planned(t, r.dir, 1)
	r.ss = []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}
	r.d = newDispatcher(t, r.dir, r.l, nil)
	r.d.List = func(context.Context) ([]agents.Session, error) { return r.ss, nil }
	r.d.Transcript = func(string) (live.Transcript, error) { return live.Transcript{Restarts: r.restarts}, nil }
	for range 2 { // 起動 → 登録
		r.tick(t)
	}
	return r
}

func (r *crashRig) tick(t *testing.T) []eventlog.Event {
	t.Helper()
	notes, err := r.d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

// crash は PG のプロセスが落ちて Claude Code が自動で再開した形を作る (新しい pid + transcript の再開の文)。
func (r *crashRig) crash(at time.Time, pid int) {
	r.ss[0].PID, r.ss[0].StartedAt = pid, at.UnixMilli()
	r.restarts = append(r.restarts, at)
}

// 1 回落ちて自動で再開した PG は、記録を新しい pid で書き直して作業中のまま続ける (外から操作された疑いにしない)。
func TestCrashOnceReregisters(t *testing.T) {
	r := newCrashRig(t)
	r.crash(t0.Add(time.Minute), 43)
	notes := r.tick(t)
	reg, _ := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile))
	c := states(t, r.dir)["C-001"]
	if len(reg) != 1 || reg[0].PID != 43 || c.State != card.Running || len(c.Crashes) != 1 || len(r.l.stops) != 0 {
		t.Fatalf("1 回の自動の再開を取り込まない: reg=%+v %v crashes=%v stops=%v notes=%v", reg, c.State, c.Crashes, r.l.stops, notes)
	}
	if strings.Contains(joinNotes(notes), "外から操作された疑い") {
		t.Fatalf("自動の再開を外からの操作と知らせた: %v", notes)
	}
}

// 短い間に上限 (2 回) 落ちた PG は止め、カードを人間の回答待ち (WaitCrashed) にする。
func TestCrashTwiceStopsAndAsksHuman(t *testing.T) {
	r := newCrashRig(t)
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.d.Now = func() time.Time { return t0.Add(3 * time.Minute) }
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.stops) != 1 || r.l.stops[0] != "id-pc-c-001" || c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed {
		t.Fatalf("2 回落ちた PG を止めて人間に上げていない: stops=%v %v %v", r.l.stops, c.State, c.Wait.Kind)
	}
}

// 間が CrashWindow より空いた 2 回は数えない (長く走る PG が時々落ちるだけで止めない)。
func TestCrashesOutsideWindowDoNotStop(t *testing.T) {
	r := newCrashRig(t)
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	later := t0.Add(time.Minute + defaultCrashWindow + time.Minute)
	r.crash(later, 44)
	r.d.Now = func() time.Time { return later.Add(time.Second) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.stops) != 0 || c.State != card.Running {
		t.Fatalf("間の空いた 2 回で止めた: stops=%v %v", r.l.stops, c.State)
	}
}

// 止められなかったら作業中のまま残し、次の Tick でまた止めにいく。
func TestCrashStopRetriesOnFailure(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running {
		t.Fatalf("止められなかったのに回答待ちにした: %v", c.State)
	}
	r.l.stopFail = false
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.stops) != 1 || c.State != card.Waiting {
		t.Fatalf("次の Tick で止め直さない: stops=%v %v", r.l.stops, c.State)
	}
}

// 止めたカードに回答すると同じ session を再開し、前の回数ですぐ止め直さない。
func TestAnswerAfterCrashStopResumesWithoutRestop(t *testing.T) {
	r := newCrashRig(t)
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "続けて"}); err != nil {
		t.Fatal(err)
	}
	r.d.Now = func() time.Time { return t0.Add(3 * time.Minute) }
	r.tick(t) // 再開
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.resumes) != 1 || len(r.l.stops) != 1 || c.State != card.Running {
		t.Fatalf("回答の後に再開しない / 前の回数で止め直した: resumes=%v stops=%v %v", r.l.resumes, r.l.stops, c.State)
	}
}

// 再開した session の startedAt が元の開始時刻のまま (未実測。3f で測る) でも、前に数えた再開の文を次に落ちたときにまた数えない。
func TestCrashDoesNotRecountEarlierNote(t *testing.T) {
	r := newCrashRig(t)
	r.d.CrashLimit = 10 // 止めずに回数だけ見る
	orig := r.ss[0].StartedAt
	r.crash(t0.Add(time.Minute), 43)
	r.ss[0].StartedAt = orig
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.ss[0].StartedAt = orig
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(c.Crashes) != 2 {
		t.Fatalf("前に数えた再開の文をまた数えた: %v", c.Crashes)
	}
}

// watchRig は作業中・登録済みの PG を 1 本用意し、transcript の新しい出力の時刻を差し替えられる形にする。
func newWatchRig(t *testing.T) (*crashRig, *time.Time) {
	t.Helper()
	r := newCrashRig(t)
	var lastNew time.Time
	r.d.Transcript = func(string) (live.Transcript, error) { return live.Transcript{LastNew: lastNew}, nil }
	r.d.StallAfter = 10 * time.Minute
	return r, &lastNew
}

// 新しい出力が StallAfter の間出なければ停滞にし、新しい出力が出たら外す。
func TestWatchdogStallsAndRecovers(t *testing.T) {
	r, lastNew := newWatchRig(t)
	r.d.Now = func() time.Time { return t0.Add(9 * time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.Stalled {
		t.Fatal("閾値の前に停滞にした")
	}
	r.d.Now = func() time.Time { return t0.Add(11 * time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.Stalled {
		t.Fatal("閾値を過ぎても停滞にしない")
	}
	*lastNew = t0.Add(12 * time.Minute)
	r.d.Now = func() time.Time { return t0.Add(13 * time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.Stalled || !c.LastProgress.Equal(*lastNew) {
		t.Fatalf("新しい出力が出ても停滞のまま: stalled=%v LastProgress=%v", c.Stalled, c.LastProgress)
	}
}

// コマンドの実行中は、見込みの所要の 2 倍まで停滞にしない (長いテストを停滞と誤判定しない)。
func TestWatchdogUsesExecThreshold(t *testing.T) {
	r, _ := newWatchRig(t)
	setCard(t, r.dir, "C-001", func(c *card.Card) {
		c.Exec = card.Exec{Command: "make test", Since: t0, Expected: 20 * time.Minute}
	})
	r.d.Now = func() time.Time { return t0.Add(30 * time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.Stalled {
		t.Fatal("見込みの 2 倍 (40 分) の前に停滞にした")
	}
}

// 作業中の列を離れたカードは停滞の印を持たない (質問待ちのカードに停滞を出さない)。
func TestStallClearedWhenLeavingRunning(t *testing.T) {
	r, _ := newWatchRig(t)
	r.d.Now = func() time.Time { return t0.Add(11 * time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.Stalled {
		t.Fatal("前提: 停滞になっていない")
	}
	if _, err := store.Submit(r.dir, store.Request{Kind: "ask", CardID: "C-001", Question: "q"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Waiting || c.Stalled {
		t.Fatalf("質問待ちのカードに停滞の印が残った: %v %v", c.State, c.Stalled)
	}
}

// 再開は、pro-con の記録にある session の作業ディレクトリ (PG の worktree) で走らせる。
func TestResumeRunsInSessionCwd(t *testing.T) {
	r := newCrashRig(t) // 一覧の session に cwd を足してから登録し直す
	r.ss[0].Cwd = "/w/dotfiles/.claude/worktrees/pc-c-001"
	r.ss[0].PID = 50
	r.d.Now = func() time.Time { return t0 }
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.LaunchedAt = t0.Add(-time.Hour) }) // 登録し直しを通す (自分の再開の後の形)
	reg, _ := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile))
	reg[0].StartedAt = t0.Add(-2 * time.Hour)
	if err := live.Register(filepath.Join(r.dir, live.RegistryFile), reg[0]); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	r.tick(t)
	if len(r.l.cwds) != 1 || r.l.cwds[0] != "/w/dotfiles/.claude/worktrees/pc-c-001" {
		t.Fatalf("session の cwd で再開していない: %v", r.l.cwds)
	}
}

// 止められないまま時間の窓を過ぎても、止めるのを諦めない (止められたら回答待ちにする)。
func TestCrashStopKeepsTryingPastWindow(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t)
	r.l.stopFail = false
	r.d.Now = func() time.Time { return t0.Add(2*time.Minute + defaultCrashWindow + time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.stops) != 1 || c.State != card.Waiting || c.StopWanted {
		t.Fatalf("窓を過ぎたら止めるのをやめた: stops=%v %v StopWanted=%v", r.l.stops, c.State, c.StopWanted)
	}
}

// 落ち続けた PG の session が一覧に無い (または短い id が別の session を指す) なら、止めずに回答待ちにする (別の session を止めない)。
func TestCrashStopSkipsSessionNotOurs(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t) // 上限に達したが止められない (止める印が立つ)
	if c := states(t, r.dir)["C-001"]; !c.StopWanted {
		t.Fatal("前提: 止める印が立っていない")
	}
	r.l.stopFail = false
	r.ss[0].SessionID = "OTHER" // 自分の PG は消え、短い id を別の session が得た
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.stops) != 0 || c.State != card.Waiting {
		t.Fatalf("別の session を止めた / 回答待ちにしない: stops=%v %v", r.l.stops, c.State)
	}
}

// 結果の返っていないツール呼び出しがある間 (長いコマンドの実行中) は、通常の閾値を過ぎても停滞にしない。
func TestWatchdogWaitsForPendingTool(t *testing.T) {
	r, _ := newWatchRig(t)
	r.d.Transcript = func(string) (live.Transcript, error) { return live.Transcript{PendingSince: t0.Add(time.Minute)}, nil }
	r.d.Now = func() time.Time { return t0.Add(30 * time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.Stalled {
		t.Fatal("長いコマンドの実行中に停滞にした")
	}
	r.d.Now = func() time.Time { return t0.Add(longToolLimit + time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.Stalled {
		t.Fatal("longToolLimit を過ぎても停滞にしない")
	}
}

// 止める印は、作業中の列を離れたら外れる (質問 → 回答 → 再開した PG を、前の印ですぐ止めない)。
func TestStopWantedClearedByAskAndResume(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.StopWanted {
		t.Fatal("前提: 止める印が立っていない")
	}
	r.l.stopFail = false
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	r.d.Now = func() time.Time { return t0.Add(3 * time.Minute) }
	r.tick(t) // 質問・回答を適用して再開
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.resumes) != 1 || len(r.l.stops) != 0 || c.State != card.Running {
		t.Fatalf("前の印で再開した PG を止めた: resumes=%v stops=%v %v", r.l.resumes, r.l.stops, c.State)
	}
}

// 落ち続けた PG の session が一覧に無ければ、消えたのを最初に見てから restartWait は待つ (Claude Code の自動の再開の途中かもしれない)。
// 過ぎても無ければ、止めずに回答待ちにし、止めなかったことを履歴に書く。
func TestCrashStopWaitsForAutoRestartWhenUnlisted(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t) // 止める印が立つ
	r.l.stopFail = false
	r.ss = nil // 死んで一覧から消えた
	r.d.Now = func() time.Time { return t0.Add(2*time.Minute + 30*time.Second) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running {
		t.Fatalf("自動の再開を待たずに回答待ちにした: %v", c.State)
	}
	r.d.Now = func() time.Time { return t0.Add(2*time.Minute + 30*time.Second + restartWait + time.Second) } // 消えたのを最初に見てから 1 分
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.State != card.Waiting || !strings.Contains(c.Wait.Question, "一覧に無い") || len(r.l.stops) != 0 {
		t.Fatalf("待った後に回答待ちにしない / 止めたと書いた: %v %q stops=%v", c.State, c.Wait.Question, r.l.stops)
	}
}

// 前の session の作業ディレクトリが記録に無ければ再開しない (dispatcher の cwd で再開すると別の tree を書く)。
func TestResumeRefusesWithoutCwd(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	ss := []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}} // cwd 無し
	d := newDispatcher(t, dir, l, nil)
	d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
	for range 2 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(dir, q); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.resumes) != 0 || !strings.Contains(joinNotes(notes), "作業ディレクトリ") {
		t.Fatalf("cwd の無い session を再開した: %v %v", l.resumes, notes)
	}
}

// 記録の行に cwd が無く (前の版が書いた等)、一覧に cwd が出ていれば、行を書き直して再開できるようにする。
func TestRegisterFillsMissingCwd(t *testing.T) {
	r := newCrashRig(t)
	regPath := filepath.Join(r.dir, live.RegistryFile)
	reg, _ := live.LoadRegistry(regPath)
	reg[0].Cwd = ""
	if err := live.Register(regPath, reg[0]); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if reg, _ = live.LoadRegistry(regPath); len(reg) != 1 || reg[0].Cwd != "/w/dotfiles/.claude/worktrees/pc-c-001" {
		t.Fatalf("cwd の無い行を埋め直さない: %+v", reg)
	}
}

// 本物の claude --bg --resume は、元の session を続けずに別の session id・別の短い id の session を立てる (427 の 3f で実測)。
// dispatcher 自身の再開が返した短い id の session は、カードの行をそれに置き換えて登録する (前の行は消す)。
func TestResumeWithNewSessionReplacesRow(t *testing.T) {
	r := newCrashRig(t)
	r.l.resumeID = "db1e"
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	t1 := t0.Add(time.Minute)
	r.d.Now = func() time.Time { return t1 }
	r.tick(t) // 再開
	r.ss = []agents.Session{{ID: "db1e", SessionID: "S2", PID: 60, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t1.Add(time.Second).UnixMilli()}}
	r.tick(t)
	reg, _ := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile))
	if c := states(t, r.dir)["C-001"]; len(reg) != 1 || reg[0].SessionID != "S2" || reg[0].ID != "db1e" || c.Session != "db1e" {
		t.Fatalf("再開で新しくなった session を登録しない / 前の行が残る: %+v Session=%q", reg, c.Session)
	}
}

// 再開に「失敗」と返っても、同じ作業ディレクトリで印の後に始まった session が立っていれば取り込む (再開は別の session id になる)。
func TestFailedResumeAdoptsNewSessionByCwd(t *testing.T) {
	r := newCrashRig(t)
	r.l.resumeFail = true
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	t1 := t0.Add(time.Minute)
	r.d.Now = func() time.Time { return t1 }
	r.tick(t)
	r.ss = []agents.Session{{ID: "db1e", SessionID: "S2", PID: 60, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t1.Add(time.Second).UnixMilli()}}
	r.d.Now = func() time.Time { return t1.Add(launchGrace + time.Second) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.resumes) != 1 || c.State != card.Running || c.Session != "db1e" {
		t.Fatalf("別の session id で立った再開を取り込まずに再開し直した: resumes=%v %v %q", r.l.resumes, c.State, c.Session)
	}
}

// 質問待ちの間に PG が落ちて自動で再開しても、記録を新しい pid で書き直す (作業中の列に限らない。427 の 3f で実測)。
func TestCrashWhileWaitingReregisters(t *testing.T) {
	r := newCrashRig(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "ask", CardID: "C-001", Question: "q"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	reg, _ := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile))
	if c := states(t, r.dir)["C-001"]; len(reg) != 1 || reg[0].PID != 43 || c.State != card.Waiting {
		t.Fatalf("質問待ちの間の自動の再開を取り込まない: %+v %v", reg, c.State)
	}
}

// 起動できない理由が変わらなければ、Tick ごとに履歴を足さない。
func TestRepeatedFailureDoesNotGrowHistory(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.Repos = map[string]string{} // repo の場所が設定に無い
	for range 5 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	c := states(t, dir)["C-001"]
	n := 0
	for _, e := range c.History {
		if strings.Contains(e.Text, "場所が設定に無い") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("同じ理由を %d 回履歴に書いた", n)
	}
}

// 再開の結果が分からないときの取り込みは、pro-con が起動する形 (bg・PG の worktree・印の後に始まった) の session だけ。
// 人間が同じ worktree で開いた対話の session / 別の worktree / 印より前に始まった session は取り込まない。
func TestAdoptResumeRejectsOthers(t *testing.T) {
	wt := "/w/dotfiles/.claude/worktrees/pc-c-001"
	for _, tc := range []struct {
		name string
		s    agents.Session
	}{
		{"対話", agents.Session{ID: "hum1", SessionID: "H", PID: 70, Kind: "interactive", Cwd: wt}},
		{"別の worktree", agents.Session{ID: "oth1", SessionID: "O", PID: 71, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-002"}},
		{"印より前", agents.Session{ID: "old1", SessionID: "P", PID: 72, Kind: "background", Cwd: wt}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			s := tc.s
			s.StartedAt = t1.Add(time.Second).UnixMilli()
			if tc.name == "印より前" {
				s.StartedAt = t1.Add(-time.Second).UnixMilli()
			}
			r.ss = []agents.Session{s} // 元の session は一覧から消えた (候補だけにする。元の session が先に当たると退行が隠れる)
			r.tick(t)
			if c := states(t, r.dir)["C-001"]; c.Session == s.ID {
				t.Fatalf("%s の session を取り込んだ: %q", tc.name, c.Session)
			}
		})
	}
}

// 短い id が使い回されて別の session を指している (dispatcher の再開の後ではない) なら、カードの行を置き換えない。
func TestRegisterDoesNotReplaceOnReusedShortID(t *testing.T) {
	r := newCrashRig(t) // 行は LaunchedAt 以降に登録済み (自分の再開の後ではない)
	r.ss[0].SessionID, r.ss[0].PID = "OTHER", 80
	r.tick(t)
	reg, _ := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile))
	if len(reg) != 1 || reg[0].SessionID != "S1" {
		t.Fatalf("使い回された短い id の session で行を置き換えた: %+v", reg)
	}
}

// 登録は bg の session だけ (同じ短い id でも対話の session は取り込まない)。
func TestRegisterIgnoresInteractive(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.List = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{ID: "id-pc-c-001", SessionID: "H", PID: 9, Kind: "interactive", StartedAt: t0.Add(time.Second).UnixMilli()}}, nil
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reg, _ := live.LoadRegistry(filepath.Join(dir, live.RegistryFile)); len(reg) != 0 {
		t.Fatalf("対話の session を登録した: %+v", reg)
	}
}

// 起動・再開に「失敗と返った」は、同じ文でも毎回履歴に残す (claude を走らせた回数 = 立っているかもしれない session の数)。
func TestFailedLaunchIsAlwaysRecorded(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	d := newDispatcher(t, dir, &fakeLauncher{fail: true}, nil)
	for i := range 2 {
		d.Now = func() time.Time { return t0.Add(time.Duration(i) * (launchGrace + time.Second)) }
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for _, e := range states(t, dir)["C-001"].History {
		if strings.Contains(e.Text, "失敗したと返った") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("走らせた 2 回の失敗のうち %d 回しか残っていない", n)
	}
}

// 自動の再開で記録を書き直すとき、一覧の cwd が repo root (落ちている間の形) でも、記録の worktree の cwd を残す。
func TestCrashKeepsRecordedCwd(t *testing.T) {
	r := newCrashRig(t)
	r.crash(t0.Add(time.Minute), 43)
	r.ss[0].Cwd = "/w/dotfiles"
	r.tick(t)
	reg, _ := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile))
	if len(reg) != 1 || reg[0].PID != 43 || reg[0].Cwd != "/w/dotfiles/.claude/worktrees/pc-c-001" {
		t.Fatalf("記録の cwd を repo root で上書きした: %+v", reg)
	}
}

// 起動の結果を確かめるとき、名前が同じでも cwd がこのカードの repo の worktree でなければ取り込まない
// (別の状態の置き場の dispatcher が同じカード ID で立てた PG)。
func TestAdoptStartRequiresOwnWorktree(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	setCard(t, dir, "C-001", func(c *card.Card) { c.Launching, c.LaunchedAt = "起動", t0 })
	d := newDispatcher(t, dir, &fakeLauncher{}, []agents.Session{{ID: "zz99", SessionID: "Z", PID: 3, Name: "pc-c-001", Kind: "background",
		Cwd: "/w/other-repo/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}})
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; c.Session == "zz99" {
		t.Fatal("別の repo の worktree で立った同名の PG を取り込んだ")
	}
}

// 回答で再開するとき、session の pid が記録と違えば (外から操作された疑い) 止めも再開もしない。
func TestResumeRefusesWhenPidDiffers(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].PID = 99 // 再開の文なしに pid が変わった
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	notes := r.tick(t)
	if len(r.l.resumes) != 0 || len(r.l.stops) != 0 || !strings.Contains(joinNotes(notes), "pid が記録") {
		t.Fatalf("pid の違う session を止めた / 再開した: resumes=%v stops=%v %v", r.l.resumes, r.l.stops, notes)
	}
}

// 短い id は一致するのに kind が background でない session は、取り込まずに知らせる (claude の版で値が変わった疑い)。
func TestRegisterWarnsOnUnexpectedKind(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Kind = ""
	notes := r.tick(t)
	if !strings.Contains(joinNotes(notes), "background ではない") {
		t.Fatalf("kind の違う session を黙って飛ばした: %v", notes)
	}
}

// 止める印が立った後、再開の文なしに pid だけが変わった session (外から操作された疑い) は止めない。
func TestCrashStopSkipsPidMismatch(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.StopWanted {
		t.Fatal("前提: 止める印が立っていない")
	}
	r.l.stopFail = false
	r.ss[0].PID = 99 // 再開の文は増えていない
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; len(r.l.stops) != 0 || c.State != card.Waiting || !strings.Contains(c.Wait.Question, "一致しない") {
		t.Fatalf("pid の違う session を止めた / 止めなかったことを書いて回答待ちにしない: stops=%v %v %q", r.l.stops, c.State, c.Wait.Question)
	}
}

// 止める印が立った後、自分の PG が落ちている最中 (一覧に pid 無しで出る) なら、最後に落ちてから restartWait は待つ
// (「記録と一致しない」として回答待ちへ送ると、自動の再開で戻った PG が回答待ちのカードの下で走り続ける)。
func TestCrashStopWaitsWhileDead(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t)
	r.l.stopFail = false
	r.ss[0].PID = 0 // 落ちて自動の再開を待っている
	r.d.Now = func() time.Time { return t0.Add(2*time.Minute + 10*time.Second) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running {
		t.Fatalf("落ちている最中の自分の PG を待たずに回答待ちへ送った: %v %q", c.State, c.Wait.Question)
	}
}

// 回答の後、前の session が落ちている最中 (pid 無し) なら restartWait は再開しない。過ぎたら止めずに再開する。
func TestResumeWaitsWhileDead(t *testing.T) {
	r := newCrashRig(t)
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	r.ss[0].PID = 0
	r.tick(t)
	if len(r.l.resumes) != 0 {
		t.Fatalf("落ちている最中に再開した (自動の再開と重なる): %v", r.l.resumes)
	}
	r.d.Now = func() time.Time { return t0.Add(restartWait + time.Second) }
	r.tick(t)
	if len(r.l.resumes) != 1 || r.l.resumes[0] != ":a" {
		t.Fatalf("待った後に止めずに再開しない: %v", r.l.resumes)
	}
}

// パスは symlink を解決して比べる (設定の repo のパスが symlink を含んでも、一覧の解決済みの cwd と一致する)。空は何とも一致しない。
func TestSamePath(t *testing.T) {
	real := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(real, ".claude", "worktrees", "pc-c-001"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(filepath.Join(real, ".claude", "worktrees", "pc-c-001"))
	if !samePath(filepath.Join(link, ".claude", "worktrees", "pc-c-001"), resolved) {
		t.Fatal("symlink を含むパスと解決済みのパスを別の場所と読んだ")
	}
	if samePath("", "") || samePath("/a", "") {
		t.Fatal("空のパスを一致と読んだ")
	}
}

// repo の場所が分からなければ、名前が同じでも起動の取り込みをしない (空の cwd どうしで一致させない)。
func TestAdoptStartNeedsRepoPath(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	setCard(t, dir, "C-001", func(c *card.Card) { c.Launching, c.LaunchedAt = "起動", t0 })
	d := newDispatcher(t, dir, &fakeLauncher{}, []agents.Session{{ID: "nn11", SessionID: "N", PID: 3, Name: "pc-c-001", Kind: "background", StartedAt: t0.Add(time.Second).UnixMilli()}})
	d.Repos = map[string]string{}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; c.Session == "nn11" {
		t.Fatal("repo の場所が分からないのに名前だけで取り込んだ")
	}
}

// 再開から長く走った後に落ちた PG も、落ちたのを見てから restartWait は待つ (直前の再開の時刻から数えると、待たずに回答待ちへ送る)。
func TestCrashStopWaitsFromDeathNotLastRestart(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t) // 止める印が立つ
	r.l.stopFail = false
	r.ss[0].PID = 0 // 最後の再開から 1 分半走ってから落ちた
	r.d.Now = func() time.Time { return t0.Add(3*time.Minute + 30*time.Second) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running {
		t.Fatalf("落ちたのを見た直後に回答待ちへ送った: %v %q", c.State, c.Wait.Question)
	}
	r.crash(t0.Add(3*time.Minute+45*time.Second), 45) // 自動の再開で戻る
	r.d.Now = func() time.Time { return t0.Add(3*time.Minute + 45*time.Second) }
	r.tick(t)
	if len(r.l.stops) != 1 {
		t.Fatalf("戻った自分の PG を止めない: stops=%v", r.l.stops)
	}
}

// 回答の後、前の session が落ちたのを見てから restartWait は再開しない (回答から 1 分過ぎていても。上限で再開が待たされた形)。
func TestResumeWaitsFromDeathNotAnswer(t *testing.T) {
	r := newCrashRig(t)
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	r.d.Limit = 0
	r.tick(t) // 回答 (Since = t0)。上限で再開は待たされる
	if c := states(t, r.dir)["C-001"]; c.State != card.Planned || len(r.l.resumes) != 0 {
		t.Fatalf("前提: 回答で分解済みに戻り、まだ再開していない: %v %v", c.State, r.l.resumes)
	}
	r.d.Limit = 2
	r.ss[0].PID = 0 // 回答から 2 分後、前の session が落ちている
	r.d.Now = func() time.Time { return t0.Add(2 * time.Minute) }
	r.tick(t)
	if len(r.l.resumes) != 0 {
		t.Fatalf("落ちたのを見た直後に再開した (自動の再開と重なる): %v", r.l.resumes)
	}
}

// 起動の取り込みは、設定の repo のパスが symlink を含んでも、一覧の解決済みの cwd と照らせる (二重起動にしない)。
func TestAdoptStartWithSymlinkedRepo(t *testing.T) {
	real := filepath.Join(t.TempDir(), "repo")
	wt := filepath.Join(real, ".claude", "worktrees", "pc-c-001")
	if err := os.MkdirAll(wt, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(wt)
	dir := t.TempDir()
	planned(t, dir, 1)
	setCard(t, dir, "C-001", func(c *card.Card) { c.Launching, c.LaunchedAt = "起動", t0 })
	d := newDispatcher(t, dir, &fakeLauncher{}, []agents.Session{{ID: "sy11", SessionID: "Y", PID: 3, Name: "pc-c-001", Kind: "background", Cwd: resolved, StartedAt: t0.Add(time.Second).UnixMilli()}})
	d.Repos = map[string]string{"dotfiles": link}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; c.Session != "sy11" {
		t.Fatalf("symlink を含む repo のパスで、立っていた自分の PG を取り込めない: %q", c.Session)
	}
}

// 一度落ちて戻った PG の「落ちたのを見た時刻」は、戻ったのを見たら外す。後でまた落ちたときは、その時刻から待つ。
func TestDeadSinceClearedWhenAlive(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.ss[0].PID = 0 // 一度落ちる
	r.d.Now = func() time.Time { return t0.Add(time.Minute) }
	r.tick(t)
	r.crash(t0.Add(time.Minute+15*time.Second), 43) // 戻る (1 回目)
	r.d.Now = func() time.Time { return t0.Add(time.Minute + 15*time.Second) }
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44) // 2 回目。止めに行くが失敗 (印が立つ)
	r.d.Now = func() time.Time { return t0.Add(2 * time.Minute) }
	r.tick(t)
	r.l.stopFail = false
	r.ss[0].PID = 0 // 5 分後にまた落ちる
	r.d.Now = func() time.Time { return t0.Add(7 * time.Minute) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running {
		t.Fatalf("前に落ちた時刻が残っていて、今落ちた PG を待たずに回答待ちへ送った: %v %q", c.State, c.Wait.Question)
	}
}

// 作業中の PG の session が自動の再開なしに一覧から消えたら (外からの claude stop・再起動)、消えたのを見てから restartWait は待ち、
// 過ぎても戻らなければ分解済みへ戻して同じ session を再開する (作業中のまま枠を占め続けない = 458)。戻したことは履歴と出来事に書く。
func TestVanishedRunningCardResumes(t *testing.T) {
	r := newCrashRig(t)
	r.ss = nil // 一覧から消えた (再開の文は出ない)
	r.d.Now = func() time.Time { return t0.Add(time.Minute) }
	r.tick(t)
	r.d.Now = func() time.Time { return t0.Add(time.Minute + restartWait - time.Second) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || len(r.l.resumes) != 0 {
		t.Fatalf("自動の再開を待たずに戻した: %v resumes=%v", c.State, r.l.resumes)
	}
	r.d.Now = func() time.Time { return t0.Add(time.Minute + restartWait + time.Second) }
	notes := r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.resumes) != 1 || r.l.resumes[0] != ":"+resumeAfterVanish || len(r.l.stops) != 0 || c.State != card.Running {
		t.Fatalf("消えた PG を分解済みへ戻して同じ session を止めずに再開しない: resumes=%v stops=%v %v", r.l.resumes, r.l.stops, c.State)
	}
	found := false
	for _, h := range c.History {
		found = found || strings.Contains(h.Text, "一覧から消えて戻らない")
	}
	var kinds []string
	for _, n := range notes {
		if n.Card == "C-001" && strings.Contains(n.Reason, "一覧から消えて戻らない") {
			kinds = append(kinds, n.Kind)
		}
	}
	if !found || !slices.Equal(kinds, []string{eventlog.KindCrash}) {
		t.Fatalf("戻したことを履歴 / 出来事に書かない: history=%+v notes=%v", c.History, notes)
	}
}

// 消えた PG を再開した直後 (まだ一覧に出ない) に、前に消えたのを見た時刻で再開し直さない
// (再開が同じ短い id を返すと、記録の前の行がそのまま当たる。再開で短い id が変わるかは未実測)。
func TestVanishedResumeDoesNotRepeatBeforeListed(t *testing.T) {
	r := newCrashRig(t)
	r.l.resumeID = "id-pc-c-001"
	r.ss = nil
	r.d.Now = func() time.Time { return t0.Add(time.Minute) }
	r.tick(t)
	r.d.Now = func() time.Time { return t0.Add(time.Minute + restartWait + time.Second) }
	r.tick(t) // 戻して再開
	r.d.Now = func() time.Time { return t0.Add(time.Minute + restartWait + 2*time.Second) }
	r.tick(t)
	if len(r.l.resumes) != 1 {
		t.Fatalf("再開した直後に前の時刻で再開し直した (同じ session が 2 本立つ): %v", r.l.resumes)
	}
}

// 短い id が一覧から消えても、同じ session id の session が生きていれば消えたと読まない (再開すると同じ session が 2 本立つ)。
func TestLiveSessionUnderOtherShortIDIsNotVanished(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].ID = "other-short-id"
	r.d.Now = func() time.Time { return t0.Add(time.Minute) }
	r.tick(t)
	r.d.Now = func() time.Time { return t0.Add(time.Minute + restartWait + time.Second) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || len(r.l.resumes) != 0 {
		t.Fatalf("生きている session を消えたと読んで再開した: %v resumes=%v", c.State, r.l.resumes)
	}
}

// 落ちた回数が上限に達した PG の session が一覧から消えたときは、今までどおり回答待ちにする (消えた PG として再開し直さない)。
func TestVanishedAfterCrashLimitStillAsksHuman(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t) // 止める印が立つ
	r.l.stopFail = false
	r.ss = nil
	r.d.Now = func() time.Time { return t0.Add(2*time.Minute + 30*time.Second) }
	r.tick(t)
	r.d.Now = func() time.Time { return t0.Add(2*time.Minute + 30*time.Second + restartWait + time.Second) }
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed || len(r.l.resumes) != 0 || c.Resume != "" {
		t.Fatalf("落ち続けて消えた PG を回答待ちにせず再開へ回した: %v %v resumes=%v Resume=%q", c.State, c.Wait.Kind, r.l.resumes, c.Resume)
	}
}

// vanish は一覧から PG を消し、消えたのを見る Tick と restartWait を過ぎた Tick を回す (消えた PG を戻して再開するところまで)。
// 再開した PG は一覧に戻す (next の短い id)。
func (r *crashRig) vanish(t *testing.T, at time.Time, next string) {
	t.Helper()
	keep := r.ss[0]
	r.ss = nil
	r.d.Now = func() time.Time { return at }
	r.tick(t)
	r.d.Now = func() time.Time { return at.Add(restartWait + time.Second) }
	r.tick(t)
	// 本物の再開は別の session id の session を立てる (427 の 3f)
	keep.ID, keep.SessionID, keep.PID, keep.StartedAt = next, "S-"+next, keep.PID+1, at.Add(restartWait+2*time.Second).UnixMilli()
	r.ss = []agents.Session{keep}
	r.d.Now = func() time.Time { return at.Add(restartWait + 3*time.Second) }
	r.tick(t) // 再開した session を登録する
}

// 消える → 再開 → また消える、を繰り返す PG は、落ちた回数と同じ上限 (CrashWindow の間に CrashLimit 回) で止め、
// 分解済みへ戻さずに回答待ち (人の番) へ送って理由を書く (上限の無い再開で利用枠を使い続けない)。
func TestVanishingRepeatedlyAsksHuman(t *testing.T) {
	r := newCrashRig(t)
	r.l.resumeID = "id-r1"
	r.vanish(t, t0.Add(time.Minute), "id-r1")
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || len(r.l.resumes) != 1 {
		t.Fatalf("前提: 1 回目は戻して再開する: %v resumes=%v", c.State, r.l.resumes)
	}
	r.vanish(t, t0.Add(5*time.Minute), "id-r2")
	c := states(t, r.dir)["C-001"]
	if len(r.l.resumes) != 1 || c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed || !strings.Contains(c.Wait.Question, "一覧から消えた") {
		t.Fatalf("消え続ける PG を上限で人の番へ送らない: resumes=%v %v %v %q", r.l.resumes, c.State, c.Wait.Kind, c.Wait.Question)
	}
}

// 消えた回数も CrashWindow の外のものは数えない (長く走る PG が時々消えるだけで止めない)。
func TestVanishesOutsideWindowDoNotStop(t *testing.T) {
	r := newCrashRig(t)
	r.l.resumeID = "id-r1"
	r.vanish(t, t0.Add(time.Minute), "id-r1")
	r.l.resumeID = "id-r2"
	r.vanish(t, t0.Add(time.Minute+defaultCrashWindow+time.Minute), "id-r2")
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || len(r.l.resumes) != 2 {
		t.Fatalf("間の空いた 2 回で止めた: %v resumes=%v", c.State, r.l.resumes)
	}
}

// 人の回答で再開したら、それより前に消えた回数は数えない (回答の後の 1 回目で、すぐ人の番へ戻さない)。
func TestAnswerResetsVanishCount(t *testing.T) {
	r := newCrashRig(t)
	r.l.resumeID = "id-r1"
	r.vanish(t, t0.Add(time.Minute), "id-r1")
	for _, q := range []store.Request{{Kind: "ask", CardID: "C-001", Question: "q"}, {Kind: "answer", CardID: "C-001", Answer: "a"}} {
		if _, err := store.Submit(r.dir, q); err != nil {
			t.Fatal(err)
		}
	}
	r.l.resumeID = "id-r2"
	r.d.Now = func() time.Time { return t0.Add(3 * time.Minute) }
	r.tick(t) // 回答で再開
	r.ss[0].ID, r.ss[0].SessionID, r.ss[0].StartedAt = "id-r2", "S-id-r2", t0.Add(3*time.Minute+time.Second).UnixMilli()
	r.d.Now = func() time.Time { return t0.Add(3*time.Minute + 2*time.Second) }
	r.tick(t)
	r.l.resumeID = "id-r3"
	r.vanish(t, t0.Add(4*time.Minute), "id-r3")
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || len(r.l.resumes) != 3 {
		t.Fatalf("回答の前に消えた回数で人の番へ戻した: %v %q resumes=%v", c.State, c.Wait.Question, r.l.resumes)
	}
}
