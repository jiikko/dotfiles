package usage

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// colOf は行内の sub が始まる表示幅カラム位置を返す (CJK 幅考慮、ANSI 無し行専用)。
func colOf(t *testing.T, row, sub string) int {
	t.Helper()
	i := strings.Index(row, sub)
	if i < 0 {
		t.Fatalf("%q が %q に含まれない", sub, row)
	}
	return termwidth.Of(row[:i])
}

const sampleResult = `You are currently using your subscription to power your Claude Code usage

Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)
Current week (all models): 29% used · resets Jul 24 at 8am (Asia/Tokyo)
Current week (Fable): 48% used · resets Jul 24 at 8am (Asia/Tokyo)

What's contributing to your limits usage?
Last 24h · 875 requests · 7 sessions`

func TestParse(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	snap, err := Parse(sampleResult, now)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(snap.Windows) != 3 {
		t.Fatalf("枠数 = %d, want 3", len(snap.Windows))
	}

	w, ok := snap.Find("5h")
	if !ok {
		t.Fatal("5h 枠が見つからない")
	}
	if w.Percent != 2 {
		t.Errorf("5h percent = %d, want 2", w.Percent)
	}
	want := time.Date(2026, 7, 22, 3, 9, 0, 0, time.Local)
	if !w.ResetAt.Equal(want) {
		t.Errorf("5h ResetAt = %v, want %v", w.ResetAt, want)
	}

	wk, ok := snap.Find("7d")
	if !ok {
		t.Fatal("7d 枠が見つからない")
	}
	if wk.Percent != 29 {
		t.Errorf("7d percent = %d, want 29", wk.Percent)
	}
	// 正時 "8am" (分なし) が 8:00 としてパースされる回帰テスト。
	wantWk := time.Date(2026, 7, 24, 8, 0, 0, 0, time.Local)
	if !wk.ResetAt.Equal(wantWk) {
		t.Errorf("7d ResetAt = %v, want %v", wk.ResetAt, wantWk)
	}

	// Fable 週は別ラベルで格納される (既定描画には出ないが Snapshot には残る)。
	if _, ok := snap.Find("7d(Fable)"); !ok {
		t.Errorf("Fable 週枠のラベルが 7d(Fable) でない: %+v", snap.Windows)
	}
}

func TestParseYearRollover(t *testing.T) {
	// 12/31 時点で "Jan 1" のリセットは翌年になる。
	now := time.Date(2026, 12, 31, 23, 0, 0, 0, time.Local)
	snap, err := Parse("Current session: 5% used · resets Jan 1 at 2:00am (Asia/Tokyo)", now)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	w, _ := snap.Find("5h")
	if w.ResetAt.Year() != 2027 {
		t.Errorf("year rollover 失敗: %v", w.ResetAt)
	}
}

// am/pm が大文字でも枠を取りこぼさない (silent partial loss 回帰)。
func TestParseUppercaseMeridiem(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	snap, err := Parse("Current session: 2% used · resets Jul 22 at 3:09PM (Asia/Tokyo)", now)
	if err != nil {
		t.Fatalf("大文字 PM でパース失敗: %v", err)
	}
	w, _ := snap.Find("5h")
	want := time.Date(2026, 7, 22, 15, 9, 0, 0, time.Local)
	if !w.ResetAt.Equal(want) {
		t.Errorf("3:09PM の解釈 = %v, want %v", w.ResetAt, want)
	}
}

func TestParseError(t *testing.T) {
	if _, err := Parse("no usage lines here", time.Now()); err == nil {
		t.Error("枠なしでエラーにならなかった")
	}
}

func TestRenderLine(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	snap := &Snapshot{Windows: []Window{
		{Label: "5h", Percent: 2, ResetAt: time.Date(2026, 7, 22, 3, 9, 0, 0, time.Local)},
		{Label: "7d", Percent: 28, ResetAt: time.Date(2026, 7, 24, 7, 59, 0, 0, time.Local)},
		{Label: "7d(Fable)", Percent: 48, ResetAt: time.Date(2026, 7, 24, 7, 59, 0, 0, time.Local)},
	}}
	got := RenderLine(snap, now, false)
	// 5h: 12:00 → 翌日 3:09 = 15時間9分。7d: → 7/24 7:59 = 2日19時間59分。
	want := "5h:[▱▱▱▱▱▱▱▱▱▱]2%(残:15時間9分 / 7月22日03:09) 7d:[▰▰▰▱▱▱▱▱▱▱]28%(残:2日19時間 / 7月24日07:59)"
	if got != want {
		t.Errorf("RenderLine:\n got=%q\nwant=%q", got, want)
	}
}

