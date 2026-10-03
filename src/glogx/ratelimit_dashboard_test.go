package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"ratelimit/usage"
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

// Claude 側だけ取れなかったとき (codex だけ取れ、Claude 枠は前回値のまま) は、
// 古い残量を黙って出さず理由を見出しの下に出す。理由は外部コマンドの stderr なので無害化する。
func TestRatelimitDashShowsClaudeFetchError(t *testing.T) {
	var d ratelimitDash
	d.toggle()
	if got := d.lines(rlTestOpts(rlTestSnap(), nil))[1]; got != "" {
		t.Fatalf("取得できているのに注記行がある: %q", got)
	}
	snap := rlTestSnap()
	snap.ClaudeErr = "claude /usage 実行失敗: exit status 127: nodenv: \x1b[31mcommand not found"
	o := rlTestOpts(snap, nil)
	lines := d.lines(o)
	note := stripANSI(lines[1])
	for _, want := range []string{"Claude Code の取得に失敗", "前回の値を表示中", "exit status 127"} {
		if !strings.Contains(note, want) {
			t.Errorf("注記に %q が無い: %q", want, note)
		}
	}
	if strings.Contains(lines[1], "\x1b[31m") {
		t.Errorf("理由の制御列が無害化されていない: %q", lines[1])
	}
	if len(lines) != o.page {
		t.Errorf("行数 = %d, want %d", len(lines), o.page)
	}
	for i, l := range lines {
		if w := dispWidth(l); w > o.width {
			t.Errorf("%d 行目が幅 %d を超える (%d): %q", i, o.width, w, l)
		}
	}
}

// 閉じる / 更新 / 横断以外のキーは飲み切る (全画面なので裏の一覧をスクロールさせない)。
func TestRatelimitDashHandleKey(t *testing.T) {
	for _, key := range []string{"R", "q", "esc", "h", "left"} {
		var d ratelimitDash
		d.toggle()
		if got := d.handleKey(key); got != rlDashClosed {
			t.Errorf("%q: action=%v, want rlDashClosed", key, got)
		}
		if d.visible() {
			t.Errorf("%q で閉じていない", key)
		}
	}
	var d ratelimitDash
	d.toggle()
	if got := d.handleKey("r"); got != rlDashRefresh {
		t.Errorf("r: action=%v, want rlDashRefresh", got)
	}
	if !d.visible() {
		t.Error("r で閉じてしまった")
	}
	// i / s は viewer への横断 (ユーザー要望 2026-09-01)。🚨 横断でも自分は閉じる:
	// 開いたまま viewer を開くと「見えている画面」と「キーを受ける画面」が食い違う。
	for _, tc := range []struct {
		key  string
		want rlDashAction
	}{{"i", rlDashIssues}, {"s", rlDashStatus}} {
		var d ratelimitDash
		d.toggle()
		if got := d.handleKey(tc.key); got != tc.want {
			t.Errorf("%q: action=%v, want %v", tc.key, got, tc.want)
		}
		if d.visible() {
			t.Errorf("%q の横断でダッシュボードが閉じていない", tc.key)
		}
	}
	d = ratelimitDash{}
	d.toggle()
	for _, key := range []string{"j", "k", "b", "u", "enter"} {
		if got := d.handleKey(key); got != rlDashSwallow {
			t.Errorf("%q を飲み込んでいない: action=%v", key, got)
		}
	}
	if !d.visible() {
		t.Error("飲み込むキーで閉じてしまった")
	}
}

// ダッシュボード ⇄ viewer の往復 (ダッシュボードの i/s と viewer の R。ユーザー要望 2026-09-01)。
// 全画面は同時に 1 枚なので、横断のたびに「相手が開く」と「自分が閉じる」の両方を見る。
func TestRatelimitDashCrossSwitching(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("R")
	if !m.rlDash.visible() {
		t.Fatal("前提が崩れた: R でダッシュボードが開かない")
	}
	releaseKey(m)
	m.handleKey("i")
	if m.rlDash.visible() || !m.issuesOv.visible() {
		t.Fatalf("ダッシュボードの i で issues viewer へ移らない (dash=%v issues=%v)", m.rlDash.visible(), m.issuesOv.visible())
	}
	releaseKey(m)
	m.handleKey("R")
	if !m.rlDash.visible() || m.issuesOv.visible() {
		t.Fatalf("issues viewer の R でダッシュボードへ移らない (dash=%v issues=%v)", m.rlDash.visible(), m.issuesOv.visible())
	}
	releaseKey(m)
	m.handleKey("s")
	if m.rlDash.visible() || !m.statusOv.visible() {
		t.Fatalf("ダッシュボードの s で status viewer へ移らない (dash=%v status=%v)", m.rlDash.visible(), m.statusOv.visible())
	}
	releaseKey(m)
	m.handleKey("R")
	if !m.rlDash.visible() || m.statusOv.visible() {
		t.Fatalf("status viewer の R でダッシュボードへ移らない (dash=%v status=%v)", m.rlDash.visible(), m.statusOv.visible())
	}
}

