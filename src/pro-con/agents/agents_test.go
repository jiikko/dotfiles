package agents

import (
	"context"
	"errors"
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
	ss, err := List(context.Background(), runner(sample, "", nil))
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
	if ss, err := List(context.Background(), runner("[]\n", "", nil)); err != nil || len(ss) != 0 {
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
			_, err := List(context.Background(), tc.run)
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
	_, err := List(ctx, hang)
	if err == nil || !strings.Contains(err.Error(), "終わらない") {
		t.Fatalf("timeout のエラーのはず: %v", err)
	}
}

// 止まっているかは pid と state で決める (state の名前の一覧に頼らない。実測した 6 通り)。
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
		{"working", 0, false},  // kill -9 の直後 (Claude Code が自動で再開する)
		{"working", 42, false}, // 作業中
	} {
		if got := (Session{State: tc.state, PID: tc.pid}).Stopped(); got != tc.want {
			t.Errorf("state=%s pid=%d: 止まっている=%v (%v のはず)", tc.state, tc.pid, got, tc.want)
		}
	}
}
