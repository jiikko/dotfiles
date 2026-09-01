package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"glogx/usage"
)

func rlTestOpts(snap *usage.Snapshot, err error) ratelimitRenderOpts {
	return ratelimitRenderOpts{
		width: 100, page: 30, colored: false, spinner: "⠋",
		snap: snap, err: err,
		now: time.Date(2026, 8, 31, 22, 14, 0, 0, time.Local),
	}
}

func rlTestSnap() *usage.Snapshot {
	now := time.Date(2026, 8, 31, 22, 14, 0, 0, time.Local)
	return &usage.Snapshot{
		Version: "2.1.216",
		Windows: []usage.Window{
			{Label: "5h", Percent: 62, ResetAt: now.Add(108 * time.Minute), WindowMins: 300},
			{Label: "7d", Percent: 78, ResetAt: now.Add(3420 * time.Minute), WindowMins: 7 * 24 * 60},
		},
	}
}

// 全画面 viewer の契約: lines はどの状態でもちょうど page 行を返し、どの行も width を
// 超えない (超えると finishWindow が枠を組めず画面が折り返す)。
func TestRatelimitDashLinesFillsPage(t *testing.T) {
	var d ratelimitDash
	d.toggle()
	states := map[string]ratelimitRenderOpts{
		"取得済み": rlTestOpts(rlTestSnap(), nil),
		"取得中":  rlTestOpts(nil, nil),
		"失敗":   rlTestOpts(nil, errors.New("boom")),
	}
	for name, o := range states {
		for _, page := range []int{30, 12, 4, 1} {
			o.page = page
			got := d.lines(o)
			if len(got) != page {
				t.Errorf("%s page=%d: 行数 %d", name, page, len(got))
			}
			for i, ln := range got {
				if w := dispWidth(ln); w > o.width {
					t.Errorf("%s page=%d: %d 行目の幅 %d > %d", name, page, i, w, o.width)
				}
			}
		}
	}
}

// 取得中はスピナー、失敗は理由を出す (盤が出ないまま無言にならない)。
func TestRatelimitDashLoadingAndError(t *testing.T) {
	var d ratelimitDash
	d.toggle()
	if got := strings.Join(d.lines(rlTestOpts(nil, nil)), "\n"); !strings.Contains(got, "取得中") {
		t.Errorf("取得中の表示が無い:\n%s", got)
	}
	if got := strings.Join(d.lines(rlTestOpts(nil, errors.New("x"))), "\n"); !strings.Contains(got, "取得失敗") {
		t.Errorf("失敗の表示が無い:\n%s", got)
	}
}

// last-good があるときは、取得に失敗していても盤を出し続ける (右上オーバーレイと同じ契約)。
func TestRatelimitDashKeepsLastGoodOnError(t *testing.T) {
	var d ratelimitDash
	d.toggle()
	got := strings.Join(d.lines(rlTestOpts(rlTestSnap(), errors.New("x"))), "\n")
	if strings.Contains(got, "取得失敗") {
		t.Errorf("last-good があるのに失敗表示へ落ちた:\n%s", got)
	}
	if !strings.Contains(got, "復活まで") {
		t.Errorf("last-good の盤が出ていない:\n%s", got)
	}
}

// 見出しには両 CLI のバージョンが出る (右上オーバーレイと同じ情報量)。
func TestRatelimitDashHeaderShowsVersions(t *testing.T) {
	var d ratelimitDash
	d.toggle()
	snap := rlTestSnap()
	snap.CodexVersion = "0.144.6"
	snap.Windows = append(snap.Windows, usage.Window{
		Label: "cx5h", Source: usage.SourceCodex, Percent: 10,
		ResetAt: time.Now().Add(time.Hour), WindowMins: 300,
	})
	head := d.lines(rlTestOpts(snap, nil))[0]
	for _, want := range []string{"v2.1.216", "codex v0.144.6", "ratelimit"} {
		if !strings.Contains(head, want) {
			t.Errorf("見出しに %q が無い: %q", want, head)
		}
	}
}

// 閉じる / 更新以外のキーは飲み切る (全画面なので裏の一覧をスクロールさせない)。
func TestRatelimitDashHandleKey(t *testing.T) {
	for _, key := range []string{"R", "q", "esc", "h", "left"} {
		var d ratelimitDash
		d.toggle()
		closed, refresh := d.handleKey(key)
		if !closed || refresh {
			t.Errorf("%q: closed=%v refresh=%v", key, closed, refresh)
		}
		if d.visible() {
			t.Errorf("%q で閉じていない", key)
		}
	}
	var d ratelimitDash
	d.toggle()
	if closed, refresh := d.handleKey("r"); closed || !refresh {
		t.Errorf("r: closed=%v refresh=%v", closed, refresh)
	}
	if !d.visible() {
		t.Error("r で閉じてしまった")
	}
	for _, key := range []string{"j", "k", "i", "s", "b", "u", "enter"} {
		closed, refresh := d.handleKey(key)
		if closed || refresh {
			t.Errorf("%q を飲み込んでいない: closed=%v refresh=%v", key, closed, refresh)
		}
	}
}

