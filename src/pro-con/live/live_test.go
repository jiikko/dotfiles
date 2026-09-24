package live

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/backend"
	"pro-con/card"
	"pro-con/store"
)

// 実測 (Claude Code 2.1.281) の transcript の形を縮めた見本。人間の発言は origin.kind=human、ツールの結果と
// 他の session からのメッセージは人間の発言に数えない。題名と last-prompt は後に書かれた方が新しい。
const sample = `{"type":"ai-title","aiTitle":"古い題名"}
{"type":"user","timestamp":"2026-09-24T10:00:00Z","origin":{"kind":"human"},"message":{"content":"最初の依頼"}}
{"type":"assistant","timestamp":"2026-09-24T10:00:05Z","message":{"content":[{"type":"text","text":"調べます"},{"type":"tool_use","name":"Read"}]}}
{"type":"user","timestamp":"2026-09-24T10:00:06Z","message":{"content":[{"type":"tool_result","content":"ファイルの中身"}]}}
{"type":"user","timestamp":"2026-09-24T10:00:07Z","origin":{"kind":"peer"},"message":{"content":"Another Claude session sent a message"}}
{壊れた行
{"type":"last-prompt","lastPrompt":"次の依頼"}
{"type":"ai-title","aiTitle":"新しい題名"}
{"type":"user","timestamp":"2026-09-24T10:01:00Z","origin":{"kind":"human"},"message":{"content":[{"type":"text","text":"次の\n依頼"}]}}
{"type":"assistant","timestamp":"2026-09-24T10:01:30Z","message":{"content":[{"type":"text","text":"終わりました"}]}}
`

func TestParseTranscript(t *testing.T) {
	tr := parse([]byte(sample))
	if tr.Title != "新しい題名" || tr.LastPrompt != "次の依頼" {
		t.Fatalf("題名か最後の依頼が最新でない: %q / %q", tr.Title, tr.LastPrompt)
	}
	if len(tr.Prompts) != 2 || tr.Prompts[0].Text != "最初の依頼" || tr.Prompts[1].Text != "次の 依頼" {
		t.Fatalf("人間の発言だけを拾うはず (ツールの結果・他 session のメッセージは拾わない): %+v", tr.Prompts)
	}
	if strings.Join(tr.Outputs, "|") != "調べます|終わりました" {
		t.Fatalf("出力は assistant の text だけ: %q", tr.Outputs)
	}
	if want := time.Date(2026, 9, 24, 10, 1, 30, 0, time.UTC); !tr.LastAt.Equal(want) {
		t.Fatalf("最後の時刻が %v (期待 %v)", tr.LastAt, want)
	}
}

// 末尾だけを読む (対話の transcript は 14MB を超える)。先頭の側にある人間の発言は拾わない。
func TestReadTailReadsOnlyTheEnd(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	head := `{"type":"user","timestamp":"2026-09-24T09:00:00Z","origin":{"kind":"human"},"message":{"content":"先頭の依頼"}}`
	pad := head + "\n" + strings.Repeat(`{"type":"system","content":"`+strings.Repeat("x", 1000)+`"}`+"\n", tailBytes/1000+10)
	if err := os.WriteFile(p, []byte(pad+sample), 0o600); err != nil {
		t.Fatal(err)
	}
	tr, err := ReadTail(p)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Title != "新しい題名" || len(tr.Prompts) != 2 {
		t.Fatalf("末尾だけを読んでいない (先頭の依頼まで拾った / 題名が古い): 題名 %q / 発言 %d", tr.Title, len(tr.Prompts))
	}
}

// testBackend は ss を一覧に返す backend。ss の session はすべて「pro-con が起動した」記録に入れる (絞り込みは TestOnlyOwnedSessionsShowActivity)。
func testBackend(t *testing.T, ss []agents.Session, err error) (*Backend, *int) {
	t.Helper()
	reads := 0
	state := t.TempDir()
	b := New([]backend.Repo{{Name: "dotfiles", Path: "/w/dotfiles"}}, t.TempDir(), state)
	for _, s := range ss {
		if e := Register(b.registry, Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID}); e != nil {
			t.Fatal(e)
		}
	}
	b.list = func(context.Context) ([]agents.Session, error) { return ss, err }
	b.findPath = func(string) (string, error) { return os.Args[0], nil } // 存在するファイルなら何でもよい (read を差し替える)
	b.read = func(string) (Transcript, error) {
		reads++
		return parse([]byte(sample)), nil
	}
	b.now = func() time.Time { return time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC) }
	return b, &reads
}

