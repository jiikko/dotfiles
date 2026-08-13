package main

import (
	"strings"
	"testing"
)

// prTargetSHA (p/P 共通の対象解決) の回帰ガード。ガード 3 連は openPR / openPRStatus に
// 文言まで同一で複製されていたのを一本化したもので、ここが壊れると「未 push なのに GitHub へ
// 問い合わせる」「remote なしで PR 検索が走る」形で静かに退行する。

func TestPRTargetSHAEmptyCommits(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.commits = nil
	if sha, ok := m.prTargetSHA(); ok || sha != "" {
		t.Errorf("コミット 0 件で ok=true: sha=%q", sha)
	}
	if m.toast.visible() {
		t.Errorf("コミット 0 件は無言で nil の契約なのにトーストが出た: %q", m.toast.text)
	}
}

func TestPRTargetSHANoRepo(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.hasRepo = false
	if _, ok := m.prTargetSHA(); ok {
		t.Error("remote なしで ok=true")
	}
	if !m.toast.visible() || m.toast.ok || !strings.Contains(m.toast.text, "remote が無いため") {
		t.Errorf("remote なしの理由 error トーストが出ない: %q", m.toast.text)
	}
}

func TestPRTargetSHAUnpushed(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateUnpushed
	if _, ok := m.prTargetSHA(); ok {
		t.Error("未 push コミットで ok=true (GitHub 上に存在しない SHA を問い合わせてしまう)")
	}
	if !m.toast.visible() || m.toast.ok || !strings.Contains(m.toast.text, "未 push") {
		t.Errorf("未 push の理由 error トーストが出ない: %q", m.toast.text)
	}
}

func TestPRTargetSHAOK(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.cursor = 1
	sha, ok := m.prTargetSHA()
	if !ok || sha != m.commits[1].SHA {
		t.Errorf("カーソル位置の SHA が返らない: sha=%q ok=%v", sha, ok)
	}
	if m.toast.visible() {
		t.Errorf("成功経路でトーストが出た: %q", m.toast.text)
	}
}