// 1 分ごとの再取得は「右上オーバーレイ」か「全画面ダッシュボード」のどちらかが見えていれば回る。
// ダッシュボードを外すと、開いている間だけ値が固まる (毎分更新の契約が静かに壊れる)。
func TestWantsUsageRefresh(t *testing.T) {
	var m browseModel
	if m.wantsUsageRefresh() {
		t.Error("どちらも非表示なのに再取得する")
	}
	m.usageOv.visible = true
	if !m.wantsUsageRefresh() {
		t.Error("usage オーバーレイ表示中に再取得しない")
	}
	m.usageOv.visible = false
	m.rlDash.toggle()
	if !m.wantsUsageRefresh() {
		t.Error("ダッシュボード表示中に再取得しない")
	}
}

// ダッシュボードが取得待ちのあいだはスピナーの tick を回す (右上オーバーレイの loading()
// は自分の表示状態しか見ないので、ダッシュボード単独では止まってしまう)。
func TestRLDashLoading(t *testing.T) {
	var m browseModel
	if m.rlDashLoading() {
		t.Error("非表示なのに loading")
	}
	m.rlDash.toggle()
	if !m.rlDashLoading() {
		t.Error("取得前なのに loading でない")
	}
	m.usageOv.snap = rlTestSnap()
	if m.rlDashLoading() {
		t.Error("取得済みなのに loading")
	}
}

// R でダッシュボードが開き、開いている間は移動キーが裏の一覧へ届かない (全画面の契約)。
// 届くと、閉じたときにカーソルが知らない場所へ動いている。
func TestRatelimitDashKeyRouting(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatalf("前提: j でカーソルが動くこと (cursor=%d)", m.cursor)
	}
	m.handleKey("R")
	if !m.rlDash.visible() {
		t.Fatal("R でダッシュボードが開かない")
	}
	m.handleKey("j")
	m.handleKey("G")
	if m.cursor != 1 {
		t.Errorf("ダッシュボード表示中に一覧が動いた: cursor=%d", m.cursor)
	}
	m.handleKey("q")
	if m.rlDash.visible() {
		t.Error("q で閉じない")
	}
	m.handleKey("j")
	if m.cursor != 2 {
		t.Errorf("閉じた後に一覧が動かない: cursor=%d", m.cursor)
	}
}

// R を開くと右上の usage オーバーレイは引っ込む (同じ値を 2 か所に出さない)。
func TestRatelimitDashDismissesUsageOverlay(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.usageOv.visible = true
	m.usageOv.snap = rlTestSnap()
	m.handleKey("R")
	if m.usageOv.visible {
		t.Error("ダッシュボードを開いても右上オーバーレイが残っている")
	}
}

// 取得できていても「描ける枠が 1 つも無い」ことがある (Claude 側は既定の枠ラベルでしか
// 拾わないので、/usage の文言が変わると起こる)。全画面なので、無言の白画面にせず理由を出す。
func TestRatelimitDashNoRenderableWindows(t *testing.T) {
	var d ratelimitDash
	d.toggle()
	snap := &usage.Snapshot{Windows: []usage.Window{
		{Label: "Current opus quota", Percent: 5, ResetAt: time.Now().Add(time.Hour)},
	}}
	got := strings.Join(d.lines(rlTestOpts(snap, nil)), "\n")
	if !strings.Contains(got, "表示できる利用枠がありません") {
		t.Errorf("無言の白画面になっている:\n%s", got)
	}
}

// 見出しは狭い端末でも width を超えない。フレームが自動 OFF になる帯ではクリップが
// 効かないので、超えると折り返して画面全体が崩れる。
func TestRatelimitDashHeaderFitsWidth(t *testing.T) {
	var d ratelimitDash
	d.toggle()
	snap := rlTestSnap()
	snap.CodexVersion = "0.144.6"
	snap.Windows = append(snap.Windows, usage.Window{
		Label: "cx5h", Source: usage.SourceCodex, Percent: 10,
		ResetAt: time.Now().Add(time.Hour), WindowMins: 300,
	})
	for w := 1; w <= 120; w++ {
		o := rlTestOpts(snap, nil)
		o.width, o.page = w, 30
		for i, ln := range d.lines(o) {
			if got := dispWidth(ln); got > w {
				t.Errorf("w=%d: %d 行目の幅 %d: %q", w, i, got, ln)
			}
		}
	}
}
