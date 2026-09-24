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

func testBackend(ss []agents.Session, err error) (*Backend, *int) {
	reads := 0
	dir := os.TempDir()
	b := New([]backend.Repo{{Name: "dotfiles", Path: "/w/dotfiles"}, {Name: "sub", Path: "/w/dotfiles/src/sub"}}, dir)
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
	{SessionID: "aaaaaaaa-1", Kind: "interactive", Status: "busy", Cwd: "/w/dotfiles/src/sub/x", Name: "対話"},
	{SessionID: "bbbbbbbb-2", ID: "bbbbbbbb", Kind: "background", Status: "waiting", WaitingFor: "permission prompt", Cwd: "/w/dotfiles"},
	{SessionID: "cccccccc-3", ID: "cccccccc", Kind: "background", Status: "waiting", WaitingFor: "input needed", Cwd: "/w/other"},
	{SessionID: "dddddddd-4", Kind: "interactive", Status: "idle", Cwd: "/w/dotfiles-wt-x"},
}

// session 1 本をカード 1 枚に写す。状態・待ちの種類・repo (一番長く一致したもの)・attach の口 (裏の session だけ)。
func TestSessionsBecomeCards(t *testing.T) {
	b, _ := testBackend(sessions, nil)
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
	b, _ := testBackend(sessions, nil)
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
	b, reads := testBackend(sessions[:1], nil)
	b.Refresh(context.Background())
	b.Refresh(context.Background())
	if *reads != 1 {
		t.Fatalf("変わっていない transcript を %d 回読んだ", *reads)
	}
}

// 書き込みは受け付けない。attach できるのは裏の session だけ。
func TestReadOnly(t *testing.T) {
	b, _ := testBackend(nil, nil)
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
