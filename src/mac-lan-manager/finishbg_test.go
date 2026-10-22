package main

import (
	"strings"
	"testing"

	"mac-lan-manager/lanmgr"
)

type fakeBG struct {
	pending int
	got     []lanmgr.Notice
	left    int
	waited  bool
}

func (f *fakeBG) PendingBG() int { return f.pending }
func (f *fakeBG) WaitBG(<-chan struct{}) ([]lanmgr.Notice, int) {
	f.waited = true
	return f.got, f.left
}

// 画面を閉じた後に、広告を止める ssh の結果を出す。待たずに終えた分は、止まったか分からないことと止め方を出す。何も走っていなければ黙る
func TestFinishBGReportsResults(t *testing.T) {
	var b strings.Builder
	finishBG(&b, &fakeBG{pending: 2, got: []lanmgr.Notice{{Text: "a.wired.local の広告を止めた", OK: true}, {Text: "b.wired.local の広告を止められなかった", OK: false}}}, nil)
	out := b.String()
	for _, want := range []string{"Ctrl-C で取り消して終える", "✓ a.wired.local の広告を止めた", "✗ b.wired.local の広告を止められなかった"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "待たずに終えた") {
		t.Errorf("全部届いたのに待たずに終えたと出した:\n%s", out)
	}

	b.Reset()
	finishBG(&b, &fakeBG{pending: 1, left: 1}, nil)
	if out := b.String(); !strings.Contains(out, "1 台の結果を待たずに終えた") || !strings.Contains(out, "launchctl bootout") {
		t.Errorf("待たずに終えた分の止め方を出さない:\n%s", out)
	}

	b.Reset()
	f := &fakeBG{}
	finishBG(&b, f, nil)
	if b.Len() != 0 || f.waited {
		t.Errorf("裏の後始末が無いのに待った / 出した: waited=%v %q", f.waited, b.String())
	}
}
