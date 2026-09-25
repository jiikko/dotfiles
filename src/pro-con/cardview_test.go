package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
	"pro-con/wake"
)

// viewDir は socket のパスが上限に収まる置き場 (t.TempDir は macOS では長く、socket が一時ディレクトリへ逃がす側に倒れる)。
func viewDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "pcv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func viewCmd(t *testing.T, env viewEnv, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	rc := runCard(args, env, &o, &e)
	return rc, o.String(), e.String()
}

func mustSubmit(t *testing.T, dir string, r store.Request) {
	t.Helper()
	if _, err := store.Submit(dir, r); err != nil {
		t.Fatal(err)
	}
}

func mustApply(t *testing.T, dir string) {
	t.Helper()
	res, err := store.Apply(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Err != "" {
			t.Fatalf("適用で除けられた: %+v", r)
		}
	}
}

// fixture: C-001 は質問待ち (PG の session s1 に transcript あり)、C-002 は依頼、C-003 は完了して片付けた
func viewFixture(t *testing.T) viewEnv {
	t.Helper()
	dir, projects := viewDir(t), t.TempDir()
	mustSubmit(t, dir, store.Request{Kind: "add", Title: "色を直す", Request: "statusline の色を直して"})
	mustSubmit(t, dir, store.Request{Kind: "add", Title: "別件", Request: "README を読みやすく"})
	mustSubmit(t, dir, store.Request{Kind: "add", Title: "終わったもの", Request: "片付け済み"})
	mustApply(t, dir)
	mustSubmit(t, dir, store.Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 415, Status: "open"}}})
	mustSubmit(t, dir, store.Request{Kind: "close", CardID: "C-003", Ending: card.EndAnswered})
	mustApply(t, dir)
	if err := store.Update(dir, func(st *store.State) error {
		for i := range st.Cards {
			switch st.Cards[i].ID {
			case "C-001":
				st.Cards[i].State, st.Cards[i].Session = card.Running, "s1"
			case "C-003":
				st.Cards[i].Archived = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mustSubmit(t, dir, store.Request{Kind: "ask", CardID: "C-001", Question: "どの色にしますか"})
	mustApply(t, dir)
	reg, _ := json.Marshal([]live.Owned{{ID: "s1", SessionID: "sess-1", PID: 42, CardID: "C-001", StartedAt: time.Now()}})
	if err := os.WriteFile(filepath.Join(dir, live.RegistryFile), reg, 0o600); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	tr := `{"type":"assistant","timestamp":"2026-09-25T01:00:00Z","message":{"content":[{"type":"text","text":"色の候補を 3 つ作った"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "sess-1.jsonl"), []byte(tr), 0o600); err != nil {
		t.Fatal(err)
	}
	return viewEnv{dir: dir, projects: projects, now: time.Now}
}

// list は片付けたものを除いて出し、--state で絞り、--json は Claude が読める形で出す。
func TestCardList(t *testing.T) {
	env := viewFixture(t)
	rc, out, errOut := viewCmd(t, env, "list")
	if rc != 0 || errOut != "" || !strings.Contains(out, "C-001") || !strings.Contains(out, "C-002") || strings.Contains(out, "C-003") {
		t.Fatalf("list: rc=%d out=%q err=%q", rc, out, errOut)
	}
	if !strings.Contains(out, "待ち: 質問") {
		t.Fatalf("質問待ちが一覧で見えない: %q", out)
	}
	if _, out, _ := viewCmd(t, env, "list", "--all"); !strings.Contains(out, "C-003") {
		t.Fatalf("--all で片付けたものが出ない: %q", out)
	}
	rc, out, _ = viewCmd(t, env, "list", "--state", "waiting", "--json")
	var got []cardSummary
	if rc != 0 || json.Unmarshal([]byte(out), &got) != nil || len(got) != 1 || got[0].ID != "C-001" || got[0].Question != "どの色にしますか" {
		t.Fatalf("--state waiting --json: rc=%d %q", rc, out)
	}
	if _, out, _ := viewCmd(t, env, "list", "--state", "依頼"); !strings.Contains(out, "C-002") || strings.Contains(out, "C-001") {
		t.Fatalf("--state 依頼 (画面の見出し) で絞れない: %q", out)
	}
	if rc, _, _ := viewCmd(t, env, "list", "--state", "nosuch"); rc != 2 {
		t.Fatalf("未知の列は rc=2: %d", rc)
	}
}

// show は画面の詳細と同じ中身 (依頼の原文・質問・履歴・PG の出力の末尾) を出す。出力の末尾は起動の記録と transcript から読む。
func TestCardShow(t *testing.T) {
	env := viewFixture(t)
	rc, out, errOut := viewCmd(t, env, "show", "C-001")
	for _, want := range []string{"依頼の原文: 「statusline の色を直して」", "質問: どの色にしますか", "履歴", "依頼を受けた", "dotfiles#415", "色の候補を 3 つ作った"} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Fatalf("show に %q が無い: rc=%d out=%q err=%q", want, rc, out, errOut)
		}
	}
	rc, out, _ = viewCmd(t, env, "show", "C-001", "--json")
	var d cardDetail
	if rc != 0 || json.Unmarshal([]byte(out), &d) != nil || d.Card.ID != "C-001" || len(d.Log) != 1 || d.Log[0] != "色の候補を 3 つ作った" {
		t.Fatalf("show --json: rc=%d %q", rc, out)
	}
	// 同じ短い id の行が 2 本あり、先の行の transcript が無くても、後ろの行の transcript を出す (先の行で打ち切らない)
	two, _ := json.Marshal([]live.Owned{
		{ID: "s1", SessionID: "sess-gone", PID: 41, CardID: "C-001", StartedAt: time.Now()},
		{ID: "s1", SessionID: "sess-1", PID: 42, CardID: "C-001", StartedAt: time.Now()},
	})
	if err := os.WriteFile(filepath.Join(env.dir, live.RegistryFile), two, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := viewCmd(t, env, "show", "C-001"); !strings.Contains(out, "色の候補を 3 つ作った") {
		t.Fatalf("先の行の transcript が無いと、後ろの行の出力を出さない: %q", out)
	}
	// 起動の記録で短い id が同じでも、別のカードのために起動した session の transcript は出さない
	other, _ := json.Marshal([]live.Owned{{ID: "s1", SessionID: "sess-1", PID: 42, CardID: "C-002", StartedAt: time.Now()}})
	if err := os.WriteFile(filepath.Join(env.dir, live.RegistryFile), other, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := viewCmd(t, env, "show", "C-001"); strings.Contains(out, "色の候補") {
		t.Fatalf("別のカードの session の出力を出した: %q", out)
	}
	// 起動の記録に無い session の transcript は読まない (画面の詳細と同じ範囲)
	if err := os.WriteFile(filepath.Join(env.dir, live.RegistryFile), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := viewCmd(t, env, "show", "C-001"); strings.Contains(out, "色の候補") {
		t.Fatalf("起動の記録に無い session の出力を出した: %q", out)
	}
	if rc, _, errOut := viewCmd(t, env, "show", "C-999"); rc != 1 || !strings.Contains(errOut, "C-999") {
		t.Fatalf("無いカード: rc=%d err=%q", rc, errOut)
	}
}

// fakeDispatcher は置き場の socket で待ち受け、届いた行を記録する (購読には、呼ばれたら 1 行知らせる)。
type fakeDispatcher struct {
	ln    net.Listener
	mu    sync.Mutex
	lines []string
	subs  []net.Conn
	wg    sync.WaitGroup
}

func listenFake(t *testing.T, dir string) *fakeDispatcher {
	t.Helper()
	ln, err := net.Listen("unix", wake.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeDispatcher{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					f.mu.Lock()
					f.lines = append(f.lines, strings.TrimSpace(l))
					if strings.TrimSpace(l) == "sub" {
						f.subs = append(f.subs, c)
					}
					f.mu.Unlock()
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); _ = os.Remove(wake.Path(dir)) })
	return f
}

// broadcast は購読している口へ「読み直して」を送る (本物の dispatcher の Broadcast と同じ 1 行)。
func (f *fakeDispatcher) broadcast() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.subs {
		_, _ = c.Write([]byte("changed\n"))
	}
}

func (f *fakeDispatcher) subscribed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs) > 0
}

// drain は待ち受けを閉じ、受けた接続を読み終えるまで待ってから、届いた行を返す (読み終える前に見ると、最後の送信を取りこぼす)。
func (f *fakeDispatcher) drain() []string {
	_ = f.ln.Close()
	f.wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 500 { // 10 秒
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(what)
}

// wait は dispatcher の知らせですぐ読み直す (ポーリングの間隔を 1 時間にしても待たない)。
func TestCardWaitWakesOnNotification(t *testing.T) {
	env := viewFixture(t)
	f := listenFake(t, env.dir)
	old := viewPoll
	viewPoll = time.Hour
	t.Cleanup(func() { viewPoll = old })
	type result struct {
		rc       int
		out, err string
	}
	ch := make(chan result, 1)
	go func() {
		rc, out, e := viewCmd(t, env, "wait", "C-001", "--until", "review", "--timeout", "30s")
		ch <- result{rc, out, e}
	}()
	waitUntil(t, "wait が購読しない", f.subscribed)
	mustSubmit(t, env.dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "青", From: "PM"})
	mustApply(t, env.dir) // 質問待ち → 作業中 (--until review なので、まだ返らない)
	f.broadcast()
	if err := store.Update(env.dir, func(st *store.State) error {
		st.Cards[0].State = card.Running // 回答で作業中へ (dispatcher の再開を模す)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mustSubmit(t, env.dir, store.Request{Kind: "review", CardID: "C-001"})
	mustApply(t, env.dir)
	f.broadcast()
	select {
	case r := <-ch:
		if r.rc != 0 || !strings.Contains(r.out, "C-001") || !strings.Contains(r.out, "レビュー") {
			t.Fatalf("wait: rc=%d out=%q err=%q", r.rc, r.out, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("知らせを送っても wait が返らない (ポーリングの 1 時間を待っている)")
	}
}

// socket が無くても (dispatcher が居ない)、ポーリングで待てる。時間切れは rc=1。
// 🚨 socket を置かない: 置くと購読が繋がった直後の読み直し (wake.Subscriber は繋がるたびに 1 度 onChange を呼ぶ) が
// ポーリングの代わりに返してしまい、ポーリングを消しても緑になる (敵対的レビュー 2026-09-25 P3)
func TestCardWaitPollsAndTimesOut(t *testing.T) {
	env := viewFixture(t)
	old := viewPoll
	viewPoll = 20 * time.Millisecond
	t.Cleanup(func() { viewPoll = old })
	read := make(chan struct{})
	waitFirstRead = func() { close(read) }
	t.Cleanup(func() { waitFirstRead = func() {} })
	ch := make(chan int, 1)
	var out string
	go func() {
		var rc int
		rc, out, _ = viewCmd(t, env, "wait", "C-002", "--until", "分解済み", "--timeout", "30s")
		ch <- rc
	}()
	<-read // wait が最初の列 (依頼) を読んでから変える (先に変えると 1 回目の読みで返り、ポーリングを通らない)
	waitFirstRead = func() {}
	mustSubmit(t, env.dir, store.Request{Kind: "plan", CardID: "C-002"})
	mustApply(t, env.dir)
	select {
	case rc := <-ch:
		if rc != 0 || !strings.Contains(out, "依頼 → 分解済み") {
			t.Fatalf("wait (ポーリング): rc=%d out=%q", rc, out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dispatcher が居ないと wait が返らない (ポーリングしていない)")
	}
	if rc, _, errOut := viewCmd(t, env, "wait", "C-002", "--timeout", "100ms"); rc != 1 || !strings.Contains(errOut, "分解済み のまま") {
		t.Fatalf("時間切れ: rc=%d err=%q", rc, errOut)
	}
	if rc, _, _ := viewCmd(t, env, "wait", "C-999", "--timeout", "100ms"); rc != 1 {
		t.Fatalf("無いカード: rc=%d", rc)
	}
}

// --until が無ければ、最初に読んだ列から変わるまで待つ。
func TestCardWaitWithoutUntilWaitsForChange(t *testing.T) {
	env := viewFixture(t)
	f := listenFake(t, env.dir)
	old := viewPoll
	viewPoll = time.Hour
	t.Cleanup(func() { viewPoll = old })
	ch := make(chan string, 1)
	go func() {
		_, out, _ := viewCmd(t, env, "wait", "C-002", "--timeout", "30s")
		ch <- out
	}()
	// 購読したなら、wait は最初の列 (依頼) を読み終えている (読む前に変えると「最初から分解済み」になり、変化を待ち続ける)
	waitUntil(t, "wait が購読しない", f.subscribed)
	mustSubmit(t, env.dir, store.Request{Kind: "plan", CardID: "C-002"})
	mustApply(t, env.dir)
	f.broadcast()
	select {
	case out := <-ch:
		if !strings.Contains(out, "依頼 → 分解済み") {
			t.Fatalf("wait: %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("列が変わっても wait が返らない")
	}
}

// 完了したカードは --until の列に来ないので、時間切れを待たずに rc=1 で返す。
func TestCardWaitGivesUpWhenDone(t *testing.T) {
	env := viewFixture(t)
	old := viewPoll
	viewPoll = 20 * time.Millisecond
	t.Cleanup(func() { viewPoll = old })
	mustSubmit(t, env.dir, store.Request{Kind: "close", CardID: "C-002", Ending: card.EndAnswered})
	mustApply(t, env.dir)
	if rc, _, errOut := viewCmd(t, env, "wait", "C-002", "--timeout", "5s"); rc != 1 || !strings.Contains(errOut, "列はもう変わらない") {
		t.Fatalf("完了したカードの変化待ち: rc=%d err=%q", rc, errOut)
	}
	// 上限は短くしておく (諦めないと上限の時間切れの文「… のまま」で返るので、文で見分けられる)
	rc, _, errOut := viewCmd(t, env, "wait", "C-002", "--until", "review", "--timeout", "5s")
	if rc != 1 || !strings.Contains(errOut, "完了になったので レビュー には来ない") {
		t.Fatalf("完了したカードの --until: rc=%d err=%q", rc, errOut)
	}
}

// inboxCount は受付の箱に置かれている依頼の数。
func inboxCount(dir string) int {
	ms, _ := filepath.Glob(filepath.Join(dir, store.InboxDir, "*.json"))
	return len(ms)
}

// add は dispatcher の適用を待って、**この依頼から作られた**カードの ID を返す (最後に作られたカードではない)。
// 除けられたら、**この依頼の**除けられた理由を返す (先に除けられた別の依頼の理由ではない)。
func TestCardAddWaitsForCardID(t *testing.T) {
	dir := viewDir(t)
	old := viewPoll
	viewPoll = 20 * time.Millisecond
	t.Cleanup(func() { viewPoll = old })
	mustSubmit(t, dir, store.Request{Kind: "add", Title: "先のカード"})
	mustSubmit(t, dir, store.Request{Kind: "plan", CardID: "C-999"}) // 別の依頼が先に除けられている (理由は「カードが無い」)
	if _, err := store.Apply(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	env := viewEnv{dir: dir, now: time.Now}
	type result struct {
		rc       int
		out, err string
	}
	run := func(f func() (int, string, string)) chan result {
		ch := make(chan result, 1)
		go func() { rc, o, e := f(); ch <- result{rc, o, e} }()
		return ch
	}
	// 1. この依頼 (C-002 になる) の後ろに別の依頼 (C-003) を並べてから適用する
	ch := run(func() (int, string, string) {
		return viewCmd(t, env, "add", "--title", "このカード", "--wait", "10s")
	})
	waitUntil(t, "add が箱に置かない", func() bool { return inboxCount(dir) == 1 })
	mustSubmit(t, dir, store.Request{Kind: "add", Title: "後ろのカード"})
	mustApply(t, dir)
	if r := <-ch; r.rc != 0 || strings.TrimSpace(r.out) != "C-002" || r.err != "" {
		t.Fatalf("add: rc=%d out=%q err=%q (期待 C-002。C-003 なら最後のカードを返している)", r.rc, r.out, r.err)
	}
	// 2. 除けられる依頼 (題名も原文も空白だけ。parseCardWait は通らない形なので直接置く)
	ch = run(func() (int, string, string) {
		var o, e bytes.Buffer
		rc := addAndWait(dir, store.Request{Kind: "add", Title: " "}, 10*time.Second, &o, &e)
		return rc, o.String(), e.String()
	})
	waitUntil(t, "add が箱に置かない", func() bool { return inboxCount(dir) == 1 })
	if _, err := store.Apply(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r := <-ch; r.rc != 1 || !strings.Contains(r.err, "題名も依頼の原文も空") {
		t.Fatalf("除けられた依頼: rc=%d out=%q err=%q (「カードが無い」なら別の依頼の理由を返している)", r.rc, r.out, r.err)
	}
}

func TestCardAddTimeoutReturnsRequestID(t *testing.T) {
	dir := viewDir(t)
	env := viewEnv{dir: dir, now: time.Now}
	rc, out, errOut := viewCmd(t, env, "add", "--title", "dispatcher が居ない", "--wait", "100ms")
	id := strings.TrimSpace(out)
	if rc != exitNotApplied || !strings.Contains(errOut, "依頼 ID を返す") {
		t.Fatalf("時間切れ: rc=%d out=%q err=%q", rc, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, store.InboxDir, id+".json")); err != nil {
		t.Fatalf("返した %q が箱に置いた依頼の ID でない: %v", id, err)
	}
	if rc, _, _ := viewCmd(t, env, "add", "--title", "x", "--wait", "nosuch"); rc != 2 {
		t.Fatalf("--wait の誤りは rc=2: %d", rc)
	}
}

// snapshotTree は置き場の下のファイルの中身・権限・種類 (socket を除く) を写す。
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSocket != 0 {
			return nil // 偽の dispatcher の socket (テストが置いた)
		}
		v := info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			v += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		m[p] = v + " " + info.ModTime().Format(time.RFC3339Nano)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// 🚨 読む口 (list / show / wait) は、状態の置き場を 1 バイトも変えず、socket へ wake / notify を送らない (441 の守ること 1 / 445)。
// 購読の sub だけは送ってよい (dispatcher は購読の数を判断に使わない)。
func TestViewCommandsDoNotWrite(t *testing.T) {
	env := viewFixture(t)
	f := listenFake(t, env.dir)
	old := viewPoll
	viewPoll = 20 * time.Millisecond
	t.Cleanup(func() { viewPoll = old })
	before, beforeProj := snapshotTree(t, env.dir), snapshotTree(t, env.projects)
	for _, args := range [][]string{
		{"list"}, {"list", "--all", "--json"}, {"list", "--state", "waiting"},
		{"show", "C-001"}, {"show", "C-001", "--json"}, {"show", "C-999"},
		{"wait", "C-001", "--timeout", "200ms"}, {"wait", "C-001", "--until", "質問待ち", "--timeout", "1s"},
		{"wait", "C-999", "--timeout", "100ms"},
	} {
		viewCmd(t, env, args...)
	}
	after := snapshotTree(t, env.dir)
	afterProj := snapshotTree(t, env.projects) // transcript の置き場も読むだけ (消した・変えた・足したのどれも見る)
	if len(afterProj) != len(beforeProj) {
		t.Fatalf("transcript の置き場のファイルが増減した: 前 %d / 後 %d", len(beforeProj), len(afterProj))
	}
	for p, v := range beforeProj {
		if afterProj[p] != v {
			t.Fatalf("読む口が %s (transcript の置き場) を変えた", p)
		}
	}
	if len(before) != len(after) {
		t.Fatalf("置き場のファイルが増減した: 前 %d / 後 %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("読む口が %s を変えた:\n前 %s\n後 %s", p, v, after[p])
		}
	}
	lines := f.drain()
	if !f.subscribed() {
		t.Fatal("前提: wait が購読していない (socket の検査が空振りしている)")
	}
	for _, l := range lines {
		if l != "sub" {
			t.Fatalf("読む口が socket に %q を送った (送ってよいのは sub だけ)", l)
		}
	}
}