var sessions = []agents.Session{
	{SessionID: "bbbbbbbb-2", ID: "bbbbbbbb", PID: 102, Kind: "background", Status: "busy", Cwd: "/w/dotfiles"},
	{SessionID: "cccccccc-3", ID: "cccccccc", PID: 103, Kind: "background", Status: "waiting", WaitingFor: "input needed", Cwd: "/w/other"},
}

// runningCard は記録に作業中のカードを 1 枚置く (session は短い id)。記録を書くのは daemon の仕事なので、store.Update で直接置く。
func runningCard(t *testing.T, b *Backend, id, session string) {
	t.Helper()
	if _, err := store.Submit(b.dir, store.Request{Kind: "add", Title: "t-" + id, Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(b.dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(b.dir, func(st *store.State) error {
		for i := range st.Cards {
			if st.Cards[i].ID == id {
				st.Cards[i].State, st.Cards[i].Session = card.Running, session
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// カードは記録 (store) から出る。作業中のカードに、pro-con が起動した session の様子 (PG の出力の末尾・pid) を足す。
// 記録に無い session (外のもの) の様子は足さない。
func TestOnlyOwnedSessionsShowActivity(t *testing.T) {
	b, _ := testBackend(t, sessions[:1], nil) // bbbbbbbb だけが pro-con の記録にある
	b.list = func(context.Context) ([]agents.Session, error) { return sessions, nil }
	runningCard(t, b, "C-001", "bbbbbbbb")
	runningCard(t, b, "C-002", "cccccccc") // 記録に無い session を指すカード
	b.Refresh(context.Background())
	s := b.Poll()
	if len(s.Cards) != 2 {
		t.Fatalf("記録のカードが 2 枚出るはず: %d", len(s.Cards))
	}
	if len(s.Cards[0].Log) == 0 || len(s.Cards[1].Log) != 0 {
		t.Fatalf("pro-con が起動した session の様子だけを足すはず: %v / %v", s.Cards[0].Log, s.Cards[1].Log)
	}
	if len(s.Consumers) != 1 || s.Consumers[0].CardID != "C-001" || s.Consumers[0].PID != 102 {
		t.Fatalf("PG は pro-con が起動した session だけ: %+v", s.Consumers)
	}
}

// 記録を読めなければ前のカードを残し、理由を出す (0 枚と区別する)。session の一覧を取れないときは、カードは出して理由を足す。
func TestReadFailures(t *testing.T) {
	b, _ := testBackend(t, sessions[:1], nil)
	runningCard(t, b, "C-001", "bbbbbbbb")
	b.Refresh(context.Background())
	b.list = func(context.Context) ([]agents.Session, error) { return nil, errors.New("timeout") }
	b.Refresh(context.Background())
	if s := b.Poll(); len(s.Cards) != 1 || len(s.Violations) != 1 || !strings.Contains(s.Violations[0].Reason, "timeout") {
		t.Fatalf("一覧を取れないときもカードは出して理由を足すはず: %d 枚 / %v", len(s.Cards), s.Violations)
	}
	if err := os.WriteFile(filepath.Join(b.dir, store.StateFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	if s := b.Poll(); len(s.Cards) != 1 || !strings.Contains(s.Violations[0].Reason, "カードの記録を読めない") {
		t.Fatalf("記録を読めないときは前のカードを残して理由を出すはず: %d 枚 / %v", len(s.Cards), s.Violations)
	}
}

// transcript の大きさと更新時刻が変わっていなければ読み直さない (3 秒ごとに 14MB 級を読まない)。
func TestTranscriptIsCached(t *testing.T) {
	b, reads := testBackend(t, sessions[:1], nil)
	runningCard(t, b, "C-001", "bbbbbbbb")
	b.Refresh(context.Background())
	b.Refresh(context.Background())
	if *reads != 1 {
		t.Fatalf("変わっていない transcript を %d 回読んだ", *reads)
	}
}

// 書き込みは受付の箱に置く (記録へ適用するのは daemon)。受けるのは新しい依頼と回答だけ。attach できるのは裏の session だけ。
func TestApplySubmitsToInbox(t *testing.T) {
	b, _ := testBackend(t, sessions, nil)
	if _, err := b.Apply(backend.NewRequest{Repo: backend.Repo{Name: "dotfiles", Path: "/w/dotfiles"}, Text: "色を直して\n詳しく"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(backend.Answer{CardID: "C-001", Text: "青"}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []backend.Command{backend.AddOrder{CardID: "C-001", Text: "x"}, backend.Btw{CardID: "C-001", Question: "?"}, backend.ClearDone{}} {
		if _, err := b.Apply(cmd); !errors.Is(err, ErrNotYet) {
			t.Fatalf("%T を受けた: %v", cmd, err)
		}
	}
	if _, err := b.Apply(backend.NewRequest{Text: " "}); !errors.Is(err, backend.ErrEmptyText) {
		t.Fatalf("空の依頼を置いた: %v", err)
	}
	res, err := store.Apply(b.dir, time.Now())
	if err != nil || len(res) != 2 || res[0].Kind != "add" || res[1].Kind != "answer" {
		t.Fatalf("箱に新しい依頼と回答が置かれていない: %+v %v", res, err)
	}
	st, _ := store.Load(b.dir)
	if c := st.Cards[0]; c.Title != "色を直して" || c.Repo != "dotfiles" || !strings.Contains(c.Prompt, "dotfiles") {
		t.Fatalf("依頼のカード: %+v", c)
	}
	if !b.Accepts(backend.OpNew) || !b.Accepts(backend.OpAnswer) || b.Accepts(backend.OpOrder) || b.Accepts(backend.OpClear) {
		t.Fatal("受ける操作が新しい依頼と回答だけになっていない")
	}
	if _, err := b.AttachCommand(""); err == nil {
		t.Fatal("session id 無しで attach できてしまう")
	}
	if cmd, err := b.AttachCommand("bbbbbbbb"); err != nil || strings.Join(cmd.Args, " ") != "claude attach bbbbbbbb" {
		t.Fatalf("裏の session の attach: %v %v", cmd, err)
	}
}

// 受付の箱に適用待ちが溜まっていたら (daemon が動いていない)、ヘッダーで知らせる。
func TestDescribeShowsPendingInbox(t *testing.T) {
	b, _ := testBackend(t, nil, nil)
	b.Refresh(context.Background())
	if strings.Contains(b.Describe(), "適用待ち") {
		t.Fatalf("箱が空なのに適用待ちと出た: %q", b.Describe())
	}
	if _, err := b.Apply(backend.NewRequest{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	if !strings.Contains(b.Describe(), "適用待ち 1 件") {
		t.Fatalf("適用待ちを知らせない: %q", b.Describe())
	}
}

// Start は最初の読み取りを待たずに戻る (claude agents --json は最大 3 秒待つので、画面を出す前に待たない)。
func TestStartDoesNotBlock(t *testing.T) {
	b, _ := testBackend(t, sessions[:1], nil)
	release := make(chan struct{})
	b.list = func(context.Context) ([]agents.Session, error) { <-release; return sessions[:1], nil }
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() { b.Start(ctx); close(returned) }()
	select {
	case <-returned:
	case <-time.After(5 * time.Second): // 安全網 (Start が待つ退行ならここで止まる)
		t.Fatal("Start が最初の読み取りを待っている")
	}
	if !strings.Contains(b.Describe(), "読み込み中") {
		t.Fatalf("最初の読み取りの前なのに %q", b.Describe())
	}
	close(release)
	cancel()
	b.Wait()
}

// Register は追記し、一時ファイルを残さない (途中で落ちても壊れた記録を残さないように rename で書く)。
func TestRegisterAppendsAtomically(t *testing.T) {
	p := filepath.Join(t.TempDir(), "live", RegistryFile)
	for _, id := range []string{"a", "b"} {
		if err := Register(p, Owned{SessionID: id, ID: id, PID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := LoadRegistry(p)
	if err != nil || len(reg) != 2 || reg[0].ID != "a" || reg[1].ID != "b" {
		t.Fatalf("追記されていない: %+v %v", reg, err)
	}
	if ms, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "*.tmp-*")); len(ms) != 0 {
		t.Fatalf("一時ファイルが残った: %v", ms)
	}
}

// 照合は記録の 1 行にある欄が全部一致したときだけ。どれか 1 つの一致で通すと、短い id の衝突や、
// pro-con が起動した session を外で同じ session id のまま再開したもの (pid が違う) を拾う (敵対的レビューの P2)。
func TestOwnsRequiresEveryRecordedField(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  Owned
		want bool
	}{
		{"長い id と pid が一致", Owned{SessionID: "X", PID: 100}, true},
		{"短い id と pid が一致", Owned{ID: "bb", PID: 100}, true},
		{"長い id は一致・短い id が違う", Owned{SessionID: "X", ID: "zz", PID: 100}, false},
		{"短い id は一致・長い id が違う", Owned{SessionID: "Y", ID: "bb", PID: 100}, false},
		{"pid が違う (外で再開した)", Owned{SessionID: "X", PID: 200}, false},
		{"pid の無い行 (古い形式・書き忘れ)", Owned{SessionID: "X"}, false},
		{"id を持たない行", Owned{PID: 100}, false},
	} {
		if got := owns([]Owned{tc.rec}, "X", "bb", 100); got != tc.want {
			t.Fatalf("%s: owns=%v (期待 %v)", tc.name, got, tc.want)
		}
	}
}

// attach は押した瞬間に一覧を取り直して照合し直す。同じ短い id でも記録と違う session (外のもの) には撃たない。
func TestAttachReverifiesOwnership(t *testing.T) {
	b, _ := testBackend(t, nil, nil)
	if err := Register(b.registry, Owned{SessionID: "bbbbbbbb-2", ID: "bbbbbbbb", PID: 102}); err != nil {
		t.Fatal(err)
	}
	b.list = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{SessionID: "bbbbbbbb-2", ID: "bbbbbbbb", PID: 102, Kind: "background"}}, nil
	}
	if _, err := b.AttachCommand("bbbbbbbb"); err != nil {
		t.Fatalf("記録にある session に attach できない: %v", err)
	}
	b.list = func(context.Context) ([]agents.Session, error) { // 同じ短い id の、記録に無い session に入れ替わった
		return []agents.Session{{SessionID: "ffffffff-9", ID: "bbbbbbbb", PID: 102, Kind: "background"}}, nil
	}
	if _, err := b.AttachCommand("bbbbbbbb"); err == nil {
		t.Fatal("記録と違う session に attach しようとした")
	}
}

// Register は PID の無い行を拒み (照合で誰とも一致しないので書く意味が無い)、同じ session の行は書き直す (再開のたびに PID が変わる)。
func TestRegisterRejectsNoPIDAndUpserts(t *testing.T) {
	p := filepath.Join(t.TempDir(), RegistryFile)
	if err := Register(p, Owned{SessionID: "X"}); !errors.Is(err, ErrNoPID) {
		t.Fatalf("PID の無い行を記録した: %v", err)
	}
	if err := Register(p, Owned{ID: "x", PID: 1}); !errors.Is(err, ErrNoSessionID) {
		t.Fatalf("session id の無い行を記録した (短い id だけで書き直すと照合が緩む): %v", err)
	}
	for _, pid := range []int{100, 200} {
		if err := Register(p, Owned{SessionID: "X", PID: pid}); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := LoadRegistry(p)
	if err != nil || len(reg) != 1 || reg[0].PID != 200 {
		t.Fatalf("同じ session の行を書き直すはず: %+v %v", reg, err)
	}
}

// Claude Code がプロセスの死から自動で再開したときに足す文 (425 で実測) を、発言者を問わず再開の時刻として読む。
// 文の先頭にあるときだけ数える (人間の発言や依頼の原文に引用された文は数えない)。再開の文は人間の発言にも数えない。
func TestParseTranscriptRestarts(t *testing.T) {
	data := `{"type":"user","timestamp":"2026-09-25T01:00:00Z","origin":{"kind":"human"},"message":{"content":"直して"}}
{"type":"user","timestamp":"2026-09-25T01:05:00Z","message":{"content":"Continue from where you left off. Note: this session was automatically restarted after its process exited unexpectedly; the user has not sent a new message since the restart."}}
{"type":"user","timestamp":"2026-09-25T01:07:00Z","origin":{"kind":"human"},"message":{"content":"ログに Continue from where you left off. Note: this session was automatically restarted after its process exited unexpectedly と出た"}}
{"type":"user","timestamp":"2026-09-25T01:09:00Z","message":{"content":[{"type":"text","text":"Continue from where you left off. Note: this session was automatically restarted after its process exited unexpectedly; ..."}]}}
`
	tr := parse([]byte(data))
	want := []time.Time{time.Date(2026, 9, 25, 1, 5, 0, 0, time.UTC), time.Date(2026, 9, 25, 1, 9, 0, 0, time.UTC)}
	if len(tr.Restarts) != 2 || !tr.Restarts[0].Equal(want[0]) || !tr.Restarts[1].Equal(want[1]) {
		t.Fatalf("再開の時刻を読めない: %v", tr.Restarts)
	}
	if len(tr.Prompts) != 2 {
		t.Fatalf("再開の文を人間の発言に数えた: %+v", tr.Prompts)
	}
}

// LastNew は、末尾の中でそれまでに無かった PG の出力が最後に出た時刻。同じ出力の繰り返しでは進まない。
func TestParseTranscriptLastNew(t *testing.T) {
	data := `{"type":"assistant","timestamp":"2026-09-25T01:00:00Z","message":{"content":"テストを回す"}}
{"type":"assistant","timestamp":"2026-09-25T01:01:00Z","message":{"content":"まだ終わっていない"}}
{"type":"assistant","timestamp":"2026-09-25T01:02:00Z","message":{"content":"まだ終わっていない"}}
{"type":"assistant","timestamp":"2026-09-25T01:03:00Z","message":{"content":"まだ終わっていない"}}
`
	if tr := parse([]byte(data)); !tr.LastNew.Equal(time.Date(2026, 9, 25, 1, 1, 0, 0, time.UTC)) || !tr.LastAt.Equal(time.Date(2026, 9, 25, 1, 3, 0, 0, time.UTC)) {
		t.Fatalf("同じ出力の繰り返しで進捗が進んだ / 読めない: LastNew=%v LastAt=%v", tr.LastNew, tr.LastAt)
	}
}

// ツール呼び出し (名前 + 引数) も進捗に数え、同じ呼び出しの繰り返しは数えない。結果の返っていない呼び出しの時刻を PendingSince に出す。
func TestParseTranscriptToolProgress(t *testing.T) {
	data := `{"type":"assistant","timestamp":"2026-09-25T01:00:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make test"}}]}}
{"type":"user","timestamp":"2026-09-25T01:01:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","timestamp":"2026-09-25T01:02:00Z","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"make test"}}]}}
{"type":"user","timestamp":"2026-09-25T01:03:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}
{"type":"assistant","timestamp":"2026-09-25T01:04:00Z","message":{"content":[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"go test ./..."}}]}}
`
	tr := parse([]byte(data))
	at := func(m int) time.Time { return time.Date(2026, 9, 25, 1, m, 0, 0, time.UTC) }
	if !tr.LastNew.Equal(at(4)) {
		t.Fatalf("新しいツール呼び出しを進捗に数えない: %v", tr.LastNew)
	}
	if !tr.PendingSince.Equal(at(4)) {
		t.Fatalf("結果の返っていない呼び出しを出さない: %v", tr.PendingSince)
	}
	tr = parse([]byte(data[:strings.LastIndex(data[:len(data)-1], "\n")+1])) // t3 を除く
	if !tr.LastNew.Equal(at(0)) || !tr.PendingSince.IsZero() {
		t.Fatalf("同じ呼び出しの繰り返しを進捗に数えた / 結果の返った呼び出しを実行中にした: LastNew=%v Pending=%v", tr.LastNew, tr.PendingSince)
	}
}

// 同じ session id の transcript が複数の project にあれば、更新の新しい方を読む。新しい方を辞書順の先頭に置く
// (末尾を取る実装と区別する。先頭を取る実装とは、並びが逆の fixture の既存テストが無いので下の 2 つ目で区別する)。
func TestFindTranscriptPrefersNewest(t *testing.T) {
	root := t.TempDir()
	var newest string
	for i, dir := range []string{"b-old", "a-new"} {
		p := filepath.Join(root, dir, "S1.jsonl")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		mt := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		newest = p
	}
	if p, err := FindTranscript(root, "S1"); err != nil || p != newest {
		t.Fatalf("更新の新しい方を読まない: %q %v", p, err)
	}
}

// 結果の無いツール呼び出しの後に PG の出力が続いたら、その呼び出しは置き去り (実行中に数えない)。
func TestParseTranscriptOrphanToolIsNotPending(t *testing.T) {
	data := `{"type":"assistant","timestamp":"2026-09-25T01:00:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make test"}}]}}
{"type":"user","timestamp":"2026-09-25T01:05:00Z","message":{"content":"Continue from where you left off. Note: this session was automatically restarted after its process exited unexpectedly"}}
{"type":"assistant","timestamp":"2026-09-25T01:06:00Z","message":{"content":"続きをやる"}}
`
	if tr := parse([]byte(data)); !tr.PendingSince.IsZero() {
		t.Fatalf("置き去りの呼び出しを実行中に数えた: %v", tr.PendingSince)
	}
}
