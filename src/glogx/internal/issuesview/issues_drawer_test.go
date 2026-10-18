package issuesview

import (
	"glogx/internal/term"
	"glogx/internal/testenv"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/anim"
)

func TestDrawerPhasesAndWidth(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	var d issuesDrawer
	const total = 100
	target := d.targetWidth(total) // 80

	if d.width(total, now) != 0 {
		t.Error("閉じているのに幅がある")
	}
	d.open(now)
	if got := d.width(total, now); got != 0 {
		t.Errorf("開き始めの幅 = %d, want 0", got)
	}
	mid := d.width(total, now.Add(issuesDrawerDuration/2))
	if mid <= 0 || mid >= target {
		t.Errorf("途中の幅 = %d, want 0 < x < %d", mid, target)
	}
	if got := d.width(total, now.Add(issuesDrawerDuration)); got != target {
		t.Errorf("開ききった幅 = %d, want %d", got, target)
	}
	// 着地で静止状態へ
	if closed := d.settle(now.Add(issuesDrawerDuration)); closed || d.phase() != anim.Open {
		t.Errorf("開き切りの settle: closed=%v phase=%v", closed, d.phase())
	}
}

// targetWidth は比率 + 固定の上乗せ。🚨 上乗せのための下限で「上乗せ前より狭くなる」ことは
// 起きてはいけない (ユーザー要望で広げたのに狭くなる本末転倒)。
func TestDrawerTargetWidth(t *testing.T) {
	var d issuesDrawer
	// 寸法はユーザー要望で決めた値 (80% + 10 桁・覗き見は 8〜18 桁・16 桁以下は全幅) をリテラルで固定する。
	// 下のループは定数どうしの関係しか見ないので、定数を変えても緑のままだった
	for total, want := range map[int]int{16: 16, 20: 16, 40: 32, 84: 76, 100: 90, 200: 182, 312: 294} {
		if got := d.targetWidth(total); got != want {
			t.Errorf("total=%d: 本文の幅 %d, want %d", total, got, want)
		}
	}
	ratioOnly := func(total int) int { return int(float64(total)*issuesDrawerRatio + 0.5) }

	// 広い端末では一覧の覗き見が上限で止まり、余りは全部本文へ回る (比率のままだと画面が
	// 広いほど一覧が場所を食う。実測 2026-07-31: 端末 312 桁で一覧に 50 桁超)
	for _, total := range []int{200, 280, 312} {
		got := d.targetWidth(total)
		if peek := total - got; peek != issuesDrawerMaxPeek {
			t.Errorf("total=%d: 一覧の覗き見 %d 桁, want %d (上限で止まるはず)", total, peek, issuesDrawerMaxPeek)
		}
		if got <= ratioOnly(total) {
			t.Errorf("total=%d: 本文 %d が比率ぶん %d を超えていない", total, got, ratioOnly(total))
		}
	}
	// 覗き見は上限を超えない
	for total := issuesDrawerMinList*2 + 1; total <= 400; total++ {
		if peek := total - d.targetWidth(total); peek > issuesDrawerMaxPeek {
			t.Fatalf("total=%d: 覗き見が %d 桁 (上限 %d を超えた)", total, peek, issuesDrawerMaxPeek)
		}
	}
	// どの幅でも「比率ぶん」より狭くならない
	for total := 1; total <= 200; total++ {
		got := d.targetWidth(total)
		if total > issuesDrawerMinList*2 && got < ratioOnly(total) {
			t.Fatalf("total=%d: %d が比率ぶん %d より狭い", total, got, ratioOnly(total))
		}
		if got > total {
			t.Fatalf("total=%d: %d が画面幅を超えた", total, got)
		}
	}
	// 余裕があるときは一覧を最小幅ぶん残す (狭すぎて比率ぶんと両立しない場合を除く)
	for _, total := range []int{60, 84, 120, 280} {
		if peek := total - d.targetWidth(total); peek < issuesDrawerMinList {
			t.Errorf("total=%d で一覧が %d 桁しか残らない (最小 %d)", total, peek, issuesDrawerMinList)
		}
	}
	// 覗き見の余地が無い狭さでは全幅
	if got := d.targetWidth(issuesDrawerMinList); got != issuesDrawerMinList {
		t.Errorf("狭い端末で全幅にならない: %d", got)
	}
}

// 閉じるときは開くときの逆再生 (同じ進捗で同じ幅を通る)。
func TestDrawerCloseIsReverseOfOpen(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	const total = 100
	var open issuesDrawer
	open.open(now)
	closing := issuesDrawer{t: anim.NewOpen()}
	closing.startClose(now)

	for _, f := range []float64{0, 0.25, 0.5, 0.75, 1} {
		at := now.Add(time.Duration(f * float64(issuesDrawerDuration)))
		rev := now.Add(time.Duration((1 - f) * float64(issuesDrawerDuration)))
		if o, c := open.width(total, at), closing.width(total, rev); o != c {
			t.Errorf("進捗 %.2f: 開く幅 %d と逆再生の幅 %d が一致しない", f, o, c)
		}
	}
}

