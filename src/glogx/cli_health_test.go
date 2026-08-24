package main

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestParseClaudeAuthStatus(t *testing.T) {
	tests := []struct {
		name string
		json string
		want cliHealthState
	}{
		{"true", `{"loggedIn":true}`, cliHealthy},
		{"false", `{"loggedIn":false}`, cliLoggedOut},
		{"missing", `{"authMethod":"none"}`, cliUnknown},
		{"broken", `{`, cliUnknown},
		{"empty", ``, cliUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseClaudeAuthStatus([]byte(tt.json)); got != tt.want {
				t.Fatalf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJudgeCodexLoginStatus(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		stderr   string
		want     cliHealthState
	}{
		{"logged in", 0, "Logged in using ChatGPT\n", cliHealthy},
		{"logged out", 1, "Not logged in\n", cliLoggedOut},
		{"exit one other", 1, "other\n", cliUnknown},
		{"exit zero other", 0, "other\n", cliUnknown},
		{"empty", 0, "", cliUnknown},
		{"whitespace", 0, "  Logged in using ChatGPT\n\n", cliHealthy},
		{"whitespace logged out", 1, "\n Not logged in \t", cliLoggedOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := judgeCodexLoginStatus(tt.exitCode, []byte(tt.stderr)); got != tt.want {
				t.Fatalf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

// exitOneError と signalExitError はテストで *exec.ExitError を作るためのヘルパー。
// os.ProcessState は外から合成できないため、検査対象 CLI ではない sh で終了形を作る。
func exitOneError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	if err == nil {
		t.Fatal("exit 1 returned nil")
	}
	return err
}

func signalExitError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	if err == nil {
		t.Fatal("signal exit returned nil")
	}
	return err
}

func TestCommandExitCodeSignalIsUnknown(t *testing.T) {
	if _, ok := commandExitCode(signalExitError(t)); ok {
		t.Fatal("signal exit was treated as a known exit code")
	}
}

func TestCheckClaudeHealthExitStatus(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })
	lookPathFn = func(string) (string, error) { return "/fake/claude", nil }

	tests := []struct {
		name   string
		stdout string
		err    error
		want   cliHealthState
	}{
		{"exit 1 logged out", `{"loggedIn":false}`, exitOneError(t), cliLoggedOut},
		{"exit 0 logged out", `{"loggedIn":false}`, nil, cliLoggedOut},
		{"exit 0 logged in", `{"loggedIn":true}`, nil, cliHealthy},
		{"exit 1 broken JSON", `{`, exitOneError(t), cliUnknown},
		{"exit 1 empty stdout", ``, exitOneError(t), cliUnknown},
		{"deadline exceeded", `{"loggedIn":false}`, context.DeadlineExceeded, cliUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cliHealthRunner = func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
				return []byte(tt.stdout), nil, tt.err
			}
			if got := checkClaudeHealth(context.Background()); got != tt.want {
				t.Fatalf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckCLIHealthExpiredContextDoesNotUseOutput(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })
	lookPathFn = func(name string) (string, error) { return "/fake/" + name, nil }
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	exitErr := exitOneError(t)
	cliHealthRunner = func(_ context.Context, name string, _ ...string) ([]byte, []byte, error) {
		if strings.HasSuffix(name, "claude") {
			return []byte(`{"loggedIn":false}`), nil, exitErr
		}
		return nil, []byte("Not logged in\n"), exitErr
	}
	if got := checkClaudeHealth(expired); got != cliUnknown {
		t.Fatalf("claude state = %v, want unknown", got)
	}
	if got := checkCodexHealth(expired); got != cliUnknown {
		t.Fatalf("codex state = %v, want unknown", got)
	}
}

func TestCheckCLIHealthSignalExitWithOutputIsUnknown(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })
	lookPathFn = func(name string) (string, error) { return "/fake/" + name, nil }
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	signalErr := signalExitError(t)
	cliHealthRunner = func(_ context.Context, name string, _ ...string) ([]byte, []byte, error) {
		if strings.HasSuffix(name, "claude") {
			return []byte(`{"loggedIn":false}`), nil, signalErr
		}
		return nil, []byte("Not logged in\n"), signalErr
	}
	if got := checkClaudeHealth(expired); got != cliUnknown {
		t.Fatalf("claude state = %v, want unknown", got)
	}
	if got := checkCodexHealth(expired); got != cliUnknown {
		t.Fatalf("codex state = %v, want unknown", got)
	}
}

func TestCheckCodexHealthExitZeroLoggedIn(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })
	lookPathFn = func(string) (string, error) { return "/fake/codex", nil }
	cliHealthRunner = func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		return nil, []byte("Logged in using ChatGPT\n"), nil
	}
	if got := checkCodexHealth(context.Background()); got != cliHealthy {
		t.Fatalf("codex state = %v, want healthy", got)
	}
}

