package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"tuikit/toast"
)

// 操作の結果の通知は、下端の行ではなくボードの右下の toast に出る (tuikit/toast。2026-09-25 のユーザーの依頼)。
func TestNoticeGoesToToastNotFooter(t *testing.T) {
	m, _ := cursorModel(t)
	m.done("コピーした")
	for range toast.SlideFrames + 1 { // 滑り込みを終える
		m.onFrame()
	}
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	foot := len(lines) - len(m.footGroup())
	at := -1
	for i, l := range lines {
		if strings.Contains(l, "コピーした") {
			at = i
		}
	}
	if at < 0 || at >= foot {
		t.Fatalf("通知がボードの領域 (下端の群より上) に出ていない: 行 %d / 下端の群は %d 行目から", at, foot)
	}
	if i := strings.Index(lines[at], "コピーした"); ansi.StringWidth(lines[at][:i]) < m.width/2 {
		t.Fatalf("通知が右寄せで出ていない: %q", lines[at])
	}
}

// 成功・失敗・案内がそれぞれ ✓ / ✗ / … の toast になる (どれを使うかで色と印が決まる)。
func TestNoticeKinds(t *testing.T) {
	for _, tc := range []struct {
		say      func(*Model, string)
		ok, info bool
	}{{(*Model).done, true, false}, {(*Model).fail, false, false}, {(*Model).info, false, true}} {
		m, _ := cursorModel(t)
		tc.say(m, "x")
		if m.toasts.Text() != "x" || m.toasts.OK() != tc.ok || m.toasts.Info() != tc.info {
			t.Fatalf("種類が違う: text=%q ok=%v info=%v (期待 ok=%v info=%v)", m.toasts.Text(), m.toasts.OK(), m.toasts.Info(), tc.ok, tc.info)
		}
	}
}

// 通知を積むと演出の frame が回り、滑り込み終えたら「静止の後に引っ込む」合図 (toast.Msg) の tick が返る。合図を受けると引っ込み始める。
func TestToastDrivesFramesAndLeaves(t *testing.T) {
	m, _ := cursorModel(t)
	m.framing = false
	m.fail("失敗した")
	if !m.animating() {
		t.Fatal("通知を積んだのに演出として数えない (frame が回らず滑り込まない)")
	}
	var hold bool
	for range toast.SlideFrames + 1 {
		if cmd := m.onFrame(); cmd != nil && !m.toasts.Animating() {
			hold = true
		}
	}
	if !hold || m.toasts.Phase() != toast.Holding {
		t.Fatalf("滑り込み終えても静止の後の合図を頼まない: hold=%v phase=%v", hold, m.toasts.Phase())
	}
}
