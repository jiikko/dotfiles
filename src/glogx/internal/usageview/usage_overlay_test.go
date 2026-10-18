package usageview

import (
	"errors"
	"glogx/internal/app"
	"glogx/internal/term"
	"os"
	"path/filepath"
	"ratelimit/usage"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// overlayBoxTopRight は box を右上へ右揃えで重ね、覆った各行の表示幅が width ちょうどに
// 揃うこと。box 行より下のウィンドウ行は変わらないこと。
func TestOverlayBoxTopRightAligns(t *testing.T) {
	window := []string{"commit line one", "author line two", "date line three", "keep me"}
	box := []string{"┌ usage ─┐", "│ 5h ok │", "└────────┘"}
	width := 40
	got := OverlayBoxTopRight(window, box, width, false)

	for i, b := range box {
		if !strings.HasSuffix(got[i], b) {
			t.Errorf("行 %d が box 行で終わっていない: %q", i, got[i])
		}
		if w := term.DispWidth(got[i]); w != width {
			t.Errorf("行 %d の表示幅 = %d, want %d", i, w, width)
		}
	}
	if got[3] != "keep me" {
		t.Errorf("box より下の行を壊した: %q", got[3])
	}
}

// 覆う行の左側 (見えている部分) の色は保持される (取得中に上部行の色が抜けない回帰)。
func TestOverlayBoxTopRightKeepsLeftColor(t *testing.T) {
	colored := term.AnsiGreen + "green subject text here" + term.AnsiReset
	window := []string{colored}
	box := []string{"┌ usage ┐"}
	got := OverlayBoxTopRight(window, box, 40, true)
	if !strings.Contains(got[0], term.AnsiGreen) {
		t.Errorf("左側の色 (%q) が保持されていない: %q", term.AnsiGreen, got[0])
	}
	// 幅は width ちょうど、右端は box。
	if w := term.DispWidth(got[0]); w != 40 {
		t.Errorf("表示幅 = %d, want 40", w)
	}
	if !strings.HasSuffix(got[0], box[0]) {
		t.Errorf("右端が box で終わっていない: %q", got[0])
	}
}

// box が window より高くても (行数超過) パニックせず、収まる分だけ重ねる。
func TestOverlayBoxTopRightTallBox(t *testing.T) {
	window := []string{"only one row"}
	box := []string{"row0", "row1", "row2"}
	got := OverlayBoxTopRight(window, box, 20, false)
	if len(got) != 1 {
		t.Fatalf("行数が変わった: %d", len(got))
	}
	if w := term.DispWidth(got[0]); w != 20 {
		t.Errorf("表示幅 = %d, want 20", w)
	}
}

func TestOverlayBoxTopRightEmpty(t *testing.T) {
	if got := OverlayBoxTopRight(nil, []string{"x"}, 10, false); got != nil {
		t.Errorf("空ウィンドウで nil を返さない: %v", got)
	}
	// 空 box / 幅 0 は**ウィンドウをそのまま返す**。戻り値を捨てると「背景を丸ごと落とす」
	// 退行 (overlayBoxRight の早期 return を nil にする) が素通りする。
	window := []string{"a", "b"}
	if got := OverlayBoxTopRight(window, nil, 10, false); !slices.Equal(got, window) {
		t.Errorf("空 box でウィンドウが変わった: got %v want %v", got, window)
	}
	if got := OverlayBoxTopRight(window, []string{"x"}, 0, false); !slices.Equal(got, window) {
		t.Errorf("幅 0 でウィンドウが変わった: got %v want %v", got, window)
	}
}

// loading は「表示中 かつ 結果未着」のときだけ true。err 到着後も false になり (tick が
// 止まりスピナーが無限に回らない)、非表示中も false。browseModel を作らず型単体で検証する。
func TestUsageOverlayLoadingStates(t *testing.T) {
	cases := []struct {
		name string
		ov   UsageOverlay
		want bool
	}{
		{"表示中・結果未着", UsageOverlay{Visible: true}, true},
		{"表示中・snap 到着", UsageOverlay{Visible: true, Snap: &usage.Snapshot{}}, false},
		{"表示中・err 到着", UsageOverlay{Visible: true, Err: errors.New("boom")}, false},
		{"非表示・結果未着", UsageOverlay{Visible: false}, false},
	}
	for _, c := range cases {
		if got := c.ov.Loading(); got != c.want {
			t.Errorf("%s: loading()=%v, want %v", c.name, got, c.want)
		}
	}
}

// 取得失敗時の box は "取得失敗" を表示する (エラー描画パスの回帰ガード)。型単体で検証。
func TestUsageOverlayBoxLinesError(t *testing.T) {
	ov := UsageOverlay{Visible: true, Err: errors.New("boom")}
	box := ov.BoxLines(80, false, "|")
	if len(box) == 0 {
		t.Fatal("エラー時に box が空")
	}
	plain := term.StripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "取得失敗") {
		t.Errorf("エラー box に '取得失敗' が無い:\n%s", plain)
	}
	// エラー時も title は省略しない (ユーザー要望 2026-07-23)。
	if !strings.Contains(plain, "Claude Code") {
		t.Errorf("エラー box の title が省略されている ('Claude Code' が無い):\n%s", plain)
	}
}

