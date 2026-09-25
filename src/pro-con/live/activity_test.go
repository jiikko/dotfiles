package live

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/backend"
)

// 活動は PG の応答の文と道具の呼び出しだけ (思考・道具の結果・人間の発言は出さない)。道具は要点 (コマンド・worktree の中の相対パス) を 1 行で、
// 応答の文は改行を残して (詳細が markdown として描く。486)、どちらも制御文字は落とす。
func TestActivitiesFromAssistantRecord(t *testing.T) {
	o := Owned{ID: "s1", SessionID: "sess-1", Cwd: "/wt/pc-c-001"}
	line := `{"type":"assistant","timestamp":"2026-09-25T11:31:00Z","message":{"content":[` +
		`{"type":"thinking","thinking":""},` +
		`{"type":"text","text":"テストを\n回す"},` +
		`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./dispatcher/...\n","description":"テスト"}},` +
		`{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/wt/pc-c-001/dispatcher/close.go","old_string":"a","new_string":"b"}},` +
		`{"type":"tool_use","id":"t3","name":"Read","input":{"file_path":"/elsewhere/x.go"}},` +
		`{"type":"tool_use","id":"t4","name":"TaskList","input":{}},` +
		`{"type":"tool_use","id":"t5","name":"Bash","input":{"command":"printf '\u001b]52;c;x\u0007'"}},` +
		`{"type":"text","text":"見出し\u001b]52;c;x\u0007\n- 項目"}]}}`
	got := activities([]byte(line), o, nil)
	want := []string{"テストを\n回す", "Bash: go test ./dispatcher/...", "Edit: dispatcher/close.go", "Read: /elsewhere/x.go", "TaskList: -"}
	if len(got) != 7 {
		t.Fatalf("活動の数が違う: %d %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Line() != w || got[i].Session != "s1" || !got[i].At.Equal(time.Date(2026, 9, 25, 11, 31, 0, 0, time.UTC)) {
			t.Fatalf("%d 番目: %+v (want %q)", i, got[i], w)
		}
	}
	if strings.ContainsAny(got[5].Text, "\x1b\x07") {
		t.Fatalf("道具の引数の制御文字を落としていない: %q", got[5].Text)
	}
	if strings.ContainsAny(got[6].Text, "\x1b\x07") || !strings.Contains(got[6].Text, "\n- 項目") {
		t.Fatalf("応答の文の制御文字を落として改行は残す、になっていない: %q", got[6].Text)
	}
	for _, other := range []string{
		`{"type":"user","timestamp":"2026-09-25T11:32:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"user","timestamp":"2026-09-25T11:32:00Z","origin":{"kind":"human"},"message":{"content":"人間の発言"}}`,
		`{"type":"ai-title","aiTitle":"題名"}`,
	} {
		if as := activities([]byte(other), o, nil); len(as) != 0 {
			t.Fatalf("PG の応答・呼び出しでないものを活動にした: %s → %+v", other, as)
		}
	}
}

// 長いコマンドは切る (heredoc 1 本で画面を埋めない)。
func TestActivityClipsLongToolInput(t *testing.T) {
	in, _ := json.Marshal(map[string]string{"command": strings.Repeat("あ", toolRunes+50)})
	line := `{"type":"assistant","timestamp":"2026-09-25T11:31:00Z","message":{"content":[{"type":"tool_use","name":"Bash","input":` + string(in) + `}]}}`
	got := activities([]byte(line), Owned{ID: "s1"}, nil)
	if len(got) != 1 || len([]rune(got[0].Text)) != toolRunes+1 || !strings.HasSuffix(got[0].Text, "…") {
		t.Fatalf("長いコマンドを切っていない: %d 文字", len([]rune(got[0].Text)))
	}
}

// cardLogFixture は C-001 の前の session (退いた側) と今の session、別のカードの session、カードの無い古い行を記録に置く。
func cardLogFixture(t *testing.T) (dir, projects string) {
	t.Helper()
	dir, projects = t.TempDir(), t.TempDir()
	now := time.Now()
	putRows(t, filepath.Join(dir, RetiredFile), []Owned{{ID: "s0", SessionID: "sess-0", PID: 1, CardID: "C-001", StartedAt: now.Add(-time.Hour)}})
	putRows(t, filepath.Join(dir, RegistryFile), []Owned{
		{ID: "s1", SessionID: "sess-1", PID: 2, CardID: "C-001", StartedAt: now},
		{ID: "s9", SessionID: "sess-9", PID: 3, CardID: "C-009", StartedAt: now},
		{ID: "s1", SessionID: "sess-old", PID: 4, StartedAt: now}, // カードの無い古い行 (短い id が今の session と同じ)
	})
	for id, text := range map[string]string{"sess-0": "前の session の出力", "sess-1": "今の session の出力", "sess-9": "別のカード", "sess-old": "古い行"} {
		at := map[string]string{"sess-0": "10:00", "sess-1": "11:00", "sess-9": "11:00", "sess-old": "10:30"}[id]
		appendLine(t, filepath.Join(projects, "-wt", id+".jsonl"), assistantText("2026-09-25T"+at+":00Z", text))
	}
	return dir, projects
}

