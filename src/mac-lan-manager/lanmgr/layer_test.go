package lanmgr

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// 詳細のパネルから測りはじめると、測定の進捗のモーダルがパネルの上に重なる: キー・最下行はモーダルのもの (esc はモーダルを閉じ、パネルは残す)
func TestSpeedProgressIsAboveDetailPanel(t *testing.T) {
	h, _ := speedHarness(t)
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter", "t", "y")
	if !h.m.detailOpen || !h.m.speedShown() {
		t.Fatalf("前提: パネルの上で測定の進捗が出ない (panel=%v shown=%v)", h.m.detailOpen, h.m.speedShown())
	}
	if hs := h.m.hints(); !slices.Equal(hs, speedProgressHints) {
		t.Errorf("最下行がモーダルのキーでない: %v", hs)
	}
	h.keys("esc")
	if !h.m.detailOpen || h.m.speedRun == nil || h.m.speedRun.shown {
		t.Errorf("esc がモーダルでなくパネルに届いた (panel=%v run=%v)", h.m.detailOpen, h.m.speedRun)
	}
}

// 見出しの待ちの文は、固定 IP の確認待ちを裏の名前の確かめより先に出す (確認待ちは期限があり、相手の上で元に戻りうる)
func TestHeaderPrefersConfirmOverWiredLookup(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.m.Advance(t0)
	h.m.wiredWait = 1
	h.m.pending["m2macmini.local"] = &pendingConfirm{deadline: t0.Add(time.Hour)}
	if head := termwidth.StripSGR(h.m.header(200)); !strings.Contains(head, "固定 IP の確認待ち") {
		t.Errorf("見出し = %q", head)
	}
}