// 不変条件: 一度取れた usage は定期リフレッシュの一時失敗で失わない。既に snap がある状態で
// 失敗結果が来たら last-good を保持し "取得失敗" へ落とさない (毎分の再取得が瞬断で転けても
// 右上表示がチラつかない)。
func TestUsageHandlePreservesLastGoodOnRefreshError(t *testing.T) {
	o := &UsageOverlay{Visible: true}
	good := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 10}}}
	o.Handle(UsageMsg{snap: good})
	// 定期リフレッシュが失敗
	o.Handle(UsageMsg{err: errors.New("boom")})
	if o.Snap != good {
		t.Error("リフレッシュ失敗で last-good スナップショットが消えた")
	}
	if o.Err != nil {
		t.Errorf("last-good があるのにエラーが表面化した: %v", o.Err)
	}
}

// 初回取得の失敗 (snap 未取得) はそのままエラー表示する。
func TestUsageHandleInitialErrorSurfaces(t *testing.T) {
	o := &UsageOverlay{Visible: true}
	o.Handle(UsageMsg{err: errors.New("boom")})
	if o.Err == nil {
		t.Error("初回取得失敗はエラー表示すべき")
	}
	if o.Snap != nil {
		t.Error("初回失敗で snap が nil でない")
	}
}

// リフレッシュ成功は last-good を新値へ置き換える (err はクリア)。
func TestUsageHandleRefreshSuccessReplaces(t *testing.T) {
	o := &UsageOverlay{Visible: true}
	v1 := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 10}}}
	v2 := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 42}}}
	o.Handle(UsageMsg{snap: v1})
	o.Handle(UsageMsg{snap: v2})
	if o.Snap != v2 {
		t.Error("リフレッシュ成功で新値へ置き換わっていない")
	}
	if o.Err != nil {
		t.Errorf("成功なのに err が残った: %v", o.Err)
	}
}

// fetch は single-flight: 走行中にもう 1 本発行しない。cancel が単一スロットのため、overlap
// させると先行 fetch の cancel を上書きで取りこぼし、quit 後も subprocess が app.FetchTimeout まで
// 残る (シナリオ: 起動時 fetch の完了前に U を 2 回 = 非表示 → stale 再表示で 2 本目)。
func TestUsageFetchCmdIsSingleFlight(t *testing.T) {
	o := &UsageOverlay{Visible: true}
	if o.FetchCmd(false) == nil {
		t.Fatal("最初の fetchCmd が nil (fetch が始まらない)")
	}
	// closure は実行しない (実 subprocess を起こさない) ので defer cancel() が走らない。
	// ctx タイマーを残さないよう、上書きされる前の cancel をテスト側で退避して呼ぶ
	firstCancel := o.cancel
	defer firstCancel()
	if o.FetchCmd(false) != nil {
		t.Fatal("走行中なのに 2 本目の fetchCmd が発行された (先行分の cancel を取りこぼす)")
	}
	// 結果が届けば (成否によらず) 次の fetch を発行できる
	o.Handle(UsageMsg{err: errors.New("boom")})
	if o.FetchCmd(false) == nil {
		t.Fatal("結果を受け取った後も fetch が発行できない (inFlight が降りていない)")
	}
	o.Stop() // 3 本目の ctx の後始末
}

