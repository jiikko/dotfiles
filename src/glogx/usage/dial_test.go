package usage

import (
	"slices"
	"strings"
	"testing"
	"time"

	"glogx/termwidth"
)

func dialTestNow() time.Time { return time.Date(2026, 8, 31, 22, 14, 0, 0, time.Local) }

func dialTestSnap() *Snapshot {
	now := dialTestNow()
	mins := func(d time.Duration) int64 { return int64(d / time.Minute) }
	return &Snapshot{
		Windows: []Window{
			{Label: "5h", Percent: 62, ResetAt: now.Add(108 * time.Minute), WindowMins: mins(5 * time.Hour)},
			{Label: "7d", Percent: 78, ResetAt: now.Add(3420 * time.Minute), WindowMins: mins(7 * 24 * time.Hour)},
			{Label: "cx5h", Source: SourceCodex, Percent: 31, ResetAt: now.Add(185 * time.Minute), WindowMins: mins(5 * time.Hour)},
			{Label: "cx7d", Source: SourceCodex, Percent: 44, ResetAt: now.Add(5900 * time.Minute), WindowMins: mins(7 * 24 * time.Hour)},
		},
	}
}

// 全画面ダッシュボードは「ちょうど height 行」「どの行も width を超えない」が契約。
// 超えると呼び出し側 (finishWindow) が枠を組めず、行が折り返して画面全体が崩れる。
func TestRenderDashboardFitsBox(t *testing.T) {
	snap := dialTestSnap()
	sizes := []struct{ w, h int }{
		{120, 36}, {100, 30}, {80, 24}, {60, 20}, {40, 12}, {30, 8}, {26, 9}, {200, 50},
	}
	for _, colored := range []bool{false, true} {
		for _, s := range sizes {
			lines := RenderDashboard(snap, dialTestNow(), s.w, s.h, colored)
			if len(lines) != s.h {
				t.Fatalf("colored=%v %dx%d: 行数 %d (want %d)", colored, s.w, s.h, len(lines), s.h)
			}
			for i, ln := range lines {
				if got := termwidth.Of(ln); got > s.w {
					t.Errorf("colored=%v %dx%d: %d 行目の幅 %d > %d\n%q", colored, s.w, s.h, i, got, s.w, ln)
				}
			}
		}
	}
}

// 枠が 1 つも無い / Snapshot が nil のときは nil を返す (呼び出し側が取得中・失敗を出す)。
func TestRenderDashboardEmpty(t *testing.T) {
	if got := RenderDashboard(nil, dialTestNow(), 80, 24, false); got != nil {
		t.Errorf("nil Snapshot で %v", got)
	}
	if got := RenderDashboard(&Snapshot{}, dialTestNow(), 80, 24, false); got != nil {
		t.Errorf("枠なしで %v", got)
	}
}

// 盤には「復活まで」と使用率が必ず出る (見た目が変わっても情報は落とさない)。
func TestRenderDashboardShowsRemainAndPercent(t *testing.T) {
	all := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), 120, 36, false), "\n")
	for _, want := range []string{"復活まで", "1時間48分", "62%", "78%", "31%", "44%"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q が出ていない", want)
		}
	}
}

// 窓幅が不明な枠 (codex が windowDurationMins を返さない) でも落ちず、盤の代わりに
// 「窓幅が不明」と明示する。経過が分からないまま針を描くと嘘になる。
func TestRenderDashboardUnknownSpan(t *testing.T) {
	snap := &Snapshot{Windows: []Window{
		{Label: "cx", Source: SourceCodex, Percent: 50, ResetAt: dialTestNow().Add(time.Hour)},
	}}
	all := strings.Join(RenderDashboard(snap, dialTestNow(), 80, 24, false), "\n")
	if !strings.Contains(all, "窓幅が不明") {
		t.Errorf("窓幅不明の断りが無い:\n%s", all)
	}
	if strings.ContainsRune(all, '⠿') || strings.Contains(all, "想定 50%") {
		t.Errorf("窓幅不明なのに盤・想定を描いている:\n%s", all)
	}
}