func TestCheckCLIHealthRunnerArguments(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })
	lookPathFn = func(name string) (string, error) { return "/fake/" + name, nil }
	var calls []string
	cliHealthRunner = func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if strings.HasSuffix(name, "claude") {
			return []byte(`{"loggedIn":true}`), nil, nil
		}
		return nil, []byte("Logged in using ChatGPT\n"), nil
	}
	if got := checkClaudeHealth(context.Background()); got != cliHealthy {
		t.Fatalf("claude state = %v, want healthy", got)
	}
	if got := checkCodexHealth(context.Background()); got != cliHealthy {
		t.Fatalf("codex state = %v, want healthy", got)
	}
	want := []string{"/fake/claude auth status", "/fake/codex login status"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("runner calls = %#v, want %#v", calls, want)
	}
}

func TestCheckCLIHealthCmdIssues(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })

	tests := []struct {
		name        string
		missing     string
		claudeJSON  string
		codexErr    error
		codexStderr string
		want        cliHealthIssue
	}{
		{"claude missing", "claude", ``, nil, ``, cliHealthIssue{cli: "claude", state: cliMissing}},
		{"claude logged out", "", `{"loggedIn":false}`, nil, ``, cliHealthIssue{cli: "claude", state: cliLoggedOut}},
		{"codex missing", "codex", `{"loggedIn":true}`, nil, ``, cliHealthIssue{cli: "codex", state: cliMissing}},
		{"codex logged out", "", `{"loggedIn":true}`, nil, "Not logged in\n", cliHealthIssue{cli: "codex", state: cliLoggedOut}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookPathFn = func(name string) (string, error) {
				if name == tt.missing {
					return "", exec.ErrNotFound
				}
				return "/fake/" + name, nil
			}
			cliHealthRunner = func(_ context.Context, name string, _ ...string) ([]byte, []byte, error) {
				if strings.Contains(name, "claude") {
					claudeErr := error(nil)
					if tt.want.cli == "claude" && tt.want.state == cliLoggedOut {
						claudeErr = exitOneError(t)
					}
					return []byte(tt.claudeJSON), nil, claudeErr
				}
				codexErr := tt.codexErr
				if tt.want.cli == "codex" && tt.want.state == cliLoggedOut {
					codexErr = exitOneError(t)
				}
				return nil, []byte(tt.codexStderr), codexErr
			}
			msg := checkCLIHealthCmd()().(cliHealthMsg)
			if !reflect.DeepEqual(msg.issues, []cliHealthIssue{tt.want}) {
				t.Fatalf("issues = %#v, want %#v", msg.issues, []cliHealthIssue{tt.want})
			}
		})
	}
}

func TestCheckCLIHealthCmdBothIssuesInSpecOrder(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })
	lookPathFn = func(name string) (string, error) { return "/fake/" + name, nil }
	claudeErr, codexErr := exitOneError(t), exitOneError(t)
	cliHealthRunner = func(_ context.Context, name string, _ ...string) ([]byte, []byte, error) {
		if name == "/fake/claude" {
			return []byte(`{"loggedIn":false}`), nil, claudeErr
		}
		return nil, []byte("Not logged in\n"), codexErr
	}
	msg := checkCLIHealthCmd()().(cliHealthMsg)
	want := []cliHealthIssue{
		{cli: "claude", state: cliLoggedOut},
		{cli: "codex", state: cliLoggedOut},
	}
	if !reflect.DeepEqual(msg.issues, want) {
		t.Fatalf("issues = %#v, want %#v", msg.issues, want)
	}
}

func TestCheckCLIHealthCmdKeepsCodexIssueWhenClaudeTimesOut(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	defer func() { lookPathFn, cliHealthRunner = origLookPath, origRunner }()
	lookPathFn = func(name string) (string, error) { return "/fake/" + name, nil }
	codexErr := exitOneError(t)
	codexReturned := make(chan struct{})
	cliHealthRunner = func(ctx context.Context, name string, _ ...string) ([]byte, []byte, error) {
		if name == "/fake/claude" {
			<-ctx.Done()
			return []byte(`{"loggedIn":false}`), nil, ctx.Err()
		}
		close(codexReturned)
		return nil, []byte("Not logged in\n"), codexErr
	}
	result := make(chan cliHealthMsg, 1)
	go func() { result <- checkCLIHealthCmd()().(cliHealthMsg) }()
	codexWasParallel := false
	select {
	case <-codexReturned:
		codexWasParallel = true
	case <-time.After(time.Second):
	}
	var msg cliHealthMsg
	select {
	case msg = <-result:
	case <-time.After(cliHealthTimeout + time.Second):
		t.Fatal("health command did not finish")
	}
	if !codexWasParallel {
		t.Fatal("codex check did not run before claude timeout")
	}
	want := []cliHealthIssue{{cli: "codex", state: cliLoggedOut}}
	if !reflect.DeepEqual(msg.issues, want) {
		t.Fatalf("issues = %#v, want %#v", msg.issues, want)
	}
}

