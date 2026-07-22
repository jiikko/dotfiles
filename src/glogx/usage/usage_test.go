package usage

import (
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

// colOf は行内の sub が始まる表示幅カラム位置を返す (CJK 幅考慮、ANSI 無し行専用)。
func colOf(t *testing.T, row, sub string) int {
	t.Helper()
	i := strings.Index(row, sub)
	if i < 0 {
		t.Fatalf("%q が %q に含まれない", sub, row)
	}
	return runewidth.StringWidth(row[:i])
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
	if want := "枠   使用                        残り / リセット"; header != want {
		t.Errorf("header:\n got=%q\nwant=%q", header, want)
	}
	if len(rows) != 2 {
		t.Fatalf("行数 = %d, want 2 (Fable は既定除外)", len(rows))
	}
	// 残り時間は 日/時間/分 の 3 列に右寄せ整列。時間・分は両行で常に出し (粒度を揃える)、
	// 日は 1 日以上のときだけ。5h の空「日」列と 7d の "5日" が同幅になり単位が同じ桁に来る。
	if want := "5h   [▱▱▱▱▱▱▱▱▱▱]   4%      4時間26分 / 7月21日16:26"; rows[0] != want {
		t.Errorf("row0:\n got=%q\nwant=%q", rows[0], want)
	}
	if want := "7d   [▰▰▰▱▱▱▱▱▱▱]  29%   5日3時間 0分 / 7月26日15:00"; rows[1] != want {
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