// 初回失敗 → リフレッシュ成功 で回復する (err クリア + snap セット)。
func TestUsageHandleRecoversFromInitialError(t *testing.T) {
	o := &UsageOverlay{Visible: true}
	o.Handle(UsageMsg{err: errors.New("boom")}) // 初回失敗
	good := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 10}}}
	o.Handle(UsageMsg{snap: good}) // リフレッシュで回復
	if o.Snap != good || o.Err != nil {
		t.Errorf("初回失敗から回復していない: snap=%v err=%v", o.Snap, o.Err)
	}
}

// 成功時の usage box は自動更新を明示するフッター (usageFooter) を末尾に出す
// (ユーザー要望 2026-07-22。間隔は全プロセス共有のゲートの間隔)。
func TestUsageBoxLinesShowsAutoRefreshFooter(t *testing.T) {
	ov := UsageOverlay{
		Visible: true,
		Snap: &usage.Snapshot{Windows: []usage.Window{
			{Label: "5h", Percent: 20, ResetAt: time.Now().Add(4 * time.Hour)},
		}},
	}
	box := ov.BoxLines(80, false, "|")
	plain := term.StripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, usageFooter) {
		t.Errorf("usage box に自動更新フッターが無い:\n%s", plain)
	}
	// フッターは末尾付近 (データ行の後) に出る: 5h 行より後であること
	if strings.Index(plain, usageFooter) < strings.Index(plain, "5h") {
		t.Errorf("フッターがデータ行より前に出ている:\n%s", plain)
	}
	// 右寄せ: フッター文言は行の右端 (右罫線 │ の直前) に来る。左端 "│ " 直後には出さない。
	var footerLine string
	for _, l := range box {
		if strings.Contains(term.StripANSI(l), usageFooter) {
			footerLine = term.StripANSI(l)
			break
		}
	}
	if footerLine == "" {
		t.Fatal("フッター行が見つからない")
	}
	if !strings.Contains(footerLine, usageFooter+" │") {
		t.Errorf("フッターが右寄せされていない (右罫線に接していない):\n%q", footerLine)
	}
	if strings.Contains(footerLine, "│ "+usageFooter) {
		t.Errorf("フッターが左寄せのまま (左罫線直後に出ている):\n%q", footerLine)
	}
}

// Claude と codex の枠の間に区切り罫線が入る (ユーザー要望 2026-07-31)。単一出所なら罫線なし。
func TestUsageBoxLinesCodexDivider(t *testing.T) {
	o := UsageOverlay{Visible: true, Snap: &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(4 * time.Hour)},
		{Label: "7d", Percent: 29, ResetAt: time.Now().Add(50 * time.Hour)},
		{Label: "cx7d", Source: usage.SourceCodex, Percent: 69, ResetAt: time.Now().Add(120 * time.Hour)},
	}}}
	box := o.BoxLines(120, false, "")
	// 区切り行 = 側罫線の内側が ─ の連続だけの行。位置は 7d 行と cx7d 行の間。
	divider, idx7d, idxCx := -1, -1, -1
	for i, line := range box {
		content := strings.Trim(term.StripANSI(line), "│░▒▓█ ")
		switch {
		case content != "" && strings.Trim(content, "─") == "" && i > 0: // 上辺 (┌...┐) は除外
			divider = i
		case strings.Contains(line, "cx7d"):
			idxCx = i
		case strings.Contains(line, "7d"):
			idx7d = i
		}
	}
	if divider < 0 {
		t.Fatalf("区切り罫線が無い:\n%s", strings.Join(box, "\n"))
	}
	if idx7d >= divider || divider >= idxCx {
		t.Errorf("区切り罫線の位置が 7d と cx7d の間でない (7d=%d divider=%d cx=%d)", idx7d, divider, idxCx)
	}

	// codex 枠が無ければ罫線は出ない。
	o.Snap = &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(4 * time.Hour)},
	}}
	for i, line := range o.BoxLines(120, false, "") {
		content := strings.Trim(term.StripANSI(line), "│░▒▓█ ")
		if i > 0 && content != "" && strings.Trim(content, "─") == "" {
			t.Errorf("単一出所なのに区切り罫線がある (行 %d):\n%s", i, line)
		}
	}
}

