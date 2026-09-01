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
		// ⚠️ バージョンを空にしないこと。見出しはバージョンを右へ後付けするので、空だと
		// 幅の契約テストがその経路を通らない (実測 2026-09-01: 56 通りの幅で契約が
		// 破れていたのに素通りしていた)。
		Version:      "2.1.216",
		CodexVersion: "0.144.6",
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
	var sizes []struct{ w, h int }
	for w := 1; w <= 200; w += 3 {
		for h := 1; h <= 60; h += 3 {
			sizes = append(sizes, struct{ w, h int }{w, h})
		}
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
	// ⚠️ 特定の braille 1 文字 ('⠿' 等) を検出子にしない。braille は 256 通りあり、正常な盤にも
	// 出ない字が大半なので「盤を描いた」の検出にならない (実測: 盤を描かせる変異を当てても
	// 発火しなかった)。範囲で判定する。
	if slices.ContainsFunc([]rune(all), isBrailleRune) {
		t.Errorf("窓幅不明なのに盤を描いている:\n%s", all)
	}
	// 窓幅が分からないので想定・乖離・状態語は出さない (根拠のない断定をしない)。
	for _, ng := range []string{"想定", "pt ", "適正", "超過", "先行", "余裕", "余剰"} {
		if strings.Contains(all, ng) {
			t.Errorf("窓幅不明なのにペース判定を出している (%q):\n%s", ng, all)
		}
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
	// ⚠️ 幅を 2 点見る。120 は肩書き罫線 (案 A)、225 は素の罫線 (案 B) で、線を作る関数が
	// 別なので片方だけでは「もう一方が罫線を出さなくなった」を検出できない
	// (変異検証で実測 2026-09-01: plainRule を空にしても 120 だけの検査は green)。
	for _, w := range []int{120, 225} {
		assertGroupRules(t, w)
	}
}

func assertGroupRules(t *testing.T, width int) {
	t.Helper()
	lines := RenderDashboard(dialTestSnap(), dialTestNow(), width, 44, false)
	// 罫線 = ─ が 10 桁以上続く行 (肩書き付きの案 A では CLI 名が線の中に入るので、
	// 「全部 ─」では拾えない)
	rules := []int{}
	for i, ln := range lines {
		if strings.Contains(ln, strings.Repeat("─", 10)) {
			rules = append(rules, i)
		}
	}
	if len(rules) != 2 {
		t.Fatalf("w=%d: 罫線が段の数だけ出ていない (%d 本): %v", width, len(rules), rules)
	}
	if rules[0] != 0 {
		t.Errorf("w=%d: 1 段目の罫線が先頭に無い (index=%d)", width, rules[0])
	}
	// 2 段目の罫線の直前は 1 段目の中身 (罫線が余った空行の中に浮いていない)
	if prev := strings.TrimSpace(lines[rules[1]-1]); prev == "" {
		t.Errorf("w=%d: 2 段目の罫線の前が空 (1 段目が短すぎる): index=%d", width, rules[1])
	}
}

// 盤に載せた文字 (残り時間・使用率の AA) はリングにも針にも接しない。接すると数字が
// 読めなくなるが、幅も行数も契約どおりなので他のテストでは検出できない
// (実測: 中央行を 1 行上へずらす / 針を中心から引く / 余白を 1 セルにする、のいずれでも接した)。
//
// 「盤の行」= 点描 (braille) を含む行。その行の中で点描と文字が隣り合っていたら接触。
// 文字の中身を書かずに接触だけを見るので、中央の表記が変わってもこのテストは張り替え不要。
func TestRenderDashboardCenterTextHasClearance(t *testing.T) {
	checked := 0
	for w := 40; w <= 200; w += 4 {
		for h := 12; h <= 60; h += 2 {
			for _, ln := range RenderDashboard(dialTestSnap(), dialTestNow(), w, h, false) {
				rs := []rune(ln)
				if !slices.ContainsFunc(rs, isBrailleRune) {
					continue // 盤の行ではない (見出し・ゲージ・数値行)
				}
				checked++
				for i := 1; i < len(rs); i++ {
					a, b := rs[i-1], rs[i]
					if isBrailleRune(a) && isFaceText(b) || isFaceText(a) && isBrailleRune(b) {
						t.Errorf("%dx%d: 盤と文字が接している (%q と %q): %q", w, h, a, b, ln)
					}
				}
			}
		}
	}
	// ⚠️ 「盤の行が 1 つも無い」= このテストは何も検査していない。中央の描画をまるごと
	// 落とす変異でも pass してしまうので、検査したことを数で確かめる。
	if checked == 0 {
		t.Fatal("盤の行が 1 つも無い (テストが空回りしている)")
	}
	t.Logf("検査した盤の行: %d", checked)
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

// 盤の中央の AA が「正しい値」を出していること。存在と幅だけを見ていると、値を取り違える
// 変異 (pct を定数に差し替える等) が素通りする — 使用率は盤の下の数値行にも出るので、
// 盤の外の文字列だけでは AA の中身を守れない。
func TestRenderDashboardCenterAAShowsActualPercent(t *testing.T) {
	face := faceLines(RenderDashboard(dialTestSnap(), dialTestNow(), 120, 44, false))
	// 62 の 1 行目 (4 桁幅の字形)。リテラルで固定する — production の digitLines から
	// 期待値を作ると自己言及になり、字形が変わっても検出できない。
	if !strings.Contains(face, "█▀▀▀ ▀▀▀█") {
		t.Errorf("62%% の AA が出ていない:\n%s", face)
	}
	// 78 の 2 行目。カードごとに違う値が出ていること (全カードが同じ定数に化ける変異を弾く)。
	if !strings.Contains(face, " ▄▀  █▀▀█") {
		t.Errorf("78%% の AA が出ていない:\n%s", face)
	}
}

// faceLines は盤 (点描を含む行) だけを返す。
func faceLines(lines []string) string {
	var b strings.Builder
	for _, ln := range lines {
		if slices.ContainsFunc([]rune(ln), isBrailleRune) {
			b.WriteString(ln + "\n")
		}
	}
	return b.String()
}

// 全角が最終桁に来たら書かない。書くと 2 セル目に「覆われている」印を打てず、その行の
// 表示幅が cols+1 になる (格子が 1 桁広がる)。左へはみ出す分も同様に落とす。
func TestBraillePutTextClipsAtEdges(t *testing.T) {
	for _, c := range []struct {
		name  string
		start int
	}{{"右端に全角", 11}, {"左へはみ出す", -3}, {"完全に外", 99}} {
		cv := newBraille(12, 1)
		cv.putText(0, c.start, "残2日9時間", "")
		if w := termwidth.Of(cv.lines(false)[0]); w > 12 {
			t.Errorf("%s: 幅 %d > 12 (%q)", c.name, w, cv.lines(false)[0])
		}
	}
}

// 段によって盤が出たり出なかったりしない。見出しの AA は CLI 名の桁数で可否が決まるので、
// 「AA が 4 行食ったせいでその段だけ盤が消える」= 同じ画面で Claude 段は盤あり・codex 段は
// 空行だけ、という非対称が起きていた (セルフレビュー指摘 2026-09-01)。
func TestRenderDashboardGroupsStayConsistent(t *testing.T) {
	for w := 26; w <= 200; w += 3 {
		for h := 12; h <= 60; h += 2 {
			lines := RenderDashboard(dialTestSnap(), dialTestNow(), w, h, false)
			half := len(lines) / 2
			upper, lower := 0, 0
			for i, ln := range lines {
				if !slices.ContainsFunc([]rune(ln), isBrailleRune) {
					continue
				}
				if i < half {
					upper++
				} else {
					lower++
				}
			}
			if (upper == 0) != (lower == 0) {
				t.Errorf("%dx%d: 片方の段だけ盤が消えた (上 %d 行 / 下 %d 行)", w, h, upper, lower)
			}
		}
	}
}

// カード見出しの枠ラベルは 4 桁幅の AA。種別 (セッション / weekly) は AA にできないので
// 中段の右へ普通の字で添える (ユーザー要望 2026-09-01)。
func TestRenderDashboardCardHeadAA(t *testing.T) {
	all := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), 130, 50, false), "\n")
	for _, want := range append(bigLines("5H"), bigLines("7D")...) {
		// ⚠️ 行末の空白は格子の組み立てで落ちるので、期待値も落としてから探す。
		if !strings.Contains(all, strings.TrimRight(want, " ")) {
			t.Errorf("枠ラベルの AA が出ていない (%q):\n%s", want, all)
		}
	}
	for _, want := range []string{"セッション", "weekly"} {
		if !strings.Contains(all, want) {
			t.Errorf("種別 %q が出ていない", want)
		}
	}
	// 普通の字の見出しは残っていない (AA が効いている)。
	if strings.Contains(all, "5h セッション") {
		t.Errorf("普通の字の見出しのまま:\n%s", all)
	}
}