func TestPaceState(t *testing.T) {
	cases := []struct {
		used     int
		elapsed  float64
		band     float64
		wantWord string
	}{
		{100, 10, 10, "上限"}, // 上限は乖離に関わらず優先
		{90, 60, 10, "超過"},  // +30pt >= band*2
		{75, 60, 10, "先行"},  // +15pt >= band
		{62, 64, 25, "適正"},  // -2pt (5h の帯)
		{62, 64, 10, "適正"},  // 帯の内側
		{40, 60, 10, "余裕"},  // -20pt
		{10, 60, 10, "余剰"},  // -50pt < -band*2.5
	}
	for _, c := range cases {
		if _, word := paceState(c.used, c.elapsed, c.band); word != c.wantWord {
			t.Errorf("paceState(%d, %v, %v) = %q, want %q", c.used, c.elapsed, c.band, word, c.wantWord)
		}
	}
}

// 帯は窓の長さで変える。5h と weekly を同じ帯にすると 5h が常時「先行」になって信号にならない。
func TestPaceBandDependsOnSpan(t *testing.T) {
	if got := paceBand(5 * time.Hour); got != 25 {
		t.Errorf("5h の帯 = %v", got)
	}
	if got := paceBand(7 * 24 * time.Hour); got != 10 {
		t.Errorf("weekly の帯 = %v", got)
	}
}

