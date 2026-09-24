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

// testBackend は ss を一覧に返す backend。ss の session はすべて「pro-con が起動した」記録に入れる (絞り込みは TestOnlyOwnedSessions)。
func testBackend(t *testing.T, ss []agents.Session, err error) (*Backend, *int) {
	t.Helper()
	reads := 0
	state := t.TempDir()
	b := New([]backend.Repo{{Name: "dotfiles", Path: "/w/dotfiles"}, {Name: "sub", Path: "/w/dotfiles/src/sub"}}, t.TempDir(), state)
	for _, s := range ss {
		if e := Register(b.registry, Owned{SessionID: s.SessionID, PID: s.PID}); e != nil {
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
	{SessionID: "aaaaaaaa-1", PID: 101, Kind: "interactive", Status: "busy", Cwd: "/w/dotfiles/src/sub/x", Name: "対話"},
	{SessionID: "bbbbbbbb-2", ID: "bbbbbbbb", PID: 102, Kind: "background", Status: "waiting", WaitingFor: "permission prompt", Cwd: "/w/dotfiles"},
	{SessionID: "cccccccc-3", ID: "cccccccc", PID: 103, Kind: "background", Status: "waiting", WaitingFor: "input needed", Cwd: "/w/other"},
	{SessionID: "dddddddd-4", PID: 104, Kind: "interactive", Status: "idle", Cwd: "/w/dotfiles-wt-x"},
}

// session 1 本をカード 1 枚に写す。状態・待ちの種類・repo (一番長く一致したもの)・attach の口 (裏の session だけ)。
func TestSessionsBecomeCards(t *testing.T) {
	b, _ := testBackend(t, sessions, nil)
	b.Refresh(context.Background())
	s := b.Poll()
	if len(s.Cards) != 4 || len(s.Violations) != 0 {
		t.Fatalf("カード %d 枚 / 不変条件の違反 %v", len(s.Cards), s.Violations)
	}
	want := []struct {
		id, repo, session string
		state             card.State
		wait              card.WaitKind
	}{
		{"S-aaaaaaaa", "sub", "", card.Running, card.WaitNone},
		{"S-bbbbbbbb", "dotfiles", "bbbbbbbb", card.Waiting, card.WaitPermission},
		{"S-cccccccc", "other", "cccccccc", card.Waiting, card.WaitQuestion},
		{"S-dddddddd", "dotfiles-wt-x", "", card.Review, card.WaitNone},
	}
	for i, w := range want {
		c := s.Cards[i]
		if c.ID != w.id || c.Repo != w.repo || c.Session != w.session || c.State != w.state || c.Wait.Kind != w.wait {
			t.Fatalf("%d 枚目: %+v (期待 %+v)", i, c, w)
		}
		if c.Title != "新しい題名" || c.Request != "次の依頼" {
			t.Fatalf("%d 枚目の題名・依頼: %q / %q", i, c.Title, c.Request)
		}
	}
	if len(s.Consumers) != 2 {
		t.Fatalf("裏の session だけを PG として数えるはず: %d", len(s.Consumers))
	}
}

// 一覧を取れなかったら、前のカードを残し、取れなかった理由を出す (0 本と区別する)。
func TestListFailureKeepsLastCards(t *testing.T) {
	b, _ := testBackend(t, sessions, nil)
	b.Refresh(context.Background())
	b.list = func(context.Context) ([]agents.Session, error) { return nil, errors.New("timeout") }
	b.Refresh(context.Background())
	s := b.Poll()
	if len(s.Cards) != 4 || len(s.Violations) != 1 || !strings.Contains(s.Violations[0].Reason, "timeout") {
		t.Fatalf("前のカードを残して理由を出すはず: %d 枚 / %v", len(s.Cards), s.Violations)
	}
}

// transcript の大きさと更新時刻が変わっていなければ読み直さない (3 秒ごとに 14MB 級を読まない)。
func TestTranscriptIsCached(t *testing.T) {
	b, reads := testBackend(t, sessions[:1], nil)
	b.Refresh(context.Background())
	b.Refresh(context.Background())
	if *reads != 1 {
		t.Fatalf("変わっていない transcript を %d 回読んだ", *reads)
	}
}

// 書き込みは受け付けない。attach できるのは裏の session だけ。
func TestReadOnly(t *testing.T) {
	b, _ := testBackend(t, sessions, nil)
	if _, err := b.Apply(backend.Answer{CardID: "x", Text: "y"}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("書き込みを受け付けた: %v", err)
	}
	if _, err := b.AttachCommand(""); err == nil {
		t.Fatal("対話の session (session id 無し) に attach できてしまう")
	}
	if cmd, err := b.AttachCommand("bbbbbbbb"); err != nil || strings.Join(cmd.Args, " ") != "claude attach bbbbbbbb" {
		t.Fatalf("裏の session の attach: %v %v", cmd, err)
	}
	if !b.ReadOnly() {
		t.Fatal("ReadOnly が偽")
	}
}

// 本物のモードは、pro-con が起動した session (記録にあるもの) だけを出す。照合は session id (長い方) か claude --bg の短い id。
// Desktop や他の shell の session は出さない (選べると pro-con の外の session に入力・停止できてしまう)。
func TestOnlyOwnedSessions(t *testing.T) {
	b, _ := testBackend(t, nil, nil)
	b.list = func(context.Context) ([]agents.Session, error) { return sessions, nil }
	if err := Register(b.registry, Owned{SessionID: "aaaaaaaa-1", PID: 101}); err != nil {
		t.Fatal(err)
	}
	if err := Register(b.registry, Owned{SessionID: "cccccccc-3", ID: "cccccccc", PID: 103}); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	var ids []string
	for _, c := range b.Poll().Cards {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "S-aaaaaaaa,S-cccccccc" {
		t.Fatalf("記録にある session だけを出すはず: %v", ids)
	}
	if !strings.Contains(b.Describe(), "2 本") {
		t.Fatalf("ヘッダーに記録の本数が出ない: %q", b.Describe())
	}
}

// 記録が空なら 0 本で、ヘッダーでそう言う。記録が壊れていたら、空とは区別して理由を出す。
func TestEmptyAndBrokenRegistry(t *testing.T) {
	b, _ := testBackend(t, nil, nil)
	b.list = func(context.Context) ([]agents.Session, error) { return sessions, nil }
	b.Refresh(context.Background())
	if len(b.Poll().Cards) != 0 || !strings.Contains(b.Describe(), "まだ無い") {
		t.Fatalf("記録が空なのに %d 枚 / %q", len(b.Poll().Cards), b.Describe())
	}
	if err := os.WriteFile(b.registry, []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	if v := b.Poll().Violations; len(v) != 1 || !strings.Contains(v[0].Reason, "記録を読めない") {
		t.Fatalf("壊れた記録を空と区別していない: %v", v)
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

// 依頼の原文は上限で切る (貼り付けた巨大な依頼を、詳細の描画のたびに折り返さない)。
func TestRequestIsClipped(t *testing.T) {
	b, _ := testBackend(t, sessions[:1], nil)
	b.read = func(string) (Transcript, error) {
		return Transcript{LastPrompt: strings.Repeat("あ", requestRunes*3)}, nil
	}
	b.Refresh(context.Background())
	if n := len([]rune(b.Poll().Cards[0].Request)); n > requestRunes+1 {
		t.Fatalf("依頼の原文が %d 文字 (上限 %d)", n, requestRunes)
	}
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