func TestCheckCLIHealthCmdUnknownIsSilent(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	t.Cleanup(func() { lookPathFn, cliHealthRunner = origLookPath, origRunner })
	lookPathFn = func(string) (string, error) { return "/fake/cli", nil }
	cliHealthRunner = func(_ context.Context, name string, _ ...string) ([]byte, []byte, error) {
		if strings.Contains(name, "claude") {
			return []byte(`{"loggedIn":false`), nil, errors.New("execution failed")
		}
		return nil, []byte("future output"), errors.New("execution failed")
	}
	msg := checkCLIHealthCmd()().(cliHealthMsg)
	if len(msg.issues) != 0 {
		t.Fatalf("unknown result produced issues: %#v", msg.issues)
	}

	cliHealthRunner = func(ctx context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	started := time.Now()
	msg = checkCLIHealthCmd()().(cliHealthMsg)
	if len(msg.issues) != 0 || time.Since(started) < cliHealthTimeout {
		t.Fatalf("timeout result = %#v, elapsed = %s", msg.issues, time.Since(started))
	}

	lookPathFn = func(string) (string, error) { return "", errors.New("permission denied") }
	msg = checkCLIHealthCmd()().(cliHealthMsg)
	if len(msg.issues) != 0 {
		t.Fatalf("non-NotFound LookPath error produced issues: %#v", msg.issues)
	}
}

func TestBrowseUpdateCLIHealth(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	_, cmd := m.Update(cliHealthMsg{issues: []cliHealthIssue{
		{cli: "claude", state: cliLoggedOut},
		{cli: "codex", state: cliMissing},
	}})
	if cmd == nil {
		t.Fatal("cliHealthMsg returned nil command")
	}
	if len(m.toast.older) != 1 || !strings.Contains(m.toast.text, "codex が見つかりません") ||
		!strings.Contains(m.toast.older[0].text, "claude がログアウト状態です") {
		t.Fatalf("toast stack = top %q older %#v", m.toast.text, m.toast.older)
	}
	if !strings.Contains(m.lastWarning, "codex が見つかりません") {
		t.Fatalf("lastWarning = %q", m.lastWarning)
	}
}

func TestBrowseInitWiresCLIHealth(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	origLatestClaude, origLatestCodex := fetchLatestClaudeVersion, fetchLatestCodexVersion
	origTmuxPrefix := loadTmuxPrefix
	t.Cleanup(func() {
		lookPathFn, cliHealthRunner = origLookPath, origRunner
		fetchLatestClaudeVersion, fetchLatestCodexVersion = origLatestClaude, origLatestCodex
		loadTmuxPrefix = origTmuxPrefix
	})
	lookPathFn = func(name string) (string, error) { return "/fake/" + name, nil }
	claudeErr := exitOneError(t)
	cliHealthRunner = func(_ context.Context, name string, _ ...string) ([]byte, []byte, error) {
		if name == "/fake/claude" {
			return []byte(`{"loggedIn":false}`), nil, claudeErr
		}
		return nil, []byte("Logged in using ChatGPT\n"), nil
	}
	fetchLatestClaudeVersion = func(context.Context) string { return "" }
	fetchLatestCodexVersion = func(context.Context) string { return "" }
	loadTmuxPrefix = func() string { return "" }
	m := newTestBrowse(t, 1, nil, nil)
	// usage と通常 tick はこの配線テストの対象外なので、長時間 Cmd を作らない。
	m.usageOv.inFlight = true
	m.ticking = true

	got, found := initCLIHealthMsg(t, m)
	if !found {
		t.Fatal("Init Cmd tree did not deliver cliHealthMsg")
	}
	m.Update(got)
	want := []cliHealthIssue{{cli: "claude", state: cliLoggedOut}}
	if !reflect.DeepEqual(got.issues, want) {
		t.Fatalf("issues = %#v, want %#v", got.issues, want)
	}
}

func TestCLIHealthWarningTexts(t *testing.T) {
	tests := []struct {
		name  string
		issue cliHealthIssue
		want  string
	}{
		{"claude missing", cliHealthIssue{cli: "claude", state: cliMissing}, "claude が見つかりません (Claude Code をインストールしてください)"},
		{"claude logged out", cliHealthIssue{cli: "claude", state: cliLoggedOut}, "claude がログアウト状態です (claude auth login で再ログイン)"},
		{"codex missing", cliHealthIssue{cli: "codex", state: cliMissing}, "codex が見つかりません (codex をインストールしてください)"},
		{"codex logged out", cliHealthIssue{cli: "codex", state: cliLoggedOut}, "codex がログアウト状態です (codex login で再ログイン)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cliHealthWarning(tt.issue); got != tt.want {
				t.Fatalf("warning = %q, want %q", got, tt.want)
			}
		})
	}
}

