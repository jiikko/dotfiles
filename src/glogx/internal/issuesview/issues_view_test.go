package issuesview

import (
	"errors"
	"fmt"
	"glogx/internal/app"
	"glogx/internal/extcmd"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"glogx/issues"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/anim"
	"github.com/jiikko/dotfiles/src/tuikit/editor"
)

// atProgress は演出の進みを p (0..1) に固定した viewer を返す (壁時計を巻き戻して作る)。
func atProgress(v *IssuesView, p float64) *IssuesView {
	v.AnimStart = app.TimeNow().Add(-time.Duration(float64(issuesAnimDuration) * p))
	return v
}

func TestIssuesViewToggleRescansKeepingLastGood(t *testing.T) {
	v := newTestIssuesView()
	if cmd := v.Toggle("/repo"); cmd == nil {
		t.Fatal("初回の toggle でスキャンの Cmd が返らない")
	}
	if !v.Visible() || !v.Loading() {
		t.Fatalf("表示・スキャン中になっていない: shown=%v scanning=%v", v.Visible(), v.Loading())
	}
	if cmd := v.ScanCmd("/repo"); cmd != nil {
		t.Fatal("スキャン中に 2 本目の探索を発行した (single-flight が効いていない)")
	}
	v.Receive(IssuesScanMsg{Dirs: []string{"/repo/issues"}, Issues: issuesviewtest.SampleIssues()})
	if v.Loading() {
		t.Fatal("receive 後もスキャン中のまま")
	}
	v.Toggle("/repo") // 閉じる
	if v.Visible() {
		t.Fatal("2 回目の toggle で閉じない")
	}
	// 🚨 再表示では取り直す: 「初回だけ」にすると N (次の番号) が古い最大番号 + 1 を返し続け、
	// 番号の再利用を招く。ただし取り直し中も前回の結果を出したままにする (スピナーで瞬かない)。
	if cmd := v.Toggle("/repo"); cmd == nil {
		t.Fatal("再表示で取り直していない (N が古い番号を返し続ける)")
	}
	v.FinishAnim() // 開く演出は別テストで見る (ここは取り直し中の中身を見たい)
	out := strings.Join(v.Lines(renderOpts(10)), "\n")
	if strings.Contains(out, "探しています") || !strings.Contains(out, "030") {
		t.Fatalf("取り直し中に前回の結果が消えた:\n%s", out)
	}
}

func TestIssuesViewRescanReanchorsTabAndCursor(t *testing.T) {
	// 再スキャンは新しい *Issue を作る。位置を安定キー (タブ名・パス) で引き直さないと、
	// カーソルが別の issue へ滑り、タブが別カテゴリを指す (tabs は件数降順なので並びが変わる)。
	first := []*issues.Issue{
		issuesviewtest.FakeIssue("003", "feat", "c", issues.StatusOpen),
		issuesviewtest.FakeIssue("002", "feat", "b", issues.StatusOpen),
		issuesviewtest.FakeIssue("001", "feat", "a", issues.StatusOpen),
		issuesviewtest.FakeIssue("012", "refactor", "y", issues.StatusOpen),
		issuesviewtest.FakeIssue("011", "refactor", "x", issues.StatusOpen),
	}
	v := loadedView(first...)
	v.HandleKey("tab", vp(10)) // human (All の右に常設。0 件でも席がある)
	v.HandleKey("tab", vp(10)) // feat (3 件で先頭)
	v.HandleKey("tab", vp(10)) // refactor
	if got := v.currentTab(); got != "refactor" {
		t.Fatalf("前提が崩れた: 選択タブが refactor でない (%q)", got)
	}
	v.HandleKey("j", vp(10)) // 2 行目 (011)
	want := v.current().Path

	// refactor が 4 件になり、タブの並びが refactor → feat へ入れ替わる
	second := append([]*issues.Issue{
		issuesviewtest.FakeIssue("014", "refactor", "w", issues.StatusOpen),
		issuesviewtest.FakeIssue("013", "refactor", "v", issues.StatusOpen),
	}, first...)
	v.Receive(IssuesScanMsg{Dirs: []string{"/repo/issues"}, Issues: second})

	if got := v.currentTab(); got != "refactor" {
		t.Fatalf("再スキャンで選択タブが別カテゴリへ滑った: %q", got)
	}
	if got := v.current().Path; got != want {
		t.Fatalf("再スキャンでカーソルが別の issue へ滑った: want %q got %q", want, got)
	}
}

// カテゴリ移動は左右対称に効く。右は tui.go が ctrl+f を "right" へ正規化するので、viewer が
// 自分で持つのは左の ctrl+b だけ (ユーザー要望 2026-07-31)。右だけ効いて左が無い状態は
// 「Tab で行き過ぎたら 1 周するしかない」体験になる。
func TestIssuesViewTabMovesBothDirections(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	v.HandleKey("tab", vp(10)) // All → human (常設タブ)
	v.HandleKey("tab", vp(10)) // human → feat
	if got := v.currentTab(); got != "feat" {
		t.Fatalf("前提が崩れた: %q", got)
	}
	v.HandleKey("ctrl+b", vp(10)) // feat → human
	v.HandleKey("ctrl+b", vp(10)) // human → All
	if got := v.currentTab(); got != "" {
		t.Fatalf("ctrl+b で左へ戻らない: %q", got)
	}
	// tui.go の正規化を経た右移動 (ctrl+f → "right") と同じ経路も対称に効く
	v.HandleKey("right", vp(10)) // All → human
	if got := v.currentTab(); got != issues.HumanTab {
		t.Fatalf("right で右へ動かない: %q", got)
	}
	// 端で止まらず巡回する (moveTab の契約) 方向にも ctrl+b が乗る。
	// 🚨 All の左には疑似カテゴリ [next] が居るので、そこを 1 段挟んでから末尾へ回り込む
	v.HandleKey("ctrl+b", vp(10)) // All
	v.HandleKey("ctrl+b", vp(10)) // [next] (All の左)
	if got := v.currentTab(); got != tabNextName {
		t.Fatalf("All の左が [next] になっていない: %q", got)
	}
	v.HandleKey("ctrl+b", vp(10)) // 末尾へ回り込む
	if got := v.currentTab(); got != v.tabs[len(v.tabs)-1].Name {
		t.Fatalf("ctrl+b が末尾へ巡回しない: %q", got)
	}
}

func TestIssuesViewRescanRebindsOpenBody(t *testing.T) {
	// 本文モードで開いている Issue を繋ぎ直さないと、ヘッダーの状態・進捗が編集前の値のまま
	// 固まり、v.all から外れたポインタを掴み続ける。
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "001-feat-x.md"))
	landBody(t, v, 10)

	fresh := &issues.Issue{
		Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat",
		Status: issues.StatusPending,
	}
	v.Receive(IssuesScanMsg{Dirs: []string{dir}, Issues: []*issues.Issue{fresh}})
	if v.Open != fresh {
		t.Fatal("再スキャン後も破棄済みの Issue ポインタを掴んでいる")
	}
	// 🚨 本文ヘッダーそのもの (bodyHeadLines) を見る。画面全体 (lines) で "pending" を探すと、
	// 引き出しが着地前 (幅 0) で本文が描かれていなくても、一覧の空表示の案内
	// 「(a: pending も表示)」に一致して緑になった (issue 667)
	if head := strings.Join(v.bodyHeadLines(80, false), "\n"); !strings.Contains(head, "pending") {
		t.Fatalf("本文ヘッダーの状態が古いまま:\n%s", head)
	}
}

func TestIssuesViewTabsAndDoneFilter(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	// 既定は open だけ: open 2 行 (pending / done は伏せる)
	if len(v.Rows) != 2 {
		t.Fatalf("既定の行数が違う: %d", len(v.Rows))
	}
	// タブは human 0 (常設) / feat 2 / refactor 2 / (bug 1 は other へ)
	if len(v.tabs) != 4 || v.tabs[0].Name != issues.HumanTab || v.tabs[1].Name != "feat" {
		t.Fatalf("タブの組み立てが違う: %+v", v.tabs)
	}
	v.HandleKey("a", vp(10)) // 1 段目: + pending
	if len(v.Rows) != 3 {
		t.Fatalf("a 1 回で pending を含めていない: %d", len(v.Rows))
	}
	v.HandleKey("a", vp(10)) // 2 段目: + done
	if len(v.Rows) != 5 {
		t.Fatalf("a 2 回で done を含めていない: %d", len(v.Rows))
	}
	v.HandleKey("a", vp(10)) // 3 段目で既定へ戻る
	if len(v.Rows) != 2 {
		t.Fatalf("a 3 回で open のみへ戻らない: %d", len(v.Rows))
	}
	v.HandleKey("a", vp(10))
	v.HandleKey("a", vp(10)) // 以降のタブ検証は全件表示で行う
	// Tab 巡回: All → human → feat → refactor → other → [next] → All
	// (human は All の右に固定、[next] は All の左に固定なので右回りでは All の 1 つ手前)
	names := []string{issues.HumanTab, "feat", "refactor", "other", tabNextName, ""}
	for _, want := range names {
		v.HandleKey("tab", vp(10))
		if got := v.currentTab(); got != want {
			t.Fatalf("タブ巡回が想定と違う: want %q got %q", want, got)
		}
	}
	// feat タブでは feat の 2 件だけ (All → human → feat)
	v.HandleKey("tab", vp(10))
	v.HandleKey("tab", vp(10))
	if len(v.Rows) != 2 {
		t.Fatalf("feat タブの行数が違う: %d", len(v.Rows))
	}
	for _, iss := range v.Rows {
		if iss.Category != "feat" {
			t.Fatalf("feat タブに %s が混ざった", iss.Rel)
		}
	}
}

// 画面幅よりカテゴリが多いとき、選択中のタブが必ず画面に出る (横スクロール。ユーザー要望
// 2026-08-01)。🚨 幅で切って捨てるだけだと、端のカテゴリを選んでも画面に現れず「選んだはずの
// タブがどこにも無い」状態になる (Tab で右へ進んでいるのに画面が動かない)。
func TestIssuesViewTabLineFollowsSelection(t *testing.T) {
	cats := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	list := make([]*issues.Issue, 0, len(cats)*2)
	num := 200
	for _, c := range cats {
		for range 2 { // TabMinCount 未満だと other へ寄せられてタブにならない
			num--
			list = append(list, issuesviewtest.FakeIssue(fmt.Sprintf("%03d", num), c, "x", issues.StatusOpen))
		}
	}
	v := loadedView(list...)
	const width = 40 // 全チップ (8 カテゴリ + All) は到底収まらない幅
	if len(v.tabs) < len(cats) {
		t.Fatalf("前提が崩れた: タブが %d 個しかない", len(v.tabs))
	}

	for i := range len(v.tabs) + 1 { // 0 = All、以降は各カテゴリ
		v.tabIdx = i
		v.Refresh()
		line := v.tabLine(IssuesRenderOpts{Width: width})
		if w := term.DispWidth(line); w > width {
			t.Fatalf("tabIdx=%d でタブ行が幅 %d を超えた (w=%d): %q", i, width, w, line)
		}
		want := "[All " + strconv.Itoa(v.allCount) + "]"
		if i > 0 {
			want = "[" + v.tabs[i-1].Name + " " + strconv.Itoa(v.tabCount[i-1]) + "]"
		}
		if !strings.Contains(line, want) {
			t.Fatalf("選択中のタブ %q が画面に出ていない: %q", want, line)
		}
	}

	// 端が隠れている側にだけ印を出す (その先にタブがあると分かるように)
	v.tabIdx = 0
	v.Refresh()
	if head := v.tabLine(IssuesRenderOpts{Width: width}); !strings.Contains(head, tabScrollRight) ||
		strings.Contains(head, tabScrollLeft) {
		t.Fatalf("先頭では右にだけ続きの印が要る: %q", head)
	}
	v.tabIdx = len(v.tabs)
	v.Refresh()
	if tail := v.tabLine(IssuesRenderOpts{Width: width}); !strings.Contains(tail, tabScrollLeft) ||
		strings.Contains(tail, tabScrollRight) {
		t.Fatalf("末尾では左にだけ続きの印が要る: %q", tail)
	}
}

// 全部収まる幅では横スクロールの印を出さない (従来どおりの見た目)。
func TestIssuesViewTabLineNoMarksWhenAllFit(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	line := v.tabLine(IssuesRenderOpts{Width: 120})
	if strings.Contains(line, tabScrollLeft) || strings.Contains(line, tabScrollRight) {
		t.Fatalf("収まっているのに続きの印が出た: %q", line)
	}
}