// drainUsageFetch は fetchCmd の Cmd を走らせ、届く usageMsg を順に handle へ入れる (保存の Cmd も走らせる)。
func drainUsageFetch(t *testing.T, o *UsageOverlay, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("fetchCmd が nil")
	}
	msgs := []tea.Msg{cmd()}
	if b, ok := msgs[0].(tea.BatchMsg); ok {
		msgs = msgs[:0]
		for _, c := range b {
			msgs = append(msgs, c())
		}
	}
	for _, msg := range msgs {
		um, ok := msg.(UsageMsg)
		if !ok {
			t.Fatalf("usageMsg でない: %T", msg)
		}
		if c := o.Handle(um); c != nil {
			c()
		}
	}
}

// 片側 (claude) の一時失敗で取れていた枠を失わない (出所単位の last-good)。stub CLI で claude 失敗 + codex 成功を作る。
func TestUsageFetchCmdKeepsOtherSourceOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	stub := func(name, script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub("claude", "exit 1\n")
	stub("codex", `read _l1
printf '%s\n' '{"id":1,"result":{}}'
read _l2
read _l3
printf '%s\n' '{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":69,"windowDurationMins":10080,"resetsAt":1785903020},"secondary":null}}}'
`)
	t.Setenv("PATH", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // キャッシュ書き込みを隔離
	cachePath, err := usageCachePath()
	if err != nil {
		t.Fatal(err)
	}
	prev := &usage.Snapshot{Version: "9.9.9", Windows: []usage.Window{
		{Label: "5h", Percent: 4},
		{Label: "7d", Percent: 29},
		{Label: "cx7d", Source: usage.SourceCodex, Percent: 1},
	}}
	o := UsageOverlay{Visible: true, Snap: prev}
	drainUsageFetch(t, &o, o.FetchCmd(false))
	if o.Err != nil || o.StaleErr != nil || o.inFlight {
		t.Fatalf("err=%v staleErr=%v inFlight=%v", o.Err, o.StaleErr, o.inFlight)
	}
	if _, found := o.Snap.Find("5h"); !found {
		t.Errorf("claude 一時失敗で 5h 枠が消えた: %+v", o.Snap.Windows)
	}
	if w, _ := o.Snap.Find("cx7d"); w.Percent != 69 {
		t.Errorf("codex 枠が新値でない: %d, want 69", w.Percent)
	}
	if o.Snap.Version != "9.9.9" || o.Snap.ClaudeErr == "" {
		t.Errorf("Version の last-good / Claude の失敗理由: %q %q", o.Snap.Version, o.Snap.ClaudeErr)
	}
	// 表示用 snap は last-good で Claude 枠を補完するが、今回取得できたのは codex だけ。
	// キャッシュ契約を満たさないため、補完済み snap を保存してはならない。
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("claude 失敗 + codex 成功でキャッシュが作られた: err=%v", err)
	}
}

// usagePart は出所 src の取得の成功を作る。残りは実際の値に近い長さにする (5h は時間と分・weekly は日も出る。
// 箱の幅の比較が値の長さを見るので、値を短くすると幅の差が隠れる)。
func usagePart(src string, pct int) *usage.Part {
	now := app.TimeNow()
	if src == usage.SourceCodex {
		return &usage.Part{Source: src, Version: "0.144.6", Windows: []usage.Window{
			{Label: "cx7d", Source: src, Percent: pct, WindowMins: 10080, ResetAt: now.Add(6*24*time.Hour + 23*time.Hour + 59*time.Minute)}}}
	}
	return &usage.Part{Version: "2.1.216", Windows: []usage.Window{
		{Label: "5h", Percent: pct, WindowMins: 300, ResetAt: now.Add(4*time.Hour + 26*time.Minute)},
		{Label: "7d", Percent: pct, WindowMins: 10080, ResetAt: now.Add(5*24*time.Hour + 3*time.Hour)}}}
}

func usageLabels(s *usage.Snapshot) string {
	if s == nil {
		return "<nil>"
	}
	ls := make([]string, 0, len(s.Windows))
	for _, w := range s.Windows {
		ls = append(ls, w.Label)
	}
	return strings.Join(ls, ",")
}

