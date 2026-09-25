package dispatcher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

func btw(t *testing.T, dir, id, q string) {
	t.Helper()
	if _, err := store.Submit(dir, store.Request{Kind: "btw", CardID: id, Question: q}); err != nil {
		t.Fatal(err)
	}
}

// answered は答えが記録に出るまで Tick する (答えは裏で作る)。上限は止まったときの保険で、時間を測る検査ではない。
func answered(t *testing.T, r *crashRig) card.Btw {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r.tick(t)
		if c := states(t, r.dir)["C-001"]; len(c.Btws) > 0 && !c.Btws[0].Answered.IsZero() {
			return c.Btws[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("btw に答えない")
	return card.Btw{}
}

// btw は PG を止めず (再開も止めもしない)、別のプロセス (Ask) が PG の出力の末尾と質問から答え、答えを記録に書く (415 要件 9 / issue 438)。
func TestBtwAnswersWithoutTouchingPG(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = "busy"
	r.d.Transcript = func(string) (live.Transcript, error) {
		return live.Transcript{Outputs: []string{"make test を待っている", "lint を直した"}}, nil
	}
	var prompt string
	r.d.Ask = func(_ context.Context, p string) (string, error) {
		prompt = p
		return "lint を直して make test 待ち", nil
	}
	btw(t, r.dir, "C-001", "今どう?")
	b := answered(t, r)
	if b.Answer != "lint を直して make test 待ち" || !strings.Contains(prompt, "今どう?") || !strings.Contains(prompt, "lint を直した") {
		t.Fatalf("PG の出力と質問から答えていない: answer=%q prompt=%q", b.Answer, prompt)
	}
	if len(r.l.resumes) != 0 || len(r.l.stops) != 0 {
		t.Fatalf("btw で PG を止めた / 再開した: resumes=%v stops=%v", r.l.resumes, r.l.stops)
	}
	c := states(t, r.dir)["C-001"]
	if !strings.Contains(c.History[len(c.History)-1].Text, "lint を直して make test 待ち") {
		t.Fatalf("答えが履歴に出ない: %+v", c.History)
	}
}

// PG の出力が無ければ (まだ起動していない等) 答えを作る口を呼ばず、記録から答える。答えを作れなければ、その旨と記録からの答えを書く。
func TestBtwFallsBackToRecord(t *testing.T) {
	r := newCrashRig(t)
	called := 0
	r.d.Ask = func(context.Context, string) (string, error) { called++; return "", errors.New("枠切れ") }
	btw(t, r.dir, "C-001", "今どう?")
	if b := answered(t, r); called != 0 || !strings.Contains(b.Answer, "C-001 は 作業中") {
		t.Fatalf("材料の無い btw で答えを作る口を呼んだ / 記録から答えていない: called=%d %q", called, b.Answer)
	}
	r = newCrashRig(t)
	r.d.Transcript = func(string) (live.Transcript, error) { return live.Transcript{Outputs: []string{"x"}}, nil }
	r.d.Ask = func(context.Context, string) (string, error) { return "", errors.New("枠切れ") }
	btw(t, r.dir, "C-001", "今どう?")
	if b := answered(t, r); !strings.Contains(b.Answer, "答えを作れなかった") || !strings.Contains(b.Answer, "枠切れ") || !strings.Contains(b.Answer, "作業中") {
		t.Fatalf("作れなかったことと記録からの答えを書いていない: %q", b.Answer)
	}
}