// 枠の並びは RenderLine / RenderTable と同じ (Claude → codex)。見出しの codex ラベルは
// "cx" を落とす (カードは CLI 名を持つので接頭辞が重複する)。
func TestDialCardsOrderAndLabels(t *testing.T) {
	cards := dialCards(dialTestSnap())
	got := make([]string, 0, len(cards))
	for _, c := range cards {
		got = append(got, c.cli+"/"+c.label+"/"+c.kind)
	}
	want := []string{
		"Claude Code/5h/セッション", "Claude Code/7d/weekly",
		"codex/5h/セッション", "codex/7d/weekly",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestDialDivisions(t *testing.T) {
	cases := map[time.Duration]int{
		5 * time.Hour:      5,
		7 * 24 * time.Hour: 7,
		0:                  6,
		90 * time.Minute:   6, // 割り切れない窓幅は目安の 6 等分
	}
	for span, want := range cases {
		if got := dialDivisions(span); got != want {
			t.Errorf("dialDivisions(%v) = %d, want %d", span, got, want)
		}
	}
}

// 窓幅は「表示ラベル」ではなく取得時に確定した値から来る (Span)。
func TestWindowSpan(t *testing.T) {
	if got := (Window{WindowMins: 300}).Span(); got != 5*time.Hour {
		t.Errorf("Span() = %v", got)
	}
	if got := (Window{}).Span(); got != 0 {
		t.Errorf("未設定の Span() = %v (0 = 不明であるべき)", got)
	}
}

func TestWindowMinsFor(t *testing.T) {
	cases := map[string]int64{
		"Current session":           300,
		"Current week (all models)": 7 * 24 * 60,
		"Current week (Fable)":      7 * 24 * 60,
		"Something else":            0,
	}
	for raw, want := range cases {
		if got := windowMinsFor(raw); got != want {
			t.Errorf("windowMinsFor(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestCodexWindowMins(t *testing.T) {
	m := int64(300)
	neg := int64(-5)
	if got := codexWindowMins(&m); got != 300 {
		t.Errorf("= %d", got)
	}
	if got := codexWindowMins(nil); got != 0 {
		t.Errorf("null = %d (0 であるべき)", got)
	}
	if got := codexWindowMins(&neg); got != 0 {
		t.Errorf("負値 = %d (0 へ倒すべき)", got)
	}
}

// 点描キャンバスは「どの行も cols セルちょうど」を守る。ここが崩れると盤の下に敷いた
// テキストと縦が揃わず、格子全体がずれる。
func TestBrailleLineWidth(t *testing.T) {
	cv := newBraille(20, 5)
	cv.arc(20, 10, 8, 0, 1, "", 2, 1)
	cv.putText(2, 8, "残1時間48分", "")
	for i, ln := range cv.lines(false) {
		if got := termwidth.Of(ln); got > 20 {
			t.Errorf("%d 行目の幅 %d > 20: %q", i, got, ln)
		}
	}
}

// 全角文字は 2 セルを占め、2 セル目には何も出さない。素朴に 1 セル 1 文字で置くと
// 行の表示幅が cols を超え、盤の下のテキストと縦が揃わなくなる。
func TestBraillePutTextKeepsGridWithWideChars(t *testing.T) {
	cv := newBraille(12, 2)
	cv.arc(12, 4, 5, 0, 1, "", 1, 1) // 下地の点描があっても覆った桁に漏れないこと
	cv.putText(0, 0, "残2日9時間", "")
	line := cv.lines(false)[0]
	if w := termwidth.Of(line); w > 12 {
		t.Errorf("全角で格子が広がった: 幅 %d > 12 (%q)", w, line)
	}
	if !strings.Contains(line, "残2日9時間") {
		t.Errorf("全角が落ちた: %q", line)
	}
}

// 段と段のあいだには空行が入る (ユーザー要望 2026-08-31: 1 段目と 2 段目が密着して見える)。
// 空行が消えても幅・行数の契約は満たされるので、間隔は別の主張として固定する。
func TestRenderDashboardSeparatesRows(t *testing.T) {
	lines := RenderDashboard(dialTestSnap(), dialTestNow(), 120, 44, false)
	head2 := -1
	codexBanner := bannerLines("codex")[0]
	for i, ln := range lines {
		if strings.Contains(ln, codexBanner) {
			head2 = i
			break
		}
	}
	if head2 < 2 {
		t.Fatalf("2 段目の見出しが見つからない (index=%d)", head2)
	}
	if strings.TrimSpace(lines[head2-1]) != "" {
		t.Errorf("段のあいだに空行が無い: %q", lines[head2-1])
	}
	if strings.TrimSpace(lines[head2-2]) == "" {
		t.Errorf("空行の前が空 (1 段目が短すぎる / 空行が余っている): %q", lines[head2-2])
	}
}

// 盤に載せた文字 (残り時間・使用率の AA) はリングにも針にも接しない。接すると数字が
// 読めなくなるが、幅も行数も契約どおりなので他のテストでは検出できない
// (実測: 中央行を 1 行上へずらす / 針を中心から引く / 余白を 1 セルにする、のいずれでも接した)。
//
// 「盤の行」= 点描 (braille) を含む行。その行の中で点描と文字が隣り合っていたら接触。
// 文字の中身を書かずに接触だけを見るので、中央の表記が変わってもこのテストは張り替え不要。
func TestRenderDashboardCenterTextHasClearance(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 44}, {100, 40}, {160, 50}} {
		for _, ln := range RenderDashboard(dialTestSnap(), dialTestNow(), size.w, size.h, false) {
			rs := []rune(ln)
			if !slices.ContainsFunc(rs, isBrailleRune) {
				continue // 盤の行ではない (見出し・ゲージ・数値行)
			}
			for i := 1; i < len(rs); i++ {
				a, b := rs[i-1], rs[i]
				if isBrailleRune(a) && isFaceText(b) || isFaceText(a) && isBrailleRune(b) {
					t.Errorf("%dx%d: 盤と文字が接している (%q と %q): %q", size.w, size.h, a, b, ln)
				}
			}
		}
	}
}

func isBrailleRune(r rune) bool { return r >= 0x2800 && r <= 0x28FF }

// isFaceText は盤の上に載せた文字か (空白と点描以外)。AA のブロック文字も含む。
func isFaceText(r rune) bool { return r != ' ' && !isBrailleRune(r) }

// 盤の中央には残り時間と使用率が必ず出る (表記は幅で変わるので、数字だけを見る)。
func TestRenderDashboardCenterShowsRemainAndPercent(t *testing.T) {
	lines := RenderDashboard(dialTestSnap(), dialTestNow(), 120, 44, false)
	face := make([]string, 0, len(lines))
	for _, ln := range lines {
		if slices.ContainsFunc([]rune(ln), isBrailleRune) {
			face = append(face, ln)
		}
	}
	all := strings.Join(face, "\n")
	for _, want := range []string{"1時間48分", "2日9時間"} {
		if !strings.Contains(all, want) {
			t.Errorf("盤の中央に %q が無い:\n%s", want, all)
		}
	}
	// 使用率は AA (ブロック文字) で出す。普通の字の "62%" が盤に残っていたら AA が
	// 効いていない (ユーザー要望 2026-09-01: 普通の文字より大きく)。
	if !strings.Contains(all, "%") {
		t.Errorf("盤の中央に使用率が無い:\n%s", all)
	}
	if strings.Contains(all, "62%") {
		t.Errorf("使用率が普通の字のまま (AA になっていない):\n%s", all)
	}
	// 残り時間は日本語表記 (ユーザー要望 2026-09-01: 6d23h ではなく 6日23時間)。
	if strings.Contains(all, "2d09h") {
		t.Errorf("ASCII 表記が残っている:\n%s", all)
	}
}
