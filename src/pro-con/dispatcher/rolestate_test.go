package dispatcher

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// pmStateIn は dispatcher が最後に書いた PM の様子 (issue 476)。
func pmStateIn(t *testing.T, dir string) card.RoleState {
	t.Helper()
	ds, _, err := store.LoadDispatcherState(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := card.FindRole(ds.RoleStates, card.PMName)
	if !ok {
		t.Fatalf("dispatcher の様子に PM が無い: %+v", ds.RoleStates)
	}
	return s
}

// dispatcher は Tick ごとに PM の様子を書く: 起動した Tick と一覧に出るまでは起動中、一覧の status で作業中 / idle / 入力待ち。
// 知らせ済みで依頼の列に残っているカードも添え、分けて列を離れたら外す。
func TestRoleStateFollowsPM(t *testing.T) {
	r := newPMRig(t)
	r.tick(t)
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleNone || s.Max != 1 || s.Alive() {
		t.Fatalf("依頼が無く起こしていない PM の様子: %+v", s)
	}
	request(t, r.dir, "一つ目")
	r.tick(t)
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleLaunch || !slices.Equal(s.Cards, []string{"C-001"}) {
		t.Fatalf("起動した Tick の PM の様子: %+v", s)
	}
	r.tick(t) // 一覧にまだ出ていない (起動の直後)。落ちたと出さない
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleLaunch {
		t.Fatalf("起動の直後で一覧に出ていない PM を %s と出した", s.Phase)
	}
	for _, tc := range []struct {
		status string
		want   card.RolePhase
	}{{"busy", card.RoleBusy}, {"waiting", card.RoleAsking}, {"idle", card.RoleIdle}} {
		r.ss = []agents.Session{pmSession("id-"+pmName, "P1", 60, tc.status, t0.Add(time.Second))}
		r.tick(t)
		if s := pmStateIn(t, r.dir); s.Phase != tc.want || !s.Alive() || s.Session != "id-"+pmName {
			t.Fatalf("status %s の PM の様子: %+v (want %s)", tc.status, s, tc.want)
		}
	}
	request(t, r.dir, "二つ目") // idle の PM を再開して知らせた Tick は、一覧 (再開の前に取った) の idle ではなく起動中
	r.tick(t)
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleLaunch || !slices.Equal(s.Cards, []string{"C-001", "C-002"}) {
		t.Fatalf("再開した Tick の PM の様子: %+v", s)
	}
	if _, err := store.Submit(r.dir, store.Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1}}}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if s := pmStateIn(t, r.dir); !slices.Equal(s.Cards, []string{"C-002"}) {
		t.Fatalf("分けて依頼の列を離れたカードを PM の手元に残した: %v", s.Cards)
	}
}

// session の一覧を取れない Tick は、前の Tick の様子 (生きている) を今のものとして書かない。
func TestRoleStateNotStaleWhenListFails(t *testing.T) {
	r := startedPM(t)
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleIdle {
		t.Fatalf("前提: 一覧に idle で出た PM の様子 = %s", s.Phase)
	}
	r.d.List = func(context.Context) ([]agents.Session, error) {
		return nil, errors.New("claude agents が返らない")
	}
	r.tick(t)
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleChecking || s.Alive() || s.Why == "" {
		t.Fatalf("一覧を取れない Tick の PM の様子: %+v", s)
	}
}

// 起こせないときは理由を添える (枠が尽きた)。起こさない設定なら off。
func TestRoleStateBlockedAndOff(t *testing.T) {
	r := newPMRig(t)
	r.d.Usage = func(context.Context) (Usage, error) { return Usage{Session: usageStopAt}, nil }
	request(t, r.dir, "一つ目")
	r.tick(t)
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleBlocked || !strings.Contains(s.Why, "新しく起動・再開しない") {
		t.Fatalf("枠で起こさない PM の様子: %+v", s)
	}
	r.d.PMOff = true
	r.tick(t)
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleOff || s.Why != "" {
		t.Fatalf("起こさない設定の PM の様子: %+v", s)
	}
}

