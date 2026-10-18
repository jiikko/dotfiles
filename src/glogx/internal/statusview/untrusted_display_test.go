package statusview

import (
	"errors"
	"glogx/internal/testenv"
	"testing"
)

// X の確認モーダルに載るパスは git status 由来 (POSIX ファイル名は制御文字を許す)。一覧行と
// pager タイトルは dispPath で無害化していたのに、破壊操作の確認画面だけが raw だった回帰テスト。
func TestDiscardBoxSanitizesPath(t *testing.T) {
	v := NewStatusView()
	v.discarding = true
	v.discard = worktreeRow{
		section: sectionUnstaged, code: 'M', x: ' ', y: 'M',
		path: "notes" + testenv.OSC8 + "0;PWNED" + testenv.ST8 + ".txt",
		orig: "old" + testenv.CSI8 + "2J.txt",
	}
	testenv.AssertSanitizedAndVisible(t, "discard 確認モーダル",
		v.discardBox(StatusRenderOpts{Width: 80, Page: 20}), "notes", ".txt")
}

func TestStatusBranchHeaderIsSanitized(t *testing.T) {
	branch, track := parseBranchHeader("## main" + evil417 + "...origin/main [ahead 1" + evil417 + "]")
	testenv.AssertNoTermControl(t, "branch", branch)
	testenv.AssertNoTermControl(t, "track", track)
}

func TestStatusErrorsAreSanitized(t *testing.T) {
	v := newTestStatusView(t, "")
	v.Receive(StatusLoadMsg{err: errors.New("fatal: " + evil417), gen: v.gen})
	testenv.AssertNoTermControl(t, "git status のエラー", v.err)

	v.preview.Begin("k")
	v.ReceivePreview(StatusPreviewMsg{gen: v.gen, key: "k", err: errors.New("open " + evil417 + ".txt: permission denied")})
	lines, ok := v.preview.Get("k")
	if !ok || len(lines) != 1 {
		t.Fatalf("プレビューのエラーが保存されていない: ok=%v lines=%q", ok, lines)
	}
	testenv.AssertNoTermControl(t, "プレビューのエラー", lines[0])
}
