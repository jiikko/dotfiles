package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestBrowse(t *testing.T, n int, statuses map[string]CIState, toFetch []string) *browseModel {
	t.Helper()
	commits := make([]Commit, n)
	for i := range commits {
		sha := strings.Repeat(string(rune('a'+i)), 40)
		commits[i] = Commit{
			SHA: sha, ShortSHA: sha[:7], Subject: "subject", Author: "koji", AuthorEmail: "k@x",
			Date: "Thu Jul 16 19:12:47 2026 +0900", RelDate: "now", Message: "subject",
		}
	}
	// NoFrame: true = 最外周フレームを明示 OFF (issue 025)。既存の View/overlay/panel テストの
	// 期待値を変えない。現行 80×10 は frameMinHeight 未満で自動 OFF だが、途中で width/height を
	// 大きくするテストが誤ってフレームを踏まないよう明示 OFF を決定的にする。
	m := newBrowseModel(commits, statuses, toFetch, Repo{Owner: "o", Name: "r"}, true,
		&Options{NoFrame: true}, false, 80, 10)
	t.Cleanup(m.cancel)
	return m
}

func statusesFor(m *browseModel, state CIState) map[string]CIState {
	s := map[string]CIState{}
	for _, c := range m.commits {
		s[c.SHA] = state
	}
	return s
}

// deliverMsgs は tea.Cmd の結果 msg を BatchMsg の入れ子ごと再帰展開し、match が true を
// 返した msg だけを m.Update へ届ける (tick 等の無関係な msg で状態を進めないためのフィルタ)。
func deliverMsgs(m *browseModel, msg tea.Msg, match func(tea.Msg) bool) {
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				deliverMsgs(m, c(), match)
			}
		}
		return
	}
	if match(msg) {
		m.Update(msg)
	}
}

// withJobs は commit idx の details を job 2 件で埋めるテストヘルパー。
func withJobs(m *browseModel, idx int) {
	m.details[m.commits[idx].SHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: ""},
	}
}

// stubDiff は loadCommitDiff を差し替え、呼び出し記録と固定行を返す。
func stubDiff(t *testing.T, lines []string, err error) *[]string {
	t.Helper()
	var calls []string
	orig := loadCommitDiff
	loadCommitDiff = func(sha string, colored bool) ([]string, error) {
		calls = append(calls, sha)
		return lines, err
	}
	t.Cleanup(func() { loadCommitDiff = orig })
	return &calls
}

// runCmd は tea.Cmd (tea.Batch 含む) を同期実行して diffMsg を探して Update へ流す。
func deliverDiffMsg(t *testing.T, m *browseModel, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd が nil (diff 取得コマンドが返っていない)")
	}
	var deliver func(msg tea.Msg)
	deliver = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					deliver(c())
				}
			}
		case diffMsg:
			m.Update(v)
		}
	}
	deliver(cmd())
}

// runCmdTree は cmd を再帰実行する。tea.Batch (BatchMsg) は各要素を辿るので、開く Cmd が
// maybeTick と束ねられていても副作用 (openInBrowser など) が発火する。
func runCmdTree(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmdTree(c)
		}
	}
}

// withFailedJob は commit idx の details を「失敗 job (CheckID あり)」1 件で埋める。
func withFailedJob(m *browseModel, idx int, checkID int64, state CIState) {
	m.details[m.commits[idx].SHA] = []CheckDetail{
		{Name: "lint", State: state, URL: "https://github.com/o/r/runs/9", CheckID: checkID},
	}
}