// 1 回の turn で数枚知らせても、PM が今扱っているカードは、新しい方から見て対象に知らせ済みのカードが 1 枚だけ出た呼び出しのもの (480)。
// 最後の起動・再開より前 (再開した session が写した前の turn)・人の発言より前の呼び出しは見ない。どのカードとも言えない呼び出しで打ち切る。
func TestRoleStateCurrentCard(t *testing.T) {
	r := startedPM(t)
	request(t, r.dir, "二つ目")
	request(t, r.dir, "三つ目")
	r.now = t0.Add(time.Minute)
	r.tick(t) // idle の PM を再開して C-002 / C-003 を知らせた
	if s := pmStateIn(t, r.dir); s.Phase != card.RoleLaunch || s.Current != "" {
		t.Fatalf("知らせを渡した Tick に扱っているカードを決めた: %+v", s)
	}
	var tr live.Transcript
	var asked []string
	r.d.Transcript = func(sid string) (live.Transcript, error) {
		asked = append(asked, sid)
		return tr, nil
	}
	at := func(sec int) time.Time { return r.now.Add(time.Duration(sec) * time.Second) }
	use := func(sec int, name, text, target string) {
		tr.Uses = append(tr.Uses, live.Call{Name: name, Text: text, Target: target, At: at(sec)})
	}
	want := func(current, last string) {
		t.Helper()
		r.tick(t)
		if s := pmStateIn(t, r.dir); s.Current != current || s.Last != last {
			t.Fatalf("扱っているカード %q / 最後の道具の呼び出し %q (want %q / %q): %+v", s.Current, s.Last, current, last, s)
		}
	}
	use(-30, "Bash", "pro-con card show C-003", "pro-con card show C-003") // 前の turn
	r.ss = []agents.Session{pmSession("id-"+pmName, "P1", 60, "busy", t0.Add(time.Second))}
	want("", "")
	use(3, "Read", "/w/dotfiles/pm-guide.md", "/w/dotfiles/pm-guide.md")
	want("", "Read: /w/dotfiles/pm-guide.md") // ID がまだ出ていない (知らせ済みが何枚でも当て推量で決めない)
	use(4, "Bash", "まとめて読む", "pro-con card show C-002; pro-con card show C-003")
	want("", "Bash: まとめて読む") // 2 枚出た呼び出しはどちらとも言えない
	use(5, "Bash", "カードを読む", "pro-con card show C-002")
	use(7, "Read", "/w/dotfiles/issues/C-002_notes.md", "/w/dotfiles/issues/C-002_notes.md")
	want("C-002", "Read: /w/dotfiles/issues/C-002_notes.md")
	if len(asked) == 0 || asked[len(asked)-1] != "P1" {
		t.Fatalf("PM の今の session の transcript を読んでいない: %v", asked)
	}
	use(8, "Bash", "issue を書く", "cat > /w/issues/9.md <<'EOF'") // ID の無い呼び出しは扱っているカードを変えない (heredoc の本文を切るのは live.callTarget)
	want("C-002", "Bash: issue を書く")
	use(9, "Bash", "積む", "pro-con card plan C-003 --after C-002") // 順番の前のカードは数えない
	want("C-003", "Bash: 積む")
	use(10, "Bash", "足す", "pro-con card add C-099") // 知らせていないカード: 最後の呼び出しは C-003 のものではない
	want("", "Bash: 足す")
	tr.Prompts = []live.Prompt{{At: at(11), Text: "別のことをして"}} // 人が attach して打った: それより前の呼び出しは今の turn ではない
	want("", "")
	use(12, "Bash", "見る", "pro-con card show C-001")
	want("C-001", "Bash: 見る")
	use(13, "Read", "外のチケット", "/w/notes/ABC-002.md") // 別の番号の一部 (C-002 ではない)
	want("C-001", "Read: 外のチケット")
	r.ss[0].Status = "idle"
	want("", "")
}