func TestRenderTable(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	snap := &Snapshot{Windows: []Window{
		{Label: "5h", Percent: 4, ResetAt: time.Date(2026, 7, 21, 16, 26, 0, 0, time.Local)},
		{Label: "7d", Percent: 29, ResetAt: time.Date(2026, 7, 26, 15, 0, 0, 0, time.Local)},
		{Label: "7d(Fable)", Percent: 48, ResetAt: now}, // 既定描画には出ない
	}}
	header, rows := RenderTable(snap, now, false)
	if want := "枠   使用                         残り / リセット"; header != want {
		t.Errorf("header:\n got=%q\nwant=%q", header, want)
	}
	if len(rows) != 2 {
		t.Fatalf("行数 = %d, want 2 (Fable は既定除外)", len(rows))
	}
	// 残り時間は 日/時間/分 の 3 列に右寄せ整列。時間・分は両行で常に出し (粒度を揃える)、
	// 日は 1 日以上のときだけ。各列は取りうる最大の幅 ("6日" / "23時間" / "59分" / "12月" / "31日") から始める
	// (値で列が広がって箱の幅が変わらないように。issue 626)。
	if want := "5h   [▱▱▱▱▱▱▱▱▱▱]   4%       4時間26分 /  7月21日16:26"; rows[0] != want {
		t.Errorf("row0:\n got=%q\nwant=%q", rows[0], want)
	}
	if want := "7d   [▰▰▰▱▱▱▱▱▱▱]  29%   5日 3時間 0分 /  7月26日15:00"; rows[1] != want {
		t.Errorf("row1:\n got=%q\nwant=%q", rows[1], want)
	}
}

// 残り時間の単位とリセット時刻の桁位置が、月日/時分の桁数が違っても縦に揃う。
func TestRenderTableAlignsColumns(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.Local)
	snap := &Snapshot{Windows: []Window{
		{Label: "5h", Percent: 4, ResetAt: time.Date(2026, 12, 28, 14, 30, 0, 0, time.Local)},
		{Label: "7d", Percent: 29, ResetAt: time.Date(2026, 7, 3, 9, 5, 0, 0, time.Local)},
	}}
	header, rows := RenderTable(snap, now, false)
	// " / " 区切り = 残り列の右端が両行で同じ桁 (残り時間の整列)。ヘッダーの区切りも
	// データ行と同じ桁に揃う (固定文字列ヘッダーだと横ずれする回帰の防止)。
	if a, b := colOf(t, rows[0], " / "), colOf(t, rows[1], " / "); a != b {
		t.Errorf("残り列の / 位置がずれる: %d vs %d\n%q\n%q", a, b, rows[0], rows[1])
	}
	if a, b := colOf(t, header, " / "), colOf(t, rows[0], " / "); a != b {
		t.Errorf("ヘッダーの / 位置がデータ行とずれる: %d vs %d\n%q\n%q", a, b, header, rows[0])
	}
	// リセット時刻 HH:MM が両行で同じ桁 (月日の桁数が違っても揃う)。
	if a, b := colOf(t, rows[0], "14:30"), colOf(t, rows[1], "09:05"); a != b {
		t.Errorf("リセット時刻の位置がずれる: %d vs %d\n%q\n%q", a, b, rows[0], rows[1])
	}
}

func TestBar(t *testing.T) {
	cases := map[int]string{
		0:   "[▱▱▱▱▱▱▱▱▱▱]",
		2:   "[▱▱▱▱▱▱▱▱▱▱]",
		13:  "[▰▱▱▱▱▱▱▱▱▱]",
		28:  "[▰▰▰▱▱▱▱▱▱▱]",
		50:  "[▰▰▰▰▰▱▱▱▱▱]",
		90:  "[▰▰▰▰▰▰▰▰▰▱]",
		100: "[▰▰▰▰▰▰▰▰▰▰]",
	}
	for pct, want := range cases {
		if got := bar(pct, false); got != want {
			t.Errorf("bar(%d) = %q, want %q", pct, got, want)
		}
	}
}