// shift+↑/↓ で範囲を選び、y / p / Y がその範囲へ効く (ユーザー要望 2026-08-01)。
func TestIssuesViewMultiSelectYank(t *testing.T) {
	copied := testenv.StubClipboard(t)

	v := loadedView(issuesviewtest.SampleIssues()...)
	v.Filter = issues.FilterAll
	v.Refresh() // 5 件すべてを対象にする
	v.Cursor = 2
	v.Marked, v.MarkAt = true, 0 // 0..2 の 3 行を選んだ状態にする

	v.HandleKey("y", vp(10))
	want := v.Rows[0].Path + "\n" + v.Rows[1].Path + "\n" + v.Rows[2].Path
	if *copied != want {
		t.Fatalf("選択範囲のパスがコピーされていない:\n got=%q\nwant=%q", *copied, want)
	}
	if text, ok := testenv.NoticeOKOf(v.TakeNotice()); !ok || !strings.Contains(text, "3 件のパスをコピーしました") {
		t.Fatalf("複数コピーの通知が想定と違う: %q", text)
	}
	// 🚨 通知に改行を含めない (トーストは 1 行。含めると枠が壊れる)
	v.HandleKey("Y", vp(10))
	if text, _ := testenv.NoticeOKOf(v.TakeNotice()); strings.Contains(text, "\n") {
		t.Fatalf("通知に改行が入った: %q", text)
	}
	v.HandleKey("p", vp(10))
	if *copied != v.Rows[0].Number+"\n"+v.Rows[1].Number+"\n"+v.Rows[2].Number {
		t.Fatalf("選択範囲の番号がコピーされていない: %q", *copied)
	}
	// 単数のときの文言は変えない (複数選択を足したせいで普段の見た目が変わらないように)
	v.ClearMark()
	v.HandleKey("y", vp(10))
	if text, _ := testenv.NoticeOKOf(v.TakeNotice()); !strings.Contains(text, "パスをコピーしました: ") ||
		strings.Contains(text, "件の") {
		t.Fatalf("選択なしの通知が複数形になった: %q", text)
	}
}

// shift+↑/↓ の伸張と、選択が畳まれる経路。畳み忘れると「見えていない行がコピー対象」になる。
func TestIssuesViewMultiSelectExtendAndClear(t *testing.T) {
	newView := func() *IssuesView {
		v := loadedView(issuesviewtest.SampleIssues()...)
		v.Filter = issues.FilterAll
		v.Refresh()
		v.Cursor = 2
		v.HandleKey("shift+up", vp(10)) // 1..2 を選択 (カーソルは 1 へ)
		return v
	}
	v := newView()
	if lo, hi, ok := v.Selection(); !ok || lo != 1 || hi != 2 || v.Cursor != 1 {
		t.Fatalf("shift+up の範囲が違う: lo=%d hi=%d ok=%v cursor=%d", lo, hi, ok, v.Cursor)
	}
	v.HandleKey("shift+down", vp(10)) // 錨 (2) へ戻る = 1 行
	if lo, hi, _ := v.Selection(); lo != 2 || hi != 2 {
		t.Fatalf("shift+down で錨へ戻らない: lo=%d hi=%d", lo, hi)
	}
	v.HandleKey("shift+down", vp(10)) // 錨より下へ = 2..3
	if lo, hi, _ := v.Selection(); lo != 2 || hi != 3 {
		t.Fatalf("錨の下側へ伸ばせない: lo=%d hi=%d", lo, hi)
	}
	// 矢印と j/k の 2 系統あるので伸張も両方から効く (ユーザー要望 2026-08-01)。
	// 🚨 矢印だけだと、shift+矢印を通さない端末・tmux 設定で機能ごと沈黙する。
	for _, tc := range []struct {
		name           string
		arrow, vimKey  string
		wantLo, wantHi int
	}{
		{"下へ伸ばす", "shift+down", "J", 2, 4},
		{"上へ縮める", "shift+up", "K", 2, 3},
	} {
		byArrow := newView()
		byArrow.HandleKey("shift+down", vp(10)) // 2..3 まで揃えてから
		byArrow.HandleKey(tc.arrow, vp(10))
		byVim := newView()
		byVim.HandleKey("J", vp(10))
		byVim.HandleKey(tc.vimKey, vp(10))
		aLo, aHi, _ := byArrow.Selection()
		vLo, vHi, _ := byVim.Selection()
		if aLo != vLo || aHi != vHi {
			t.Fatalf("%s: 矢印と %s で範囲が違う (arrow=%d..%d vim=%d..%d)",
				tc.name, tc.vimKey, aLo, aHi, vLo, vHi)
		}
	}

	for _, tc := range []struct {
		name string
		do   func(v *IssuesView)
	}{
		{"素の移動 (j)", func(v *IssuesView) { v.HandleKey("j", vp(10)) }},
		{"端へジャンプ (G)", func(v *IssuesView) { v.HandleKey("G", vp(10)) }},
		{"カテゴリ切替 (Tab)", func(v *IssuesView) { v.HandleKey("tab", vp(10)) }},
		{"状態フィルタ (a)", func(v *IssuesView) { v.HandleKey("a", vp(10)) }},
		{"本文を開く (Enter)", func(v *IssuesView) { v.HandleKey("enter", vp(10)) }},
		{"解除 (Esc)", func(v *IssuesView) { v.HandleKey("esc", vp(10)) }},
	} {
		v := newView()
		tc.do(v)
		if _, _, ok := v.Selection(); ok {
			t.Fatalf("%s で選択が畳まれていない", tc.name)
		}
	}

	// Esc は選択の解除が先で、viewer は閉じない (閉じると解除の手段が無い状態から再開する)
	v = newView()
	v.HandleKey("esc", vp(10))
	if !v.Visible() {
		t.Fatal("Esc 1 回で viewer まで閉じた (選択の解除が先のはず)")
	}
	// 選択が無い状態の Esc は glogx ごと終了の信号 (ユーザー要望 2026-08-06。viewer は
	// 開いたまま = 再開記憶に残る)
	v.HandleKey("esc", vp(10))
	if !v.TakeWantQuit() {
		t.Fatal("選択が無い状態の Esc が終了の信号を立てない")
	}
	if !v.Visible() {
		t.Fatal("終了の信号で viewer を閉じた (開いたまま終了して再開記憶に残すはず)")
	}
}

// 本文の左にソース (.md) の行番号を出す (ユーザー要望 2026-08-01)。🚨 溝は整形前に幅から
// 引く: 後付けすると本文が枠を突き破る。
func TestIssuesViewBodyShowsSrcLineNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "042-feat-x.md")
	src := "# 042 feat: 行番号\n\n段落。\n\n```go\na := 1\n```\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "042-feat-x.md"))
	v.HandleKey("enter", vp(20))
	v.Drawer.Finish()
	const width = 60
	out := v.Lines(IssuesRenderOpts{Width: width, Page: 16})
	joined := strings.Join(out, "\n")
	for _, want := range []string{" 1 █ 042 feat: 行番号", " 3 段落。", " 6 ┃ a := 1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("行番号つきの行 %q が出ない:\n%s", want, joined)
		}
	}
	for _, ln := range out {
		if w := term.DispWidth(ln); w > width {
			t.Fatalf("行番号の溝を足して幅を超えた (w=%d): %q", w, ln)
		}
	}
}

// Enter は「TUI 内の開閉 toggle」(ユーザー要望 2026-08-01): 一覧で開き、本文で閉じる。
// 🚨 本文の Enter は以前 1 行送りだった。glogx 本体の job パネルが既に Enter = 開閉 toggle
// なので、viewer だけ意味が違うと同じキーが画面ごとに別物になる。
func TestIssuesViewEnterTogglesBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	body := "# 028 refactor: x\n\n" + strings.Repeat("段落。\n\n", 30)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "028-refactor-x.md"))

	v.HandleKey("enter", vp(10)) // 一覧の Enter = 開く
	if v.Open == nil {
		t.Fatal("一覧の Enter で本文が開かない")
	}
	v.Drawer.Finish()
	v.Lines(renderOpts(20))  // 行数は描画で確定する (未描画だと Len()=0 でスクロール上限が 0)
	v.HandleKey("j", vp(10)) // 行送りは j/k のまま (Enter を奪ったので、こちらは残っていること)
	if v.BodyPager.Offset != 1 {
		t.Fatalf("本文の j で 1 行送れていない: %d", v.BodyPager.Offset)
	}

	v.HandleKey("enter", vp(10)) // 本文の Enter = 閉じる
	if v.BodyPager.Offset != 1 {
		t.Fatalf("本文の Enter が行送りとして効いた: bodyOff=%d", v.BodyPager.Offset)
	}
	landed := app.TimeNow().Add(issuesDrawerDuration + time.Millisecond) // 演出の起点は実時刻なので、そこから進める
	testenv.StubClockFrom(t, &landed)
	v.Lines(renderOpts(20)) // 閉じる演出を着地させる
	if v.Open != nil {
		t.Fatal("本文の Enter で一覧へ戻らない")
	}
}

// n = 「次にやる」の目印 (ユーザー要望 2026-08-01)。確認を挟んでから next/ へ移す。
// 🚨 viewer で唯一、実ファイルを動かす操作なので、確認なしで動かないことも一緒に固定する。
func TestIssuesViewMarkNextMovesAfterConfirm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := issuesviewtest.Path2Issue(dir, "001-feat-x.md")
	v := loadedView(iss)
	v.Cwd = dir

	v.HandleKey("n", vp(10))
	if !v.MarkNext.Active {
		t.Fatal("n で確認が出ない")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("確認の段階でファイルが動いた")
	}
	// 確認は最前面に描く (裏の一覧に紛れない)
	if out := strings.Join(v.Lines(renderOpts(20)), "\n"); !strings.Contains(out, "next の目印") {
		t.Fatalf("確認モーダルが描かれない:\n%s", out)
	}

	if cmd := v.HandleKey("y", vp(10)); cmd == nil {
		t.Fatal("実行後に取り直しの Cmd が返らない (一覧が古いままになる)")
	}
	if v.MarkNext.Active {
		t.Fatal("実行後も確認が残っている")
	}
	// 直下の issue の claim は symlink の目印 (issues/nextlink.go, issue 263)。実ファイルは動かない
	dest := filepath.Join(dir, issues.NextDirName, "001-feat-x.md")
	if target, err := os.Readlink(dest); err != nil || target != "../001-feat-x.md" {
		t.Fatalf("next/ に目印 symlink が置かれない (ディレクトリ作成も含む): target=%q err=%v", target, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("claim で実ファイルが動いた")
	}
	if text, ok := testenv.NoticeOKOf(v.TakeNotice()); !ok || !strings.Contains(text, "next の目印を付けました") {
		t.Fatalf("結果が通知に載らない: %q ok=%v", text, ok)
	}
}

// 🚨 y / Enter 以外はすべて取り消し。「知らないキーを押したら実ファイルが動いた」を作らない。
// 大文字 Y も取り消し (confirm.IsYesStrict。push / 変更の破棄は Y も実行する側で、揃えない理由は
// status_view.go:discardKey の注記)。
func TestIssuesViewMarkNextCancels(t *testing.T) {
	for _, key := range []string{"n", "esc", "q", "j", "x", "Y"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "001-feat-x.md")
		if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		v := loadedView(issuesviewtest.Path2Issue(dir, "001-feat-x.md"))
		v.Cwd = dir
		v.HandleKey("n", vp(10))
		if cmd := v.HandleKey(key, vp(10)); cmd != nil {
			t.Fatalf("%q で取り消したのに Cmd が返った", key)
		}
		if v.MarkNext.Active {
			t.Fatalf("%q で確認が閉じない", key)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%q で取り消したのにファイルが動いた", key)
		}
	}
}

