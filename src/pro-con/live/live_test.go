package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/backend"
	"pro-con/card"
	"pro-con/presence"
	"pro-con/store"
	"pro-con/wake"
	"sync/atomic"
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

// runningCard は記録に作業中のカードを 1 枚置く (session は短い id)。記録を書くのは dispatcher の仕事なので、store.Update で直接置く。
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

// 書き込みは受付の箱に置く (記録へ適用するのは dispatcher)。受けるのは新しい依頼と回答だけ。attach できるのは裏の session だけ。
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
	if !b.Accepts(backend.OpNew) || !b.Accepts(backend.OpAnswer) || !b.Accepts(backend.OpDelete) || b.Accepts(backend.OpOrder) || b.Accepts(backend.OpClear) {
		t.Fatal("受ける操作が新しい依頼と回答と削除だけになっていない")
	}
	if _, err := b.Apply(backend.DeleteCard{CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	if res, err := store.Apply(b.dir, time.Now()); err != nil || len(res) != 1 || res[0].Kind != "delete" || res[0].Err != "" {
		t.Fatalf("箱に削除の依頼が置かれていない: %+v %v", res, err)
	}
	if st, _ := store.Load(b.dir); len(st.Cards) != 0 {
		t.Fatalf("依頼の列のカードが消えていない: %+v", st.Cards)
	}
	if _, err := b.AttachCommand(""); err == nil {
		t.Fatal("session id 無しで attach できてしまう")
	}
	if cmd, err := b.AttachCommand("bbbbbbbb"); err != nil || strings.Join(cmd.Args, " ") != "claude attach bbbbbbbb" {
		t.Fatalf("裏の session の attach: %v %v", cmd, err)
	}
}

// 受付の箱に適用待ちが溜まっていたら (dispatcher が動いていない)、ヘッダーで知らせる。
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

// 並列のツール呼び出しは 1 件ずつ時刻の違う別の行に書かれる。短い方の結果が先に返っても、長い方は実行中のまま。
func TestParseTranscriptParallelToolsKeepLongOnePending(t *testing.T) {
	data := `{"type":"assistant","timestamp":"2026-09-25T01:00:00Z","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make test"}}]}}
{"type":"assistant","timestamp":"2026-09-25T01:00:04Z","message":{"id":"m1","content":[{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"a.go"}}]}}
{"type":"user","timestamp":"2026-09-25T01:00:05Z","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"..."}]}}
`
	if tr := parse([]byte(data)); !tr.PendingSince.Equal(time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("並列の長い方の呼び出しを実行中に数えない: %v", tr.PendingSince)
	}
}

// attach のコマンドを差し替えたら (e2e モード)、本物の claude attach ではなくそれを使う。
func TestAttachUsesReplacement(t *testing.T) {
	b, _ := testBackend(t, nil, nil)
	if err := Register(b.registry, Owned{SessionID: "e2e-session-1", ID: "e2e00001", PID: 900001}); err != nil {
		t.Fatal(err)
	}
	b.list = func(context.Context) ([]agents.Session, error) {
		return []agents.Session{{SessionID: "e2e-session-1", ID: "e2e00001", PID: 900001, Kind: "background"}}, nil
	}
	b.SetAttach(func(id string) *exec.Cmd { return exec.Command("/bin/echo", "fake", id) })
	cmd, err := b.AttachCommand("e2e00001")
	if err != nil || cmd.Path != "/bin/echo" || strings.Join(cmd.Args, " ") != "/bin/echo fake e2e00001" {
		t.Fatalf("差し替えた attach を使わない: %v %v", cmd, err)
	}
}

// 上限と dispatcher の生存は、dispatcher が書く様子 (store.DispatcherStateFile) から出す。無ければ 1 度も回っていない (zero)。
func TestSnapshotReadsDispatcherState(t *testing.T) {
	b, _ := testBackend(t, sessions[:1], nil)
	b.Refresh(context.Background())
	if s := b.Poll(); !s.DispatcherTick.IsZero() || s.Limit != 0 {
		t.Fatalf("dispatcher が回っていないのに生存・上限を出した: %v %d", s.DispatcherTick, s.Limit)
	}
	tick := time.Date(2026, 9, 25, 1, 2, 3, 0, time.UTC)
	if err := store.SaveDispatcherState(b.dir, store.DispatcherState{Tick: tick, Limit: 3, Cap: 1, Why: "枠 85%"}); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	if s := b.Poll(); !s.DispatcherTick.Equal(tick) || s.Limit != 1 || s.LimitMax != 3 || s.LimitWhy != "枠 85%" {
		t.Fatalf("dispatcher の様子を出さない: %v %d/%d %q", s.DispatcherTick, s.Limit, s.LimitMax, s.LimitWhy)
	}
}

// dispatcher に知らされたら (package wake の Broadcast)、一覧を取り直さずにすぐカードを読み直し、画面へ知らせる。
func TestBroadcastRefreshesWithoutList(t *testing.T) {
	short, err := os.MkdirTemp("/tmp", "pclv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	b, _ := testBackend(t, sessions[:1], nil)
	b.dir, b.registry = short, filepath.Join(short, RegistryFile) // socket のパスの上限に収める
	if err := Register(b.registry, Owned{SessionID: sessions[0].SessionID, ID: sessions[0].ID, PID: sessions[0].PID}); err != nil {
		t.Fatal(err)
	}
	var lists atomic.Int32
	b.list = func(context.Context) ([]agents.Session, error) { lists.Add(1); return sessions[:1], nil }
	srv, err := wake.Listen(short)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	t.Cleanup(func() { cancel(); b.Wait() })
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for range 400 {
			if cond() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal(what)
	}
	waitFor("購読しない", func() bool { return srv.Subscribers() == 1 && lists.Load() == 1 })
	for len(b.Changed()) > 0 { // 最初の読み直しの知らせを捨てる
		<-b.Changed()
	}
	runningCard(t, b, "C-001", "bbbbbbbb")
	srv.Broadcast()
	waitFor("知らされてもカードを読み直さない", func() bool { return len(b.Poll().Cards) == 1 })
	select {
	case <-b.Changed():
	case <-time.After(10 * time.Second):
		t.Fatal("読み直しを画面へ知らせない")
	}
	if n := lists.Load(); n != 1 {
		t.Fatalf("知らされた読み直しで一覧を取り直した (%d 回)", n)
	}
	if c := b.Poll().Consumers; len(c) != 1 || c[0].PID != 102 {
		t.Fatalf("前に取った一覧で PG を出していない: %+v", c)
	}
}

// shortState は socket のパスの上限に収まる置き場の backend (testBackend と同じ差し替え)。
func shortState(t *testing.T) *Backend {
	t.Helper()
	short, err := os.MkdirTemp("/tmp", "pcst")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	b, _ := testBackend(t, nil, nil)
	b.dir, b.registry = short, filepath.Join(short, RegistryFile)
	return b
}

// 同じ置き場で複数の画面を開いてよい: ほかの画面が開いていれば、閉じても dispatcher と PG を止めない。最後の画面は止める
// (package presence が数える。dispatcher が居なくてもよい)。この画面の印が無ければ (置けなかった) 最後の画面として止める。
func TestStopAllOnlyByLastScreen(t *testing.T) {
	a, b := shortState(t), shortState(t)
	b.dir, b.registry = a.dir, a.registry
	var stops atomic.Int32
	for _, be := range []*Backend{a, b} {
		be.SetStopper(func(context.Context) error { stops.Add(1); return nil })
		sc, err := presence.Open(be.dir)
		if err != nil {
			t.Fatal(err)
		}
		be.screen = sc
	}
	ctx := context.Background()
	var kept backend.KeptRunning
	if err := a.StopAll(ctx); !errors.As(err, &kept) || kept.Others != 1 || stops.Load() != 0 {
		t.Fatalf("ほかの画面が開いているのに止めた / 数を返さない: %v stops=%d", err, stops.Load())
	}
	if err := b.StopAll(ctx); err != nil || stops.Load() != 1 {
		t.Fatalf("最後の画面が止めない: %v stops=%d", err, stops.Load())
	}
	c := shortState(t) // 印を置けなかった画面
	c.SetStopper(func(context.Context) error { stops.Add(1); return nil })
	if err := c.StopAll(ctx); err != nil || stops.Load() != 2 {
		t.Fatalf("印が無いのに止めない: %v stops=%d", err, stops.Load())
	}
}

// inboxEvents は受付の箱に置かれた画面の出来事の文 (置いた順)。
func inboxEvents(t *testing.T, dir string) []string {
	t.Helper()
	names, _ := filepath.Glob(filepath.Join(dir, store.InboxDir, "*.json"))
	sort.Strings(names)
	var out []string
	for _, n := range names {
		data, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		var r store.Request
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		if r.Kind == store.KindEvent {
			out = append(out, r.Note)
		}
	}
	return out
}

// 画面の出来事 (開いた・quit で閉じた・止めた / 止めなかった / 止めきれなかった) は受付の箱に置く (events.jsonl へ書くのは dispatcher。
// issue 445)。止めるときは止める前に置く (止める dispatcher が Shutdown の前に箱を適用して書く)。
func TestScreenEventsGoToInbox(t *testing.T) {
	a, b := shortState(t), shortState(t)
	b.dir, b.registry = a.dir, a.registry
	var atStop []string // 止める口が呼ばれた時点で箱にあった出来事
	fail := errors.New("止まらない")
	for _, be := range []*Backend{a, b} {
		be.SetStopper(func(context.Context) error { atStop = inboxEvents(t, a.dir); return fail })
	}
	ctx, cancel := context.WithCancel(context.Background())
	for _, be := range []*Backend{a, b} {
		be.Start(ctx)
	}
	defer func() { cancel(); a.Wait(); b.Wait() }()
	got := inboxEvents(t, a.dir)
	if len(got) != 2 || !strings.Contains(got[0], "開いた (開いている画面 1)") || !strings.Contains(got[1], "開いた (開いている画面 2)") {
		t.Fatalf("開いたことを置かない: %q", got)
	}
	var kept backend.KeptRunning
	if err := a.StopAll(ctx); !errors.As(err, &kept) {
		t.Fatal(err)
	}
	if got = inboxEvents(t, a.dir); len(got) != 3 || !strings.Contains(got[2], "ほかに 1 画面が開いているので dispatcher と PG は止めなかった") {
		t.Fatalf("止めなかったことを置かない: %q", got)
	}
	if err := b.StopAll(ctx); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if len(atStop) != 4 || !strings.Contains(atStop[3], "最後の画面なので dispatcher と PG を止める") {
		t.Fatalf("止める前に置かない: %q", atStop)
	}
	if got = inboxEvents(t, a.dir); len(got) != 5 || !strings.Contains(got[4], "止めきれなかった: 止まらない") {
		t.Fatalf("止めきれなかったことを置かない: %q", got)
	}
	if !strings.HasPrefix(got[0], fmt.Sprintf("画面 (pid %d): ", os.Getpid())) {
		t.Fatalf("どの画面の出来事か分からない: %q", got[0])
	}
}

// 画面を開くと印を置き、ほかの画面へ知らせる: ほかの画面は 3 秒のポーリングを待たずに「画面 2」になる。閉じると 1 に戻る。
func TestOpeningScreenUpdatesOthers(t *testing.T) {
	a := shortState(t)
	srv, err := wake.Listen(a.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	a.interval = time.Hour // 読み直すのは知らされたときだけ
	actx, acancel := context.WithCancel(context.Background())
	a.Start(actx)
	t.Cleanup(func() { acancel(); a.Wait() })
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for range 400 {
			if cond() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal(what)
	}
	waitFor("自分の画面を数えない / 購読しない", func() bool { return a.Poll().Screens == 1 && srv.Subscribers() == 1 })
	b := shortState(t)
	b.dir, b.registry, b.interval = a.dir, a.registry, time.Hour
	b.SetStopper(func(context.Context) error { return nil })
	bctx, bcancel := context.WithCancel(context.Background())
	b.Start(bctx)
	waitFor("ほかの画面が開いても「画面 2」にならない", func() bool { return a.Poll().Screens == 2 })
	var kept backend.KeptRunning
	if err := b.StopAll(context.Background()); !errors.As(err, &kept) {
		t.Fatalf("ほかの画面が開いているのに止めた: %v", err)
	}
	bcancel()
	b.Wait()
	waitFor("ほかの画面が閉じても 1 に戻らない", func() bool { return a.Poll().Screens == 1 })
}

// Wait は購読の goroutine も待つ (画面の終了で購読の接続を残さない)。
func TestWaitWaitsForSubscription(t *testing.T) {
	b := shortState(t)
	var ended atomic.Bool
	b.subscribe = func(ctx context.Context, _ *wake.Subscriber) {
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // 後始末に時間がかかる形
		ended.Store(true)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	cancel()
	b.Wait()
	if !ended.Load() {
		t.Fatal("購読が終わる前に Wait が戻った")
	}
}

// 一覧が空 (nil) でも、知らせによる読み直しのたびに一覧を取り直さない (claude agents は最大 10 秒)。
func TestEmptyListNotRefetchedOnKick(t *testing.T) {
	b := shortState(t)
	var lists atomic.Int32
	b.list = func(context.Context) ([]agents.Session, error) { lists.Add(1); return nil, nil }
	ctx := context.Background()
	b.refresh(ctx, true)
	b.refresh(ctx, false)
	b.refresh(ctx, false)
	if n := lists.Load(); n != 1 {
		t.Fatalf("空の一覧を知らせのたびに取り直した: %d 回", n)
	}
}

// 画面は dispatcher が居なければ (1 度も回っていない / 長く回っていない) 起こす。回っていれば起こさない。閉じる途中なら起こさない
// (最後の画面が止めた後で起こし直さない)。開いている印を置けなかった画面も起こさない (止める・起こすを繰り返さない)。
func TestKeepStartsDispatcherWhenAbsent(t *testing.T) {
	b := shortState(t)
	var starts int
	b.SetKeeper(func() error { starts++; return nil })
	b.SetStopper(func(context.Context) error { return nil })
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	set := func(tick time.Time) { b.mu.Lock(); b.snap.Now, b.snap.DispatcherTick = now, tick; b.mu.Unlock() }
	set(time.Time{})
	b.keep()
	if starts != 0 {
		t.Fatal("開いている印の無い画面が dispatcher を起こした")
	}
	sc, err := presence.Open(b.dir)
	if err != nil {
		t.Fatal(err)
	}
	b.screen = sc
	b.keep()
	set(now.Add(-keepAfter - time.Second))
	b.keep()
	set(now.Add(-time.Second))
	b.keep()
	if starts != 2 {
		t.Fatalf("居ない dispatcher を 2 回起こし、回っているときは起こさないはず: %d", starts)
	}
	if err := b.StopAll(context.Background()); err != nil { // 閉じる (最後の画面)
		t.Fatal(err)
	}
	set(time.Time{})
	b.keep()
	if starts != 2 {
		t.Fatal("閉じる途中の画面が dispatcher を起こした")
	}
}

// 画面の読み直しのループが、dispatcher が居ないときに起こす (配線)。
func TestStartKeepsDispatcher(t *testing.T) {
	b := shortState(t)
	b.interval = 20 * time.Millisecond
	var starts atomic.Int32
	b.SetKeeper(func() error { starts.Add(1); return nil })
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	defer func() { cancel(); b.Wait() }()
	for range 400 {
		if starts.Load() > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("dispatcher が居ないのに、読み直しのループが起こさない")
}

// attach の間 (from〜to) に人間が打った指示だけを、受付の箱に置く。dispatcher が適用すると原文のまま履歴に入る (issue 428)。
// transcript は全体を読む (末尾を読む ReadTail だと、長い attach の先頭の発言を落とす)。
func TestRecordAttachSubmitsHumanPromptsInWindow(t *testing.T) {
	b, _ := testBackend(t, sessions, nil)
	runningCard(t, b, "C-001", "bbbbbbbb")
	p := filepath.Join(t.TempDir(), "s.jsonl")
	head := `{"type":"user","timestamp":"2026-09-24T09:59:00Z","origin":{"kind":"human"},"message":{"content":"attach の最初の指示"}}`
	pad := strings.Repeat(`{"type":"system","content":"`+strings.Repeat("x", 1000)+`"}`+"\n", tailBytes/1000+10)
	huge := `{"type":"user","message":{"content":[{"type":"tool_result","content":"` + strings.Repeat("y", 9<<20) + `"}]}}` // 大きなツールの結果 1 行の後も読み続ける
	restart := `{"type":"user","timestamp":"2026-09-24T09:59:30Z","origin":{"kind":"human"},"message":{"content":"` + RestartNote + `."}}`
	if err := os.WriteFile(p, []byte(head+"\n"+huge+"\n"+restart+"\n"+pad+sample), 0o600); err != nil {
		t.Fatal(err)
	}
	var looked string
	b.findPath = func(id string) (string, error) { looked = id; return p, nil }
	from, to := time.Date(2026, 9, 24, 9, 59, 0, 0, time.UTC), time.Date(2026, 9, 24, 10, 0, 30, 0, time.UTC)
	n, err := b.RecordAttach("C-001", "bbbbbbbb", from, to)
	if err != nil || n != 2 || looked != "bbbbbbbb-2" {
		t.Fatalf("窓の中の人間の発言 2 件を、記録の session id の transcript から拾うはず: n=%d err=%v 引いた id=%q", n, err, looked)
	}
	if _, err := store.Apply(b.dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := store.Load(b.dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range st.Cards[0].History {
		if strings.HasPrefix(e.Text, store.AttachPrefix) {
			got = append(got, e.At.UTC().Format("15:04:05")+" "+strings.TrimPrefix(e.Text, store.AttachPrefix))
		}
	}
	if strings.Join(got, "|") != "09:59:00 attach の最初の指示|10:00:00 最初の依頼" {
		t.Fatalf("履歴に原文と打った時刻で入っていない (窓の外の発言・ツールの結果・他 session のメッセージ・再開の文は入れない): %q", got)
	}

	// 窓に発言が無ければ何も置かない
	if n, err := b.RecordAttach("C-001", "bbbbbbbb", to.Add(time.Hour), to.Add(2*time.Hour)); n != 0 || err != nil {
		t.Fatalf("発言の無い窓で n=%d err=%v", n, err)
	}
	if left, _ := filepath.Glob(filepath.Join(b.dir, store.InboxDir, "*.json")); len(left) != 0 {
		t.Fatalf("発言が無いのに箱に置いた: %v", left)
	}
	// pro-con が起動していない session は引かない
	if _, err := b.RecordAttach("C-001", "zzzzzzzz", from, to); err == nil {
		t.Fatal("記録に無い session の transcript を読んだ")
	}
}

// 見ているだけの画面 (pro-con --view) の backend は、止める口・書く口を持たず (型の上で)、依頼・回答・attach を受けない。
// 画面の印を置かないので、ほかの画面の「最後の画面か」の数えにも入らない。
func TestViewOnlyBackend(t *testing.T) {
	b := shortState(t)
	be := b.View()
	if _, ok := be.(backend.Stopper); ok {
		t.Fatal("見ているだけの backend が止める口を持っている")
	}
	if _, ok := be.(backend.AttachRecorder); ok {
		t.Fatal("見ているだけの backend が書く口 (attach の記録) を持っている")
	}
	if _, ok := be.(backend.ReadOnly); !ok {
		t.Fatal("読み取りだけだと画面に知らせない")
	}
	acc, ok := be.(backend.Accepter)
	if !ok {
		t.Fatal("操作を断れない")
	}
	for _, op := range []backend.Op{backend.OpNew, backend.OpAnswer, backend.OpOrder, backend.OpBtw, backend.OpClear, backend.OpDelete} {
		if acc.Accepts(op) {
			t.Fatalf("見ているだけなのに %s を受ける", op)
		}
	}
	if _, err := be.Apply(backend.NewRequest{Text: "x"}); !errors.Is(err, ErrViewOnly) {
		t.Fatalf("見ているだけなのに依頼を置いた: %v", err)
	}
	if _, err := be.AttachCommand("bbbbbbbb"); !errors.Is(err, ErrViewOnly) {
		t.Fatalf("見ているだけなのに attach のコマンドを返した: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	defer func() { cancel(); b.Wait() }()
	select { // 最初の読み直しが済むまで (読み直しの途中で書く経路も見る)
	case <-be.(backend.Notifier).Changed():
	case <-time.After(10 * time.Second):
		t.Fatal("読み直しが済まない")
	}
	if n, err := presence.Count(b.dir); err != nil || n != 0 {
		t.Fatalf("見ているだけの画面を数えた: %d %v", n, err)
	}
	if left, _ := filepath.Glob(filepath.Join(b.dir, store.InboxDir, "*.json")); len(left) != 0 {
		t.Fatalf("見ているだけの画面が受付の箱に書いた: %v", left)
	}
}

// 🚨 見ているだけの画面 (--view) は、socket の逃がし先 (/tmp/pro-con-<uid>/) の緩い権限を直さず、つながずに読み直しで出し、
// 違反の行に 1 行知らせる (直すのは dispatcher だけ。issue 445)。
func TestViewDoesNotFixFallbackDir(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "pcfb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Cleanup(wake.SetFallbackRoot(root)) // 本物の /tmp/pro-con-<uid> に触らない
	fallback := filepath.Join(root, fmt.Sprintf("pro-con-%d", os.Getuid()))
	if err := os.Mkdir(fallback, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fallback, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := testBackend(t, nil, nil)
	long := filepath.Join(root, strings.Repeat("d", 110))
	if err := os.Mkdir(long, 0o700); err != nil {
		t.Fatal(err)
	}
	b.dir, b.registry, b.interval = long, filepath.Join(long, RegistryFile), 20*time.Millisecond
	if filepath.Dir(wake.Path(long)) != fallback {
		t.Fatalf("前提: socket が逃がし先に倒れない: %s", wake.Path(long))
	}
	be := b.View()
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	defer func() { cancel(); b.Wait() }()
	deadline := time.After(10 * time.Second)
	for told := false; !told; {
		select {
		case <-be.(backend.Notifier).Changed():
		case <-deadline:
			t.Fatalf("逃がし先を使えないことを知らせない: %+v", be.Snapshot().Violations)
		}
		for _, v := range be.Snapshot().Violations {
			told = told || strings.Contains(v.Reason, "読み直しで出す")
		}
	}
	if st, err := os.Stat(fallback); err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("見ているだけの画面が逃がし先の権限を直した: %v %v", st.Mode(), err)
	}
	srv, err := wake.Listen(long) // dispatcher が起動して直すと、つながって知らせが消える
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	for told := true; told; {
		select {
		case <-be.(backend.Notifier).Changed():
		case <-deadline:
			t.Fatalf("つながった後も知らせが残る: %+v", be.Snapshot().Violations)
		}
		told = false
		for _, v := range be.Snapshot().Violations {
			told = told || strings.Contains(v.Reason, "読み直しで出す")
		}
	}
}

// 開いている印を置けない画面も、開いたことは置く (理由つき。閉じるときは最後の画面として止める)。
func TestScreenEventWithoutPresence(t *testing.T) {
	b := shortState(t)
	if err := os.WriteFile(filepath.Join(b.dir, presence.Dir), nil, 0o600); err != nil { // screens/ を作れなくする
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	defer func() { cancel(); b.Wait() }()
	if got := inboxEvents(t, b.dir); len(got) != 1 || !strings.Contains(got[0], "開いた (開いている印を置けない: ") {
		t.Fatalf("印を置けないまま開いたことを置かない: %q", got)
	}
}
