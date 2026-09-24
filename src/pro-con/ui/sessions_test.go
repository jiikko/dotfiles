package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/agents"
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

// s で一覧のパネルを開閉する。開いている間は session の名前と、裏の session の待ちの理由が出る。
func TestSessionsPanelToggles(t *testing.T) {
	m := sessionsModel(t, twoSessions, nil)
	m.Update(m.fetchSessions()())
	press(m, "s")
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "dotfiles-5c") || !strings.Contains(out, "permission prompt") {
		t.Fatalf("パネルに session が出ていない:\n%s", out)
	}
	press(m, "s")
	if strings.Contains(ansi.Strip(m.render()), "permission prompt") {
		t.Fatal("閉じたのにパネルが残っている")
	}
}
