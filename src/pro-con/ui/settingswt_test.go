package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/wtclean"
)

// wtSpy は残した worktree を読み・決める backend (backend.WorktreeReader / WorktreeDecider)。決めた依頼を控える。
type wtSpy struct {
	*inspSpy
	vs      []wtclean.Verdict
	decided []string // "消す pc-c-004" / "残す pc-c-004"
}

func (s *wtSpy) Worktrees() ([]wtclean.Verdict, error) { return s.vs, nil }
func (s *wtSpy) DecideWorktree(v wtclean.Verdict, remove bool) (wtclean.Result, error) {
	verb, out := "残す", wtclean.Held
	if remove {
		verb, out = "消す", wtclean.Removed
	}
	s.decided = append(s.decided, verb+" "+v.Name)
	return wtclean.Result{Verdict: v, Outcome: out, Detail: "ok"}, nil
}

// viewWTSpy は見ているだけの画面 (決める口を受けない)。
type viewWTSpy struct{ *wtSpy }

func (viewWTSpy) Accepts(backend.Op) bool { return false }
func (viewWTSpy) ReadOnly()               {}

func newWTSpy() *wtSpy {
	return &wtSpy{inspSpy: newInspSpy(), vs: []wtclean.Verdict{
		{Name: "pc-c-001", CardID: "C-001", Repo: "dotfiles", Action: wtclean.RemoveAll, Why: "先端が origin/master の祖先"},
		{Name: "pc-c-004", CardID: "C-004", Repo: "dotfiles", Path: "/r/.claude/worktrees/pc-c-004", Action: wtclean.Keep, Ask: true, Why: "origin/master に無い commit が 1 本ある"},
		{Name: "pc-c-026", Repo: "dotfiles", Path: "/r/.claude/worktrees/pc-c-026", Action: wtclean.Keep, Why: "未 commit の変更・追跡していないファイルが 1 件 (x.txt)"},
	}}
}

// ディスクのタブに、自動の片付けが消さない worktree を理由つきで出す (消すものは出さない)。人が決められる行だけを選べる。
func TestDiskTabListsKeptWorktrees(t *testing.T) {
	m := openSettingsFor(t, newWTSpy())
	toTab(t, m, tabDisk)
	screen := ansi.Strip(strings.Join(m.settingsPanel(60), "\n"))
	for _, want := range []string{"残した worktree", "pc-c-004", "origin/master に無い commit が 1 本ある", "pc-c-026", "未 commit の変更"} {
		if !strings.Contains(screen, want) {
			t.Errorf("%q が出ない:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "pc-c-001  ") && strings.Contains(screen, "先端が origin/master の祖先") {
		t.Errorf("自動で消すものまで残したものに出した:\n%s", screen)
	}
	if got := m.worktreeKeys(); len(got) != 1 || got[0] != wtKeyPrefix+"/r/.claude/worktrees/pc-c-004" {
		t.Errorf("選べる行 = %v (人が決められる pc-c-004 だけ)", got)
	}
}

// D は 2 回押して消す (1 回目は構えるだけ・ほかのキーで外れる)。L は 1 回で残す。
func TestDiskTabDecidesWorktree(t *testing.T) {
	be := newWTSpy()
	m := openSettingsFor(t, be)
	toTab(t, m, tabDisk)
	m.set.cursor = len(m.set.disk.Groups) // 置き場の行の次 = pc-c-004
	run(m, press(m, "D"))
	if len(be.decided) != 0 || m.set.wt.armed == "" {
		t.Fatalf("1 回目の D で消した / 構えていない: %v", be.decided)
	}
	press(m, "r") // ほかのキーで構えが外れる
	if m.set.wt.armed != "" {
		t.Fatal("ほかのキーで構えが外れない")
	}
	m.set.cursor = len(m.set.disk.Groups)
	run(m, press(m, "D"))
	run(m, press(m, "D"))
	run(m, press(m, "L"))
	if strings.Join(be.decided, ",") != "消す pc-c-004,残す pc-c-004" {
		t.Errorf("決めた依頼 = %v", be.decided)
	}
}

// 見ているだけの画面は決めない。置き場の行で D を押しても何もしない。
func TestDiskTabDecideRefused(t *testing.T) {
	be := newWTSpy()
	m := openSettingsFor(t, viewWTSpy{be})
	toTab(t, m, tabDisk)
	m.set.cursor = len(m.set.disk.Groups)
	run(m, press(m, "D", "D", "L"))
	m.set.cursor = 0
	run(m, press(m, "D", "D"))
	if len(be.decided) != 0 {
		t.Errorf("見ているだけの画面で決めた: %v", be.decided)
	}
}