// 開き切る前に閉じても幅が飛ばない (今見えている幅から折り返す)。
func TestDrawerCloseFromPartialOpen(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	const total = 100
	var d issuesDrawer
	d.open(now)
	at := now.Add(issuesDrawerDuration / 3)
	before := d.width(total, at)
	d.startClose(at)
	if after := d.width(total, at); after != before {
		t.Errorf("閉じ始めで幅が飛んだ: %d -> %d", before, after)
	}
	// そのまま進めば 0 へ着地する
	if got := d.width(total, at.Add(issuesDrawerDuration)); got != 0 {
		t.Errorf("閉じ切りの幅 = %d, want 0", got)
	}
	if closed := d.settle(at.Add(issuesDrawerDuration)); !closed || d.phase() != anim.Closed {
		t.Errorf("閉じ切りの settle: closed=%v phase=%v", closed, d.phase())
	}
}

func TestDrawerFinishLandsImmediately(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	var d issuesDrawer
	d.open(now)
	if closed := d.finish(); closed || d.phase() != anim.Open {
		t.Errorf("開く演出の finish: closed=%v phase=%v", closed, d.phase())
	}
	d.startClose(now)
	if closed := d.finish(); !closed || d.phase() != anim.Closed {
		t.Errorf("閉じる演出の finish: closed=%v phase=%v", closed, d.phase())
	}
}

func TestDrawerAnimatingDrivesTick(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	var d issuesDrawer
	if d.animating(now) {
		t.Error("閉じているのにアニメ中")
	}
	d.open(now)
	if !d.animating(now.Add(issuesDrawerDuration / 2)) {
		t.Error("開く途中がアニメ中でない (tick が止まって固まる)")
	}
	if d.animating(now.Add(issuesDrawerDuration)) {
		t.Error("着地後もアニメ中 (tick が残る)")
	}
}

// 統合: 本文を開くと一覧が右に残り、閉じる演出のあいだ本文は生きている。
func TestIssuesViewDrawerIntegration(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	now := base
	testenv.StubClockFrom(t, &now)

	v := realIssuesView(t)
	o := IssuesRenderOpts{Width: 80, Page: 12}
	v.Lines(o)
	v.HandleKey("enter", vp(o.Page))
	if v.open == nil {
		t.Fatal("Enter で本文が開かない")
	}

	// 開ききると本文が主役、一覧はタブ行の右端が残る (全画面置き換えではない)
	now = base.Add(issuesDrawerDuration)
	out := strings.Join(v.Lines(o), "\n")
	if !strings.Contains(out, "▏") {
		t.Errorf("引き出しの境界が出ていない:\n%s", out)
	}
	if !strings.Contains(term.StripANSI(out), v.open.Rel) {
		t.Errorf("本文のヘッダーが出ていない:\n%s", out)
	}
	// 🚨 下地は一覧のヘッダー (タブ) であって本文のヘッダーではない
	if strings.Count(term.StripANSI(out), v.open.Rel) != 1 {
		t.Errorf("本文のヘッダーが下地にも出ている:\n%s", out)
	}

	// 閉じる: 演出のあいだ本文は生きていて、幅は縮んでいく
	v.HandleKey("h", vp(o.Page))
	if v.open == nil {
		t.Fatal("h の直後に本文が消えた (逆再生に何も映らない)")
	}
	now = base.Add(issuesDrawerDuration + issuesDrawerDuration/2)
	mid := v.drawer.width(o.Width, now)
	if mid <= 0 || mid >= v.drawer.targetWidth(o.Width) {
		t.Errorf("閉じ途中の幅 = %d (0 と満杯の間のはず)", mid)
	}
	// 着地で本文を捨てる
	now = base.Add(issuesDrawerDuration * 3)
	v.Lines(o)
	if v.open != nil || v.drawer.phase() != anim.Closed {
		t.Errorf("閉じ切っても本文が残る: open=%v phase=%v", v.open != nil, v.drawer.phase())
	}
}

// 演出中は tick が回り続ける (止まると開きかけで固まる)。
func TestIssuesViewDrawerKeepsTickAlive(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	now := base
	testenv.StubClockFrom(t, &now)

	v := realIssuesView(t)
	v.Lines(IssuesRenderOpts{Width: 80, Page: 12})
	v.HandleKey("enter", vp(12))
	now = base.Add(issuesDrawerDuration / 2)
	if !v.Animating() {
		t.Error("開く演出の途中でアニメ扱いになっていない (tick が止まる)")
	}
	now = base.Add(issuesDrawerDuration)
	if v.Animating() {
		t.Error("着地後もアニメ扱い (tick が残る)")
	}
}