func TestFormatRemain(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{4*time.Hour + 39*time.Minute, "4時間39分"},
		{2*24*time.Hour + 9*time.Hour, "2日9時間"},
		{-time.Minute, "リセット済み"},
	}
	for _, c := range cases {
		if got := formatRemain(c.d); got != c.want {
			t.Errorf("formatRemain(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"2.1.216 (Claude Code)\n": "2.1.216",
		"2.1.216":                 "2.1.216",
		"  1.0.0 (x)  ":           "1.0.0",
		"":                        "",
		"   \n":                   "",
	}
	for in, want := range cases {
		if got := parseVersion(in); got != want {
			t.Errorf("parseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// parseResetTime の 1 時間緩衝: リセット再計算のレースで「直近に過ぎたリセット」を翌年へ繰り上げ
// ない緩衝 (usage.go の -time.Hour)。定数を 0 に変えても既存 TestParse 系は green のままだった
// 無防備な意図的ロジックなので閾値を pin する。
func TestParseResetTimeOneHourBuffer(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	// 30 分前 (緩衝内) は当年のまま — レースで翌年へ繰り上げない
	within, err := parseResetTime("Jun 15", "11:30am", now)
	if err != nil {
		t.Fatal(err)
	}
	if within.Year() != now.Year() {
		t.Errorf("30分前 (緩衝内) が当年でない: %v", within)
	}
	// 2 時間前 (緩衝超え) は過去のリセット = 年境界とみなし翌年へ
	past, err := parseResetTime("Jun 15", "10:00am", now)
	if err != nil {
		t.Fatal(err)
	}
	if past.Year() != now.Year()+1 {
		t.Errorf("2時間前が翌年へ繰り上がらない: %v", past)
	}
}

// 取得中の場所取りの枠 (PendingWindows) は、届いた後と同じ幅の行で「取得中...」だけを出す (issue 626)。
func TestRenderTablePendingKeepsWidth(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	got := (&Snapshot{}).With(Part{Windows: PendingWindows("", nil)})
	arrived := (&Snapshot{}).With(Part{Windows: []Window{
		{Label: "5h", Percent: 4, ResetAt: now.Add(4*time.Hour + 26*time.Minute)},
		{Label: "7d", Percent: 29, ResetAt: now.Add(5*24*time.Hour + 3*time.Hour)},
	}})
	_, rows := RenderTable(got, now, false)
	_, want := RenderTable(arrived, now, false)
	if len(rows) != len(want) {
		t.Fatalf("行の数 = %d, want %d: %q", len(rows), len(want), rows)
	}
	for i := range rows {
		if termwidth.Of(rows[i]) != termwidth.Of(want[i]) {
			t.Errorf("行 %d の幅 = %d, want %d:\n%q\n%q", i, termwidth.Of(rows[i]), termwidth.Of(want[i]), rows[i], want[i])
		}
		if !strings.Contains(rows[i], "取得中...") || strings.Contains(rows[i], "/") || strings.Contains(rows[i], "%") {
			t.Errorf("場所取りの行が値や区切りを出す: %q", rows[i])
		}
	}
}

// 盤も、場所取りのカードで届いた後と同じ段の割り付けにする (段の罫線が同じ行に来る)。
func TestRenderDashboardPendingKeepsLayout(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)
	cx := Part{Source: SourceCodex, Windows: []Window{{Label: "cx7d", Source: SourceCodex, Percent: 12, WindowMins: 10080, ResetAt: now.Add(6 * 24 * time.Hour)}}}
	cl := Part{Windows: []Window{
		{Label: "5h", Percent: 34, WindowMins: 300, ResetAt: now.Add(4*time.Hour + 26*time.Minute)},
		{Label: "7d", Percent: 41, WindowMins: 10080, ResetAt: now.Add(5*24*time.Hour + 3*time.Hour)},
	}}
	rules := func(s *Snapshot) []int {
		var at []int
		for i, l := range RenderDashboard(s, now, 110, 34, false) {
			if strings.HasPrefix(l, "──") {
				at = append(at, i)
			}
		}
		return at
	}
	before := rules((*Snapshot)(nil).With(cx).With(Part{Windows: PendingWindows("", nil)}))
	after := rules((*Snapshot)(nil).With(cx).With(cl))
	if len(after) != 2 || fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("段の罫線の位置: 取得中 %v / 届いた後 %v", before, after)
	}
	lines := strings.Join(RenderDashboard((*Snapshot)(nil).With(cx).With(Part{Windows: PendingWindows("", nil)}), now, 110, 34, false), "\n")
	if strings.Count(lines, "復活まで") != 1 || strings.Count(lines, "想定") != 1 {
		t.Errorf("場所取りのカードが値の欄を出す (codex の 1 枚だけのはず):\n%s", lines)
	}
	if strings.Count(lines, "取得中...") != 2 {
		t.Errorf("Claude の 2 枚が場所取りになっていない:\n%s", lines)
	}
}

// codex の場所取りは、前回の枠の構成を写す (プランで 1 枠か 2 枠かが変わる)。無ければ weekly 1 枠。
func TestPendingWindowsCopiesCodexShape(t *testing.T) {
	like := []Window{{Label: "5h", Percent: 9}, {Label: "cx5h", Source: SourceCodex, WindowMins: 300, Percent: 3}, {Label: "cx7d", Source: SourceCodex, WindowMins: 10080}}
	got := PendingWindows(SourceCodex, like)
	if len(got) != 2 || got[0].Label != "cx5h" || got[1].Label != "cx7d" || !got[0].Pending || got[0].Percent != 3 || got[0].WindowMins != 300 {
		t.Errorf("前回の codex の形を写していない: %+v", got)
	}
	// 窓幅を持たない頃のキャッシュの Claude 枠は、既定の窓幅に戻す
	if got := PendingWindows("", []Window{{Label: "5h", Percent: 9}}); got[0].WindowMins != 300 || got[0].Percent != 9 {
		t.Errorf("Claude の窓幅 0 を写した: %+v", got[0])
	}
	if got := PendingWindows(SourceCodex, nil); len(got) != 1 || got[0].Label != "cx7d" {
		t.Errorf("前回が無いときの形: %+v", got)
	}
}

// 盤の場所取りのカードは、届いた後のカードと同じ見出しの形 (AA の 3 行か 1 行か) を採る。
func TestRenderDashboardPendingKeepsCardHead(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)
	cx := Part{Source: SourceCodex, Windows: []Window{{Label: "cx7d", Source: SourceCodex, Percent: 12, WindowMins: 10080, ResetAt: now.Add(6 * 24 * time.Hour)}}}
	cl := Part{Windows: []Window{
		{Label: "5h", Percent: 34, WindowMins: 300, ResetAt: now.Add(4*time.Hour + 26*time.Minute)},
		{Label: "7d", Percent: 41, WindowMins: 10080, ResetAt: now.Add(5*24*time.Hour + 3*time.Hour)},
	}}
	// 5h が前回も今回も未消費 (Unused) の場合も同じ形を取る (下の段の組み方が普通の枠と違う)
	unused := Part{Windows: []Window{{Label: "5h", WindowMins: 300, Unused: true}, cl.Windows[1]}}
	for _, arrivedPart := range []Part{cl, unused} {
		// 前回の値が今回に近ければ一致する (前回は 1 分前に取った 1 pt 低い値)
		like := (*Snapshot)(nil).With(arrivedPart).Windows
		for i := range like {
			if !like[i].Unused {
				like[i].Percent--
				like[i].ResetAt = like[i].ResetAt.Add(time.Minute)
			}
		}
		testPendingCardShape(t, now, cx, arrivedPart, like)
	}
}

func testPendingCardShape(t *testing.T, now time.Time, cx, cl Part, like []Window) {
	t.Helper()
	pendingSnap := (*Snapshot)(nil).With(cx).With(Part{Windows: PendingWindows("", like)})
	arrivedSnap := (*Snapshot)(nil).With(cx).With(cl)
	pc, ac := dialCards(pendingSnap), dialCards(arrivedSnap)
	if len(pc) != len(ac) {
		t.Fatalf("カードの数: 取得中 %d / 届いた後 %d", len(pc), len(ac))
	}
	// カード 1 枚の見出しと下の段の行数 (本体はその残り) が、あらゆる寸法で揃う
	mismatch := 0
	for w := 26; w <= 110; w += 3 {
		for h := 9; h <= 40; h++ {
			for i := range pc {
				ph, pf := cardFrame(pc[i], now, w, h, false, false)
				ah, af := cardFrame(ac[i], now, w, h, false, false)
				if len(ph) != len(ah) || len(pf) != len(af) {
					if mismatch++; mismatch <= 3 {
						t.Errorf("%s %dx%d: (見出し, 下の段) 取得中 (%d, %d) / 届いた後 (%d, %d)", pc[i].label, w, h, len(ph), len(pf), len(ah), len(af))
					}
				}
			}
		}
	}
	if mismatch > 3 {
		t.Errorf("ほか %d 件", mismatch-3)
	}
	// 段の罫線の位置も揃い、場所取りに前回の値の語が漏れない
	for w := 60; w <= 220; w += 16 {
		for h := 16; h <= 70; h += 6 {
			rules := func(s *Snapshot) string {
				var at []int
				for i, l := range RenderDashboard(s, now, w, h, false) {
					if strings.HasPrefix(l, "──") {
						at = append(at, i)
					}
				}
				return fmt.Sprint(at)
			}
			if p, a := rules(pendingSnap), rules(arrivedSnap); p != a {
				t.Errorf("%dx%d: 段の罫線の位置: 取得中 %s / 届いた後 %s", w, h, p, a)
			}
			if all := strings.Join(RenderDashboard(pendingSnap, now, w, h, false), "\n"); strings.Contains(all, "未消費") {
				t.Fatalf("%dx%d: 場所取りに前回の「未消費」が漏れた:\n%s", w, h, all)
			}
		}
	}
}