// 起動時 restore が返る前に R が押されていたら復元を捨てる (status viewer と同じ理由。
// 捨てないと裏に issues viewer が開き、ダッシュボードの i が toggle で「閉じる」に化ける)。
func TestIssuesRestoreDroppedWhenRatelimitDashOpen(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("R")
	m.Update(issuesRestoreMsg{})
	if m.issuesOv.visible() {
		t.Fatal("ダッシュボード表示中の遅延 restore で issues viewer が開いた")
	}
	if !m.rlDash.visible() {
		t.Fatal("前提が崩れた: ダッシュボードが開いていない")
	}
	// 横断が toggle で裏返らないこと (この restore ガードが守っている本体)
	releaseKey(m)
	m.handleKey("i")
	if !m.issuesOv.visible() {
		t.Fatal("復元を捨てた後の i で issues viewer が開かない")
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

// 片方だけ届いた最初の取得では、届いた分の盤を描き、まだの出所を見出しの下に「取得中」と出す (issue 626)。
// 盤の描画キャッシュは「取得中」の有無で外れる (同じ snap のまま待ちが終わっても行が残らない)。
func TestRatelimitDashShowsWaitingSource(t *testing.T) {
	var m browseModel
	m.rlDash.toggle()
	m.usageOv.fetchCmd(false)
	defer m.usageOv.stop()
	m.usageOv.handle(usageMsg{part: &usage.Part{Source: usage.SourceCodex,
		Windows: []usage.Window{{Label: "cx7d", Source: usage.SourceCodex, Percent: 7, WindowMins: 10080, ResetAt: timeNow().Add(time.Hour)}}}})
	if !m.rlDashLoading() {
		t.Error("Claude を待っている間にスピナーが止まる")
	}
	o := m.ratelimitOpts()
	if o.waiting == "" {
		t.Fatal("ratelimitOpts が待っている出所を渡さない")
	}
	o.width, o.page = 100, 30 // ゼロ値の browseModel は窓の寸法を持たない
	if got := stripANSI(m.rlDash.lines(o)[1]); !strings.Contains(got, "Claude Code 取得中...") {
		t.Errorf("見出しの下に待っている出所が無い: %q", got)
	}
	// 盤の本体は Claude の 2 枚を、スピナー付きの場所取りのカードで取る
	if all := stripANSI(strings.Join(m.rlDash.lines(o), "\n")); strings.Count(all, m.spinner()+" 取得中...") != 2 {
		t.Errorf("盤が場所取りのカードで Claude の段を取っていない:\n%s", all)
	}
	o.waiting = ""
	if got := stripANSI(m.rlDash.lines(o)[1]); strings.Contains(got, "取得中") {
		t.Errorf("待ちが終わっても描画キャッシュの「取得中」が残った: %q", got)
	}
}

// 何も届いていない最初の取得でも、盤は場所取りのカードで届いた後と同じ段を取る (中央の 1 行の「取得中...」にしない。issue 626)。
// 初回の全滅の後の取り直しは「取得失敗」のまま (場所取りで隠さない)。
func TestRatelimitDashPlaceholdersBeforeAnyArrives(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", "")
	stubLookPath(t, map[string]string{"codex": "/fake/codex"}) // codex あり・前回のキャッシュ無し = codex の場所取りは既定の 1 枠
	var m browseModel
	m.rlDash.toggle()
	m.usageOv.fetchCmd(false)
	defer m.usageOv.stop()
	o := m.ratelimitOpts()
	o.width, o.page = 100, 30
	all := stripANSI(strings.Join(m.rlDash.lines(o), "\n"))
	if want := len(usage.PendingWindows("", nil)) + len(usage.PendingWindows(usage.SourceCodex, nil)); strings.Count(all, m.spinner()+" 取得中...") != want {
		t.Errorf("何も届いていない盤が場所取りのカード %d 枚になっていない:\n%s", want, all)
	}
	m.usageOv.endRound()
	m.usageOv.err = errors.New("初回の全滅")
	m.usageOv.fetchCmd(false)
	o = m.ratelimitOpts()
	o.width, o.page = 100, 30
	if all := stripANSI(strings.Join(m.rlDash.lines(o), "\n")); !strings.Contains(all, "取得失敗") || strings.Contains(all, "取得中...") {
		t.Errorf("初回の全滅の後の取り直しで「取得失敗」が場所取りに隠れた:\n%s", all)
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

// 全画面ダッシュボードは画面全体 (枠・余白・影・hint 行まで) に地色を敷く。
//
// 🚨 「端末の既定へ戻す」では足りない場面のための固定色。scratch popup は display-popup 自身が
// 濃紺を敷いており、その popup では既定の背景が濃紺になる (ユーザー要望 2026-09-01)。
func TestRatelimitDashPaintsScreenBackground(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.colored = true
	m.width, m.height = 100, 30
	m.handleKey("R")
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) < 10 {
		t.Fatalf("行が少なすぎる: %d", len(lines))
	}
	for i, ln := range lines {
		if !strings.HasPrefix(ln, ansiScreenBg) {
			t.Errorf("%d 行目に地色が無い: %q", i, ln)
			continue
		}
		// 端末幅ぶん塗り切る (途中で切れると右側が端末の地色のまま残る)。
		if w := dispWidth(ln); w != m.width {
			t.Errorf("%d 行目の塗り幅 %d != %d: %q", i, w, m.width, ln)
		}
		// 行内の SGR リセットの後は地色を張り直す (リセットで地が切れる)。
		body := strings.TrimSuffix(ln, ansiReset)
		if idx := strings.LastIndex(body, ansiReset); idx >= 0 &&
			!strings.HasPrefix(body[idx+len(ansiReset):], ansiScreenBg) {
			t.Errorf("%d 行目: リセット後に地色を張り直していない: %q", i, ln)
		}
	}
}

// 面塗りはダッシュボードだけ。一覧や色なし (NO_COLOR) では敷かない — 面塗りは環境の配色
// 次第で視認性を落とすため、既定では増やさないというのが repo の判断 (bgLine の doc)。
func TestScreenBackgroundOnlyForDashboard(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.colored = true
	m.width, m.height = 100, 30
	if got := m.screenBg(); got != "" {
		t.Errorf("一覧なのに地色を敷いた: %q", got)
	}
	if strings.Contains(m.View().Content, ansiScreenBg) {
		t.Error("一覧の描画に地色が混ざっている")
	}
	m.handleKey("R")
	if m.screenBg() != ansiScreenBg {
		t.Error("ダッシュボードで地色を敷いていない")
	}
	m.colored = false
	if got := m.screenBg(); got != "" {
		t.Errorf("色なしなのに地色を敷いた: %q", got)
	}
}

// 盤のメモ (issue 275) が「出力を変えない」ことを、メモ無しの結果との差分で固定する。
//
// 🚨 キャッシュの正しさは「速いか」ではなく「**同じ入力で同じ出力か**」でしか確かめられない。
// 各ケースで dropCache() した結果 (= 素の描画) と、キャッシュを温めた結果を突き合わせる。
func TestRatelimitDashCacheMatchesUncached(t *testing.T) {
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	snapA, snapB := rlTestSnap(), rlTestSnap()
	cases := []struct {
		name string
		o    ratelimitRenderOpts
	}{
		{"基準", ratelimitRenderOpts{width: 120, page: 40, colored: false, snap: snapA, now: base}},
		{"幅ちがい", ratelimitRenderOpts{width: 100, page: 40, colored: false, snap: snapA, now: base}},
		{"高さちがい", ratelimitRenderOpts{width: 120, page: 30, colored: false, snap: snapA, now: base}},
		{"色つき", ratelimitRenderOpts{width: 120, page: 40, colored: true, snap: snapA, now: base}},
		{"秒がちがう", ratelimitRenderOpts{width: 120, page: 40, colored: false, snap: snapA, now: base.Add(time.Second)}},
		{"snap がちがう", ratelimitRenderOpts{width: 120, page: 40, colored: false, snap: snapB, now: base}},
	}
	var d ratelimitDash
	d.shown = true
	for _, c := range cases {
		d.dropCache()
		want := d.lines(c.o) // メモ無し (直前に捨てた)
		d.dropCache()
		_ = d.lines(c.o)    // 温める
		got := d.lines(c.o) // ここがキャッシュ経路
		if !d.cacheOK {
			t.Fatalf("%s: キャッシュが立っていない (この経路を測れていない)", c.name)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: 行数が違う %d != %d", c.name, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: %d 行目が違う\n  memo=%q\n  素  =%q", c.name, i, got[i], want[i])
			}
		}
	}

	// 鍵に入っている入力を 1 つ変えたら、必ずキャッシュを外して描き直す。
	// 🚨 比較は**常に基準ケースとの差**で見る。直前のケースと比べる形にすると、
	// 各ケースが 2 箇所ずつ違うせいで「鍵から 1 項目落とす」変異を素通しする
	// (最初にそう書いて踏んだ: sec を鍵から外しても緑だった)。
	for _, c := range cases[1:] {
		d.dropCache()
		_ = d.lines(cases[0].o) // 基準を焼く
		baseKey := d.cacheKey
		_ = d.lines(c.o)
		if d.cacheKey == baseKey {
			t.Errorf("%s: 基準と入力が違うのにキャッシュ鍵が同じ (描き直していない)", c.name)
		}
	}

	// 返すのはコピー (呼び出し側が書き換えてもキャッシュが壊れない)。
	// 🚨 書き換えるのは**キャッシュヒットの戻り値**にすること。ミス経路の戻り値は
	// キャッシュの裏地ではないので、そちらを書き換えても何も検出できない (これも踏んだ)。
	d.dropCache()
	_ = d.lines(cases[0].o) // ミス (ここで焼く)
	hit := d.lines(cases[0].o)
	if !d.cacheOK {
		t.Fatal("キャッシュが立っていない")
	}
	hit[0] = "PWNED"
	if again := d.lines(cases[0].o); again[0] == "PWNED" {
		t.Error("キャッシュの裏地を返している (呼び出し側の書き換えが次のフレームへ漏れる)")
	}
}