func putRows(t *testing.T, path string, rows []Owned) {
	t.Helper()
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func assistantText(at, text string) string {
	return `{"type":"assistant","timestamp":"` + at + `","message":{"content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
}

func texts(as []backend.Activity) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Session+":"+a.Text)
	}
	return out
}

// カードの活動は、再開で入れ替わった前の session から時刻の順に続けて出す。別のカードの session は出さない。
// 2 回目からは足された行だけを返し、書きかけの最後の行 (改行なし) は書き終わってから出す。
func TestCardLogFollowsSessionsOfCard(t *testing.T) {
	dir, projects := cardLogFixture(t)
	l := NewCardLog(filepath.Join(dir, RegistryFile), func(id string) (string, error) { return FindTranscript(projects, id) }, "C-001", "s1")
	got, err := l.Next()
	if err != nil {
		t.Fatal(err)
	}
	if want := "s0:前の session の出力 s1:古い行 s1:今の session の出力"; strings.Join(texts(got), " ") != want {
		t.Fatalf("カードの活動が違う:\n got %v\nwant %s", texts(got), want)
	}
	if got, _ := l.Next(); len(got) != 0 {
		t.Fatalf("足されていないのに活動を返した: %v", texts(got))
	}
	cur := filepath.Join(projects, "-wt", "sess-1.jsonl")
	half := assistantText("2026-09-25T11:05:00Z", "書き終えた")
	appendLine(t, cur, half[:len(half)/2])
	if got, _ := l.Next(); len(got) != 0 {
		t.Fatalf("書きかけの行を活動にした: %v", texts(got))
	}
	appendLine(t, cur, half[len(half)/2:])
	if got, _ := l.Next(); strings.Join(texts(got), " ") != "s1:書き終えた" {
		t.Fatalf("書き終えた行を出さない / 前に出した分を繰り返した: %v", texts(got))
	}
	// 再開で新しい session が記録に足された (transcript は後から書かれる)
	putRows(t, filepath.Join(dir, RegistryFile), []Owned{{ID: "s2", SessionID: "sess-2", PID: 5, CardID: "C-001", StartedAt: time.Now()}})
	if got, err := l.Next(); err != nil || len(got) != 0 {
		t.Fatalf("transcript のまだ無い session で: %v %v", texts(got), err)
	}
	appendLine(t, filepath.Join(projects, "-wt2", "sess-2.jsonl"), assistantText("2026-09-25T12:00:00Z", "再開した"))
	if got, _ := l.Next(); strings.Join(texts(got), " ") != "s2:再開した" {
		t.Fatalf("記録から外れた前の session を読み続けていない / 新しい session を拾わない: %v", texts(got))
	}
}

// 起動の記録が壊れていたら、空ではなくエラー (読めないのと、まだ無いのを分ける)。
func TestCardLogBrokenRegistry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RegistryFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewCardLog(filepath.Join(dir, RegistryFile), func(string) (string, error) { return "", os.ErrNotExist }, "C-001", "")
	if _, err := l.Next(); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("壊れた記録をエラーにしない: %v", err)
	}
}

// 画面の口 (Backend.Activity) は呼ぶたびに続きを読み、そのカードの今までの活動を古い順に返す。--view の画面 (View) からも読める。
// 記録に無いカードは空。
func TestBackendActivityAccumulates(t *testing.T) {
	b, _ := testBackend(t, nil, nil)
	projects := t.TempDir()
	b.findPath = func(id string) (string, error) { return FindTranscript(projects, id) }
	if err := Register(b.registry, Owned{ID: "s1", SessionID: "sess-1", PID: 2, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	runningCard(t, b, "C-001", "s1")
	b.Refresh(context.Background())
	tr := filepath.Join(projects, "-wt", "sess-1.jsonl")
	appendLine(t, tr, assistantText("2026-09-25T11:00:00Z", "一つ目"))
	if got, err := b.Activity("C-001"); err != nil || strings.Join(texts(got), " ") != "s1:一つ目" {
		t.Fatalf("活動: %v %v", texts(got), err)
	}
	appendLine(t, tr, assistantText("2026-09-25T11:01:00Z", "二つ目"))
	r, ok := b.View().(backend.ActivityReader)
	if !ok {
		t.Fatal("--view の画面が活動を読めない")
	}
	if got, err := r.Activity("C-001"); err != nil || strings.Join(texts(got), " ") != "s1:一つ目 s1:二つ目" {
		t.Fatalf("前に読んだ分と足された分を続けて返さない: %v %v", texts(got), err)
	}
	if got, err := b.Activity("C-404"); err != nil || len(got) != 0 {
		t.Fatalf("記録に無いカードの活動: %v %v", texts(got), err)
	}
}

func assistantUUID(at, uuid, text string) string {
	return `{"type":"assistant","uuid":"` + uuid + `","timestamp":"` + at + `","message":{"content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
}

// 再開した session の transcript は前の session のレコードを同じ uuid で写して始まる (実 transcript で確認)。写しは出さず、
// 元の session の活動として 1 度だけ出す (記録の並びが新しい順でも、起動の古い順に読む)。後から写しが足されても重ねない。
func TestCardLogSkipsCopiedRecordsOfResumedSession(t *testing.T) {
	dir, projects := t.TempDir(), t.TempDir()
	now := time.Now()
	putRows(t, filepath.Join(dir, RegistryFile), []Owned{
		{ID: "s1", SessionID: "sess-1", PID: 2, CardID: "C-001", StartedAt: now},
		{ID: "s0", SessionID: "sess-0", PID: 1, CardID: "C-001", StartedAt: now.Add(-time.Hour)},
	})
	appendLine(t, filepath.Join(projects, "-wt", "sess-0.jsonl"), assistantUUID("2026-09-25T10:00:00Z", "u1", "前の一つ目"))
	cur := filepath.Join(projects, "-wt", "sess-1.jsonl")
	appendLine(t, cur, assistantUUID("2026-09-25T10:00:00Z", "u1", "前の一つ目")+assistantUUID("2026-09-25T11:00:00Z", "u2", "再開した"))
	l := NewCardLog(filepath.Join(dir, RegistryFile), func(id string) (string, error) { return FindTranscript(projects, id) }, "C-001", "s1")
	if got, _ := l.Next(); strings.Join(texts(got), " ") != "s0:前の一つ目 s1:再開した" {
		t.Fatalf("写しを重ねた / 元の session の名で出さない: %v", texts(got))
	}
	appendLine(t, cur, assistantUUID("2026-09-25T10:00:00Z", "u1", "前の一つ目")+assistantUUID("2026-09-25T11:01:00Z", "u3", "続き"))
	if got, _ := l.Next(); strings.Join(texts(got), " ") != "s1:続き" {
		t.Fatalf("後から足された写しを重ねた: %v", texts(got))
	}
}

// transcript が消えて別の場所に現れたら探し直して続きを読み、読んだ位置より短く書き直されたら頭から読み直す (uuid で重ねない)。
func TestCardLogRefindsMovedAndRewrittenTranscript(t *testing.T) {
	dir, projects := t.TempDir(), t.TempDir()
	putRows(t, filepath.Join(dir, RegistryFile), []Owned{{ID: "s1", SessionID: "sess-1", PID: 2, CardID: "C-001", StartedAt: time.Now()}})
	first := filepath.Join(projects, "-a", "sess-1.jsonl")
	appendLine(t, first, assistantUUID("2026-09-25T10:00:00Z", "u1", "一つ目")+assistantUUID("2026-09-25T10:01:00Z", "u2", "長い二つ目の出力"))
	l := NewCardLog(filepath.Join(dir, RegistryFile), func(id string) (string, error) { return FindTranscript(projects, id) }, "C-001", "s1")
	if got, _ := l.Next(); len(got) != 2 {
		t.Fatalf("前提: %v", texts(got))
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Next(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("消えた transcript を知らせない: %v", err)
	}
	appendLine(t, filepath.Join(projects, "-b", "sess-1.jsonl"), assistantUUID("2026-09-25T10:00:00Z", "u1", "一つ目")+assistantUUID("2026-09-25T10:02:00Z", "u3", "三"))
	if got, err := l.Next(); err != nil || strings.Join(texts(got), " ") != "s1:三" {
		t.Fatalf("別の場所に現れた transcript を探し直して頭から読まない: %v %v", texts(got), err)
	}
	rewritten := assistantUUID("2026-09-25T10:03:00Z", "u4", "四")
	if err := os.WriteFile(filepath.Join(projects, "-b", "sess-1.jsonl"), []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := l.Next(); err != nil || strings.Join(texts(got), " ") != "s1:四" {
		t.Fatalf("読んだ位置より短く書き直された transcript を頭から読み直さない: %v %v", texts(got), err)
	}
}