// 先に届いた方をその場で出し、遅い方は場所取りの行 (「取得中...」) で待つ。届く前と後で箱の幅と
// 行の数は変わらない (場所取りは届いた後と同じ枠・列は最大の幅から)。周の終わりに、取れた両方をキャッシュへ保存する。
func TestUsageShowsEachSourceAsItArrives(t *testing.T) {
	plain, width := usageBoxPlain, linesWidth
	for _, first := range []string{usage.SourceCodex, ""} {
		second := usage.SourceCodex
		if first == usage.SourceCodex {
			second = ""
		}
		t.Run(usageSourceName(first)+" が先", func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("PATH", "")                                       // 外部コマンドを起こさない
			stubLookPath(t, map[string]string{"codex": "/fake/codex"}) // codex の入った環境 (codex の場所取りを置く)
			o := UsageOverlay{Visible: true}
			o.FetchCmd(false) // closure は走らせない (届く順をテストが決める)
			none := plain(&o) // 何も届いていない瞬間 (起動直後のグランス)
			o.Handle(UsageMsg{part: usagePart(first, 7)})
			if got := usageLabels(o.Snap); got != usageLabels((*usage.Snapshot)(nil).With(*usagePart(first, 7))) {
				t.Fatalf("先に届いた方が表示へ入らない: %s", got)
			}
			before := plain(&o)
			pending := 0
			for _, l := range before {
				if strings.Contains(l, "取得中...") {
					pending++
				}
			}
			if !o.Loading() {
				t.Error("片方を待っている間に場所取りのスピナーが止まる")
			}
			if spun := strings.Count(strings.Join(before, "\n"), "* 取得中..."); spun != pending {
				t.Errorf("場所取りの行にスピナーが付いていない (%d / %d):\n%s", spun, pending, strings.Join(before, "\n"))
			}
			if want := len(usage.PendingWindows(second, nil)); pending != want {
				t.Errorf("場所取りの行 = %d, want %d (まだの出所の枠の数):\n%s", pending, want, strings.Join(before, "\n"))
			}
			if c := o.Handle(UsageMsg{part: usagePart(second, 3)}); c != nil {
				c() // 周の終わりのキャッシュの保存
			}
			if got := usageLabels(o.Snap); got != "5h,7d,cx7d" {
				t.Errorf("届いた順に依らず Claude が先に並ばない: %s", got)
			}
			if o.inFlight || len(o.waiting()) != 0 || o.fetchedAt.IsZero() {
				t.Errorf("周が閉じていない: inFlight=%v waiting=%v", o.inFlight, o.waiting())
			}
			after := plain(&o)
			if strings.Contains(strings.Join(after, "\n"), "取得中") {
				t.Errorf("両方そろった後も「取得中」が残った:\n%s", strings.Join(after, "\n"))
			}
			if len(none) != len(after) || width(none) != width(after) {
				t.Errorf("何も届く前と後で箱の形が変わった: %d 行 x %d 桁 → %d 行 x %d 桁\n%s\n---\n%s",
					len(none), width(none), len(after), width(after), strings.Join(none, "\n"), strings.Join(after, "\n"))
			}
			if len(before) != len(after) || width(before) != width(after) {
				t.Errorf("届く前と後で箱の形が変わった: %d 行 x %d 桁 → %d 行 x %d 桁\n%s\n---\n%s",
					len(before), width(before), len(after), width(after), strings.Join(before, "\n"), strings.Join(after, "\n"))
			}
			path, _ := usageCachePath()
			if snap, ok := loadUsageCache(path, time.Now()); !ok || usageLabels(snap) != "5h,7d,cx7d" {
				t.Errorf("周の終わりに両方を保存していない: ok=%v %s", ok, usageLabels(snap))
			}
		})
	}
}

func usageBoxPlain(o *UsageOverlay) []string {
	lines := o.BoxLines(120, false, "*")
	for i, l := range lines {
		lines[i] = term.StripANSI(l)
	}
	return lines
}

func linesWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, term.DispWidth(l))
	}
	return w
}

