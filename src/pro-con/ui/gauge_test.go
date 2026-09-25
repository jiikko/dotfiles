package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ゲージの PG の数は今の同時実行数。利用枠で上限より絞っていれば上限と理由も出す。dispatcher が 1 度も回っていない /
// 長く回っていなければ、そうと分かる文を出す。
func TestGaugeShowsCapAndDispatcherLiveness(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax, be.snap.LimitWhy = 1, 3, "枠 85%: 同時に 1 本まで"
	m := New(be, nil)
	g := ansi.Strip(m.gauge())
	if !strings.Contains(g, "/1 (上限 3) 枠 85%: 同時に 1 本まで") {
		t.Fatalf("絞った上限と理由を出さない: %q", g)
	}
	if strings.Contains(g, "止まっている") || strings.Contains(g, "未起動") {
		t.Fatalf("回っているのに止まっている扱い: %q", g)
	}
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - time.Second)
	m.snap = be.snap
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "止まっている?") || strings.Contains(g, "枠 85%") {
		t.Fatalf("長く回っていないのに知らせない / 止まった dispatcher の最後の理由を出した: %q", g)
	}
	be.snap.DispatcherTick = time.Time{}
	m.snap = be.snap
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "dispatcher 未起動") {
		t.Fatalf("1 度も回っていないのに知らせない: %q", g)
	}
}

// 絞っていなければ上限を添えない。
func TestGaugeOmitsMaxWhenNotCapped(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax = 3, 3
	if g := ansi.Strip(New(be, nil).gauge()); strings.Contains(g, "上限") {
		t.Fatalf("絞っていないのに上限を出した: %q", g)
	}
	be.snap.Limit, be.snap.LimitMax = 3, 2 // 上限を下げて起動し直した直後など、絞りの外で数が上限を超えた形
	if g := ansi.Strip(New(be, nil).gauge()); strings.Contains(g, "上限") {
		t.Fatalf("絞っていないのに上限を出した: %q", g)
	}
}

// 止まった dispatcher の最後の絞り (0 本など) を今の値として出さない。上限だけを出す。
func TestGaugeStoppedDispatcherShowsMax(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax, be.snap.LimitWhy = 0, 3, "枠 96%: 新しく起動・再開しない"
	be.snap.DispatcherTick = be.snap.Now.Add(-dispatcherStale - time.Second)
	g := ansi.Strip(New(be, nil).gauge())
	if !strings.Contains(g, "PG 0/3 │") || strings.Contains(g, "上限") || strings.Contains(g, "枠 96%") {
		t.Fatalf("止まった dispatcher の最後の絞りを出した: %q", g)
	}
}

// 絞りの理由は 1 行に潰して limitWhyCells で切る (長い理由・改行でゲージの後ろの警告を押し出さない / ヘッダの行を増やさない)。
func TestGaugeBoundsLimitWhy(t *testing.T) {
	be := newSpy()
	be.snap.Limit, be.snap.LimitMax, be.snap.LimitWhy = 3, 3, "枠を読めない:\nexit status 1 "+strings.Repeat("x", 300) // 改行は切る幅より手前
	g := New(be, nil).gauge()
	if strings.Contains(g, "\n") {
		t.Fatal("理由の改行がゲージに入った (ヘッダの行が増える)")
	}
	plain := ansi.Strip(g)
	i := strings.Index(plain, "枠を読めない")
	j := strings.Index(plain[i:], " │ ")
	if i < 0 || j < 0 || ansi.StringWidth(plain[i:i+j]) > limitWhyCells {
		t.Fatalf("理由を %d セルで切らない: %q", limitWhyCells, plain)
	}
}
