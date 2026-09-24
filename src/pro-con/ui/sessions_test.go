package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/agents"
	"pro-con/backend"
)

var twoSessions = []agents.Session{
	{Kind: "interactive", Status: "idle", Name: "dotfiles-5c", Cwd: "/w/dotfiles", PID: 1, StartedAt: 1790000000000},
	{Kind: "background", Status: "waiting", WaitingFor: "permission prompt", Name: "PG", Cwd: "/w/pg", PID: 2, StartedAt: 1790000000000},
}

func sessionsModel(t *testing.T, ss []agents.Session, err error) *Model {
	t.Helper()
	m := New(newSpy(), nil)
	m.listSessions = func(context.Context) ([]agents.Session, error) { return ss, err }
	return m
}

// 取得した一覧が画面の状態に入り、次の取得が予約される (1 回で止まらない)。
func TestSessionsFetchedAndRescheduled(t *testing.T) {
	m := sessionsModel(t, twoSessions, nil)
	if _, cmd := m.Update(m.fetchSessions()()); cmd == nil {
		t.Fatal("次の取得が予約されない")
	}
	if len(m.sessions) != 2 || !strings.Contains(ansi.Strip(m.sessionsSummary()), "claude 2 本 (裏 1)") {
		t.Fatalf("一覧が入っていない: %d %q", len(m.sessions), ansi.Strip(m.sessionsSummary()))
	}
}

// 取れなかったときは 0 本として出さない。一度も取れていなければ「取れない」、取れた後なら最後の一覧を残して「更新失敗」。
func TestSessionsFailureIsNotZero(t *testing.T) {
	m := sessionsModel(t, nil, errors.New("claude が無い"))
	m.Update(m.fetchSessions()())
	if s := ansi.Strip(m.sessionsSummary()); !strings.Contains(s, "取れない") || strings.Contains(s, "0 本") {
		t.Fatalf("一度も取れていないのに 0 本に見える: %q", s)
	}
	m.listSessions = func(context.Context) ([]agents.Session, error) { return twoSessions, nil }
	m.Update(m.fetchSessions()())
	m.listSessions = func(context.Context) ([]agents.Session, error) { return nil, errors.New("timeout") }
	m.Update(m.fetchSessions()())
	if len(m.sessions) != 2 || !strings.Contains(ansi.Strip(m.sessionsSummary()), "更新失敗") {
		t.Fatalf("最後の一覧を残して更新失敗を出すはず: %d %q", len(m.sessions), ansi.Strip(m.sessionsSummary()))
	}
}

// s で PG の一覧を開閉する。PG ごとに担当カードが出る。PG でない session (Desktop の対話 等) は件数だけで名前は出さない。
// pgPanel は画面から PG の一覧の枠の中だけを切り出す (カンバンにも同じカード ID が出るので、画面全体では探さない)。
func pgPanel(m *Model) string {
	out := ansi.Strip(m.render())
	i := strings.Index(out, "PG (consumer)")
	if i < 0 {
		return ""
	}
	out = out[i:]
	if j := strings.Index(out, "╰"); j >= 0 {
		out = out[:j]
	}
	return out
}

func TestPGPanel(t *testing.T) {
	m := sessionsModel(t, twoSessions, nil)
	m.snap.Consumers = []backend.Consumer{{Session: "pg-1", CardID: "R1", Status: "busy"}}
	m.Update(m.fetchSessions()())
	press(m, "s")
	out := pgPanel(m)
	for _, want := range []string{"PG (consumer) 1/2", "pg-1", "R1", "模擬", "対話 1 / 裏 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("PG の一覧に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dotfiles-5c") {
		t.Fatal("PG でない session の名前を並べた")
	}
	press(m, "s")
	if strings.Contains(ansi.Strip(m.render()), "PG (consumer)") {
		t.Fatal("閉じたのにパネルが残っている")
	}
}

// PG と同じ session の claude の session があれば、その PG に pid を紐づけ、「ほかの session」から外す。
func TestPGPanelMatchesRealSession(t *testing.T) {
	bg := agents.Session{ID: "3feb603f", SessionID: "3feb603f-aaaa", Kind: "background", Status: "busy", PID: 4242}
	m := sessionsModel(t, append([]agents.Session{bg}, twoSessions...), nil)
	m.snap.Consumers = []backend.Consumer{{Session: "3feb603f", CardID: "R1", Status: "busy"}}
	m.Update(m.fetchSessions()())
	press(m, "s")
	out := pgPanel(m)
	if !strings.Contains(out, "pid 4242") || !strings.Contains(out, "対話 1 / 裏 1") {
		t.Fatalf("PG に pid を紐づけ、ほかの session から外すはず:\n%s", out)
	}
}
