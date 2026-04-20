package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// editorDoneMsg is emitted after the external editor exits.
type editorDoneMsg struct {
	path string
	err  error
}

// openEditorCmd suspends the TUI and runs $EDITOR (or $VISUAL, or vi) on the
// given path. When the editor exits the TUI resumes. Assigned to a var so
// tests can stub the editor launch.
var openEditorCmd = func(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("%s %s", editor, shellSingleQuote(path)))
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	})
}
