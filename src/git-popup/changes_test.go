package main

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseStatusUsesPorcelainZContract(t *testing.T) {
	// 実 git status --porcelain -z を模す: NUL 区切り・quoting 無し。rename は NEW の
	// 次の NUL トークンに OLD が来る。空白入りパスや ` -> ` を含むパスも quoting 無しで
	// そのまま来る (理想化した tab 区切りにはしない)。末尾 NUL で空トークンが 1 つ残る。
	out := " M modified.go\x00M  staged.go\x00?? new file.txt\x00R  new name.txt\x00old name.txt\x00" +
		"?? a -> b\x00 D deleted.go\x00"
	got, err := parseStatus(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{Index: ' ', Worktree: 'M', Path: "modified.go"},
		{Index: 'M', Worktree: ' ', Path: "staged.go"},
		{Index: '?', Worktree: '?', Path: "new file.txt"},
		{Index: 'R', Worktree: ' ', Path: "new name.txt", OldPath: "old name.txt"},
		{Index: '?', Worktree: '?', Path: "a -> b"}, // ` -> ` を含むパスを rename 誤認しない
		{Index: ' ', Worktree: 'D', Path: "deleted.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStatus = %#v, want %#v", got, want)
	}
}

func TestToggleGitArgs(t *testing.T) {
	if got, want := toggleGitArgs(Change{Worktree: ' ', Path: "staged.go"}), []string{"restore", "--staged", "--", "staged.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unstage args = %#v, want %#v", got, want)
	}
	if got, want := toggleGitArgs(Change{Worktree: 'D', Path: "deleted.go"}), []string{"add", "--", "deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stage args = %#v, want %#v", got, want)
	}
	// staged rename の unstage は新旧両パスを対象にする (片側だけ残さない)
	if got, want := toggleGitArgs(Change{Index: 'R', Worktree: ' ', Path: "new", OldPath: "old"}), []string{"restore", "--staged", "--", "new", "old"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rename unstage args = %#v, want %#v", got, want)
	}
}

func TestRootModeToggle(t *testing.T) {
	m := newRootModel(nil)
	if m.mode != modeLog {
		t.Fatalf("initial mode = %v, want log", m.mode)
	}
	model, cmd := m.Update(keyMsg("ctrl+l"))
	if cmd == nil || model.(*rootModel).mode != modeChanges {
		t.Fatalf("first toggle did not enter changes: mode=%v cmd=%v", model.(*rootModel).mode, cmd != nil)
	}
	m = model.(*rootModel)
	m.log.cursor = 1
	model, cmd = m.Update(keyMsg("ctrl+l"))
	if cmd != nil || model.(*rootModel).mode != modeLog || model.(*rootModel).log.cursor != 1 {
		t.Fatalf("second toggle did not restore log state: mode=%v cursor=%d cmd=%v", model.(*rootModel).mode, model.(*rootModel).log.cursor, cmd != nil)
	}
}

func TestCommitInput(t *testing.T) {
	m := newChangesModel()
	m.handleKey("ctrl+o")
	m.handleKey("a")
	m.handleKey("b")
	m.handleKey("backspace")
	if got := string(m.message); got != "a" {
		t.Fatalf("message after backspace = %q, want %q", got, "a")
	}
	if _, cmd := m.handleKey("enter"); cmd == nil {
		t.Fatal("non-empty Enter did not create commit command")
	}
	if m.input != inputNone {
		t.Fatal("commit input did not close after Enter")
	}
	m.handleKey("ctrl+o")
	if _, cmd := m.handleKey("enter"); cmd != nil {
		t.Fatal("empty Enter unexpectedly created commit command")
	}
	m.handleKey("x")
	m.handleKey("esc")
	if m.input != inputNone || len(m.message) != 0 {
		t.Fatal("Esc did not cancel commit input")
	}
}

func keyMsg(key string) tea.KeyMsg {
	if key == "ctrl+l" {
		return tea.KeyMsg{Type: tea.KeyCtrlL}
	}
	return tea.KeyMsg{}
}