// 何も届いていないうちに Claude が先に失敗したら、codex を待つ間も Claude の失敗の注記を出し、codex が届いた後と同じ形を取る
// (注記が無いと Claude の段が黙って消え、codex が届いた瞬間に注記の行が増える)。
func TestUsageClaudeFailureFirstKeepsNoteWhileWaiting(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", "")
	stubLookPath(t, map[string]string{"codex": "/fake/codex"})
	o := UsageOverlay{Visible: true}
	o.FetchCmd(false)
	defer o.Stop()
	o.Handle(UsageMsg{part: &usage.Part{Err: errors.New("claude /usage 実行失敗")}})
	waiting := usageBoxPlain(&o)
	if !strings.Contains(strings.Join(waiting, "\n"), "Claude Code の取得に失敗") {
		t.Errorf("codex を待つ間に Claude の失敗の注記が無い:\n%s", strings.Join(waiting, "\n"))
	}
	o.Handle(UsageMsg{part: usagePart(usage.SourceCodex, 7)})
	after := usageBoxPlain(&o)
	if len(waiting) != len(after) || linesWidth(waiting) != linesWidth(after) {
		t.Errorf("codex が届く前と後で箱の形が変わった: %d 行 x %d 桁 → %d 行 x %d 桁\n%s\n---\n%s",
			len(waiting), linesWidth(waiting), len(after), linesWidth(after), strings.Join(waiting, "\n"), strings.Join(after, "\n"))
	}
}

// codex の入っていない環境 (前回の形にも codex の枠が無い) では、最初の取得で codex の場所取りを置かない
// (置くと起動のたびに codex の行と「+ codex」が出て、codex の失敗が届くと消える)。前回の形に codex の枠があれば置く。
func TestUsageNoCodexPendingWithoutCodex(t *testing.T) {
	for _, tc := range []struct {
		name        string
		installed   bool
		cachedCodex bool // 前回のキャッシュ (TTL 切れ) に codex の枠がある
		wantCodex   bool
	}{{"codex なし", false, false, false}, {"codex あり", true, false, true}, {"codex なし・前回に codex の枠", false, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("PATH", "")
			if tc.cachedCodex {
				path, err := usageCachePath()
				if err != nil {
					t.Fatal(err)
				}
				old := (*usage.Snapshot)(nil).With(*usagePart("", 1)).With(*usagePart(usage.SourceCodex, 1))
				if err := saveUsageCache(path, old, time.Now().Add(-24*time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			avail := map[string]string{}
			if tc.installed {
				avail["codex"] = "/fake/codex"
			}
			stubLookPath(t, avail)
			o := UsageOverlay{Visible: true}
			o.FetchCmd(false)
			defer o.Stop()
			none := usageBoxPlain(&o)
			if got := strings.Contains(strings.Join(none, "\n"), "cx7d"); got != tc.wantCodex {
				t.Errorf("codex の場所取り = %v, want %v:\n%s", got, tc.wantCodex, strings.Join(none, "\n"))
			}
			if tc.wantCodex {
				return
			}
			o.Handle(UsageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("exec: codex: not found")}})
			o.Handle(UsageMsg{part: usagePart("", 3)})
			after := usageBoxPlain(&o)
			if len(none) != len(after) || linesWidth(none) != linesWidth(after) {
				t.Errorf("届く前と後で箱の形が変わった: %d 行 x %d 桁 → %d 行 x %d 桁\n%s\n---\n%s",
					len(none), linesWidth(none), len(after), linesWidth(after), strings.Join(none, "\n"), strings.Join(after, "\n"))
			}
		})
	}
}

// 最初の取得の codex の場所取りは、前回のキャッシュ (古くてよい) の枠の構成を写す (2 枠のプランで行の数を変えない)。
func TestUsagePendingUsesCachedCodexShape(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := usageCachePath()
	if err != nil {
		t.Fatal(err)
	}
	old := (*usage.Snapshot)(nil).With(*usagePart("", 1)).With(usage.Part{Source: usage.SourceCodex, Windows: []usage.Window{
		{Label: "cx5h", Source: usage.SourceCodex, WindowMins: 300}, {Label: "cx7d", Source: usage.SourceCodex, WindowMins: 10080}}})
	if err := saveUsageCache(path, old, time.Now().Add(-24*time.Hour)); err != nil { // TTL を過ぎた前回
		t.Fatal(err)
	}
	o := UsageOverlay{Visible: true}
	o.FetchCmd(false)
	defer o.Stop()
	o.Handle(UsageMsg{part: usagePart("", 3)})
	if got := usageLabels(o.View("")); got != "5h,7d,cx5h,cx7d" {
		t.Errorf("codex の場所取りが前回の形でない: %s", got)
	}
}