// 盤が残らない高さでは見出しを AA にしない。見出しを大きくして盤が消えたら本末転倒。
func TestRenderDashboardCardHeadFallsBackWhenFaceWouldDie(t *testing.T) {
	for h := 12; h <= 60; h++ {
		lines := RenderDashboard(dialTestSnap(), dialTestNow(), 130, h, false)
		hasFace := false
		for _, ln := range lines {
			if slices.ContainsFunc([]rune(ln), isBrailleRune) {
				hasFace = true
				break
			}
		}
		all := strings.Join(lines, "\n")
		hasAAHead := strings.Contains(all, bigLines("5H")[0])
		if hasAAHead && !hasFace {
			t.Errorf("h=%d: 見出しを AA にしたせいで盤が消えた:\n%s", h, all)
		}
	}
}

// 見出しの AA より中央の使用率の AA を優先する。見出しの AA は 3 行あり盤が 2 行縮むので、
// 「普通の見出しなら中央が AA になれたのに、見出しを AA にしたせいで落ちる」高さでは
// 見出しを AA にしない (盤の主役は中央の数字)。
//
// ⚠️ 描画結果から「見出しが AA かつ中央が普通の字」を探すだけでは足りない。どちらにしても
// 中央が入らない小さな盤では、見出しを AA にしても何も失っていないので正しい状態になる。
// 判断そのもの (cardHead) を突く。
func TestCardHeadYieldsToCenterAA(t *testing.T) {
	c := dialCards(dialTestSnap())[0] // Claude の 5h
	const w, footN = 58, 4
	conflicts := 0
	for h := 8; h <= 40; h++ {
		plainFits := centerAAFits(w, h-1-footN, c.win.Percent)
		aaFits := centerAAFits(w, h-bannerRows-footN, c.win.Percent)
		head := cardHead(c, "", w, h, footN, false)
		switch {
		case plainFits && !aaFits: // 見出しを AA にすると中央を失う高さ
			conflicts++
			if len(head) != 1 {
				t.Errorf("h=%d: 中央の AA を捨てて見出しを AA にした (%d 行)", h, len(head))
			}
		case aaFits && h-bannerRows-footN >= 5: // 両方入る高さ
			if len(head) != bannerRows {
				t.Errorf("h=%d: 両方入るのに見出しが AA でない (%d 行)", h, len(head))
			}
		}
	}
	if conflicts == 0 {
		t.Fatal("優先順位が問われる高さが 1 つも無い (テストが空回りしている)")
	}
	t.Logf("優先順位が問われた高さ: %d 通り", conflicts)
}