// initCLIHealthMsg は Init が積んだ Cmd ツリーを辿って cliHealthMsg を取り出す。
// 起動配線 (Init に検査が乗っているか) を見るテストが共有する。
func initCLIHealthMsg(t *testing.T, m *browseModel) (cliHealthMsg, bool) {
	t.Helper()
	cmd := m.Init()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init result = %T, want tea.BatchMsg", cmd())
	}
	for _, child := range batch {
		if child == nil {
			continue
		}
		result := make(chan tea.Msg, 1)
		go func(cmd tea.Cmd) { result <- cmd() }(child)
		select {
		case msg := <-result:
			if health, ok := msg.(cliHealthMsg); ok {
				return health, true
			}
		case <-time.After(250 * time.Millisecond):
		}
	}
	return cliHealthMsg{}, false
}

// 依頼文「glogx 起動時に、claude / codex がインストールされていない・logout 状態なら
// その旨をエラートーストで表示して」を 1 本で通す受け入れテスト。
// Init が検査を積む → 検査が issue を返す → Update が警告を積む → View に文言が出る、
// の全リンクを繋ぐ (個別に pin してあっても、繋がっていることは別に確かめる必要がある)。
func TestBrowseStartupShowsCLIHealthWarningsInView(t *testing.T) {
	origLookPath, origRunner := lookPathFn, cliHealthRunner
	origLatestClaude, origLatestCodex := fetchLatestClaudeVersion, fetchLatestCodexVersion
	origTmuxPrefix := loadTmuxPrefix
	t.Cleanup(func() {
		lookPathFn, cliHealthRunner = origLookPath, origRunner
		fetchLatestClaudeVersion, fetchLatestCodexVersion = origLatestClaude, origLatestCodex
		loadTmuxPrefix = origTmuxPrefix
	})
	// claude は未インストール (LookPath が ErrNotFound)、codex はログアウト。
	lookPathFn = func(name string) (string, error) {
		if name == "claude" {
			return "", exec.ErrNotFound
		}
		return "/fake/" + name, nil
	}
	codexErr := exitOneError(t)
	cliHealthRunner = func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		return nil, []byte("Not logged in\n"), codexErr
	}
	fetchLatestClaudeVersion = func(context.Context) string { return "" }
	fetchLatestCodexVersion = func(context.Context) string { return "" }
	loadTmuxPrefix = func() string { return "" }

	m := newTestBrowse(t, 3, nil, nil)
	// usage と通常 tick はこのテストの対象外なので長時間 Cmd を作らせない。
	m.usageOv.inFlight = true
	m.ticking = true
	// newTestBrowse は NoFrame なので page = height-1。height=10 (page=9) は
	// toastDrawBudget が重要警告 2 枚分を確保する帯 (これを外すと 1 枚しか描かれない)。
	m.width, m.height = 120, 10
	if got := m.pageSize(); got != 9 {
		t.Fatalf("height=10 の page = %d, want 9 (前提が崩れた)", got)
	}

	msg, ok := initCLIHealthMsg(t, m)
	if !ok {
		t.Fatal("Init が積んだ Cmd から cliHealthMsg が届かない (起動配線が切れている)")
	}
	m.Update(msg)
	for i := 0; m.toast.animating() && i < 100; i++ {
		m.toast.advance(m.colored)
	}

	out := stripANSI(m.View().Content)
	for _, want := range []string{
		"claude が見つかりません (Claude Code をインストールしてください)",
		"codex がログアウト状態です (codex login で再ログイン)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("起動後の View に %q が出ない:\n%s", want, out)
		}
	}
	// 表示が消えた後も w でコピーできること (showWarning 経由である保証)。
	if !strings.Contains(m.lastWarning, "codex がログアウト状態です") {
		t.Fatalf("lastWarning = %q", m.lastWarning)
	}
}