// 何も取れていないうちの Claude の失敗は、空の表示を出さずに codex を待つ。codex が取れたら Claude の失敗の注記つきで出す。
func TestUsageClaudeFailureFirstWaitsForCodex(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	o := UsageOverlay{Visible: true}
	o.FetchCmd(false)
	o.Handle(UsageMsg{part: &usage.Part{Err: errors.New("claude /usage 実行失敗: exit status 127")}})
	if o.Snap != nil || o.Err != nil || !o.Loading() {
		t.Fatalf("Claude の失敗だけで表示が変わった: snap=%v err=%v", usageLabels(o.Snap), o.Err)
	}
	o.Handle(UsageMsg{part: usagePart(usage.SourceCodex, 7)})
	if usageLabels(o.Snap) != "cx7d" || !strings.Contains(FetchNote(o.Snap, o.StaleErr), "Claude Code の取得に失敗") {
		t.Errorf("codex だけの表示 / Claude の注記: %s %q", usageLabels(o.Snap), FetchNote(o.Snap, o.StaleErr))
	}
	path, _ := usageCachePath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Claude 枠の無い周でキャッシュを書いた: %v", err)
	}
}

// 両方失敗した周は、途中で入れた失敗の注記ごと周の始まりの表示へ戻し、理由を Claude → codex の順で staleErr に置く。
// 何も無いところからの全滅は「取得失敗」。
func TestUsageBothFailRound(t *testing.T) {
	base := &usage.Snapshot{Windows: []usage.Window{{Label: "5h"}, {Label: "cx7d", Source: usage.SourceCodex}}}
	o := UsageOverlay{Visible: true, Snap: base, fetchedAt: app.TimeNow()}
	o.FetchCmd(false)
	o.Handle(UsageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("codex 起動失敗")}})
	o.Handle(UsageMsg{part: &usage.Part{Err: errors.New("claude 失敗")}})
	if o.Snap != base || o.Err != nil || o.inFlight {
		t.Errorf("全滅で周の始まりの表示へ戻らない: snap=%p base=%p err=%v", o.Snap, base, o.Err)
	}
	if o.StaleErr == nil || o.StaleErr.Error() != "claude 失敗\ncodex 起動失敗" {
		t.Errorf("staleErr = %v", o.StaleErr)
	}

	o = UsageOverlay{Visible: true}
	o.FetchCmd(false)
	o.Handle(UsageMsg{part: &usage.Part{Err: errors.New("a")}})
	o.Handle(UsageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("b")}})
	if o.Snap != nil || o.Err == nil || o.Loading() {
		t.Errorf("初回の全滅が「取得失敗」にならない: snap=%v err=%v", usageLabels(o.Snap), o.Err)
	}
}

// 初回の全滅の後、次の周で 1 本でも取れたらその場で「取得失敗」から回復する。
func TestUsageRecoversFromInitialErrorViaParts(t *testing.T) {
	o := UsageOverlay{Visible: true}
	o.FetchCmd(false)
	o.Handle(UsageMsg{part: &usage.Part{Err: errors.New("a")}})
	o.Handle(UsageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("b")}})
	o.FetchCmd(false)
	defer o.Stop()
	o.Handle(UsageMsg{part: usagePart(usage.SourceCodex, 7)})
	if o.Err != nil {
		t.Fatalf("codex が取れたのに「取得失敗」のまま: %v", o.Err)
	}
	if box := strings.Join(o.BoxLines(80, false, "*"), "\n"); strings.Contains(box, "取得失敗") {
		t.Errorf("箱が「取得失敗」のまま:\n%s", box)
	}
}

