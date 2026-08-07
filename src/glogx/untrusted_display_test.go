package main

import (
	"strings"
	"testing"

	"glogx/issues"
	"glogx/usage"
)

// 外部由来の文字列が「無害化を通らずに端末へ出る」sink が残っていないことを、sink ごとに固定する。
// 敵対的レビュー (2026-08-05) が実際に見つけた素通し経路の回帰テスト。

const (
	csi8 = "\u009b" // 8bit CSI (C1)
	osc8 = "\u009d" // 8bit OSC (C1)
	st8  = "\u009c" // 8bit ST (C1)
)

// PR の枠に載るブランチ名は外部由来。git は ref に ASCII 制御文字を許さないが C1 は許すので、
// 8bit の OSC/CSI が実際に入ってくる (直上の Title は無害化していたのにここだけ抜けていた)。
func TestPRStatusBoxSanitizesBranchNames(t *testing.T) {
	o := newPRStatusOverlay()
	o.sha = "deadbeef"
	o.cache["deadbeef"] = &PRStatus{
		PRRef: PRRef{Number: 12, State: "OPEN"}, Title: "PR タイトル",
		HeadRefName: "feat/" + osc8 + "0;PWNED" + st8 + "x",
		BaseRefName: "master" + csi8 + "2J",
	}
	for _, line := range o.boxLines(80, false, "⠋", "") {
		if hasTerminalControl(line) {
			t.Errorf("PR の枠に制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// X の確認モーダルに載るパスは git status 由来 (POSIX ファイル名は制御文字を許す)。一覧行と
// pager タイトルは dispPath で無害化していたのに、破壊操作の確認画面だけが raw だった回帰テスト。
func TestDiscardBoxSanitizesPath(t *testing.T) {
	v := newStatusView()
	v.discarding = true
	v.discard = worktreeRow{
		section: sectionUnstaged, code: 'M', x: ' ', y: 'M',
		path: "notes" + osc8 + "0;PWNED" + st8 + ".txt",
		orig: "old" + csi8 + "2J.txt",
	}
	for _, line := range v.discardBox(statusRenderOpts{width: 80, page: 20}) {
		if hasTerminalControl(line) {
			t.Errorf("discard 確認モーダルに制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// n の確認モーダルに載る issue ファイル名は issues/ 直下の実ファイル名 (Rel は同一性のため
// 生のまま保持される契約)。表示に出すここで無害化する回帰テスト。
func TestMarkNextBoxSanitizesFilename(t *testing.T) {
	v := &issuesView{}
	v.markNext = issuesMarkConfirm{active: true, targets: []*issues.Issue{
		{Rel: "036-bug-" + osc8 + "0;PWNED" + st8 + csi8 + "2J.md"},
	}}
	for _, line := range v.markNextBox(80, false) {
		if hasTerminalControl(line) {
			t.Errorf("markNext 確認モーダルに制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// usage の枠タイトルに載る CLI バージョンは外部バイナリの出力。
func TestUsageBoxSanitizesVersion(t *testing.T) {
	o := usageOverlay{
		visible: true,
		snap: &usage.Snapshot{
			Version: "1.2.3\a" + osc8 + "0;PWNED" + st8,
			Windows: []usage.Window{{Label: "5h", Percent: 20}},
		},
	}
	for _, line := range o.boxLines(80, false, "⠋") {
		if hasTerminalControl(line) {
			t.Errorf("usage の枠に制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// トーストの文言も表示 sink。gh / git のエラー出力や claude のバージョン文字列を素で
// 埋め込む呼び出しが多いので、setNotice と同じく show 自体を関門にする。
// lastWarning は w でクリップボードへ出るため、そちらも無害化する。
func TestToastAndWarningAreSanitized(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.showWarning("push に失敗: fatal" + osc8 + "0;PWNED" + st8 + " remote rejected")
	if hasTerminalControl(m.toast.text) {
		t.Errorf("トーストに制御シーケンスが残った: %q", m.toast.text)
	}
	if hasTerminalControl(m.lastWarning) {
		t.Errorf("コピー対象の警告に制御シーケンスが残った: %q", m.lastWarning)
	}
	if strings.Contains(m.lastWarning, "PWNED") {
		t.Errorf("OSC の中身が残った: %q", m.lastWarning)
	}
}

// 色なしモードでは外部由来の SGR も枠へ通さない。
//
// paint も scrollbarColumn も colored=false のとき reset を出さないので、閉じていない SGR が
// 1 行でもあると padding・枠・スクロールバー列・後続行まで属性が続く (NO_COLOR 起動時に
// 「以降が全部消える」形の画面破壊になる)。
func TestPanelBoxDropsANSIWhenNotColored(t *testing.T) {
	rows := []string{"\x1b[41;30m閉じていない SGR", "無関係な次の行"}
	for _, line := range buildShadowPanelBox(" title ", rows, 40, false, ansiDim) {
		if strings.Contains(line, "\x1b") {
			t.Errorf("色なしモードなのに ANSI が枠へ出た: %q", line)
		}
	}
	// 色ありモードでは従来どおり通す (色を出すのが仕事なので落とさない)
	got := strings.Join(buildShadowPanelBox(" title ", rows, 40, true, ansiDim), "\n")
	if !strings.Contains(got, "\x1b[41;30m") {
		t.Errorf("色ありモードで外部の SGR まで落とした: %q", got)
	}
}