// 複数選択していれば範囲ぶんまとめて目印を付ける (y/p/Y と同じ対象の決め方)。
func TestIssuesViewMarkNextUsesSelection(t *testing.T) {
	dir := t.TempDir()
	list := make([]*issues.Issue, 0, 3)
	for _, n := range []string{"001", "002", "003"} {
		p := filepath.Join(dir, n+"-feat-x.md")
		if err := os.WriteFile(p, []byte("# "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		list = append(list, &issues.Issue{Path: p, Dir: dir, Rel: n + "-feat-x.md", Number: n, Category: "feat"})
	}
	v := loadedView(list...)
	v.Cwd = dir
	v.Cursor = 0
	v.HandleKey("shift+down", vp(10)) // 2 行選択
	v.HandleKey("n", vp(10))
	if len(v.MarkNext.targets) != 2 {
		t.Fatalf("選択範囲が対象になっていない: %d 件", len(v.MarkNext.targets))
	}
	v.HandleKey("y", vp(10))
	moved := 0
	for _, iss := range list {
		if _, err := os.Stat(filepath.Join(dir, issues.NextDirName, filepath.Base(iss.Rel))); err == nil {
			moved++
		}
	}
	if moved != 2 {
		t.Fatalf("選択ぶんが移動していない: %d 件", moved)
	}
	if _, _, ok := v.Selection(); ok {
		t.Fatal("実行後も選択が残っている")
	}
}

// [next] は All の左に固定の疑似カテゴリ (ユーザー要望 2026-08-01)。ファイル名のカテゴリでは
// なく「next/ に居るか」で選ぶので、状態フィルタの段階に関係なく目印つきが全部出る。
// epic の子の目印 (`epic/<name>/next/`) は数えない: group の中で ▶ として見えるので、global の
// [next] にまで出すと同じ issue が 2 箇所に並ぶ (ユーザー要望 2026-09-24)。
func TestIssuesViewNextPseudoTab(t *testing.T) {
	list := append(issuesviewtest.SampleIssues(),
		issuesviewtest.FakeIssue("040", "feat", "next-a", issues.StatusNext),
		issuesviewtest.FakeIssue("041", "docs", "next-b", issues.StatusNext),
		fakeEpicIssue("/repo/issues", "cloud", "042", "epic-next", issues.StatusNext),
	)
	v := loadedView(list...)

	// 既定は All のまま (zero value を [next] にしない = 開いた瞬間に空の一覧を出さない)
	if v.tabIdx != 0 || v.currentTab() != "" {
		t.Fatalf("既定のタブが All でない: tabIdx=%d name=%q", v.tabIdx, v.currentTab())
	}
	// チップは左端が [next]
	line := v.tabLine(IssuesRenderOpts{Width: 120})
	if !strings.HasPrefix(strings.TrimSpace(line), "[next 2]") {
		t.Fatalf("[next] が左端に出ていない: %q", line)
	}
	if !strings.Contains(line, "[All ") {
		t.Fatalf("All のチップが消えた: %q", line)
	}

	// 選ぶと目印つきだけが並ぶ
	v.tabIdx = tabIdxNext
	v.Refresh()
	if len(v.Rows) != 2 {
		t.Fatalf("[next] の行数が違う: %d", len(v.Rows))
	}
	for _, iss := range v.Rows {
		if iss.Status != issues.StatusNext {
			t.Fatalf("[next] に目印なしが混ざった: %s", iss.Rel)
		}
		if iss.GroupKind == issues.GroupEpic {
			t.Fatalf("[next] に epic の子が混ざった: %s", iss.Rel)
		}
	}
	// 状態フィルタを動かしても [next] の中身は変わらない (目印が段階で消えない)
	for _, f := range []issues.StatusFilter{issues.FilterPending, issues.FilterAll, issues.FilterOpen} {
		v.Filter = f
		v.Refresh()
		if len(v.Rows) != 2 {
			t.Fatalf("filter=%v で [next] の中身が変わった: %d 件", f, len(v.Rows))
		}
	}
}

// 疑似カテゴリの選択も保存・復元できる (名前で持つので位置がずれても追従する)。
func TestIssuesViewNextTabSurvivesRestore(t *testing.T) {
	v := loadedView(append(issuesviewtest.SampleIssues(), issuesviewtest.FakeIssue("040", "feat", "n", issues.StatusNext))...)
	v.tabIdx = tabIdxNext
	v.Refresh()
	v.Root = "/repo"
	s, ok := v.Screen(app.TimeNow())
	if !ok || s.Tab != tabNextName {
		t.Fatalf("[next] の選択が保存されない: %+v ok=%v", s, ok)
	}
	v2 := newTestIssuesView()
	v2.Restore("/repo", s)
	v2.Receive(IssuesScanMsg{Dirs: []string{"/repo/issues"},
		Issues: append(issuesviewtest.SampleIssues(), issuesviewtest.FakeIssue("040", "feat", "n", issues.StatusNext))})
	if v2.currentTab() != tabNextName {
		t.Fatalf("復元で [next] に戻らない: %q", v2.currentTab())
	}
}

// n は目印の toggle: 既に next の issue には「外す」向きになる (ユーザー要望 2026-08-01)。
// 🚨 戻り先は issue ディレクトリ直下 (= open)。元居た場所は覚えていない。
func TestIssuesViewMarkNextTogglesOff(t *testing.T) {
	dir := t.TempDir()
	nextDir := filepath.Join(dir, issues.NextDirName)
	if err := os.MkdirAll(nextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nextDir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &issues.Issue{Path: path, Dir: dir, Rel: filepath.Join(issues.NextDirName, "001-feat-x.md"),
		Number: "001", Category: "feat", Status: issues.StatusNext}
	v := loadedView(iss)
	v.Cwd = dir

	v.HandleKey("n", vp(10))
	if !v.MarkNext.Active || !v.MarkNext.unmark {
		t.Fatalf("目印つきの issue で「外す」向きになっていない: %+v", v.MarkNext)
	}
	if out := strings.Join(v.Lines(renderOpts(20)), "\n"); !strings.Contains(out, "next を外す") {
		t.Fatalf("外す向きの確認が出ない:\n%s", out)
	}
	v.HandleKey("y", vp(10))
	if _, err := os.Stat(filepath.Join(dir, "001-feat-x.md")); err != nil {
		t.Fatalf("issues 直下へ戻っていない: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("next/ にファイルが残っている")
	}
	if text, ok := testenv.NoticeOKOf(v.TakeNotice()); !ok || !strings.Contains(text, "next を外しました") {
		t.Fatalf("結果が通知に載らない: %q", text)
	}
}

// 混在した選択では向きをカーソル行で決めて全体を揃える (1 件ずつ toggle にしない)。
func TestIssuesViewMarkNextDirectionFollowsCursor(t *testing.T) {
	dir := t.TempDir()
	nextDir := filepath.Join(dir, issues.NextDirName)
	if err := os.MkdirAll(nextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marked := filepath.Join(nextDir, "002-feat-b.md")
	plain := filepath.Join(dir, "001-feat-a.md")
	for _, p := range []string{marked, plain} {
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list := []*issues.Issue{
		{Path: marked, Dir: dir, Rel: filepath.Join(issues.NextDirName, "002-feat-b.md"),
			Number: "002", Category: "feat", Status: issues.StatusNext},
		{Path: plain, Dir: dir, Rel: "001-feat-a.md", Number: "001", Category: "feat"},
	}
	v := loadedView(list...)
	v.Cwd = dir
	// 🚨 shift+↓ はカーソルを下へ動かすので、向きを決めるカーソル行も一緒に動く。
	// 目印つき (rows[0]) にカーソルを置いた状態で選択を作るには上へ伸ばす
	v.Cursor = 1
	v.HandleKey("shift+up", vp(10)) // カーソルは rows[0] (目印つき) = 外す向き
	v.HandleKey("n", vp(10))
	if !v.MarkNext.unmark || len(v.MarkNext.targets) != 2 {
		t.Fatalf("カーソル行の向きで揃っていない: %+v", v.MarkNext)
	}
	v.HandleKey("y", vp(10))
	// 目印つきだけが動き、元から直下に居たものは対象外
	if _, err := os.Stat(filepath.Join(dir, "002-feat-b.md")); err != nil {
		t.Fatalf("目印つきが直下へ戻っていない: %v", err)
	}
	if _, err := os.Stat(plain); err != nil {
		t.Fatal("対象外のファイルが動いた")
	}
}

func TestIssuesViewTabChipCountsMatchRows(t *testing.T) {
	// チップの件数は親行を含めない子 issue の件数と一致する。issues.Tab.Count は done を含む
	// 全件なので、そのまま出すと done を伏せた既定表示で合計が All と食い違う。
	v := loadedView(issuesviewtest.SampleIssues()...)
	line := v.tabLine(IssuesRenderOpts{Width: 120})
	for _, want := range []string{"[All 2]", "[feat 2]", "[refactor 0]", "[other 0]"} {
		if !strings.Contains(line, want) {
			t.Fatalf("チップの件数が状態フィルタと合っていない (%s が無い): %q", want, line)
		}
	}
	for i := range v.tabs {
		v.tabIdx = i + 1
		v.Refresh()
		if len(v.Rows) != v.tabCount[i] {
			t.Fatalf("タブ %s の件数 %d が実際の行数 %d と違う", v.tabs[i].Name, v.tabCount[i], len(v.Rows))
		}
	}
	v.tabIdx = 0
	v.HandleKey("a", vp(10)) // + pending
	if v.allCount != 3 {
		t.Fatalf("pending 表示で All の件数が更新されない: %d", v.allCount)
	}
	v.HandleKey("a", vp(10)) // + done
	if v.allCount != 5 {
		t.Fatalf("done 表示で All の件数が更新されない: %d", v.allCount)
	}
	if got := v.tabLine(IssuesRenderOpts{Width: 120}); !strings.Contains(got, "[other 1]") {
		t.Fatalf("done 表示で other の件数が更新されない: %q", got)
	}

	// Epic があると、バッジ/rows は子件数のまま、描画だけ親行を 1 行足す。ここを同じ値で
	// 検査すると、displayRows に親を混ぜた退行を見逃すので、2 つの契約を分けて固定する。
	groupedIssues := append(issuesviewtest.SampleIssues(),
		fakeEpicIssue("/repo/issues", "cloud", "040", "a", issues.StatusOpen),
		fakeEpicIssue("/repo/issues", "cloud", "039", "b", issues.StatusOpen),
	)
	grouped := loadedView(groupedIssues...)
	if grouped.allCount != 4 || len(grouped.Rows) != 4 || len(grouped.displayRows) != 3 {
		t.Fatalf("Epic の子件数と表示行数が分離されていない: all=%d rows=%d display=%d",
			grouped.allCount, len(grouped.Rows), len(grouped.displayRows))
	}
	if got := grouped.tabLine(IssuesRenderOpts{Width: 120}); !strings.Contains(got, "[All 4]") {
		t.Fatalf("Epic 親行がバッジ件数へ混ざった: %q", got)
	}
}

func TestIssuesViewLinesAlwaysExactlyPageRows(t *testing.T) {
	// 幅は 1 から全数で振る: 狭い幅では固定部分 (溝・番号・バッジ・カテゴリ) だけで幅を
	// 超えるため、行のクリップが 1 経路でも抜けていると枠を突き破る。掃き始めが 20 だった
	// 頃は幅 1-2 の取りこぼしが眠っていた (issue 053: term.ClipToWidth の width<=0 素通しと
	// tabLine のフィルタバッジ後置)。
	for width := 1; width <= 80; width++ {
		for _, page := range []int{3, 5, 20, 40} {
			for _, v := range []*IssuesView{loadedView(issuesviewtest.SampleIssues()...), loadedView(), {Shown: true, Scanning: true}} {
				o := renderOpts(page)
				o.Width = width
				got := v.Lines(o)
				if len(got) != page {
					t.Fatalf("width=%d page=%d なのに %d 行返った", width, page, len(got))
				}
				for i, ln := range got {
					if w := term.DispWidth(ln); w > width {
						t.Fatalf("width=%d page=%d 行 %d が幅を超えた (w=%d): %q", width, page, i, w, ln)
					}
				}
			}
		}
	}
}

func TestIssuesViewRowShowsNumberBadgeCategoryTitle(t *testing.T) {
	list := issuesviewtest.SampleIssues()
	list[0].Title = "先頭のタイトル" // 030 (カーソル行)
	v := loadedView(list...)
	v.Filter = issues.FilterPending // pending のバッジ ⏸ も見たいので 1 段進める
	v.Refresh()
	lines := v.Lines(renderOpts(20))
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"030", "○", "feat", "028", "⏸", "refactor"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("一覧に %q が出ない:\n%s", want, joined)
		}
	}
	// 矢印はカーソル行だけに 1 本出る (先頭行 = 030)
	marked := make([]string, 0, 1)
	for _, ln := range lines {
		if strings.HasPrefix(ln, term.CursorGutterMark) {
			marked = append(marked, ln)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "030") {
		t.Fatalf("カーソル行の矢印が想定と違う: %q", marked)
	}
	// 🚨 行の中身はその行 (カーソル行) で見る。画面全体で探すと、カテゴリ名はタブ行のチップ、
	// ○ / ⏸ はタブ行のバッジでも満たされ、行からカテゴリ列を消しても緑だった (issue 667)
	for _, want := range []string{"030", "○", "feat", "先頭のタイトル"} {
		if !strings.Contains(marked[0], want) {
			t.Fatalf("カーソル行に %q が出ない: %q", want, marked[0])
		}
	}
}

func TestIssuesViewCursorScrollsWithinWindow(t *testing.T) {
	many := issuesviewtest.ManyIssuesOf(40, "feat")
	v := loadedView(many...)
	const rows = 10
	for range 20 {
		v.HandleKey("j", vp(rows))
	}
	if v.Cursor != 20 {
		t.Fatalf("カーソルが動いていない: %d", v.Cursor)
	}
	if v.offset == 0 || v.Cursor < v.offset || v.Cursor >= v.offset+rows {
		t.Fatalf("カーソルが画面内に収まっていない: cursor=%d offset=%d", v.Cursor, v.offset)
	}
	v.HandleKey("G", vp(rows))
	if v.Cursor != len(v.Rows)-1 {
		t.Fatalf("G で末尾へ行かない: %d", v.Cursor)
	}
	v.HandleKey("g", vp(rows))
	if v.Cursor != 0 || v.offset != 0 {
		t.Fatalf("g で先頭へ戻らない: cursor=%d offset=%d", v.Cursor, v.offset)
	}
	// 端で止まる (負のインデックスにならない)
	v.HandleKey("k", vp(rows))
	if v.Cursor != 0 {
		t.Fatalf("先頭で k がはみ出した: %d", v.Cursor)
	}
}

func TestIssuesViewBodyModeOpensAndCloses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	body := "# 028 refactor: タイトル\n\n本文の段落。\n\n- 箇条書き\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &issues.Issue{Path: path, Dir: dir, Rel: "028-refactor-x.md", Number: "028", Category: "refactor", Slug: "x"}
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	v := loadedView(iss)
	v.HandleKey("enter", vp(10))
	if v.Open == nil || v.Body == nil {
		t.Fatal("Enter で本文モードに入らない")
	}
	// 本文は引き出しとして左から開く (issues_drawer.go)。開いた直後は幅 0 なので、中身の検証は
	// 演出を着地させてから行う (実機では 450ms で着地する)。
	v.Drawer.Finish()
	out := strings.Join(v.Lines(renderOpts(20)), "\n")
	for _, want := range []string{"028-refactor-x.md", "タイトル", "本文の段落", "• 箇条書き"} {
		if !strings.Contains(out, want) {
			t.Fatalf("本文表示に %q が出ない:\n%s", want, out)
		}
	}
	v.HandleKey("h", vp(10))
	// 閉じる演出のあいだ本文は生きている (逆再生に中身が映る必要がある)
	if v.Open == nil {
		t.Fatal("h の直後に本文が消えた (閉じる演出に何も映らない)")
	}
	landed := app.TimeNow().Add(issuesDrawerDuration + time.Millisecond) // 演出の起点は実時刻なので、そこから進める
	testenv.StubClockFrom(t, &landed)
	v.Lines(renderOpts(20)) // 演出の着地を反映させる
	if v.Open != nil {
		t.Fatal("h で一覧へ戻らない")
	}
}

func TestIssuesViewBodyScrollClampsToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-long.md")
	var b strings.Builder
	for i := range 100 {
		fmt.Fprintf(&b, "段落 %d\n\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "001-feat-long.md"))
	v.HandleKey("enter", vp(10))
	// 描画で行数が確定してからスクロールする (Body は幅ごとに整形結果をキャッシュする)
	v.Lines(renderOpts(20))
	const page = 17
	for range 100 {
		v.HandleKey("ctrl+d", vp(page))
	}
	// handleKey に渡すのは画面行数 (page)。実際にスクロールに使える行数はヘッダーを
	// 差し引いた visibleRows で、これが描画側とずれると末尾に届かなくなる
	if v.BodyPager.Offset != max(v.Body.Len()-v.VisibleRows(vp(page)), 0) {
		t.Fatalf("末尾を超えてスクロールした: off=%d len=%d rows=%d", v.BodyPager.Offset, v.Body.Len(), v.VisibleRows(vp(page)))
	}
	v.HandleKey("g", vp(page))
	if v.BodyPager.Offset != 0 {
		t.Fatalf("g で先頭へ戻らない: %d", v.BodyPager.Offset)
	}
}

// hasCursorMark はカーソル行の矢印が描かれている行があるか。
func hasCursorMark(lines []string) bool {
	for _, ln := range lines {
		if strings.HasPrefix(ln, term.CursorGutterMark) {
			return true
		}
	}
	return false
}

func TestIssuesViewCursorStaysVisibleAfterRowSetChanges(t *testing.T) {
	// 🚨 回帰防止: カーソルは常に窓の中にいる。行数やヘッダー高が変わる経路 (a / Tab /
	// 通知行の増加) で窓だけ先頭へ飛ぶと、カーソル行がどこにも描かれないまま
	// current() が見えない行を返し、Enter / v (nvim) / y が別の issue を対象にする。
	testenv.StubClipboardFunc(t, func(string) error { return nil })

	list := make([]*issues.Issue, 0, 50)
	for i := 50; i > 0; i-- {
		st := issues.StatusOpen
		if i <= 10 {
			st = issues.StatusDone
		}
		list = append(list, issuesviewtest.FakeIssue(fmt.Sprintf("%03d", i), "feat", "x", st))
	}
	const page = 12
	// 🚨 tab は 2 回押す: All の右には常設の human タブ (この fixture では 0 件) が居るので、
	// 1 回だけだと「行が無いタブ」に入ってカーソル行そのものが存在しなくなる
	for _, keys := range [][]string{{"a"}, {"tab", "tab"}, {"y"}} {
		v := loadedView(list...)
		v.HandleKey("G", vp(page)) // カーソルを末尾へ (窓は下端に張り付く)
		if !hasCursorMark(v.Lines(renderOpts(page))) {
			t.Fatalf("前提が崩れた: G の直後にカーソル行が描かれていない (keys=%v)", keys)
		}
		for _, key := range keys {
			v.HandleKey(key, vp(page))
		}
		if !hasCursorMark(v.Lines(renderOpts(page))) {
			t.Fatalf("%v の後にカーソル行が窓の外へ出た:\n%s", keys, strings.Join(v.Lines(renderOpts(page)), "\n"))
		}
	}
}

func TestIssuesViewBodyScrollRecoversAfterWidthGrows(t *testing.T) {
	// 幅が広がると本文行数が減る。論理 bodyOff を収束させないと k / ctrl+u が
	// 「押しても画面が動かない」状態になる (上方向だけ固まる)。
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-long.md")
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "折り返しの対象になる長い段落 %d。ここは幅が狭いと複数行に割れ、広いと 1 行に収まる。\n\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "001-feat-long.md"))
	const page = 12
	v.HandleKey("enter", vp(page))
	v.Drawer.Finish() // 引き出しを開ききった状態で幅の検証をする

	narrow := IssuesRenderOpts{Width: 40, Page: page}
	v.Lines(narrow)
	v.HandleKey("G", vp(page)) // 狭い幅での末尾へ
	v.Lines(narrow)
	tail := v.BodyPager.Offset

	wide := IssuesRenderOpts{Width: 100, Page: page}
	v.Lines(wide) // 幅が広がって行数が減る
	if v.BodyPager.Offset >= tail {
		t.Fatalf("幅を広げても bodyOff が縮まっていない: %d -> %d", tail, v.BodyPager.Offset)
	}
	before := v.BodyPager.Offset
	v.HandleKey("k", vp(page))
	if v.BodyPager.Offset != before-1 {
		t.Fatalf("幅変更後の k が 1 行戻していない: %d -> %d", before, v.BodyPager.Offset)
	}
}

func TestIssuesViewGReachesLastLine(t *testing.T) {
	// キー操作側と描画側で使う行数が一致していることの担保 (ずれると G が末尾に届かない)。
	// 一覧と本文の両方で「G を押したら最後の行が実際に描かれる」ことを見る。
	many := issuesviewtest.ManyIssuesOf(40, "feat")
	v := loadedView(many...)
	const page = 12
	v.HandleKey("G", vp(page))
	if out := strings.Join(v.Lines(renderOpts(page)), "\n"); !strings.Contains(out, "001") {
		t.Fatalf("一覧で G が末尾に届いていない:\n%s", out)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-long.md")
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "段落 %d\n\n", i)
	}
	b.WriteString("最終行マーカー\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	v2 := loadedView(issuesviewtest.Path2Issue(dir, "001-feat-long.md"))
	v2.HandleKey("enter", vp(page))
	v2.Lines(renderOpts(page)) // 幅ごとの整形を確定させてから G
	v2.HandleKey("G", vp(page))
	if out := strings.Join(v2.Lines(renderOpts(page)), "\n"); !strings.Contains(out, "最終行マーカー") {
		t.Fatalf("本文で G が末尾に届いていない:\n%s", out)
	}
}

func TestIssuesViewCopyPathAndEditor(t *testing.T) {
	copied := testenv.StubClipboard(t)
	cmds := testenv.StubEditorCapture(t)
	testenv.PinFallbackEditor(t)

	// 🚨 実ファイルを置く: editCmd は起動前に実体を確認するので、合成パスだと起動しない
	// (stale なパスでエディタを開かせない guard。editCmd の doc 参照)
	iss := issuesviewtest.RealIssue(t)
	v := loadedView(iss)
	v.HandleKey("y", vp(10))
	if *copied != v.Rows[0].Path {
		t.Fatalf("カーソル行のパスがコピーされていない: %q", *copied)
	}
	// 結果はヘッダーに出さず、browseModel がトーストへ流すために取り出す (takeNotice)
	if text, ok := testenv.NoticeOKOf(v.TakeNotice()); !ok || !strings.Contains(text, "コピーしました") {
		t.Fatalf("コピーの結果が通知に載らない: %q ok=%v", text, ok)
	}
	if cmd := v.HandleKey("v", vp(10)); cmd == nil || len(*cmds) != 1 {
		t.Fatalf("v でエディタ起動の Cmd が返らない: cmd=%v 起動数=%d", cmd != nil, len(*cmds))
	}
	// 🚨 「開いた」だけでなく「何を開いたか」まで見る (対象の取り違えを通さない)
	if args, want := (*cmds)[0].Args, []string{editor.Fallback, iss.Path}; !slices.Equal(args, want) {
		t.Errorf("v の起動コマンドが違う: args=%v want=%v", args, want)
	}
}

func TestIssuesViewActionKeysWorkInBothModes(t *testing.T) {
	// v / y / p / Y / N は一覧でも本文でも同じ対象 (target) に効く。モードごとの switch へ
	// 写すと、追加時に片方へ入れ忘れても「そのモードでだけ効かない」形で静かに壊れる。
	copied := testenv.StubClipboard(t)
	cmds := testenv.StubEditorCapture(t)
	testenv.PinFallbackEditor(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	if err := os.WriteFile(path, []byte("# 028 refactor: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := issuesviewtest.Path2Issue(dir, "028-refactor-x.md")
	for _, mode := range []string{"一覧", "本文"} {
		v := loadedView(iss)
		if mode == "本文" {
			v.HandleKey("enter", vp(10))
			if v.Open == nil {
				t.Fatal("本文モードに入れていない")
			}
		}
		for _, key := range []string{"y", "p", "Y", "N"} {
			*copied = ""
			v.HandleKey(key, vp(10))
			if *copied == "" {
				t.Fatalf("%s モードで %q がコピーしていない", mode, key)
			}
		}
		// v は先にあった割当、e は git log 一覧の e と語彙を揃えた別名。どちらのモードでも
		// 両方が nvim を起動する (hint が案内するのは e だけ)。
		for _, key := range []string{"v", "e"} {
			before := len(*cmds)
			if cmd := v.HandleKey(key, vp(10)); cmd == nil || len(*cmds) != before+1 {
				t.Fatalf("%s モードで %q がエディタを起動しない", mode, key)
			}
			// 対象は「その issue の実ファイル」。一覧モードと本文モードで target() が
			// 切り替わるので、どちらでも同じファイルを指すことまで見る
			if args, want := (*cmds)[before].Args, []string{editor.Fallback, path}; !slices.Equal(args, want) {
				t.Errorf("%s モードの %q の起動コマンドが違う: args=%v want=%v", mode, key, args, want)
			}
		}
	}
}

func TestIssuesViewRescanReturnsCmd(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	cmd := v.HandleKey("r", vp(10))
	if cmd == nil {
		t.Fatal("r で再スキャンの Cmd が返らない")
	}
	if !v.Loading() {
		t.Fatal("r の後にスキャン中になっていない")
	}
	// Cmd を実行すると探索結果の msg が返る (cwd が空でもクラッシュしない)
	if msg := cmd(); msg == nil {
		t.Fatal("再スキャンの Cmd が msg を返さない")
	}
}

func TestIssuesViewEmptyStates(t *testing.T) {
	// ディレクトリが 1 つも無い
	v := newTestIssuesView()
	v.Shown = true
	v.Receive(IssuesScanMsg{})
	if !strings.Contains(strings.Join(v.Lines(renderOpts(10)), "\n"), "見つかりません") {
		t.Fatal("issues ディレクトリ不在の案内が出ない")
	}
	// タブに open が無い (done だけ): 既定は open のみなので「pending も表示」を案内する
	v2 := loadedView(issuesviewtest.FakeIssue("001", "feat", "a", issues.StatusDone))
	if !strings.Contains(strings.Join(v2.Lines(renderOpts(10)), "\n"), "a: pending も表示") {
		t.Fatal("open が無いときの案内が出ない")
	}
	// 1 段進めても空なら done の案内へ進む
	v2.Filter = issues.FilterPending
	v2.Refresh()
	if !strings.Contains(strings.Join(v2.Lines(renderOpts(10)), "\n"), "a: done も表示") {
		t.Fatal("pending まで見ても空のときの案内が出ない")
	}
}

func TestIssuesViewShowsScanWarning(t *testing.T) {
	v := newTestIssuesView()
	v.Shown = true
	v.Receive(IssuesScanMsg{
		Dirs:     []string{"/repo/issues"},
		Issues:   issuesviewtest.SampleIssues(),
		Warnings: []string{"同じファイル名が複数の状態ディレクトリにあります: 028-x.md / done/028-x.md"},
	})
	if !strings.Contains(strings.Join(v.Lines(renderOpts(10)), "\n"), "同じファイル名") {
		t.Fatal("スキャンの警告が表示されない")
	}
	// 警告が複数あるときは件数も出す (2 本目以降が出ていることを知らせる。issue 698 の 1)
	v.Receive(IssuesScanMsg{
		Dirs:     []string{"/repo/issues"},
		Issues:   issuesviewtest.SampleIssues(),
		Warnings: []string{"同じファイル名が複数の状態ディレクトリにあります: 028-x.md / done/028-x.md", "next/x.md: 指す先が無い", "読めないフォルダ"},
	})
	if got := strings.Join(v.Lines(renderOpts(10)), "\n"); !strings.Contains(got, "ほか 2 件") {
		t.Fatalf("警告の件数が出ない:\n%s", got)
	}
}

func TestIssuesViewNoticeIsTransientAndDoesNotHideWarning(t *testing.T) {
	// 🚨 回帰防止: notice に寿命が無いと、コピーを 1 回した時点でスキャン警告
	// (同名ファイルの二重化 = conflict も error も出ない静かな内容喪失) が二度と出なくなる。
	testenv.StubClipboardFunc(t, func(string) error { return nil })

	v := newTestIssuesView()
	v.Shown = true
	v.Receive(IssuesScanMsg{
		Dirs:     []string{"/repo/issues"},
		Issues:   issuesviewtest.SampleIssues(),
		Warnings: []string{"同じファイル名が複数の状態ディレクトリにあります: 028-x.md / done/028-x.md"},
	})
	v.HandleKey("y", vp(10))
	text, ok := testenv.NoticeOKOf(v.TakeNotice())
	if !ok || !strings.Contains(text, "コピーしました") {
		t.Fatalf("コピーの結果が通知に載らない: %q ok=%v", text, ok)
	}
	// 🚨 操作結果はヘッダーを占めない (トーストへ移した) ので、スキャン警告を一瞬も隠さない。
	// 以前は通知が警告と同じ 1 行を奪い合っていた (同名ファイルの二重化 = 静かな内容喪失の警告)。
	out := strings.Join(v.Lines(renderOpts(10)), "\n")
	if strings.Contains(out, "コピーしました") {
		t.Errorf("操作結果がヘッダーに出ている (トーストで出すべき):\n%s", out)
	}
	if !strings.Contains(out, "同じファイル名") {
		t.Fatalf("スキャン警告が出ていない:\n%s", out)
	}
}

func TestIssuesViewBodyModeShowsCopyResult(t *testing.T) {
	// 本文モードのコピーの成否も通知に載る (browseModel がトーストへ流す)。以前はヘッダーの
	// 行に出していたが、viewer の上にもトーストを合成するようにして共通の語彙へ寄せた。
	testenv.StubClipboardFunc(t, func(string) error { return errors.New("pbcopy が見つかりません") })

	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	if err := os.WriteFile(path, []byte("# 028 refactor: タイトル\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "028-refactor-x.md"))
	v.HandleKey("enter", vp(10))
	v.HandleKey("p", vp(10))
	text, ok := testenv.NoticeOKOf(v.TakeNotice())
	if ok || !strings.Contains(text, "コピーに失敗しました") {
		t.Fatalf("本文モードのコピー失敗が通知に載らない: %q ok=%v", text, ok)
	}
}

func TestIssuesViewCopyActions(t *testing.T) {
	copied := testenv.StubClipboard(t)

	v := loadedView(issuesviewtest.SampleIssues()...)
	v.Root = "/repo"
	v.All[0].Title = "030 feat: 新機能"

	v.HandleKey("p", vp(10))
	if *copied != "030" {
		t.Fatalf("p が番号をコピーしていない: %q", *copied)
	}
	if !strings.Contains(v.Notice, "番号をコピーしました: 030") {
		t.Fatalf("通知が出ていない: %q", v.Notice)
	}
	// Y = 番号 + タイトル + repo 相対パス (H1 の先頭番号は Display が落とす)
	v.HandleKey("Y", vp(10))
	if *copied != "issue 030 feat: 新機能 (issues/030-feat-a.md)" {
		t.Fatalf("Y の参照が想定と違う: %q", *copied)
	}
	// N = 次に採番すべき番号 (fixture の最大は 030)
	v.HandleKey("N", vp(10))
	if *copied != "031" {
		t.Fatalf("N が次番号をコピーしていない: %q", *copied)
	}
	// 番号なし issue ではファイル名に落として理由を通知する
	v2 := loadedView(&issues.Issue{Path: "/repo/issues/resource-leaks.md", Dir: "/repo/issues", Rel: "resource-leaks.md", Slug: "resource-leaks"})
	v2.HandleKey("p", vp(10))
	if *copied != "resource-leaks.md" || !strings.Contains(v2.Notice, "番号が無い") {
		t.Fatalf("番号なしの扱いが想定と違う: copied=%q notice=%q", *copied, v2.Notice)
	}
}

func TestCategoryColorIsStableAndReservesRed(t *testing.T) {
	if categoryColor("bug") != catRed || categoryColor("fix") != catRed {
		t.Fatal("bug / fix が赤になっていない")
	}
	if categoryColor("feat") != catGreen {
		t.Fatal("feat が緑になっていない")
	}
	if categoryColor("") != term.AnsiDim {
		t.Fatal("カテゴリ無しが dim になっていない")
	}
	// 表に無い語: 同じ語なら常に同じ色 (起動ごとに変わらない)
	first := categoryColor("waveform")
	for range 5 {
		if categoryColor("waveform") != first {
			t.Fatal("未知カテゴリの色が安定していない")
		}
	}
	// 🚨 未知語に赤を割らない (意味の無い語が「失敗」の色で出ると誤読される)
	for _, name := range []string{"waveform", "videoviewmodel", "dfs", "share", "auth", "acl", "codec", "thumbnail", "tabmanager"} {
		if categoryColor(name) == catRed {
			t.Fatalf("未知カテゴリ %q に赤を割った", name)
		}
	}
}

func TestIssuesViewRowPaintsCategoryColor(t *testing.T) {
	v := loadedView(issuesviewtest.FakeIssue("001", "bug", "x", issues.StatusOpen))
	o := renderOpts(80)
	o.Colored = true
	joined := strings.Join(v.Lines(o), "\n")
	if !strings.Contains(joined, catRed+"bug") {
		t.Fatalf("カテゴリ列に色が塗られていない:\n%q", joined)
	}
}

func TestIssuesViewSlideInAnimation(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	v.Shown = true

	// 開始直後: まだほとんど画面外 (先頭行に右オフセットが入る)
	atProgress(v, 0.05)
	if !v.Animating() {
		t.Fatal("演出中と判定されない")
	}
	early := v.Lines(renderOpts(12))
	if len(early) != 12 {
		t.Fatalf("演出中も page 行を返すべき: %d", len(early))
	}
	shifted := false
	for _, ln := range early {
		if w := term.DispWidth(ln); w > 80 {
			t.Fatalf("演出中の行が幅を超えた (w=%d): %q", w, ln)
		}
		if strings.HasPrefix(ln, " ") && strings.TrimSpace(ln) != "" {
			shifted = true
		}
	}
	if !shifted {
		t.Fatalf("右からのスライドになっていない (オフセットが無い):\n%q", early)
	}

	// 進みの途中: 行ごとに開始がずれる (stagger) ので、上の行の方が先に着地している
	atProgress(v, 0.5)
	mid := v.Lines(renderOpts(12))
	if offsetOf(mid[2]) > offsetOf(mid[len(mid)-1]) {
		t.Fatalf("上の行が先に着地していない: %q", mid)
	}

	// 着地後: 演出なしの静的描画と一致する
	v.FinishAnim()
	if v.Animating() {
		t.Fatal("finishAnim 後も演出中のまま")
	}
	want := v.Lines(renderOpts(12))
	atProgress(v, 1.2) // duration を超えた進み
	if got := v.Lines(renderOpts(12)); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("着地後の画面が静的描画と一致しない:\n%q\n%q", got, want)
	}
}

// offsetOf は行頭の空白の数 (スライドの残りオフセット)。空行は最大扱い。
func offsetOf(line string) int {
	if strings.TrimSpace(line) == "" {
		return math.MaxInt
	}
	return len(line) - len(strings.TrimLeft(line, " "))
}

func TestIssuesViewKeyLandsAnimationImmediately(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	v.Shown = true
	atProgress(v, 0.1)
	v.HandleKey("j", vp(12))
	if v.Animating() {
		t.Fatal("キー入力で演出が着地していない (演出中は操作を待たせない契約)")
	}
}

// 0 件のカテゴリはタブ行の右へ寄る。件数 > 0 / 0 の各群の中では元の順序を保ち、
// 選択中のタブは並べ替えを跨いでも同じカテゴリを指し続ける (位置で持つ tabIdx の張り替え)。
func TestIssuesZeroCountTabsGoRight(t *testing.T) {
	// feat: open 2 / refactor: done 2 / bug: done 2 → 既定 (open のみ) では feat だけ非 0。
	// issues.Tabs は done 込みの件数降順なので、並べ替えが無いと bug/refactor が feat より左に来る。
	v := loadedView(
		issuesviewtest.FakeIssue("030", "feat", "a", issues.StatusOpen),
		issuesviewtest.FakeIssue("029", "feat", "b", issues.StatusOpen),
		issuesviewtest.FakeIssue("028", "refactor", "c", issues.StatusDone),
		issuesviewtest.FakeIssue("027", "refactor", "d", issues.StatusDone),
		issuesviewtest.FakeIssue("026", "bug", "e", issues.StatusDone),
		issuesviewtest.FakeIssue("025", "bug", "f", issues.StatusDone),
	)
	if len(v.tabs) < 2 {
		t.Fatalf("タブが揃っていない: %+v", v.tabs)
	}
	// 先頭は常設の human (0 件でも固定)。その次から「非 0 → 0」の並べ替えが効く
	if v.tabs[0].Name != issues.HumanTab {
		t.Errorf("human タブが先頭に固定されていない: tabs=%+v", v.tabs)
	}
	if v.tabs[1].Name != "feat" || v.tabCount[1] == 0 {
		t.Errorf("0 件でないタブが human の右に来ていない: tabs=%+v counts=%v", v.tabs, v.tabCount)
	}
	for i := 2; i < len(v.tabCount); i++ {
		if v.tabCount[i] != 0 {
			t.Errorf("0 件タブより右に非 0 のタブがある: counts=%v", v.tabCount)
		}
	}

	// 選択を追従: refactor を選んでから全件表示へ切り替えても refactor のまま
	idx := -1
	for i, tb := range v.tabs {
		if tb.Name == "refactor" {
			idx = i + 1
		}
	}
	if idx < 0 {
		t.Fatalf("refactor タブが無い: %+v", v.tabs)
	}
	v.tabIdx = idx
	v.Refresh()
	v.HandleKey("a", vp(10)) // + pending
	v.HandleKey("a", vp(10)) // + done (件数が変わり並びも変わる)
	if got := v.currentTab(); got != "refactor" {
		t.Errorf("並べ替えで選択タブが滑った: %q (want refactor)", got)
	}
}

// reorderTabsByCount の純粋な性質: 群内の順序保持と、数え漏れ時の no-op。
func TestReorderTabsByCount(t *testing.T) {
	tabs := []issues.Tab{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}
	counts := []int{0, 3, 0, 1}
	gotTabs, gotCounts := reorderTabsByCount(tabs, counts)
	names := []string{gotTabs[0].Name, gotTabs[1].Name, gotTabs[2].Name, gotTabs[3].Name}
	if want := []string{"b", "d", "a", "c"}; !slices.Equal(names, want) {
		t.Errorf("並び = %v, want %v (非 0 を元順で、次に 0 を元順で)", names, want)
	}
	if want := []int{3, 1, 0, 0}; !slices.Equal(gotCounts, want) {
		t.Errorf("件数の並び = %v, want %v", gotCounts, want)
	}
	// 入力を破壊しない (正規順序は呼び出し側が保持し続ける)
	if tabs[0].Name != "a" || counts[0] != 0 {
		t.Error("入力を破壊している")
	}
	// tabIndexOf と組み合わせて選択を名前で張り替えられる
	if idx := tabIndexOf(gotTabs, "c"); gotTabs[idx-1].Name != "c" {
		t.Errorf("選択の張り替えが違う: idx=%d", idx)
	}
	// 数え漏れ (長さ不一致) では並べ替えない
	if got, _ := reorderTabsByCount(tabs, []int{1}); got[0].Name != "a" {
		t.Error("長さ不一致でも並べ替えてしまっている")
	}
}

// u は URL ピッカーを開き、インクリメンタルサーチで絞って Enter で開く
// (ユーザー要望 2026-07-31: 順送りだと 24 本の issue で目的の 1 本に辿り着けない)。
func TestIssuesViewURLPicker(t *testing.T) {
	var opened []string
	testenv.StubBrowserFunc(t, func(url string) error { opened = append(opened, url); return nil })

	v := newTestIssuesView()
	v.Shown, v.Loaded = true, true
	v.Body = issues.NewBody("A https://example.com/alpha\nB https://example.com/beta\nC https://other.test/gamma\n")
	v.Open = issuesviewtest.FakeIssue("001", "feat", "a", issues.StatusOpen)

	if cmd := v.HandleKey("u", vp(20)); cmd != nil {
		t.Fatal("u はピッカーを開くだけで Cmd を返さない")
	}
	if !v.UrlPick.Active {
		t.Fatal("u でピッカーが開かない")
	}
	// 絞り込み前は全件、先頭が選択されている
	if got := v.UrlPick.selected(); got != "https://example.com/alpha" {
		t.Errorf("初期選択 = %q", got)
	}
	// インクリメンタルサーチ: "beta" で 1 件に絞れる
	for _, k := range []string{"b", "e", "t", "a"} {
		v.HandleKey(k, vp(20))
	}
	if len(v.UrlPick.Match) != 1 || v.UrlPick.selected() != "https://example.com/beta" {
		t.Fatalf("検索で絞れない: match=%d selected=%q", len(v.UrlPick.Match), v.UrlPick.selected())
	}
	cmd := v.HandleKey("enter", vp(20))
	if cmd == nil {
		t.Fatal("Enter で開く Cmd が返らない")
	}
	if msg, ok := cmd().(extcmd.OpenURLMsg); !ok || msg.Err != nil {
		t.Fatalf("openURLMsg でない/失敗: %#v", msg)
	}
	if len(opened) != 1 || opened[0] != "https://example.com/beta" {
		t.Errorf("開いた URL = %q", opened)
	}
	if v.UrlPick.Active {
		t.Error("確定後もピッカーが開いたまま")
	}
	if !strings.Contains(v.Notice, "https://example.com/beta") {
		t.Errorf("通知に URL が無い: %q", v.Notice)
	}
}

// ピッカーは他のどの割当よりも先にキーを飲む (印字文字は全部検索語)。
// e/v (エディタ) や y (コピー) が横取りすると、その文字を含む URL を検索できない。
func TestIssuesViewURLPickerSwallowsActionKeys(t *testing.T) {
	cmds := testenv.StubEditorCapture(t)

	// 🚨 エディタを開くキーは e と v の 2 本あるので両方回す。現行コードでは飲み込みを早期
	// return 1 箇所が支配していて片方で足りるが、キー別の分岐を作る変異には 1 キーだと効かない
	// (実際に番号入力側は v の 1 マスだけ穴が空いていた)。
	for _, key := range []string{"e", "v"} {
		before := len(*cmds)
		p := newTestIssuesView()
		p.Shown, p.Loaded = true, true
		p.Body = issues.NewBody("https://example.com/very-vivid\nhttps://example.com/plain\n")
		// 🚨 実ファイル: 実体が無いと editCmd の guard で止まり、飲み込みの検証が空振りする
		p.Open = issuesviewtest.RealIssue(t)
		p.HandleKey("u", vp(20))
		p.HandleKey(key, vp(20)) // 検索語になるべき (エディタを起動してはいけない)
		if len(*cmds) != before {
			t.Errorf("ピッカー中の %q がエディタを起動した: %v", key, (*cmds)[before].Args)
		}
		if p.UrlPick.Query() != key {
			t.Errorf("%q が検索語にならない: query=%q", key, p.UrlPick.Query())
		}
	}

	v := newTestIssuesView()
	v.Shown, v.Loaded = true, true
	v.Body = issues.NewBody("https://example.com/very-vivid\nhttps://example.com/plain\n")
	v.Open = issuesviewtest.FakeIssue("001", "feat", "a", issues.StatusOpen)
	v.HandleKey("u", vp(20))
	v.HandleKey("v", vp(20))
	if len(v.UrlPick.Match) != 1 || v.UrlPick.selected() != "https://example.com/very-vivid" {
		t.Errorf("v で絞り込めない: %v", v.UrlPick.Match)
	}
}

// 番号入力中も URL ピッカーと同じ理由でアクションキーより先に飲む (打った文字が検索語)。
// 🚨 URL ピッカー側にしかテストが無いと、actionKey を numFilter より前に動かしても何も落ちない。
// 順序を守るものをここで作る。キーは e と v の両方を回す — 「支配しているのは早期 return 1 箇所
// だから 1 キーで足りる」は現行コードの記述であって、キー別の分岐を作る変異には効かない。
func TestIssuesViewNumberFilterSwallowsActionKeys(t *testing.T) {
	cmds := testenv.StubEditorCapture(t)

	// 🚨 実ファイルを置く: 実体が無いと editCmd の guard だけで起動が止まり、
	// 「順序が守られているから起動しない」を検証できない (空振りする)
	v := loadedView(issuesviewtest.RealIssue(t))
	v.HandleKey("/", vp(10))
	if !v.NumFilter.Typing {
		t.Fatal("/ で番号入力に入れていない")
	}
	// e/v はエディタを開くキーだが、入力中は数字以外として捨てられるだけ (画面を動かさない)
	for _, key := range []string{"e", "v"} {
		before := len(*cmds)
		v.HandleKey(key, vp(10))
		if len(*cmds) != before {
			t.Errorf("番号入力中の %q がエディタを起動した: %v", key, (*cmds)[before].Args)
		}
		if !v.NumFilter.Typing {
			t.Errorf("番号入力中の %q が入力モードを抜けさせた", key)
		}
	}
}

// e は実体が無いパスではエディタを起動しない。🚨 一覧が握る Path は n (next/ へ移動) や
// 別プロセスの rename/削除で stale になる。stale なまま渡すと nvim は黙って新規バッファを開き、
// 保存すると旧位置にファイルが復活して「同じ basename が 2 箇所」を viewer 自身が作る
// (issues/move.go が宣言している不変条件の違反)。
func TestIssuesViewEditSkipsMissingFile(t *testing.T) {
	cmds := testenv.StubEditorCapture(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "001-feat-x.md"))

	// 前提: 実体があるうちは開く
	if cmd := v.HandleKey("e", vp(10)); cmd == nil || len(*cmds) != 1 {
		t.Fatalf("実体があるのに開かない (cmd==nil: %v, 起動数=%d)", cmd == nil, len(*cmds))
	}

	// 実体が動いた (n の next/ 移動・別プロセスの rename と同じ状態) 後は開かず取り直す
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cmd := v.HandleKey("e", vp(10))
	if len(*cmds) != 1 {
		t.Errorf("実体が無いのにエディタを起動した: %v", (*cmds)[len(*cmds)-1].Args)
	}
	if cmd == nil {
		t.Error("取り直しの Cmd を返していない (古い一覧が残り続ける)")
	}
	// 🚨 スキャン飛行中でも取りこぼさない (一覧が古いと分かっている経路なので予約に回す)
	v.Scanning, v.RescanPending = true, false
	if cmd := v.HandleKey("e", vp(10)); cmd != nil {
		t.Error("飛行中に 2 本目の探索を発行した (single-flight が効いていない)")
	}
	if !v.RescanPending {
		t.Error("飛行中の取り直しが予約されていない (実体なしのまま古い一覧が残る)")
	}
	if text, ok := testenv.NoticeOKOf(v.TakeNotice()); text == "" || ok {
		t.Errorf("理由を通知していない: text=%q ok=%v", text, ok)
	}
}

// 本文モードで開いている issue が再スキャンで移動していたら、新しい場所へ繋ぎ直す。
// 🚨 追わないと、読んでいる最中に n / 別プロセスで done/ へ移された issue が「実体から外れた本文」
// になり、y が消えたパスをコピーし e は実体確認で弾かれる (編集も取り直しもできない)。
func TestIssuesViewRebindOpenFollowsMove(t *testing.T) {
	dir := t.TempDir()
	rel := "001-feat-x.md"
	before := &issues.Issue{Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: "001", Category: "feat"}
	if err := os.WriteFile(before.Path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(before)
	v.HandleKey("enter", vp(10))
	if v.Open == nil {
		t.Fatal("本文モードに入れていない")
	}

	// done/ へ移動した状態のスキャン結果が届く (パスが変わる = 旧パスは見つからない)
	doneDir := filepath.Join(dir, "done")
	if err := os.MkdirAll(doneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	after := &issues.Issue{Path: filepath.Join(doneDir, rel), Dir: dir, Rel: "done/" + rel, Number: "001", Category: "feat"}
	v.Receive(IssuesScanMsg{Dirs: []string{dir}, Issues: []*issues.Issue{after}})

	if v.Open == nil {
		t.Fatal("移動しただけで本文モードが畳まれた (一覧へ引き戻している)")
	}
	if v.Open.Path != after.Path {
		t.Errorf("移動先へ繋ぎ直していない: open=%q want=%q", v.Open.Path, after.Path)
	}
}

// どこにも無くなったら本文モードを畳んで理由を通知する (消えたファイルの内容を最新として
// 見せ続けない)。
func TestIssuesViewRebindOpenDiscardsWhenGone(t *testing.T) {
	dir := t.TempDir()
	rel := "001-feat-x.md"
	iss := &issues.Issue{Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: "001", Category: "feat"}
	if err := os.WriteFile(iss.Path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(iss)
	v.HandleKey("enter", vp(10))
	if v.Open == nil {
		t.Fatal("本文モードに入れていない")
	}

	v.Receive(IssuesScanMsg{Dirs: []string{dir}}) // 1 件も無い = 消えた

	if v.Open != nil {
		t.Errorf("消えた issue の本文モードに留まっている: open=%q", v.Open.Path)
	}
	if v.Body != nil {
		t.Error("消えた issue の本文を保持し続けている")
	}
	if text, ok := testenv.NoticeOKOf(v.TakeNotice()); text == "" || ok {
		t.Errorf("理由を通知していない: text=%q ok=%v", text, ok)
	}
}

// 「一覧の生成 (scanIssues = Scan + 全件 LoadMeta) は issue の全文を読まない」(issue 050) の
// 回帰テスト (issue 052)。観測点は issues.BytesReadForTest (読んだ累計バイト数の差分)。
// LoadMeta は 64KB バッファの初回 Read で頭だけ読んで H1 で打ち切るため、それより十分
// 大きい本文を用意し「読んだ量 < ファイルサイズ」を assert する。scanIssues に全文読み
// (iss.ReadBody() 等) が紛れると読んだ量がサイズを超えて red になる (050 の敵対的レビュー
// R3 が実証した「表示が変わらないのに全 green」の変異を、この観測点は検出できる)。
func TestScanIssuesDoesNotReadFullBody(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# 001 feat: x\n\n" + strings.Repeat("本文の行 (LoadMeta はここまで来ない)\n", 5000) // ~250KB
	if err := os.WriteFile(filepath.Join(dir, "001-feat-x.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	size := int64(len(content))

	before := issues.BytesReadForTest()
	msg := scanOf(t, root)
	read := issues.BytesReadForTest() - before

	if len(msg.Issues) != 1 {
		t.Fatalf("issue が 1 件見つかるはず: %d 件", len(msg.Issues))
	}
	if read == 0 {
		t.Fatal("読んだバイト数が観測できていない (LoadMeta が観測点を通っていない)")
	}
	if read >= size {
		t.Errorf("scan 経路が全文を読んでいる: read=%d size=%d", read, size)
	}
}

// 同名が複数ある異常 (spec 3 節が警告する状態) では、どれが本人か決められないので繋ぎ直さない。
// 🚨 畳むのは「どこにも無い」ときだけなので、ここでは本文モードを維持する。
// 移動を追う照合は同じ issue dir の中だけ (issue 665)。root の issues/ と macOS/issues/ を両方持つ
// repo で、開いていた issue が消え、もう一方の dir に見出しまで同じ basename が 1 件あっても、
// そちらへ黙って繋ぎ直さない (どこにも無い = 畳んで知らせる)。
func TestIssuesViewRebindOpenStaysInsideIssueDir(t *testing.T) {
	root := t.TempDir()
	dirA, dirB := filepath.Join(root, "issues"), filepath.Join(root, "macOS", "issues")
	rel := "001-feat-x.md"
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, rel), []byte("# 001 feat: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &issues.Issue{Path: filepath.Join(dirA, rel), Dir: dirA, Rel: rel, Number: "001", Category: "feat", Title: "x"}
	b := &issues.Issue{Path: filepath.Join(dirB, rel), Dir: dirB, Rel: rel, Number: "001", Category: "feat", Title: "x"}
	v := loadedView(a)
	v.HandleKey("enter", vp(10))
	if v.Open == nil || v.Open.Path != a.Path {
		t.Fatal("本文モードに入れていない")
	}

	v.Receive(IssuesScanMsg{Dirs: []string{dirA, dirB}, Issues: []*issues.Issue{b}}) // a は消え、b だけが残る

	if v.Open != nil {
		t.Errorf("別の issue dir の同名 issue へ繋ぎ直した: open=%q", v.Open.Path)
	}
}

func TestIssuesViewRebindOpenKeepsOpenOnAmbiguousBase(t *testing.T) {
	dir := t.TempDir()
	rel := "001-feat-x.md"
	iss := &issues.Issue{Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: "001", Category: "feat"}
	if err := os.WriteFile(iss.Path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(iss)
	v.HandleKey("enter", vp(10))

	// 同名が 2 箇所 (done/ と next/) にある結果が届く
	dup := []*issues.Issue{
		{Path: filepath.Join(dir, "done", rel), Dir: dir, Rel: "done/" + rel, Number: "001", Category: "feat"},
		{Path: filepath.Join(dir, "next", rel), Dir: dir, Rel: "next/" + rel, Number: "001", Category: "feat"},
	}
	v.Receive(IssuesScanMsg{Dirs: []string{dir}, Issues: dup})

	if v.Open == nil {
		t.Fatal("同名が複数あるだけで本文モードを畳んだ (どれが本人か決められないだけで実体はある)")
	}
	if v.Open.Path != iss.Path {
		t.Errorf("曖昧なのに繋ぎ替えた: open=%q", v.Open.Path)
	}
}

// n の next/ 移動は、別のスキャンが飛行中でも一覧へ反映されなければならない。
// 🚨 scanCmd は single-flight で飛行中は nil を返す。markNextKey がそれをそのまま返すと、
// 実ファイルは動いたのに一覧は旧位置を出し続ける (しかも飛行中のスキャンは移動より前に
// 開始されているので、その結果が届いても移動は映らない)。
func TestIssuesViewMarkNextReflectsWhileScanning(t *testing.T) {
	iss := issuesviewtest.RealIssue(t)
	v := loadedView(iss)

	// 別経路のスキャンを飛ばした状態にする (r 連打・外部編集の自動反映などで普通に起きる)
	if cmd := v.ScanCmd(v.Cwd); cmd == nil {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}
	if !v.Scanning {
		t.Fatal("前提が崩れた: scanning が立っていない")
	}

	v.HandleKey("n", vp(10)) // 確認モーダル
	if !v.MarkNext.Active {
		t.Fatal("n で確認モーダルが出ない")
	}
	v.HandleKey("y", vp(10)) // 実行 (実ファイルが next/ へ動く)

	if _, err := os.Stat(filepath.Join(iss.Dir, issues.NextDirName, iss.Rel)); err != nil {
		t.Fatalf("実ファイルが next/ へ動いていない: %v", err)
	}

	// 飛行中スキャンの結果が届く。🚨 これは移動より前に始まったので旧位置しか含まない
	stale := IssuesScanMsg{Dirs: []string{iss.Dir}, Issues: []*issues.Issue{iss}}
	cmd := v.Receive(stale)
	if cmd == nil {
		t.Fatal("移動を反映する取り直しが張られない (一覧が旧位置のまま残る)")
	}
	// 🚨 張り直しは 1 回だけ。予約を消さないと receive ごとに scan を張り続け、loading() が
	// 永久 true になってスピナーも止まらない (無限スキャン)。
	if v.RescanPending {
		t.Error("予約が消費されていない (次の receive でも張り直して無限に続く)")
	}
	v.Receive(stale) // 1 本目の張り直しの結果が届いた形。ここで scanning は落ちる
	if again := v.Receive(stale); again != nil {
		t.Error("2 回目以降の receive でも取り直しが張られる (予約が sticky)")
	}
}

// エディタ復帰後の取り直しも同じ保証が要る。🚨 落とすと「保存したのに一覧・本文が古い」に
// なり、しかも飛行中のスキャンは編集より前に始まっているのでその結果でも直らない。
func TestIssuesViewReloadAfterEditReflectsWhileScanning(t *testing.T) {
	iss := issuesviewtest.RealIssue(t)
	v := loadedView(iss)
	if cmd := v.ScanCmd(v.Cwd); cmd == nil || !v.Scanning {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}

	if cmd := v.ReloadAfterEdit(); cmd != nil {
		t.Fatal("飛行中なのに 2 本目の探索を発行した (single-flight が効いていない)")
	}
	cmd := v.Receive(IssuesScanMsg{Dirs: []string{iss.Dir}, Issues: []*issues.Issue{iss}})
	if cmd == nil {
		t.Fatal("編集を反映する取り直しが張られない (保存したのに古い内容が残る)")
	}
}

// 閉じたら取り直しの予約も捨てる (片付けは finishClose に一本化されている)。
// 🚨 残すと非表示の viewer のためにスキャンが 1 回走り、その間スピナーの tick が昂進する。
func TestIssuesViewCloseDropsRescanReservation(t *testing.T) {
	iss := issuesviewtest.RealIssue(t)
	v := loadedView(iss)
	if cmd := v.ScanCmd(v.Cwd); cmd == nil {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}
	if cmd := v.ReloadAfterEdit(); cmd != nil || !v.RescanPending {
		t.Fatalf("前提が崩れた: 予約が立っていない (cmd=%v pending=%v)", cmd != nil, v.RescanPending)
	}

	v.Close() // closeAnimOff なので即座に finishClose まで通る
	if v.RescanPending {
		t.Error("閉じたのに取り直しの予約が残っている")
	}
	if cmd := v.Receive(IssuesScanMsg{Dirs: []string{iss.Dir}, Issues: []*issues.Issue{iss}}); cmd != nil {
		t.Error("閉じた viewer のためにスキャンを張り直した")
	}
}

func TestIssuesViewURLPickerNone(t *testing.T) {
	v := newTestIssuesView()
	v.Shown, v.Loaded = true, true
	v.Body = issues.NewBody("URL の無い本文\n")
	v.Open = issuesviewtest.FakeIssue("001", "feat", "a", issues.StatusOpen)
	v.HandleKey("u", vp(20))
	if v.UrlPick.Active {
		t.Error("URL が無いのにピッカーが開いた")
	}
	if !strings.Contains(v.Notice, "URL はありません") {
		t.Errorf("URL 不在の案内が出ない: %q", v.Notice)
	}
}

// 一覧モードでは u は効かない (本文を読んでいないため)。
func TestIssuesViewOpenURLListModeIsNoop(t *testing.T) {
	testenv.StubBrowserFunc(t, func(string) error { t.Fatal("一覧モードでブラウザを開いた"); return nil })

	v := loadedView(issuesviewtest.SampleIssues()...)
	if cmd := v.HandleKey("u", vp(20)); cmd != nil {
		t.Error("一覧モードで u が Cmd を返した")
	}
	if v.UrlPick.Active {
		t.Error("一覧モードでピッカーが開いた")
	}
}

// 別の issue を開いたらピッカーの状態を持ち越さない。
func TestIssuesViewURLPickerResetsOnNewIssue(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "001-feat-a.md")
	p2 := filepath.Join(dir, "002-feat-b.md")
	if err := os.WriteFile(p1, []byte("# a\n\nA https://example.com/1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("# b\n\nC https://example.com/3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(
		issuesviewtest.Path2Issue(dir, "001-feat-a.md"),
		issuesviewtest.Path2Issue(dir, "002-feat-b.md"),
	)
	v.HandleKey("enter", vp(20))
	v.HandleKey("u", vp(20))
	v.HandleKey("1", vp(20)) // 検索語を入れた状態で
	if !v.UrlPick.Active || v.UrlPick.Query() == "" {
		t.Fatal("ピッカーの前提が崩れている")
	}
	v.HandleKey("esc", vp(20)) // 閉じて一覧へ
	v.HandleKey("h", vp(20))
	v.HandleKey("j", vp(20))
	v.HandleKey("enter", vp(20))
	if v.UrlPick.Active || v.UrlPick.Query() != "" {
		t.Errorf("別の issue へ状態が持ち越された: active=%v query=%q", v.UrlPick.Active, v.UrlPick.Query())
	}
}

// キー処理と描画は同じ幅・同じヘッダーから page を分割する。
//
// ずれると半ページ移動の距離やカーソルと窓の関係が静かに食い違う (描画側には収束処理があるので
// 症状から原因へ辿り着けない)。以前はキー側が幅を知らず幅 0 で数えていたため「ヘッダーは折り返しては
// いけない」という暗黙の前提を抱えていた。幅を渡すようにしたので、折り返すヘッダーを足しても
// この一致さえ保てばよい — それをここで固定する。
func TestIssuesLayoutAgreesBetweenKeysAndRender(t *testing.T) {
	// 窓を必ず埋める件数にする (足りないと描画の行数が件数で決まり、分割の一致を測れない)
	many := make([]*issues.Issue, 0, 40)
	for i := range 40 {
		many = append(many, issuesviewtest.FakeIssue(fmt.Sprintf("%03d", i+1), "feat", "x", issues.StatusOpen))
	}
	for _, c := range []struct {
		name     string
		warnings []string
		query    string // 番号フィルタの検索語 ("" = 絞り込みなし)
	}{
		{"一覧", nil, ""},
		{"一覧 + 警告", []string{strings.Repeat("同名ファイルが複数あります ", 12)}, ""},
		// 絞り込み中はタブ行がヘッダーごと差し替わる。行数が変わるとキー側と描画側の page 分割が
		// ずれるので、置き換え先も同じ高さであることをここで縛る。検索語 "0" は全件に一致する
		// (番号は 001..040) — 窓を埋めないと分割の一致を測れないため
		{"番号で絞り込み中", nil, "0"},
		{"番号で絞り込み中 + 警告", []string{strings.Repeat("同名ファイルが複数あります ", 12)}, "0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, w := range []int{24, 40, 84, 200} {
				v := loadedView(many...)
				v.warnings = c.warnings
				if c.query != "" {
					v.HandleKey("/", vp(20))
					for _, r := range c.query {
						v.HandleKey(string(r), vp(20))
					}
				}
				o := IssuesRenderOpts{Width: w, Page: 20}
				// 描画が実際に出した行数 (ヘッダーを除く) とキー側の行数を突き合わせる
				body := len(v.ListLines(o)) - len(v.listHeadLines(w, false))
				if got := v.VisibleRows(o.Viewport()); got != body {
					t.Fatalf("幅 %d: キー側 %d 行 / 描画が出した %d 行", w, got, body)
				}
			}
		})
	}
	// 本文側の「キーと描画の一致」はここで見ない: bodyHeadLines は幅によらず 3 行で、期待値を
	// production と同じ式で作るしかなく、headWidth の本文の分岐を消しても緑のままだった (issue 751)。
	// 本文側で残す価値があるのは下の下限クランプだけ
	// 🚨 上の一覧の比較は期待値を production と同じ式で作っているので、**下限クランプを外す変異を
	// 検出できない** (issue 198 発見 2)。page がヘッダー行数以下のときに 1 を返すことを、
	// 式ではなく**リテラルの 1** で固定する。0 や負を返すとキー処理 (bodyOff の計算・
	// ページ送り) が空回りするか、末尾を超えてスクロールする。
	t.Run("page がヘッダー行数以下でも 1 行は残す", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "001-feat-x.md")
		if err := os.WriteFile(path, []byte("# 001 feat: x\n\n"+strings.Repeat("本文。\n", 60)), 0o644); err != nil {
			t.Fatal(err)
		}
		iss := issuesviewtest.Path2Issue(dir, "001-feat-x.md")
		for _, page := range []int{0, 1, 2} {
			// 一覧側 (ヘッダーは listHeadLines)
			v := loadedView(iss)
			if got := v.VisibleRows(IssuesRenderOpts{Width: 40, Page: page}.Viewport()); got != 1 {
				t.Fatalf("一覧 page=%d: visibleRows=%d; want 1 (下限クランプ)", page, got)
			}
			// 本文側 (ヘッダーは bodyHeadLines。行数が違うので別に見る)
			v2 := loadedView(iss)
			v2.HandleKey("enter", vp(20))
			if v2.Open == nil {
				t.Fatal("本文モードに入れていない")
			}
			if got := v2.VisibleRows(IssuesRenderOpts{Width: 40, Page: page}.Viewport()); got != 1 {
				t.Fatalf("本文 page=%d: visibleRows=%d; want 1 (下限クランプ)", page, got)
			}
		}
	})
}

// 再スキャンをまたいで選択を保つ (錨をパスで張り替える)。
//
// 外部編集の即時反映が入って、選択している最中に取り直しが走るのが普通になった。畳むと
// Claude Code が issue を書くたびに選択が消えて実用にならない。
func TestIssuesViewKeepsSelectionAcrossRescan(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	v.HandleKey("a", vp(20)) // pending も出して行を増やす
	v.HandleKey("J", vp(20)) // 2 行選択 (錨 = 先頭行)
	lo, hi, ok := v.Selection()
	if !ok || hi-lo != 1 {
		t.Fatalf("前提が崩れた: lo=%d hi=%d ok=%v", lo, hi, ok)
	}
	anchor, head := v.Rows[lo].Path, v.Rows[hi].Path

	// 🚨 再スキャンでは錨より前に issue を 1 件足す。同じ並びのまま返すと、選択を位置 (index) で
	// 保つ実装と区別できない (issue 667: 錨を index で保つ変異でも緑だった)
	rescanned := append([]*issues.Issue{issuesviewtest.FakeIssue("031", "feat", "new", issues.StatusOpen)}, issuesviewtest.SampleIssues()...)
	v.Receive(IssuesScanMsg{Dirs: []string{"/repo/issues"}, Issues: rescanned})

	lo2, hi2, ok2 := v.Selection()
	if !ok2 {
		t.Fatal("再スキャンで選択が消えた")
	}
	if v.Rows[lo2].Path != anchor || v.Rows[hi2].Path != head {
		t.Fatalf("選択が別の issue を指した: %q..%q", v.Rows[lo2].Path, v.Rows[hi2].Path)
	}
	// タブ・フィルタの切り替えは行集合の意味が変わるので畳んだまま
	v.HandleKey("tab", vp(20))
	if _, _, ok := v.Selection(); ok {
		t.Error("タブ切り替えで選択が残った (別の行集合へ範囲を持ち越している)")
	}
}

// 通知文は issue のファイル名・本文由来の URL を素で埋め込むので、setNotice 自体を関門にする
// (呼び出しごとに包む方式だと必ずどこかが漏れる)。status viewer 側と対の規律。
func TestIssuesNoticeIsSanitized(t *testing.T) {
	const esc, bel = "\x1b", "\a"
	v := newTestIssuesView()
	v.setNotice("URL を開きます: https://example.com/"+esc+"]0;pwned"+bel+"x", app.NoticeOK)
	if testenv.HasTerminalControl(v.Notice) {
		t.Errorf("通知に制御シーケンスが残った: %q", v.Notice)
	}
	if strings.Contains(v.Notice, "pwned") {
		t.Errorf("OSC の中身が通知に残った: %q", v.Notice)
	}
}

// hint が案内する本文モードのキーは、全部が実際に効かなければならない。
//
// 🚨 spec 5 節が「hint が案内するキーが 1 つも案内どおりに動かない」を既知の事故として挙げて
// いるのに、これまでその不変条件はテストで固定されていなかった (案内キーを未割当の z に
// 書き換えても全テストが green だった)。ここで hint 側から駆動して閉じる。
//
// 🚨 「案内された集合」と「効果を検証している表」の一致まで見る。片方だけ増やせないので、
// hint にキーを足したら効果の検証も必ず書くことになる。
// 本文 pager の半ページ送り・端ジャンプが glide の配線ごと効くことを**キー経路**で固定する。
//
// 🚨 TestHalfPageScrollGlidesOnAllSurfaces の本文サブテストは glide を startPagerGlide で直接立てる
// (body が nil の workaround) ため、handleBodyKey が glide を配線し忘れても green のままだった。
// term.PagerScrollKey への委譲 (2026-08-19) で glide を落とす退行はこのテストだけが検出する。
func TestIssuesViewBodyHalfPageGlidesViaKeyPath(t *testing.T) {
	e := newBodyKeyEnv(t)
	before := e.v.BodyPager.Offset
	e.press(" ")
	if !e.v.BodyPager.Animating() {
		t.Fatal("Space の半ページ送りが glide に載っていない (キー経路)")
	}
	if got := pagerShown(&e.v.BodyPager); got != before {
		t.Errorf("glide 開始位置 = %d, want %d (移動前の offset)", got, before)
	}
	e.press("G")
	if e.v.BodyPager.Animating() {
		t.Error("G の端ジャンプで glide が残っている (端ジャンプは即時)")
	}
}

// 表示順の並べ替えは human を右へ寄せない (0 件でも All の直後に固定する)。
func TestReorderTabsKeepsHumanPinnedAtZero(t *testing.T) {
	tabs := []issues.Tab{{Name: issues.HumanTab}, {Name: "a"}, {Name: "b"}}
	counts := []int{0, 0, 4} // human 0 件・a 0 件・b 4 件
	gotTabs, gotCounts := reorderTabsByCount(tabs, counts)
	names := []string{gotTabs[0].Name, gotTabs[1].Name, gotTabs[2].Name}
	if want := []string{issues.HumanTab, "b", "a"}; !slices.Equal(names, want) {
		t.Errorf("並び = %v, want %v (human は 0 件でも先頭)", names, want)
	}
	if want := []int{0, 4, 0}; !slices.Equal(gotCounts, want) {
		t.Errorf("件数の並び = %v, want %v", gotCounts, want)
	}
}

// 本文モードでも i で viewer が閉じる (issue 122)。
//
// --help と README は i を「viewer 内のキー」として案内しているのに、handleBodyKey に case が
// 無く default の term.PagerScrollKey へ落ちて offset 不変で返っていた = **押しても何も起きず理由も
// 出ない**。同じ関数の case "s" には「本文だけ沈黙すると案内が嘘になる」という同型の根拠が
// 既に書かれている。
func TestIssuesViewerBodyModeIClosesViewer(t *testing.T) {
	v := loadedView(issuesviewtest.RealIssue(t))
	v.openBody() // 本文モードへ
	if v.Body == nil {
		t.Fatal("前提が崩れた: 本文が開いていない")
	}

	v.HandleBodyKey("i", IssuesViewport{Width: 80, Page: 20}, 20)

	if !v.Closing && v.Shown {
		t.Error("本文モードの i で viewer が閉じない (--help の案内が嘘になる)")
	}
}

// R は一覧モードでも本文モードでも ratelimit ダッシュボードへ横断する (ユーザー要望 2026-09-01)。
// s (status への横断) と同じ扱いで、viewer は閉じてから browseModel が開く (全画面は同時に 1 枚)。
// 🚨 本文モードも見る: 横断の表引きが一覧の switch の中にあると、本文は term.PagerScrollKey へ落ちて無音になる (issue 122 と同型)。
func TestIssuesViewerRSwitchesToRatelimitDash(t *testing.T) {
	t.Run("一覧モード", func(t *testing.T) {
		v := loadedView(issuesviewtest.RealIssue(t))
		if v.Body != nil {
			t.Fatal("前提が崩れた: 一覧モードでない")
		}
		v.HandleKey("R", vp(20))
		if v.TakeWantCross() != app.FullScreenRatelimit {
			t.Error("一覧モードの R でダッシュボードへの横断を要求しない")
		}
		if !v.Closing && v.Shown {
			t.Error("一覧モードの R で viewer が閉じない")
		}
	})
	t.Run("本文モード", func(t *testing.T) {
		v := loadedView(issuesviewtest.RealIssue(t))
		v.openBody()
		if v.Body == nil {
			t.Fatal("前提が崩れた: 本文が開いていない")
		}
		v.HandleKey("R", vp(20))
		if v.TakeWantCross() != app.FullScreenRatelimit {
			t.Error("本文モードの R でダッシュボードへの横断を要求しない (--help の案内が嘘になる)")
		}
		if !v.Closing && v.Shown {
			t.Error("本文モードの R で viewer が閉じない")
		}
	})
	// 一度きりの信号 (takeNotice と同じ語彙。取り出した後は落ちている)
	v := loadedView(issuesviewtest.RealIssue(t))
	v.HandleKey("R", vp(20))
	v.TakeWantCross()
	if v.TakeWantCross() != app.FullScreenNone {
		t.Error("takeWantCross が 2 回横断先を返す (横断が二重に起きる)")
	}
}

// 一覧モードの u は無音で消えず、理由を返す (issue 122)。
//
// u は git log 一覧では pull、本文では URL ピッカー、status viewer では「pull は p です」を
// 返すのに、issues 一覧だけが無音だった。効かせない理由は openURLPicker の doc にあるが、
// それが画面に出ていなかった。
func TestIssuesViewerListModeUReturnsReason(t *testing.T) {
	v := loadedView(issuesviewtest.RealIssue(t))
	if v.Body != nil {
		t.Fatal("前提が崩れた: 一覧モードでない")
	}

	v.HandleKey("u", vp(20))

	notice, _ := testenv.NoticeOKOf(v.TakeNotice())
	if notice == "" {
		t.Error("一覧モードの u が無音 (押したのに何も起きない = 壊れて見える)")
	}
	if !strings.Contains(notice, "本文") {
		t.Errorf("理由が「本文を開いてから」を案内していない: %q", notice)
	}
}

// 本文を開いたまま J/K で隣の issue へ差し替える (docs/glogx-ui-guide.md §6)。
// 親行 (本文が無い) は飛ばし、端では止めて案内し、引き出しの演出は挟まない。
func TestIssuesViewBodyNeighborKeysSwapIssueWithoutReopening(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel, title string) *issues.Issue {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# "+title+"\n\n本文 "+title+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return issuesviewtest.Path2Issue(dir, rel)
	}
	a := mk("005-feat-a.md", "A")
	b := mk("004-feat-b.md", "B")
	child := fakeEpicIssue(dir, "cloud", "003", "child", issues.StatusOpen)
	mk(child.Rel, "CHILD")
	c := mk("002-feat-c.md", "C")
	v := loadedView(a, b, child, c)
	if got := len(v.displayRows); got != 4 || v.displayRows[2].kind != displayRowGroup {
		t.Fatalf("前提: 表示行が [a b ▸cloud c] でない: %+v", v.displayRows)
	}

	v.HandleKey("j", vp(10))
	v.HandleKey("enter", vp(10))
	v.Drawer.Finish()
	if v.Open != b {
		t.Fatalf("前提: b を開けていない: %+v", v.Open)
	}
	v.BodyPager.Offset = 3 // 次の issue で先頭へ戻ることを見るため、途中まで送った状態を作る

	v.HandleKey("J", vp(10))
	if v.Open != c || v.Cursor != 3 {
		t.Fatalf("J で親行を飛ばして c へ移らない: open=%v cursor=%d", v.Open, v.Cursor)
	}
	if v.Drawer.Phase() != anim.Open {
		t.Errorf("J で引き出しの演出が始まった (開いたままで中身だけ替える): phase=%v", v.Drawer.Phase())
	}
	if v.BodyPager.Offset != 0 {
		t.Errorf("J でスクロール位置が先頭へ戻らない: bodyOff=%d", v.BodyPager.Offset)
	}
	if out := strings.Join(v.Lines(renderOpts(20)), "\n"); !strings.Contains(out, "本文 C") {
		t.Errorf("J の後に c の本文が描かれない:\n%s", out)
	}

	v.HandleKey("J", vp(10))
	if v.Open != c {
		t.Fatalf("末尾の J で issue が変わった: %+v", v.Open)
	}
	if msg, _ := testenv.NoticeOKOf(v.TakeNotice()); !strings.Contains(msg, "最後") {
		t.Errorf("末尾の J で案内が出ない: %q", msg)
	}

	v.HandleKey("K", vp(10))
	if v.Open != b || v.Cursor != 1 {
		t.Fatalf("K で親行を飛ばして b へ戻らない: open=%v cursor=%d", v.Open, v.Cursor)
	}
	v.HandleKey("shift+up", vp(10))
	if v.Open != a || v.Cursor != 0 {
		t.Fatalf("shift+↑ で a へ戻らない: open=%v cursor=%d", v.Open, v.Cursor)
	}
	v.HandleKey("K", vp(10))
	if msg, _ := testenv.NoticeOKOf(v.TakeNotice()); v.Open != a || !strings.Contains(msg, "最初") {
		t.Errorf("先頭の K で止まらない / 案内が出ない: open=%v notice=%q", v.Open, msg)
	}
}

// issuesviewtest.Path2Issue は dir 配下の rel を単独 issue として組む (ファイル名から番号・カテゴリを読む)。
// 同じ basename でも見出しが違うなら本人とみなさない (issue 277)。
//
// basename は「番号 + スラッグ」なので、番号の一意性が守られている限り basename 一致 = 本人。
// それが壊れた repo や group が増えた状況では別 issue でありうり、そのとき症状は
// 「読んでいた本文が静かに別の issue に差し替わる」= 利用者からは気づけない形になる。
// 見出しが変わっていたら **知らせる。ただし本文は捨てない** (issue 277 → 敵対的レビュー 2026-09-06)。
//
// 🚨 初版は discardBody + 「同名の別 issue が見つかりました (本文は閉じます)」にしていたが、
// 見出しの不一致は「別 issue」と「同じ issue を移動と同時に改題した」を区別できない。
// 後者は done/ へ移しつつ H1 も直したコミットを pull するだけで起き (200ms の debounce が
// 両イベントを 1 回の再スキャンへ畳む)、**自分で直した見出しのせいで本文が飛ぶ**。
// 黙って入れ替わるのを防ぐ目的は notice で足りる。
func TestIssuesRebindOpenWarnsOnDifferentTitle(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	// 同じ basename・違う見出し
	path := filepath.Join(other, "300-bug-same-name.md")
	if err := os.WriteFile(path, []byte("# 300 bug: 別の issue\n\n本文\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := newTestIssuesView()
	v.Shown, v.Loaded = true, true
	v.Open = &issues.Issue{Number: "300", Title: "300 bug: 元の issue", Path: filepath.Join(dir, "300-bug-same-name.md")}
	replacement := &issues.Issue{Number: "300", Path: path}
	v.All = []*issues.Issue{replacement}

	v.rebindOpen(v.Open.Path)

	if v.Open != replacement {
		t.Error("繋ぎ直していない (移動 + 改題を同じサイクルで受けると読んでいた位置が飛ぶ)")
	}
	if v.Notice == "" {
		t.Error("見出しが変わったのに利用者へ何も言っていない (黙って中身が入れ替わる)")
	}
	if strings.Contains(v.Notice, "閉じます") {
		t.Errorf("本文を閉じる文言が残っている: %q", v.Notice)
	}

	// 陽性対照 1: 見出しが同じなら繋ぎ直し、**notice は出さない**
	// (これが無いと「常に notice を出す」実装でも上の assert が通る)
	same := filepath.Join(other, "301-bug-moved.md")
	if err := os.WriteFile(same, []byte("# 301 bug: 移動した\n\n本文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	movedIss := &issues.Issue{Number: "301", Path: same}
	v2 := newTestIssuesView()
	v2.Shown, v2.Loaded = true, true
	v2.Open = &issues.Issue{Number: "301", Title: "301 bug: 移動した", Path: filepath.Join(dir, "301-bug-moved.md")}
	v2.All = []*issues.Issue{movedIss}
	v2.rebindOpen(v2.Open.Path)
	if v2.Open != movedIss {
		t.Error("見出しが同じなら繋ぎ直すべき (移動を追う機能が死んでいる)")
	}
	if v2.Notice != "" {
		t.Errorf("見出しが同じなのに notice を出した: %q", v2.Notice)
	}

	// 陽性対照 2: どこにも無いときは従来どおり畳む (「捨てない」を全経路へ広げていないこと)
	v3 := newTestIssuesView()
	v3.Shown, v3.Loaded = true, true
	v3.Open = &issues.Issue{Number: "302", Title: "302 bug: 消えた", Path: filepath.Join(dir, "302-bug-gone.md")}
	v3.All = nil
	v3.rebindOpen(v3.Open.Path)
	if !strings.Contains(v3.Notice, "見つかりません") {
		t.Errorf("消えた issue は畳んで知らせるべき: notice=%q", v3.Notice)
	}
}

// issue 753: 制御文字を含む値 (PR で入りうるファイル名など) はクリップボードへ入れない。ESC や
// bracketed paste の終わり (ESC[201~) を入れると、貼った先の端末が解釈する。画面は無害化されているので見えない。
func TestIssuesViewRefusesToCopyControlChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "001-feat-a\x1b[201~b.md"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, _ := issues.Scan([]string{dir})
	if len(found) != 1 {
		t.Fatalf("前提が崩れた: 制御文字を含む名前の issue が走査に乗らない (%d 件)", len(found))
	}
	for _, key := range []string{"y", "Y"} { // パス / 参照 (どちらも生のパスを含む)
		copied := testenv.StubClipboard(t)
		v := loadedView(found...)
		v.Root = filepath.Dir(dir)
		v.HandleKey(key, vp(10))
		if *copied != "" {
			t.Fatalf("%s: 制御文字を含む値をコピーした: %q", key, *copied)
		}
		if !strings.Contains(v.Notice, "制御文字") {
			t.Fatalf("%s: 断ったことを知らせていない: %q", key, v.Notice)
		}
	}
	// 複数件をまとめてコピーする経路も同じく断る
	multi := testenv.StubClipboard(t)
	mv := loadedView(found...)
	mv.copyLines([]string{"/ok/002-feat-c.md", found[0].Path}, "パス")
	if *multi != "" || !strings.Contains(mv.Notice, "制御文字") {
		t.Fatalf("複数件: 制御文字を含む値をコピーした: %q (通知 %q)", *multi, mv.Notice)
	}
	// 普通の名前は今までどおりコピーする (断る判定が広すぎないこと)
	copied := testenv.StubClipboard(t)
	v := loadedView(issuesviewtest.Path2Issue(dir, "002-feat-c.md"))
	v.HandleKey("y", vp(10))
	if *copied != filepath.Join(dir, "002-feat-c.md") {
		t.Fatalf("普通のパスをコピーしない: %q", *copied)
	}
}

// 行番号の溝は番号だけを薄くし、区切りの空白は塗らない (issue 757: gutter.Number を塗る形へ変えたときの見た目の約束)。
func TestSrcGutterPaintsOnlyTheNumber(t *testing.T) {
	if got, want := srcGutter(7, 4, true), term.AnsiDim+"  7"+term.AnsiReset+" "; got != want {
		t.Fatalf("srcGutter(7,4,colored) = %q, want %q", got, want)
	}
	if got := srcGutter(0, 4, true); got != "    " {
		t.Fatalf("続き行の溝は空白だけのはず: %q", got)
	}
}
