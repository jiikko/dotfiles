package dispatcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// ps の etime は [[dd-]hh:]mm:ss。command は空白を含んだまま行末まで。
func TestParsePS(t *testing.T) {
	ps := parsePS("  101     1    01:02 /bin/zsh -c eval 'a  b'\n 102 101 2-03:04:05 sleep 9\n壊れた行\n 103 1 xx:yy z\n")
	if len(ps) != 2 {
		t.Fatalf("読めた行が %d (期待 2): %+v", len(ps), ps)
	}
	if ps[0].PID != 101 || ps[0].PPID != 1 || ps[0].Elapsed != 62*time.Second || ps[0].Command != "/bin/zsh -c eval 'a  b'" {
		t.Fatalf("1 行目: %+v", ps[0])
	}
	if want := 2*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second; ps[1].Elapsed != want {
		t.Fatalf("日を含む etime: %v (期待 %v)", ps[1].Elapsed, want)
	}
}

// Claude Code の Bash の包み (2.1.282 で実測した形) からコマンドを取り出す。
const wrapper = `/opt/homebrew/bin/zsh -c source /Users/u/.claude/shell-snapshots/snapshot-zsh-1.sh 2>/dev/null || true && setopt NO_EXTENDED_GLOB 2>/dev/null || true && eval 'bash /Users/u/.claude/jobs/abc/tmp/mut.sh --q '\''x y'\''' < /dev/null && pwd -P >| /tmp/claude-64b9-cwd`

