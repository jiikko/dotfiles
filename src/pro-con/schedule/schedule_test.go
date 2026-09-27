package schedule

import (
	"slices"
	"testing"
	"time"
)

func TestSlotNextDue(t *testing.T) {
	j := Job{Hour: 4, Min: 0}
	loc := time.FixedZone("JST", 9*3600)
	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, loc) }
	cases := []struct {
		name       string
		now        time.Time
		slot, next time.Time
	}{
		{"予定の前", at(27, 3, 59), at(26, 4, 0), at(27, 4, 0)},
		{"予定ちょうど", at(27, 4, 0), at(27, 4, 0), at(28, 4, 0)},
		{"予定の後", at(27, 12, 0), at(27, 4, 0), at(28, 4, 0)},
	}
	for _, c := range cases {
		if got := j.Slot(c.now); !got.Equal(c.slot) {
			t.Errorf("%s: Slot = %v; want %v", c.name, got, c.slot)
		}
		if got := j.Next(c.now); !got.Equal(c.next) {
			t.Errorf("%s: Next = %v; want %v", c.name, got, c.next)
		}
	}
	due := []struct {
		name      string
		lastStart time.Time
		now       time.Time
		want      bool
	}{
		{"記録が無い", time.Time{}, at(27, 3, 0), true},
		{"今日の枠で始めた", at(27, 4, 0), at(27, 12, 0), false},
		{"昨日の枠で始めて、今日の予定の前", at(26, 4, 1), at(27, 3, 59), false},
		{"昨日の枠で始めて、今日の予定を過ぎた", at(26, 4, 1), at(27, 4, 0), true},
		{"3 日止まっていた (1 回だけ)", at(24, 4, 0), at(27, 9, 0), true},
		{"起き直して同じ枠 (始めた記録がある)", at(27, 9, 0), at(27, 9, 5), false},
		{"時計が戻った (始めた時刻が未来)", at(29, 4, 0), at(27, 9, 0), true},
	}
	for _, c := range due {
		if got := j.Due(c.lastStart, c.now); got != c.want {
			t.Errorf("%s: Due = %v; want %v", c.name, got, c.want)
		}
	}
}

// 画面の字面と起こす argv が同じ Args から来る (worktree clean の予定は --yes を字面にも出す)。
func TestJobsCommandMatchesArgs(t *testing.T) {
	i := slices.IndexFunc(Jobs, func(j Job) bool { return j.Name == "worktree-clean" })
	if i < 0 {
		t.Fatal("worktree-clean の予定が無い")
	}
	j := Jobs[i]
	if got := j.Command(); got != "pro-con worktree clean --yes" {
		t.Errorf("Command = %q", got)
	}
	if got := j.When(); got != "毎日 04:00" {
		t.Errorf("When = %q", got)
	}
	seen := map[string]bool{}
	for _, j := range Jobs {
		if j.Name == "" || seen[j.Name] || j.Hour < 0 || j.Hour > 23 || j.Min < 0 || j.Min > 59 || len(j.Args) == 0 {
			t.Errorf("予定の行が不正: %+v", j)
		}
		seen[j.Name] = true
	}
}

// 結果の行は末尾に rc を持ち、そこから読み戻せる (dispatcher が終わりを待てなかったときの成否)。
func TestResultLineRC(t *testing.T) {
	l := ResultLine("worktree 消した 1 (x)", 1)
	if l != "結果: worktree 消した 1 (x) (rc=1)" {
		t.Errorf("ResultLine = %q", l)
	}
	if rc, ok := ResultRC("待てなかった: " + l); !ok || rc != 1 {
		t.Errorf("ResultRC = %d %v", rc, ok)
	}
	for _, s := range []string{"", "結果: 古い形", "rc=0"} {
		if _, ok := ResultRC(s); ok {
			t.Errorf("%q から rc を読んだ", s)
		}
	}
}