// 見出しの形は画面の中で 1 つに揃う (ユーザー指摘 2026-09-01: 「codex 5h のラベルだけ AA に
// なっていないか」)。段ごとに幅の判定を独立させると、CLI 名の桁数が違うぶん codex の段だけ
// 合体帯になり、同じ画面で見出しの読み方が 2 通りになる。
func TestRenderDashboardBandFormIsUniform(t *testing.T) {
	claudeAA, codexAA := bannerLines("Claude Code")[0], bannerLines("codex")[0]
	mixed := 0
	for w := 26; w <= 240; w += 7 {
		for h := 12; h <= 60; h += 3 {
			all := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), w, h, false), "\n")
			if strings.Contains(all, claudeAA) != strings.Contains(all, codexAA) {
				mixed++
				if mixed <= 3 {
					t.Errorf("%dx%d: 段によって見出しの形が違う (Claude=%v codex=%v)",
						w, h, strings.Contains(all, claudeAA), strings.Contains(all, codexAA))
				}
			}
		}
	}
	if mixed > 3 {
		t.Errorf("形が混ざったサイズが %d 通り", mixed)
	}
}

// 合体帯 (案 B) は「枠ラベル | CLI 名 | 枠ラベル」を同じ 3 行に置き、カードは自分の見出しを
// 描かない (二重に出ると帯にした意味が無く、行も節約できない)。
func TestRenderDashboardMergedBand(t *testing.T) {
	lines := RenderDashboard(dialTestSnap(), dialTestNow(), 225, 44, false)
	mid := bannerLines("codex")[1]
	var band string
	for _, ln := range lines {
		if strings.Contains(ln, mid) {
			band = ln
		}
	}
	if band == "" {
		t.Fatal("codex の合体帯が見つからない")
	}
	for _, want := range []string{bigLines("5H")[1], bigLines("7D")[1], "セッション", "weekly"} {
		if !strings.Contains(band, want) {
			t.Errorf("帯の中段に %q が無い: %q", want, band)
		}
	}
	// 枠ラベルの AA が出る行は「帯」だけ (カード側にも見出しが残っていたら別の行に出る)。
	// ⚠️ 単純な出現回数では数えない: 字形が同じ並びになる組み合わせがあり (実測 2026-09-01:
	// "78" の上段が "5H" の中段と同じ "▀▀▀█ █▀▀█")、盤の中央の数字を見出しと数えてしまう。
	bands := 0
	for _, ln := range lines {
		if !strings.Contains(ln, bigLines("5H")[1]) {
			continue
		}
		if slices.ContainsFunc([]rune(ln), isBrailleRune) {
			continue // 盤の中央の数字 (字形がたまたま一致した行)
		}
		if !strings.Contains(ln, mid) && !strings.Contains(ln, bannerLines("Claude Code")[1]) {
			t.Errorf("帯の外に枠ラベルの見出しが残っている: %q", ln)
		}
		bands++
	}
	if bands != 2 {
		t.Errorf("枠ラベルの見出しが出る行が %d 行 (段ごとに帯の中段 1 行 = 2 行のはず)", bands)
	}
	// ⚠️ AA の形だけ見ても足りない: カードが見出しを描いても、その高さでは AA に入らず
	// テキスト ("5h セッション") へ落ちるので AA の検査を素通りする (変異検証で実測 2026-09-01)。
	for _, ln := range lines {
		if strings.Contains(ln, "5h セッション") || strings.Contains(ln, "7d weekly") {
			t.Errorf("帯を採ったのにカードが自分の見出しを描いている: %q", ln)
		}
	}
}

