package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type cliHealthState uint8

const (
	cliUnknown cliHealthState = iota
	cliMissing
	cliLoggedOut
	cliHealthy
)

type cliHealthIssue struct {
	cli   string
	state cliHealthState
}

type cliHealthMsg struct {
	issues []cliHealthIssue
}

// cliHealthTimeout は CLI のコールドスタートやマシン負荷を含めて待つ上限。短すぎると
// ログアウトを判定できる前に unknown (無通知) へ落ちるため、claude --version と同じ 5 秒にする。
const cliHealthTimeout = 5 * time.Second

// cliHealthRunner は health 検査用の差し替え点。claude の判定材料は stdout、codex の判定材料は
// stderr に出るという実測を保つため、stdout と stderr を分けた既存の契約をそのまま使う。
var cliHealthRunner CommandRunner = ExecRunner

type claudeAuthStatus struct {
	LoggedIn *bool `json:"loggedIn"`
}

func parseClaudeAuthStatus(stdout []byte) cliHealthState {
	var status claudeAuthStatus
	if err := json.Unmarshal(stdout, &status); err != nil || status.LoggedIn == nil {
		return cliUnknown
	}
	if *status.LoggedIn {
		return cliHealthy
	}
	return cliLoggedOut
}

// codex は判定材料を stdout ではなく stderr に出す実測仕様なので、ここでは stderr だけを見る。
// 未ログインは文言の完全一致、ログイン済みは認証方式名が変わりうるため前方一致とする。
func judgeCodexLoginStatus(exitCode int, stderr []byte) cliHealthState {
	status := strings.TrimSpace(string(stderr))
	switch {
	case exitCode == 0 && strings.HasPrefix(status, "Logged in"):
		return cliHealthy
	case exitCode == 1 && status == "Not logged in":
		return cliLoggedOut
	default:
		return cliUnknown
	}
}

func commandExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	// シグナル終了の ExitCode は -1。出力が残っていても、終了コードを得られたとは
	// 扱わず、外部プロセスの途中結果をログイン状態の確定材料にしない。
	exitCode := exitErr.ExitCode()
	if exitCode < 0 {
		return 0, false
	}
	return exitCode, true
}

func checkClaudeHealth(ctx context.Context) cliHealthState {
	path, err := lookPathFn("claude")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return cliMissing
		}
		return cliUnknown
	}
	// claude auth status は未ログイン時に exit 1 を返すが、ログイン状態の本体は stdout の JSON。
	// exit code は出力本体より変更に弱いので、プロセスが起動して終了した ExitError なら JSON を読む。
	stdout, _, err := cliHealthRunner(ctx, path, "auth", "status")
	// timeout / cancel 後に残った stdout は途中結果であり、ログアウト判定に使わない。
	if ctx.Err() != nil {
		return cliUnknown
	}
	if err != nil {
		if _, ok := commandExitCode(err); !ok {
			return cliUnknown
		}
	}
	return parseClaudeAuthStatus(stdout)
}

func checkCodexHealth(ctx context.Context) cliHealthState {
	path, err := lookPathFn("codex")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return cliMissing
		}
		return cliUnknown
	}
	_, stderr, err := cliHealthRunner(ctx, path, "login", "status")
	// timeout / cancel 後に残った stderr は途中結果であり、ログイン判定に使わない。
	if ctx.Err() != nil {
		return cliUnknown
	}
	if err == nil {
		return judgeCodexLoginStatus(0, stderr)
	}
	exitCode, ok := commandExitCode(err)
	if !ok {
		return cliUnknown
	}
	return judgeCodexLoginStatus(exitCode, stderr)
}

type cliHealthSpec struct {
	name          string
	check         func(context.Context) cliHealthState
	missingText   string
	loggedOutText string
}

var cliHealthSpecs = []cliHealthSpec{
	{
		name:          "claude",
		check:         checkClaudeHealth,
		missingText:   "claude が見つかりません (Claude Code をインストールしてください)",
		loggedOutText: "claude がログアウト状態です (claude auth login で再ログイン)",
	},
	{
		name:          "codex",
		check:         checkCodexHealth,
		missingText:   "codex が見つかりません (codex をインストールしてください)",
		loggedOutText: "codex がログアウト状態です (codex login で再ログイン)",
	},
}

// 判定不能は正常ではないが、誤ってログアウト警告を出すより無通知にする。ログイン状態は
// 起動時点の情報を毎回検査する必要があるため、バージョン検査のような TTL キャッシュを置かない。
// cancelAll (browseModel.cancel) と結び付けないのは、既存のバージョン検査・usage.Fetch が
// context.Background()+timeout の契約で揃っているため。検査群全体を終了時 cancel へ揃える改修時に見直す。
func checkCLIHealthCmd() tea.Cmd {
	return func() tea.Msg {
		type result struct {
			index int
			state cliHealthState
		}
		results := make(chan result, len(cliHealthSpecs))
		var wg sync.WaitGroup
		for index, spec := range cliHealthSpecs {
			wg.Add(1)
			go func(index int, spec cliHealthSpec) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), cliHealthTimeout)
				defer cancel()
				results <- result{index: index, state: spec.check(ctx)}
			}(index, spec)
		}
		wg.Wait()
		close(results)

		states := make([]cliHealthState, len(cliHealthSpecs))
		for result := range results {
			states[result.index] = result.state
		}
		issues := make([]cliHealthIssue, 0, len(cliHealthSpecs))
		for index, spec := range cliHealthSpecs {
			if states[index] == cliMissing || states[index] == cliLoggedOut {
				issues = append(issues, cliHealthIssue{cli: spec.name, state: states[index]})
			}
		}
		return cliHealthMsg{issues: issues}
	}
}

func cliHealthWarning(issue cliHealthIssue) string {
	for _, spec := range cliHealthSpecs {
		if issue.cli != spec.name {
			continue
		}
		switch issue.state {
		case cliMissing:
			return spec.missingText
		case cliLoggedOut:
			return spec.loggedOutText
		default:
			return ""
		}
	}
	return ""
}