// PG の session の子孫を木の順に出す: 包んだ shell はコマンドの文で、その shell が起こした同じコマンドは重ねず、
// Claude Code の補助 (caffeinate) と、ほかの pid の木は出さない。
func TestProcessDoings(t *testing.T) {
	now := t0
	procs := []Proc{
		{PID: 10, PPID: 1, Elapsed: time.Hour, Command: "claude bg-spare"},
		{PID: 11, PPID: 10, Elapsed: 50 * time.Minute, Command: "caffeinate -i -t 300"},
		{PID: 12, PPID: 10, Elapsed: 14 * time.Minute, Command: wrapper},
		{PID: 13, PPID: 12, Elapsed: 14 * time.Minute, Command: "bash /Users/u/.claude/jobs/abc/tmp/mut.sh --q 'x y'"},
		{PID: 14, PPID: 13, Elapsed: 2 * time.Minute, Command: "bin/mutate-verify --name hints-ro"},
		{PID: 15, PPID: 10, Elapsed: 20 * time.Minute, Command: "/bin/zsh -c 形の違う包み"},
		{PID: 20, PPID: 1, Elapsed: time.Hour, Command: "claude (外の session)"},
		{PID: 21, PPID: 20, Elapsed: time.Minute, Command: "make test"},
	}
	got := processDoings(10, procs, now)
	var lines []string
	for _, d := range got {
		lines = append(lines, strings.Repeat(">", d.Depth)+d.Text+"@"+now.Sub(d.Since).String())
	}
	want := []string{
		"/bin/zsh -c 形の違う包み@20m0s",
		"bash /Users/u/.claude/jobs/abc/tmp/mut.sh --q 'x y'@14m0s",
		">bin/mutate-verify --name hints-ro@2m0s",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("子孫の並び:\n%s\n期待:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// 上限を超えたら、PG が直接起こしたものを残して後ろの子孫から落とす。
func TestProcessDoingsCapsDescendants(t *testing.T) {
	procs := []Proc{{PID: 2, PPID: 1, Elapsed: time.Hour, Command: "top"}}
	for i := range doingMax + 5 {
		procs = append(procs, Proc{PID: 100 + i, PPID: 2, Elapsed: time.Duration(i) * time.Second, Command: "child"})
	}
	procs = append(procs, Proc{PID: 3, PPID: 1, Elapsed: time.Minute, Command: "second"})
	got := processDoings(1, procs, t0)
	if len(got) != doingMax || got[0].Text != "top" || got[len(got)-1].Text != "second" {
		t.Fatalf("上限 %d で直接のものを残すはず: %d 件 %+v", doingMax, len(got), got)
	}
}

type doingRig struct {
	d         *Dispatcher
	dir, jobs string
	procs     []Proc
	procsErr  error
	tr        live.Transcript
}

// newDoingRig は作業中のカード C-001 (PG の session s1・pid 10) と、pro-con の記録に無いカード C-002 (session s2) を置く。
func newDoingRig(t *testing.T) *doingRig {
	t.Helper()
	dir := t.TempDir()
	planned(t, dir, 2)
	setCard(t, dir, "C-001", func(c *card.Card) { c.State, c.Session = card.Running, "s1" })
	setCard(t, dir, "C-002", func(c *card.Card) { c.State, c.Session = card.Running, "s2" })
	if err := live.Register(filepath.Join(dir, live.RegistryFile), live.Owned{ID: "s1", SessionID: "sess-1", PID: 10, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	r := &doingRig{dir: dir, jobs: t.TempDir()}
	ss := []agents.Session{{ID: "s1", SessionID: "sess-1", PID: 10}, {ID: "s2", SessionID: "sess-2", PID: 20}}
	r.d = newDispatcher(t, dir, &fakeLauncher{}, ss)
	r.d.JobsDir = r.jobs
	r.d.Procs = func(context.Context) ([]Proc, error) { return r.procs, r.procsErr }
	r.d.Transcript = func(id string) (live.Transcript, error) {
		if id != "sess-1" {
			t.Errorf("記録に無い session の transcript を読んだ: %s", id)
		}
		return r.tr, nil
	}
	r.procs = []Proc{
		{PID: 10, PPID: 1, Elapsed: time.Hour, Command: "claude bg-spare --bg-spare /tmp/x.sock"},
		{PID: 20, PPID: 1, Elapsed: time.Hour, Command: "claude bg-spare --bg-spare /tmp/y.sock"},
		{PID: 12, PPID: 10, Elapsed: time.Minute, Command: "make test"},
		{PID: 22, PPID: 20, Elapsed: time.Minute, Command: "外の PG のコマンド"},
	}
	return r
}

func (r *doingRig) collect(t *testing.T, now time.Time) store.Doing {
	t.Helper()
	r.d.collectDoing(context.Background(), now, []agents.Session{{ID: "s1", SessionID: "sess-1", PID: 10}, {ID: "s2", SessionID: "sess-2", PID: 20}})
	d, err := store.LoadDoing(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (r *doingRig) jobState(t *testing.T, kinds string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(r.jobs, "s1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.jobs, "s1", "state.json"), []byte(`{"inFlight":{"tasks":1,"kinds":[`+kinds+`]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 集めるのは pro-con が起動した PG (記録と一覧で pid が一致) の分だけ。Bash の呼び出しはプロセスとして出ているので重ねず、
// ほかの道具の呼び出しと裏のサブエージェントは transcript から足す。
func TestCollectDoingOnlyOwnedSessions(t *testing.T) {
	r := newDoingRig(t)
	r.tr = live.Transcript{
		Calls:  []live.Call{{Name: "Bash", Text: "make test", At: t0}, {Name: "WebFetch", Text: "https://x", At: t0}},
		Agents: []live.Call{{Name: "Agent", Text: "敵対的レビュー", At: t0.Add(-5 * time.Minute)}},
	}
	got := r.collect(t, t0)
	if !got.At.Equal(t0) || len(got.Cards) != 1 {
		t.Fatalf("記録にある C-001 だけを集めるはず: %+v", got)
	}
	var texts []string
	for _, d := range got.Cards["C-001"] {
		texts = append(texts, string(d.Kind)+":"+d.Text)
	}
	if want := "process:make test|agent:敵対的レビュー|tool:WebFetch: https://x"; strings.Join(texts, "|") != want {
		t.Fatalf("C-001 の様子: %q (期待 %q)", strings.Join(texts, "|"), want)
	}
}

// 一覧の pid が記録と違えば (落ちて別の pid で動いている / pid の使い回し)、その pid の子孫は出さない。
// ps が読めなければ、Bash の呼び出しを transcript から出し、読めない理由を残す。
func TestCollectDoingNeedsMatchingPIDAndFallsBackToCalls(t *testing.T) {
	r := newDoingRig(t)
	r.tr = live.Transcript{Calls: []live.Call{{Name: "Bash", Text: "make test", At: t0}}}
	r.tr.Agents = []live.Call{{Name: "Agent", Text: "裏の子", At: t0}}
	r.tr.Calls = append(r.tr.Calls, live.Call{Name: "WebFetch", Text: "https://x", At: t0})
	r.d.collectDoing(context.Background(), t0, []agents.Session{{ID: "s1", SessionID: "sess-1", PID: 99}})
	got, _ := store.LoadDoing(r.dir)
	if ds := got.Cards["C-001"]; len(ds) != 0 {
		t.Fatalf("pid の違う session の子孫も transcript の残り (道具・サブエージェント) も出さないはず: %+v", ds)
	}
	r.tr = live.Transcript{Calls: []live.Call{{Name: "Bash", Text: "make test", At: t0}}}
	r.procsErr = errors.New("ps が無い")
	got = r.collect(t, t0.Add(doingEvery))
	if ds := got.Cards["C-001"]; got.Err != "ps が無い" || len(ds) != 1 || ds[0].Kind != card.DoingTool || ds[0].Text != "Bash: make test" {
		t.Fatalf("ps が読めないときは Bash の呼び出しで出すはず: %+v", got)
	}
}

// jobs の state.json が「サブエージェントは走っていない」と言えば、transcript に終わりの知らせが無くても出さない。
// 走っていると言うのに transcript の末尾に起こした記録が無ければ、名前なしで出す。
func TestCollectDoingGatesAgentsByJobState(t *testing.T) {
	r := newDoingRig(t)
	r.procs = nil
	r.tr = live.Transcript{Agents: []live.Call{{Name: "Agent", Text: "終わった子", At: t0}}, LastAt: t0}
	r.jobState(t, `"local_bash"`)
	if got := r.collect(t, t0); len(got.Cards["C-001"]) != 0 {
		t.Fatalf("走っていないサブエージェントを出した: %+v", got.Cards)
	}
	r.jobState(t, `"local_agent"`)
	r.tr.Agents = nil
	got := r.collect(t, t0.Add(doingEvery))
	if ds := got.Cards["C-001"]; len(ds) != 1 || ds[0].Kind != card.DoingAgent || !strings.Contains(ds[0].Text, "記録が無い") {
		t.Fatalf("名前の分からない裏のサブエージェントを出すはず: %+v", ds)
	}
}

// 集め直すのは doingEvery ごと (毎 Tick は ps と transcript を読まない)。終わったものは次に集めたとき消える。
func TestCollectDoingThrottles(t *testing.T) {
	r := newDoingRig(t)
	calls := 0
	r.d.Procs = func(context.Context) ([]Proc, error) { calls++; return r.procs, nil }
	r.collect(t, t0)
	r.collect(t, t0.Add(doingEvery-time.Second))
	if calls != 1 {
		t.Fatalf("間隔の内に %d 回読んだ", calls)
	}
	r.procs = nil
	if got := r.collect(t, t0.Add(doingEvery)); calls != 2 || len(got.Cards) != 0 {
		t.Fatalf("間隔を過ぎたら集め直して、終わったものを消すはず: calls=%d %+v", calls, got.Cards)
	}
}

// 一覧は Tick の頭に取ったもの。ps を読む前に PG が落ちて pid が外のプロセスに使い回されたら、その子孫は出さない
// (同じ ps の中で、pid が claude のプロセスかを確かめる)。
func TestCollectDoingRejectsReusedPID(t *testing.T) {
	r := newDoingRig(t)
	r.procs[0].Command = "/usr/bin/vim 外のプロセス"
	if got := r.collect(t, t0); len(got.Cards["C-001"]) != 0 {
		t.Fatalf("pid を使い回した外のプロセスの子孫を出した: %+v", got.Cards)
	}
}

// 欄の区切りがタブでも落ちずに読む (区切りの数え方を Fields と揃える)。
func TestParsePSTabSeparated(t *testing.T) {
	ps := parsePS("101\t1\t01:02\tsleep 9\n")
	if len(ps) != 1 || ps[0].PID != 101 || ps[0].Command != "sleep 9" {
		t.Fatalf("タブ区切りの行: %+v", ps)
	}
}

// tick は画面のために、取った一覧と pro-con が起動した session の出力の末尾 (新しい live.LogOutputs 個) を store.Seen に書く (issue 502 / 503)。
// 外の session の出力は載せない (transcript も読まない。newDoingRig の Transcript が外の session を読んだら落とす)。
func TestTickPublishesSeenForScreens(t *testing.T) {
	r := newDoingRig(t)
	r.tr = live.Transcript{Outputs: []string{"1", "2", "3", "4"}}
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	r.d.Now = func() time.Time { return now }
	if _, err := r.d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen, err := store.LoadSeen(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !seen.At.Equal(now) || len(seen.Sessions) != 2 {
		t.Fatalf("一覧を書いていない: at %v / %d 本", seen.At, len(seen.Sessions))
	}
	if got := seen.Logs["s1"]; !slices.Equal(got, []string{"2", "3", "4"}) {
		t.Fatalf("pro-con が起動した session の出力の末尾: %v (期待 [2 3 4])", got)
	}
	if _, ok := seen.Logs["s2"]; ok {
		t.Fatalf("外の session の出力を載せた: %v", seen.Logs)
	}
}

// 一覧を取れなかった tick は、一覧を空にして理由だけを書く (画面に前の一覧を今の様子として使わせない。画面は自分で読む)。
func TestTickPublishesSeenErrorWhenListFails(t *testing.T) {
	r := newDoingRig(t)
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	r.d.Now = func() time.Time { return now }
	if _, err := r.d.Tick(context.Background()); err != nil { // 1 回目は取れる (前の一覧が残る状態を作る)
		t.Fatal(err)
	}
	r.d.List = func(context.Context) ([]agents.Session, error) {
		return nil, errors.New("claude agents が終わらない")
	}
	now = now.Add(3 * time.Second)
	if _, err := r.d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen, err := store.LoadSeen(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Err == "" || len(seen.Sessions) != 0 || !seen.At.Equal(now) {
		t.Fatalf("取れなかった tick の記録: at %v / err %q / %d 本 (期待: 今の時刻・理由あり・0 本)", seen.At, seen.Err, len(seen.Sessions))
	}
}

// Seen の時刻は書く時点 (tick の頭ではない)。一覧の取得は最長 10 秒かかるので、頭の時刻で書くと書いた時点で古い。
func TestTickPublishesSeenAtWriteTime(t *testing.T) {
	r := newDoingRig(t)
	start := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	calls := 0
	r.d.Now = func() time.Time { calls++; return start.Add(time.Duration(calls) * time.Second) } // 呼ぶたびに 1 秒進む時計
	if _, err := r.d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen, err := store.LoadSeen(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !seen.At.After(start.Add(time.Second)) { // tick の頭で読んだ 1 回目 (start+1s) より後
		t.Fatalf("Seen の時刻 %v が tick の頭の時刻 (%v) のまま", seen.At, start.Add(time.Second))
	}
}