// 帯の中で語が地続きにならない (種別語と CLI 名のあいだにカード間と同じ空きを取る)。
// ⚠️ 幅の合計が契約内でも「詰まって読めない」は起きる (幅テストでは検出できない形)。
func TestRenderDashboardBandKeepsGap(t *testing.T) {
	// ⚠️ 両方の CLI の帯を見る。codex の AA は短いので中央に余裕があり、詰まるのは AA が長い
	// Claude 側から (変異検証で実測 2026-09-01: codex の帯だけ見ていて空き 0 桁を見逃した)。
	mids := []string{bannerLines("codex")[1], bannerLines("Claude Code")[1]}
	for w := 26; w <= 260; w++ {
		for _, ln := range RenderDashboard(dialTestSnap(), dialTestNow(), w, 44, false) {
			if !slices.ContainsFunc(mids, func(m string) bool { return strings.Contains(ln, m) }) {
				continue
			}
			i := strings.Index(ln, "セッション")
			if i < 0 {
				t.Errorf("w=%d: 帯に種別が無い: %q", w, ln)
				continue
			}
			rest := ln[i+len("セッション"):]
			if gap := len(rest) - len(strings.TrimLeft(rest, " ")); gap < dialGap {
				t.Errorf("w=%d: 種別と CLI 名の間が %d 桁しかない: %q", w, gap, ln)
			}
		}
	}
}

