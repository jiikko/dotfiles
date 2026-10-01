package runner

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type recordingPresenter struct {
	mu     sync.Mutex
	models []Model
}

func (p *recordingPresenter) Render(m Model) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.models = append(p.models, m)
}

func (p *recordingPresenter) Keys() <-chan string { return nil }
func (p *recordingPresenter) Close() error        { return nil }

func (p *recordingPresenter) sawStage(stage TransitionStage) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.models {
		if m.Transition.Active && m.Transition.Stage == stage {
			return true
		}
	}
	return false
}

// runInProcess runs a child that exits on its own and returns rc and stdout.
func runInProcess(t *testing.T, cfg Config) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cfg.ControlPath = filepath.Join(shortTempDir(t, "rinp-"), "runner.sock")
	cfg.Stdin = strings.NewReader("")
	cfg.Stdout, cfg.Stderr = &stdout, &stderr
	code, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run: %v; stderr: %s", err, stderr.String())
	}
	return code, stdout.String()
}

// 「起動」の段 (子を exec する間) は、子が起動して段が進む前に presenter へ渡す。
func TestLaunchStageReachesPresenterBeforeChildStarts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build string
	}{{"after build", "true"}, {"without build", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			presenter := &recordingPresenter{}
			code, _ := runInProcess(t, Config{BuildCommand: tc.build, RunArgs: []string{"/bin/sh", "-c", "exit 0"},
				Headless: true, Presenter: presenter})
			if code != 0 {
				t.Fatalf("rc = %d, want 0", code)
			}
			if !presenter.sawStage(TransitionLaunch) {
				t.Fatalf("presenter never received the launch stage: %+v", presenter.models)
			}
		})
	}
}

// UI が無くても stdout が端末なら子の出力を無害化する。端末でない出力先 (pipe / file) には byte のまま流す。
func TestHeadlessOutputIsSanitizedOnlyWhenStdoutIsTerminal(t *testing.T) {
	const child = `printf 'title\033]0;evil\007ok\n\033[31mred\033[0m\n\033[2Jclear\n'`
	for _, tc := range []struct {
		name     string
		terminal bool
		want     string
	}{
		{"terminal", true, "titleok\n\x1b[0m\x1b[31mred\x1b[0m\x1b[0m\nclear\n"},
		{"pipe", false, "title\x1b]0;evil\x07ok\n\x1b[31mred\x1b[0m\n\x1b[2Jclear\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout := runInProcess(t, Config{RunArgs: []string{"/bin/sh", "-c", child},
				Headless: true, StdoutIsTerminal: tc.terminal})
			if code != 0 {
				t.Fatalf("rc = %d, want 0", code)
			}
			if stdout != tc.want {
				t.Fatalf("stdout = %q, want %q", stdout, tc.want)
			}
		})
	}
}