// 全滅した周の注記 (前回の値を表示中) は、次の周で先に届いた方では消さず、周の終わりに消す
// (まだ届いていない出所の枠は前回の値のまま)。
func TestUsageStaleNoteKeptUntilRoundEnds(t *testing.T) {
	base := (*usage.Snapshot)(nil).With(*usagePart("", 1)).With(*usagePart(usage.SourceCodex, 1))
	o := UsageOverlay{Visible: true, Snap: base, fetchedAt: app.TimeNow(), StaleErr: errors.New("前の周の全滅")}
	o.FetchCmd(false)
	o.Handle(UsageMsg{part: usagePart(usage.SourceCodex, 7)})
	if o.StaleErr == nil {
		t.Error("Claude の前回の値を出したまま注記を消した")
	}
	o.Handle(UsageMsg{part: usagePart("", 7)})
	if o.StaleErr != nil {
		t.Errorf("両方取れた周の終わりに注記が消えない: %v", o.StaleErr)
	}
}

// キャッシュへは今回の周で取れた分だけを書く: codex が失敗した周に、前回の codex 枠を混ぜて TTL を延ばさない。
func TestUsageCacheExcludesLastGoodOfFailedSource(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", "") // codex が無い環境として読む (codex 欠損のキャッシュを miss にしない。usage_cache.go)
	base := (*usage.Snapshot)(nil).With(*usagePart("", 1)).With(*usagePart(usage.SourceCodex, 1))
	o := UsageOverlay{Visible: true, Snap: base, fetchedAt: app.TimeNow()}
	o.FetchCmd(false)
	o.Handle(UsageMsg{part: usagePart("", 5)})
	c := o.Handle(UsageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("codex 起動失敗")}})
	if c == nil {
		t.Fatal("Claude が取れた周なのに保存の Cmd が無い")
	}
	c()
	path, _ := usageCachePath()
	snap, ok := loadUsageCache(path, time.Now())
	if !ok || usageLabels(snap) != "5h,7d" {
		t.Errorf("キャッシュ = ok:%v %s, want 今回の Claude 枠だけ", ok, usageLabels(snap))
	}
	if w, _ := o.Snap.Find("cx7d"); w.Percent != 1 {
		t.Errorf("表示からは前回の codex 枠を消さない: %+v", o.Snap.Windows)
	}
}

// 定期リフレッシュは静かに差し替える: 片方を待つ間も「取得中」を出さない。表示に codex の無い環境
// (codex 未導入) でも、毎分「codex 取得中」を点滅させない。
func TestUsageRefreshDoesNotShowWaiting(t *testing.T) {
	base := (*usage.Snapshot)(nil).With(*usagePart("", 1))
	o := UsageOverlay{Visible: true, Snap: base, fetchedAt: app.TimeNow()}
	o.FetchCmd(false)
	defer o.Stop()
	o.Handle(UsageMsg{part: usagePart("", 9)})
	if o.Loading() || len(o.waiting()) != 0 {
		t.Errorf("リフレッシュで「取得中」を出した: %v", o.waiting())
	}
	if w, _ := o.Snap.Find("5h"); w.Percent != 9 {
		t.Errorf("届いた Claude が表示へ入らない: %d", w.Percent)
	}
}

// 箱のタイトルは codex 側もバージョンを添える (Claude 側と対。ユーザー要望 2026-08-16)。
// 取得できていないとき (未導入 / --version 失敗) は名前だけの従来表記へ落ちる。
func TestUsageBoxLinesTitleShowsCodexVersion(t *testing.T) {
	snap := &usage.Snapshot{
		Version:      "2.1.216",
		CodexVersion: "0.144.6",
		Windows: []usage.Window{
			{Label: "5h", Percent: 20, ResetAt: time.Now().Add(4 * time.Hour)},
			{Label: "cx7d", Source: usage.SourceCodex, Percent: 69, ResetAt: time.Now().Add(48 * time.Hour)},
		},
	}
	ov := UsageOverlay{Visible: true, Snap: snap}
	plain := term.StripANSI(strings.Join(ov.BoxLines(120, false, ""), "\n"))
	if !strings.Contains(plain, "Claude Code v2.1.216 + codex v0.144.6 · usage") {
		t.Errorf("タイトルに codex バージョンが無い:\n%s", plain)
	}
	snap.CodexVersion = ""
	plain = term.StripANSI(strings.Join(ov.BoxLines(120, false, ""), "\n"))
	if !strings.Contains(plain, "Claude Code v2.1.216 + codex · usage") {
		t.Errorf("codex バージョン欠損時に名前だけの表記へ落ちない:\n%s", plain)
	}
}
