package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 実測した出力の形 (Claude Code 2.1.281。値は架空)。対話 session と、権限プロンプトで止まった裏の session。
const sample = `[
 {"pid":101,"cwd":"/Users/u/src/a","kind":"interactive","startedAt":1790000000000,"sessionId":"s-1","name":"a-1","status":"idle"},
 {"pid":202,"id":"3feb603f","cwd":"/Users/u/dotfiles/.claude/worktrees/pg-1","kind":"background","startedAt":1790000060000,
  "sessionId":"s-2","name":"PG","status":"waiting","waitingFor":"permission prompt","state":"blocked"}
]`

func runner(out, errOut string, err error) Runner {
	return func(context.Context) ([]byte, []byte, error) { return []byte(out), []byte(errOut), err }
}

func TestListParsesSessions(t *testing.T) {
	ss, err := List(context.Background(), runner(sample, "", nil), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 {
		t.Fatalf("2 本のはず: %d", len(ss))
	}
	bg := ss[1]
	if bg.Kind != "background" || bg.ID != "3feb603f" || bg.Status != "waiting" || bg.WaitingFor != "permission prompt" || bg.PID != 202 {
		t.Fatalf("裏の session の読み取りが違う: %+v", bg)
	}
	if !ss[0].Started().Equal(time.UnixMilli(1790000000000)) {
		t.Fatalf("開始時刻が違う: %v", ss[0].Started())
	}
}

// 空の配列は「0 本」として正常。空の出力・壊れた JSON・コマンドの失敗は 0 本ではなくエラー。
func TestListDistinguishesZeroFromFailure(t *testing.T) {
	if ss, err := List(context.Background(), runner("[]\n", "", nil), ""); err != nil || len(ss) != 0 {
		t.Fatalf("[] は 0 本の正常: %v %v", ss, err)
	}
	cases := []struct {
		name string
		run  Runner
		want string
	}{
		{"空の出力", runner("", "", nil), "空"},
		{"壊れた JSON", runner("[{", "", nil), "読めない"},
		{"コマンドの失敗", runner("", "Workspace not trusted\nmore", errors.New("exit status 1")), "Workspace not trusted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := List(context.Background(), tc.run, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%q を含むエラーのはず: %v", tc.want, err)
			}
		})
	}
}

// 終わらないコマンドは timeout で打ち切り、それとわかるエラーにする。
func TestListTimesOut(t *testing.T) {
	hang := func(ctx context.Context) ([]byte, []byte, error) { <-ctx.Done(); return nil, nil, ctx.Err() }
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := List(ctx, hang, "")
	if err == nil || !strings.Contains(err.Error(), "終わらない") {
		t.Fatalf("timeout のエラーのはず: %v", err)
	}
}

// 止まっているかは pid と、止まった state の許可リスト (stopped / done / failed) で決める (実測した 8 通り + 知らない state)。
func TestSessionStopped(t *testing.T) {
	for _, tc := range []struct {
		state string
		pid   int
		want  bool
	}{
		{"stopped", 0, true},   // claude stop
		{"done", 0, true},      // 作業を終えた session を claude stop (2.1.282、dogfooding で見つけた形)
		{"done", 15885, false}, // 作業を終えて idle のまま (プロセスは生きている)
		{"blocked", 42, false}, // turn を正常に終えて次の入力待ち
		{"failed", 42, false},  // API エラーで turn が落ちた
		{"failed", 0, true},    // マシンのクラッシュ・再起動でプロセスごと消えた (issue 482)
		{"working", 0, false},  // kill -9 の直後 (Claude Code が自動で再開する)
		{"working", 42, false}, // 作業中
		// 知らない state で pid 無し (再開の途中の名前が変わった版 / state の欄が無くなった版) は止まったと読まない (issue 466)
		{"resuming", 0, false},
		{"", 0, false},
	} {
		if got := (Session{State: tc.state, PID: tc.pid}).Stopped(); got != tc.want {
			t.Errorf("state=%s pid=%d: 止まっている=%v (%v のはず)", tc.state, tc.pid, got, tc.want)
		}
	}
}

// pid 無し・working の session だけ、Claude Code の記録 (<jobsDir>/<id>/state.json) を読む。記録が done なら止まっている
// (起床を予約したまま終えた session。issue 551)。kill -9 の直後の自動の再開の途中 (crashed / resuming)・記録が無い・壊れているは止まっていない。
// pid のある session・止まった形の session と、置き場の外を指す id の記録は読まない。知らない state は読む (案内に出す。issue 555)。
func TestListReadsJobStateForPidlessWorking(t *testing.T) {
	root := t.TempDir()
	jobs := filepath.Join(root, "jobs")
	write := func(id, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(jobs, id), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(jobs, id, "state.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("aaaa0001", `{"state":"done","inFlight":{"kinds":["session_cron"]}}`)
	write("aaaa0002", `{"state":"crashed"}`)
	write("aaaa0003", `{"state":"resuming"}`)
	write("aaaa0005", `{broken`)
	write("aaaa0006", `{"state":"done"}`)
	write("aaaa0007", `{"state":"crashed"}`)
	write("aaaa0008", `{"state":"done"}`)
	write("../x", `{"state":"done"}`) // 置き場の 1 つ上 (root/x)。id "../x" が指す先
	out := `[
 {"id":"aaaa0001","kind":"background","state":"working","sessionId":"s1"},
 {"id":"aaaa0002","kind":"background","state":"working","sessionId":"s2"},
 {"id":"aaaa0003","kind":"background","state":"working","sessionId":"s3"},
 {"id":"aaaa0004","kind":"background","state":"working","sessionId":"s4"},
 {"id":"aaaa0005","kind":"background","state":"working","sessionId":"s5"},
 {"id":"aaaa0006","pid":42,"kind":"background","state":"working","sessionId":"s6"},
 {"id":"../x","kind":"background","state":"working","sessionId":"s7"},
 {"id":"aaaa0007","kind":"background","state":"weird","sessionId":"s8"},
 {"id":"aaaa0008","kind":"background","state":"stopped","sessionId":"s9"}
]`
	ss, err := List(context.Background(), runner(out, "", nil), filepath.Join(jobs, "sub", ".."))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		job     string
		stopped bool
	}{
		"aaaa0001": {"done", true},     // 起床の予約が残ったまま終えた (C-089 の形)
		"aaaa0002": {"crashed", false}, // kill -9 の直後
		"aaaa0003": {"resuming", false},
		"aaaa0004": {"", false},        // 記録が無い
		"aaaa0005": {"", false},        // 記録が壊れている
		"aaaa0006": {"", false},        // pid がある (動いている) なら記録は読まない
		"../x":     {"", false},        // 置き場の外は読まない
		"aaaa0007": {"crashed", false}, // 知らない state も読む (止めきれなかったときの案内に出す = issue 555)。止まったとは読まない
		"aaaa0008": {"", true},         // 止まった形は読まない
	}
	for _, s := range ss {
		w := want[s.ID]
		if s.JobState != w.job || s.Stopped() != w.stopped {
			t.Errorf("%s: JobState=%q Stopped=%v (%q / %v のはず)", s.ID, s.JobState, s.Stopped(), w.job, w.stopped)
		}
	}
	// 置き場を渡さなければ読まない (今までどおり、pid 無し・working は止まっていない)
	ss, _ = List(context.Background(), runner(out, "", nil), "")
	if ss[0].JobState != "" || ss[0].Stopped() {
		t.Errorf("置き場なしで記録を読んだ: %+v", ss[0])
	}
}