// 逼迫している枠が 1 つだけあるとき、その枠を主役にする (ユーザー選定 2026-09-01)。
// ⚠️ 「常に非対称」にはしない: どちらを見るべきかの信号にならず、ただ大きさが揃っていない
// 画面になる。両方赤・両方平常なら対称のまま。
func TestDialHero(t *testing.T) {
	now := dialTestNow()
	card := func(pct int, mins int64, remain time.Duration) dialCard {
		w := Window{Label: "x", Percent: pct, ResetAt: now.Add(remain), WindowMins: mins}
		return dialCard{win: w, span: w.Span()}
	}
	over := card(95, 10080, 4900*time.Minute) // 消費 95% / 経過 51% = 超過 (赤)
	fine := card(62, 300, 108*time.Minute)    // 適正
	unknown := dialCard{win: Window{Label: "x", Percent: 99, ResetAt: now}, span: 0}

	for _, tc := range []struct {
		name  string
		cards []dialCard
		width int
		want  int
	}{
		{"片方だけ超過", []dialCard{fine, over}, 225, 1},
		{"逆順でも位置を返す", []dialCard{over, fine}, 225, 0},
		{"両方平常なら対称", []dialCard{fine, fine}, 225, -1},
		{"両方超過なら対称", []dialCard{over, over}, 225, -1},
		{"窓幅不明は候補にしない", []dialCard{unknown, fine}, 225, -1},
		{"狭い端末では作らない", []dialCard{fine, over}, dialHeroMinW - 1, -1},
		{"2 枚でなければ作らない", []dialCard{over}, 225, -1},
	} {
		if got := dialHero(tc.cards, now, tc.width); got != tc.want {
			t.Errorf("%s: dialHero = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// 主役が居る段では、脇役は盤をやめて数値の塊になる (盤の直径は高さで決まるので、幅を配り
// 直しても主役は大きくならない — 差が付くのは脇役が盤をやめる側)。
func TestRenderDashboardHeroCompactsTheOther(t *testing.T) {
	now := dialTestNow()
	snap := &Snapshot{Version: "2.1.216", Windows: []Window{
		{Label: "5h", Percent: 62, ResetAt: now.Add(108 * time.Minute), WindowMins: 300},
		{Label: "7d", Percent: 95, ResetAt: now.Add(4900 * time.Minute), WindowMins: 10080},
	}}
	const w = 225
	lines := RenderDashboard(snap, now, w, 40, false)
	left, right := 0, 0
	for _, ln := range lines {
		rs := []rune(ln)
		for i, r := range rs {
			if !isBrailleRune(r) {
				continue
			}
			if termwidth.Of(string(rs[:i])) < w/2 {
				left++
			} else {
				right++
			}
		}
	}
	if left != 0 {
		t.Errorf("脇役 (適正な 5h) に盤が残っている: %d セル", left)
	}
	if right == 0 {
		t.Error("主役 (超過した 7d) の盤が無い")
	}
	// 脇役でも数値は残る (盤をやめるだけで、情報を捨てるわけではない)
	if all := strings.Join(lines, "\n"); !strings.Contains(all, "62%") {
		t.Error("脇役の使用率が消えた")
	}
	// 脇役の塊は**縦の中央**にある。⚠️ 「盤が無い」だけを見ると、数値が最下段に貼り付いて
	// 上が丸ごと空白の状態 (壊れた画面に見える形) を素通りする — 変異検証で実測 2026-09-01。
	// ⚠️ 見出し帯 (罫線 + 3 行) は左半分にも枠ラベルを出すので、走査から外す。含めると塊が
	// 上に寄って見え、中央判定が常に外れる (実測 2026-09-01)。
	const headLines = 4
	first, last := -1, -1
	for i, ln := range lines[headLines:] {
		i += headLines
		rs := []rune(ln)
		half := ""
		for j, r := range rs {
			if termwidth.Of(string(rs[:j])) >= w/2 {
				break
			}
			half += string(r)
		}
		if strings.TrimSpace(half) == "" {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		t.Fatal("脇役の列が丸ごと空")
	}
	// カード領域の中心と塊の中心がおおむね一致すること
	area := (headLines + len(lines)) / 2
	if center := (first + last) / 2; center < area-3 || center > area+3 {
		t.Errorf("脇役の塊が縦中央に無い (中心 %d / カード領域の中心 %d)", center, area)
	}
}

// 平常時 (赤が無い) は対称のまま = 両方の枠に盤が出る。非対称そのものが信号なので、
// 平常時に非対称だと信号が意味を失う。
func TestRenderDashboardStaysSymmetricWhenCalm(t *testing.T) {
	lines := RenderDashboard(dialTestSnap(), dialTestNow(), 225, 40, false)
	const w = 225
	left, right := 0, 0
	for _, ln := range lines {
		rs := []rune(ln)
		for i, r := range rs {
			if !isBrailleRune(r) {
				continue
			}
			if termwidth.Of(string(rs[:i])) < w/2 {
				left++
			} else {
				right++
			}
		}
	}
	if left == 0 || right == 0 {
		t.Errorf("平常時なのに片側の盤が消えた (左 %d / 右 %d セル)", left, right)
	}
}
