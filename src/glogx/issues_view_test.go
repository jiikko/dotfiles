package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"time"

	"glogx/issues"
)

// atProgress は演出の進みを p (0..1) に固定した viewer を返す (壁時計を巻き戻して作る)。
func atProgress(v *issuesView, p float64) *issuesView {
	v.animStart = timeNow().Add(-time.Duration(float64(issuesAnimDuration) * p))
	return v
}

// issuesView は browseModel を参照しないので、モデルを組まずに直接駆動できる
// (この独立性が「結合を最後にする」を成立させている。壊れたら結合コストが跳ね上がる)。

// fakeIssue はファイルシステムを使わずに Issue を作る (一覧の表示・フィルタのテスト用)。
func fakeIssue(number, category, slug string, st issues.Status) *issues.Issue {
	rel := number + "-" + category + "-" + slug + ".md"
	if st != issues.StatusOpen {
		rel = st.String() + "/" + rel
	}
	return &issues.Issue{
		Path:     "/repo/issues/" + rel,
		Dir:      "/repo/issues",
		Rel:      rel,
		Number:   number,
		Category: category,
		Slug:     slug,
		Status:   st,
	}
}

// loadedView は receive 済みの viewer を返す。
func loadedView(list ...*issues.Issue) *issuesView {
	v := newTestIssuesView()
	v.shown = true
	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: list})
	return &v
}

func sampleIssues() []*issues.Issue {
	return []*issues.Issue{
		fakeIssue("030", "feat", "a", issues.StatusOpen),
		fakeIssue("029", "feat", "b", issues.StatusOpen),
		fakeIssue("028", "refactor", "c", issues.StatusPending),
		fakeIssue("027", "refactor", "d", issues.StatusDone),
		fakeIssue("026", "bug", "e", issues.StatusDone),
	}
}

// vp は窓の寸法 (キー処理へ渡す)。幅は renderOpts と同じにする — テストだけ幅 0 で駆動すると、
// 幅で行数が変わるヘッダーを足したときに本番でだけ page 分割がずれる。
func vp(page int) issuesViewport { return renderOpts(page).viewport() }

func renderOpts(page int) issuesRenderOpts {
	return issuesRenderOpts{width: 80, page: page, colored: false}
}

func TestIssuesViewToggleRescansKeepingLastGood(t *testing.T) {
	v := newTestIssuesView()
	if cmd := v.toggle("/repo"); cmd == nil {
		t.Fatal("初回の toggle でスキャンの Cmd が返らない")
	}
	if !v.visible() || !v.loading() {
		t.Fatalf("表示・スキャン中になっていない: shown=%v scanning=%v", v.visible(), v.loading())
	}
	if cmd := v.scanCmd("/repo"); cmd != nil {
		t.Fatal("スキャン中に 2 本目の探索を発行した (single-flight が効いていない)")
	}
	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: sampleIssues()})
	if v.loading() {
		t.Fatal("receive 後もスキャン中のまま")
	}
	v.toggle("/repo") // 閉じる
	if v.visible() {
		t.Fatal("2 回目の toggle で閉じない")
	}
	// ⚠️ 再表示では取り直す: 「初回だけ」にすると N (次の番号) が古い最大番号 + 1 を返し続け、
	// 番号の再利用を招く。ただし取り直し中も前回の結果を出したままにする (スピナーで瞬かない)。
	if cmd := v.toggle("/repo"); cmd == nil {
		t.Fatal("再表示で取り直していない (N が古い番号を返し続ける)")
	}
	v.finishAnim() // 開く演出は別テストで見る (ここは取り直し中の中身を見たい)
	out := strings.Join(v.lines(renderOpts(10)), "\n")
	if strings.Contains(out, "探しています") || !strings.Contains(out, "030") {
		t.Fatalf("取り直し中に前回の結果が消えた:\n%s", out)
	}
}

func TestIssuesViewRescanReanchorsTabAndCursor(t *testing.T) {
	// 再スキャンは新しい *Issue を作る。位置を安定キー (タブ名・パス) で引き直さないと、
	// カーソルが別の issue へ滑り、タブが別カテゴリを指す (tabs は件数降順なので並びが変わる)。
	first := []*issues.Issue{
		fakeIssue("003", "feat", "c", issues.StatusOpen),
		fakeIssue("002", "feat", "b", issues.StatusOpen),
		fakeIssue("001", "feat", "a", issues.StatusOpen),
		fakeIssue("012", "refactor", "y", issues.StatusOpen),
		fakeIssue("011", "refactor", "x", issues.StatusOpen),
	}
	v := loadedView(first...)
	v.handleKey("tab", vp(10)) // human (All の右に常設。0 件でも席がある)
	v.handleKey("tab", vp(10)) // feat (3 件で先頭)
	v.handleKey("tab", vp(10)) // refactor
	if got := v.currentTab(); got != "refactor" {
		t.Fatalf("前提が崩れた: 選択タブが refactor でない (%q)", got)
	}
	v.handleKey("j", vp(10)) // 2 行目 (011)
	want := v.current().Path

	// refactor が 4 件になり、タブの並びが refactor → feat へ入れ替わる
	second := append([]*issues.Issue{
		fakeIssue("014", "refactor", "w", issues.StatusOpen),
		fakeIssue("013", "refactor", "v", issues.StatusOpen),
	}, first...)
	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: second})

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
	v := loadedView(sampleIssues()...)
	v.handleKey("tab", vp(10)) // All → human (常設タブ)
	v.handleKey("tab", vp(10)) // human → feat
	if got := v.currentTab(); got != "feat" {
		t.Fatalf("前提が崩れた: %q", got)
	}
	v.handleKey("ctrl+b", vp(10)) // feat → human
	v.handleKey("ctrl+b", vp(10)) // human → All
	if got := v.currentTab(); got != "" {
		t.Fatalf("ctrl+b で左へ戻らない: %q", got)
	}
	// tui.go の正規化を経た右移動 (ctrl+f → "right") と同じ経路も対称に効く
	v.handleKey("right", vp(10)) // All → human
	if got := v.currentTab(); got != issues.HumanTab {
		t.Fatalf("right で右へ動かない: %q", got)
	}
	// 端で止まらず巡回する (moveTab の契約) 方向にも ctrl+b が乗る。
	// ⚠️ All の左には疑似カテゴリ [next] が居るので、そこを 1 段挟んでから末尾へ回り込む
	v.handleKey("ctrl+b", vp(10)) // All
	v.handleKey("ctrl+b", vp(10)) // [next] (All の左)
	if got := v.currentTab(); got != tabNextName {
		t.Fatalf("All の左が [next] になっていない: %q", got)
	}
	v.handleKey("ctrl+b", vp(10)) // 末尾へ回り込む
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
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"})
	v.handleKey("enter", vp(10))

	fresh := &issues.Issue{
		Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat",
		Status: issues.StatusPending,
	}
	v.receive(issuesScanMsg{dirs: []string{dir}, issues: []*issues.Issue{fresh}})
	if v.open != fresh {
		t.Fatal("再スキャン後も破棄済みの Issue ポインタを掴んでいる")
	}
	if out := strings.Join(v.lines(renderOpts(10)), "\n"); !strings.Contains(out, "pending") {
		t.Fatalf("本文ヘッダーの状態が古いまま:\n%s", out)
	}
}

func TestIssuesViewTabsAndDoneFilter(t *testing.T) {
	v := loadedView(sampleIssues()...)
	// 既定は open だけ: open 2 行 (pending / done は伏せる)
	if len(v.rows) != 2 {
		t.Fatalf("既定の行数が違う: %d", len(v.rows))
	}
	// タブは human 0 (常設) / feat 2 / refactor 2 / (bug 1 は other へ)
	if len(v.tabs) != 4 || v.tabs[0].Name != issues.HumanTab || v.tabs[1].Name != "feat" {
		t.Fatalf("タブの組み立てが違う: %+v", v.tabs)
	}
	v.handleKey("a", vp(10)) // 1 段目: + pending
	if len(v.rows) != 3 {
		t.Fatalf("a 1 回で pending を含めていない: %d", len(v.rows))
	}
	v.handleKey("a", vp(10)) // 2 段目: + done
	if len(v.rows) != 5 {
		t.Fatalf("a 2 回で done を含めていない: %d", len(v.rows))
	}
	v.handleKey("a", vp(10)) // 3 段目で既定へ戻る
	if len(v.rows) != 2 {
		t.Fatalf("a 3 回で open のみへ戻らない: %d", len(v.rows))
	}
	v.handleKey("a", vp(10))
	v.handleKey("a", vp(10)) // 以降のタブ検証は全件表示で行う
	// Tab 巡回: All → human → feat → refactor → other → [next] → All
	// (human は All の右に固定、[next] は All の左に固定なので右回りでは All の 1 つ手前)
	names := []string{issues.HumanTab, "feat", "refactor", "other", tabNextName, ""}
	for _, want := range names {
		v.handleKey("tab", vp(10))
		if got := v.currentTab(); got != want {
			t.Fatalf("タブ巡回が想定と違う: want %q got %q", want, got)
		}
	}
	// feat タブでは feat の 2 件だけ (All → human → feat)
	v.handleKey("tab", vp(10))
	v.handleKey("tab", vp(10))
	if len(v.rows) != 2 {
		t.Fatalf("feat タブの行数が違う: %d", len(v.rows))
	}
	for _, iss := range v.rows {
		if iss.Category != "feat" {
			t.Fatalf("feat タブに %s が混ざった", iss.Rel)
		}
	}
}

// 画面幅よりカテゴリが多いとき、選択中のタブが必ず画面に出る (横スクロール。ユーザー要望
// 2026-08-01)。⚠️ 幅で切って捨てるだけだと、端のカテゴリを選んでも画面に現れず「選んだはずの
// タブがどこにも無い」状態になる (Tab で右へ進んでいるのに画面が動かない)。
func TestIssuesViewTabLineFollowsSelection(t *testing.T) {
	cats := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	list := make([]*issues.Issue, 0, len(cats)*2)
	num := 200
	for _, c := range cats {
		for range 2 { // TabMinCount 未満だと other へ寄せられてタブにならない
			num--
			list = append(list, fakeIssue(fmt.Sprintf("%03d", num), c, "x", issues.StatusOpen))
		}
	}
	v := loadedView(list...)
	const width = 40 // 全チップ (8 カテゴリ + All) は到底収まらない幅
	if len(v.tabs) < len(cats) {
		t.Fatalf("前提が崩れた: タブが %d 個しかない", len(v.tabs))
	}

	for i := range len(v.tabs) + 1 { // 0 = All、以降は各カテゴリ
		v.tabIdx = i
		v.refresh()
		line := v.tabLine(issuesRenderOpts{width: width})
		if w := dispWidth(line); w > width {
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
	v.refresh()
	if head := v.tabLine(issuesRenderOpts{width: width}); !strings.Contains(head, tabScrollRight) ||
		strings.Contains(head, tabScrollLeft) {
		t.Fatalf("先頭では右にだけ続きの印が要る: %q", head)
	}
	v.tabIdx = len(v.tabs)
	v.refresh()
	if tail := v.tabLine(issuesRenderOpts{width: width}); !strings.Contains(tail, tabScrollLeft) ||
		strings.Contains(tail, tabScrollRight) {
		t.Fatalf("末尾では左にだけ続きの印が要る: %q", tail)
	}
}

// 全部収まる幅では横スクロールの印を出さない (従来どおりの見た目)。
func TestIssuesViewTabLineNoMarksWhenAllFit(t *testing.T) {
	v := loadedView(sampleIssues()...)
	line := v.tabLine(issuesRenderOpts{width: 120})
	if strings.Contains(line, tabScrollLeft) || strings.Contains(line, tabScrollRight) {
		t.Fatalf("収まっているのに続きの印が出た: %q", line)
	}
}

// shift+↑/↓ で範囲を選び、y / p / Y がその範囲へ効く (ユーザー要望 2026-08-01)。
func TestIssuesViewMultiSelectYank(t *testing.T) {
	copied := stubClipboard(t)

	v := loadedView(sampleIssues()...)
	v.filter = issues.FilterAll
	v.refresh() // 5 件すべてを対象にする
	v.handleKey("shift+up", vp(10))
	// 1 回で「元の行 + 隣の行」= 2 行 (エディタ・Finder と同じ)。先頭行では動けないので
	// 錨と同じ行に留まり 1 行選択のまま
	v.cursor = 2
	v.marked, v.markAt = true, 0 // 0..2 の 3 行を選んだ状態にする

	v.handleKey("y", vp(10))
	want := v.rows[0].Path + "\n" + v.rows[1].Path + "\n" + v.rows[2].Path
	if *copied != want {
		t.Fatalf("選択範囲のパスがコピーされていない:\n got=%q\nwant=%q", *copied, want)
	}
	if text, ok := v.takeNotice(); !ok || !strings.Contains(text, "3 件のパスをコピーしました") {
		t.Fatalf("複数コピーの通知が想定と違う: %q", text)
	}
	// ⚠️ 通知に改行を含めない (トーストは 1 行。含めると枠が壊れる)
	v.handleKey("Y", vp(10))
	if text, _ := v.takeNotice(); strings.Contains(text, "\n") {
		t.Fatalf("通知に改行が入った: %q", text)
	}
	v.handleKey("p", vp(10))
	if *copied != v.rows[0].Number+"\n"+v.rows[1].Number+"\n"+v.rows[2].Number {
		t.Fatalf("選択範囲の番号がコピーされていない: %q", *copied)
	}
	// 単数のときの文言は変えない (複数選択を足したせいで普段の見た目が変わらないように)
	v.clearMark()
	v.handleKey("y", vp(10))
	if text, _ := v.takeNotice(); !strings.Contains(text, "パスをコピーしました: ") ||
		strings.Contains(text, "件の") {
		t.Fatalf("選択なしの通知が複数形になった: %q", text)
	}
}

// shift+↑/↓ の伸張と、選択が畳まれる経路。畳み忘れると「見えていない行がコピー対象」になる。
func TestIssuesViewMultiSelectExtendAndClear(t *testing.T) {
	newView := func() *issuesView {
		v := loadedView(sampleIssues()...)
		v.filter = issues.FilterAll
		v.refresh()
		v.cursor = 2
		v.handleKey("shift+up", vp(10)) // 1..2 を選択 (カーソルは 1 へ)
		return v
	}
	v := newView()
	if lo, hi, ok := v.selection(); !ok || lo != 1 || hi != 2 || v.cursor != 1 {
		t.Fatalf("shift+up の範囲が違う: lo=%d hi=%d ok=%v cursor=%d", lo, hi, ok, v.cursor)
	}
	v.handleKey("shift+down", vp(10)) // 錨 (2) へ戻る = 1 行
	if lo, hi, _ := v.selection(); lo != 2 || hi != 2 {
		t.Fatalf("shift+down で錨へ戻らない: lo=%d hi=%d", lo, hi)
	}
	v.handleKey("shift+down", vp(10)) // 錨より下へ = 2..3
	if lo, hi, _ := v.selection(); lo != 2 || hi != 3 {
		t.Fatalf("錨の下側へ伸ばせない: lo=%d hi=%d", lo, hi)
	}
	// 矢印と j/k の 2 系統あるので伸張も両方から効く (ユーザー要望 2026-08-01)。
	// ⚠️ 矢印だけだと、shift+矢印を通さない端末・tmux 設定で機能ごと沈黙する。
	for _, tc := range []struct {
		name           string
		arrow, vimKey  string
		wantLo, wantHi int
	}{
		{"下へ伸ばす", "shift+down", "J", 2, 4},
		{"上へ縮める", "shift+up", "K", 2, 3},
	} {
		byArrow := newView()
		byArrow.handleKey("shift+down", vp(10)) // 2..3 まで揃えてから
		byArrow.handleKey(tc.arrow, vp(10))
		byVim := newView()
		byVim.handleKey("J", vp(10))
		byVim.handleKey(tc.vimKey, vp(10))
		aLo, aHi, _ := byArrow.selection()
		vLo, vHi, _ := byVim.selection()
		if aLo != vLo || aHi != vHi {
			t.Fatalf("%s: 矢印と %s で範囲が違う (arrow=%d..%d vim=%d..%d)",
				tc.name, tc.vimKey, aLo, aHi, vLo, vHi)
		}
	}

	for _, tc := range []struct {
		name string
		do   func(v *issuesView)
	}{
		{"素の移動 (j)", func(v *issuesView) { v.handleKey("j", vp(10)) }},
		{"端へジャンプ (G)", func(v *issuesView) { v.handleKey("G", vp(10)) }},
		{"カテゴリ切替 (Tab)", func(v *issuesView) { v.handleKey("tab", vp(10)) }},
		{"状態フィルタ (a)", func(v *issuesView) { v.handleKey("a", vp(10)) }},
		{"本文を開く (Enter)", func(v *issuesView) { v.handleKey("enter", vp(10)) }},
		{"解除 (Esc)", func(v *issuesView) { v.handleKey("esc", vp(10)) }},
	} {
		v := newView()
		tc.do(v)
		if _, _, ok := v.selection(); ok {
			t.Fatalf("%s で選択が畳まれていない", tc.name)
		}
	}

	// Esc は選択の解除が先で、viewer は閉じない (閉じると解除の手段が無い状態から再開する)
	v = newView()
	v.handleKey("esc", vp(10))
	if !v.visible() {
		t.Fatal("Esc 1 回で viewer まで閉じた (選択の解除が先のはず)")
	}
	// 選択が無い状態の Esc は glogx ごと終了の信号 (ユーザー要望 2026-08-06。viewer は
	// 開いたまま = 再開記憶に残る)
	v.handleKey("esc", vp(10))
	if !v.takeWantQuit() {
		t.Fatal("選択が無い状態の Esc が終了の信号を立てない")
	}
	if !v.visible() {
		t.Fatal("終了の信号で viewer を閉じた (開いたまま終了して再開記憶に残すはず)")
	}
}

// 選択範囲は溝で見える (どこを選んでいるか画面から分かる)。幅も超えない。
func TestIssuesViewMultiSelectIsVisible(t *testing.T) {
	v := loadedView(sampleIssues()...)
	v.filter = issues.FilterAll
	v.refresh()
	v.cursor = 2
	v.handleKey("shift+up", vp(10))
	out := v.lines(renderOpts(10))
	marked := 0
	for _, ln := range out {
		if strings.HasPrefix(ln, issuesSelGutter) {
			marked++
		}
		if w := dispWidth(ln); w > 80 {
			t.Fatalf("選択表示で行が幅を超えた (w=%d): %q", w, ln)
		}
	}
	// 2 行選択のうちカーソル行は → で示すので、選択の溝が付くのは 1 行
	if marked != 1 {
		t.Fatalf("選択範囲の溝が出ていない (marked=%d):\n%s", marked, strings.Join(out, "\n"))
	}
	if h := v.hint(); !strings.Contains(h, "2 件選択") {
		t.Fatalf("選択中の hint に件数が出ない: %q", h)
	}
}

// 本文の左にソース (.md) の行番号を出す (ユーザー要望 2026-08-01)。⚠️ 溝は整形前に幅から
// 引く: 後付けすると本文が枠を突き破る。
func TestIssuesViewBodyShowsSrcLineNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "042-feat-x.md")
	src := "# 042 feat: 行番号\n\n段落。\n\n```go\na := 1\n```\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "042-feat-x.md", Number: "042", Category: "feat"})
	v.handleKey("enter", vp(20))
	v.drawer.finish()
	const width = 60
	out := v.lines(issuesRenderOpts{width: width, page: 16})
	joined := strings.Join(out, "\n")
	for _, want := range []string{" 1 █ 042 feat: 行番号", " 3 段落。", " 6 ┃ a := 1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("行番号つきの行 %q が出ない:\n%s", want, joined)
		}
	}
	for _, ln := range out {
		if w := dispWidth(ln); w > width {
			t.Fatalf("行番号の溝を足して幅を超えた (w=%d): %q", w, ln)
		}
	}
}

// Enter は「TUI 内の開閉 toggle」(ユーザー要望 2026-08-01): 一覧で開き、本文で閉じる。
// ⚠️ 本文の Enter は以前 1 行送りだった。glogx 本体の job パネルが既に Enter = 開閉 toggle
// なので、viewer だけ意味が違うと同じキーが画面ごとに別物になる。
func TestIssuesViewEnterTogglesBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	body := "# 028 refactor: x\n\n" + strings.Repeat("段落。\n\n", 30)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "028-refactor-x.md", Number: "028", Category: "refactor"})

	v.handleKey("enter", vp(10)) // 一覧の Enter = 開く
	if v.open == nil {
		t.Fatal("一覧の Enter で本文が開かない")
	}
	v.drawer.finish()
	v.lines(renderOpts(20))  // 行数は描画で確定する (未描画だと Len()=0 でスクロール上限が 0)
	v.handleKey("j", vp(10)) // 行送りは j/k のまま (Enter を奪ったので、こちらは残っていること)
	if v.bodyOff != 1 {
		t.Fatalf("本文の j で 1 行送れていない: %d", v.bodyOff)
	}

	v.handleKey("enter", vp(10)) // 本文の Enter = 閉じる
	if v.bodyOff != 1 {
		t.Fatalf("本文の Enter が行送りとして効いた: bodyOff=%d", v.bodyOff)
	}
	origNow := timeNow
	timeNow = func() time.Time { return origNow().Add(issuesDrawerDuration + time.Millisecond) }
	defer func() { timeNow = origNow }()
	v.lines(renderOpts(20)) // 閉じる演出を着地させる
	if v.open != nil {
		t.Fatal("本文の Enter で一覧へ戻らない")
	}
}

// n = 「次にやる」の目印 (ユーザー要望 2026-08-01)。確認を挟んでから next/ へ移す。
// ⚠️ viewer で唯一、実ファイルを動かす操作なので、確認なしで動かないことも一緒に固定する。
func TestIssuesViewMarkNextMovesAfterConfirm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"}
	v := loadedView(iss)
	v.cwd = dir

	v.handleKey("n", vp(10))
	if !v.markNext.active {
		t.Fatal("n で確認が出ない")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("確認の段階でファイルが動いた")
	}
	// 確認は最前面に描く (裏の一覧に紛れない)
	if out := strings.Join(v.lines(renderOpts(20)), "\n"); !strings.Contains(out, "next へ移動") {
		t.Fatalf("確認モーダルが描かれない:\n%s", out)
	}

	if cmd := v.handleKey("y", vp(10)); cmd == nil {
		t.Fatal("実行後に取り直しの Cmd が返らない (一覧が古いままになる)")
	}
	if v.markNext.active {
		t.Fatal("実行後も確認が残っている")
	}
	dest := filepath.Join(dir, issues.NextDirName, "001-feat-x.md")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("next/ へ移動していない (ディレクトリ作成も含む): %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("元の場所にファイルが残っている")
	}
	if text, ok := v.takeNotice(); !ok || !strings.Contains(text, "next へ移しました") {
		t.Fatalf("結果が通知に載らない: %q ok=%v", text, ok)
	}
}

// ⚠️ y / Enter 以外はすべて取り消し。「知らないキーを押したら実ファイルが動いた」を作らない。
func TestIssuesViewMarkNextCancels(t *testing.T) {
	for _, key := range []string{"n", "esc", "q", "j", "x"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "001-feat-x.md")
		if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"})
		v.cwd = dir
		v.handleKey("n", vp(10))
		if cmd := v.handleKey(key, vp(10)); cmd != nil {
			t.Fatalf("%q で取り消したのに Cmd が返った", key)
		}
		if v.markNext.active {
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
	v.cwd = dir
	v.cursor = 0
	v.handleKey("shift+down", vp(10)) // 2 行選択
	v.handleKey("n", vp(10))
	if len(v.markNext.targets) != 2 {
		t.Fatalf("選択範囲が対象になっていない: %d 件", len(v.markNext.targets))
	}
	v.handleKey("y", vp(10))
	moved := 0
	for _, iss := range list {
		if _, err := os.Stat(filepath.Join(dir, issues.NextDirName, filepath.Base(iss.Rel))); err == nil {
			moved++
		}
	}
	if moved != 2 {
		t.Fatalf("選択ぶんが移動していない: %d 件", moved)
	}
	if _, _, ok := v.selection(); ok {
		t.Fatal("実行後も選択が残っている")
	}
}

// [next] は All の左に固定の疑似カテゴリ (ユーザー要望 2026-08-01)。ファイル名のカテゴリでは
// なく「next/ に居るか」で選ぶので、状態フィルタの段階に関係なく目印つきが全部出る。
func TestIssuesViewNextPseudoTab(t *testing.T) {
	list := append(sampleIssues(),
		fakeIssue("040", "feat", "next-a", issues.StatusNext),
		fakeIssue("041", "docs", "next-b", issues.StatusNext),
	)
	v := loadedView(list...)

	// 既定は All のまま (zero value を [next] にしない = 開いた瞬間に空の一覧を出さない)
	if v.tabIdx != 0 || v.currentTab() != "" {
		t.Fatalf("既定のタブが All でない: tabIdx=%d name=%q", v.tabIdx, v.currentTab())
	}
	// チップは左端が [next]
	line := v.tabLine(issuesRenderOpts{width: 120})
	if !strings.HasPrefix(strings.TrimSpace(line), "[next 2]") {
		t.Fatalf("[next] が左端に出ていない: %q", line)
	}
	if !strings.Contains(line, "[All ") {
		t.Fatalf("All のチップが消えた: %q", line)
	}

	// 選ぶと目印つきだけが並ぶ
	v.tabIdx = tabIdxNext
	v.refresh()
	if len(v.rows) != 2 {
		t.Fatalf("[next] の行数が違う: %d", len(v.rows))
	}
	for _, iss := range v.rows {
		if iss.Status != issues.StatusNext {
			t.Fatalf("[next] に目印なしが混ざった: %s", iss.Rel)
		}
	}
	// 状態フィルタを動かしても [next] の中身は変わらない (目印が段階で消えない)
	for _, f := range []issues.StatusFilter{issues.FilterPending, issues.FilterAll, issues.FilterOpen} {
		v.filter = f
		v.refresh()
		if len(v.rows) != 2 {
			t.Fatalf("filter=%v で [next] の中身が変わった: %d 件", f, len(v.rows))
		}
	}
}

// 疑似カテゴリの選択も保存・復元できる (名前で持つので位置がずれても追従する)。
func TestIssuesViewNextTabSurvivesRestore(t *testing.T) {
	v := loadedView(append(sampleIssues(), fakeIssue("040", "feat", "n", issues.StatusNext))...)
	v.tabIdx = tabIdxNext
	v.refresh()
	v.root = "/repo"
	s, ok := v.screen(timeNow())
	if !ok || s.Tab != tabNextName {
		t.Fatalf("[next] の選択が保存されない: %+v ok=%v", s, ok)
	}
	v2 := newTestIssuesView()
	v2.restore("/repo", s)
	v2.receive(issuesScanMsg{dirs: []string{"/repo/issues"},
		issues: append(sampleIssues(), fakeIssue("040", "feat", "n", issues.StatusNext))})
	if v2.currentTab() != tabNextName {
		t.Fatalf("復元で [next] に戻らない: %q", v2.currentTab())
	}
}

// n は目印の toggle: 既に next の issue には「外す」向きになる (ユーザー要望 2026-08-01)。
// ⚠️ 戻り先は issue ディレクトリ直下 (= open)。元居た場所は覚えていない。
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
	v.cwd = dir

	v.handleKey("n", vp(10))
	if !v.markNext.active || !v.markNext.unmark {
		t.Fatalf("目印つきの issue で「外す」向きになっていない: %+v", v.markNext)
	}
	if out := strings.Join(v.lines(renderOpts(20)), "\n"); !strings.Contains(out, "next を外す") {
		t.Fatalf("外す向きの確認が出ない:\n%s", out)
	}
	v.handleKey("y", vp(10))
	if _, err := os.Stat(filepath.Join(dir, "001-feat-x.md")); err != nil {
		t.Fatalf("issues 直下へ戻っていない: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("next/ にファイルが残っている")
	}
	if text, ok := v.takeNotice(); !ok || !strings.Contains(text, "next を外しました") {
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
	v.cwd = dir
	// ⚠️ shift+↓ はカーソルを下へ動かすので、向きを決めるカーソル行も一緒に動く。
	// 目印つき (rows[0]) にカーソルを置いた状態で選択を作るには上へ伸ばす
	v.cursor = 1
	v.handleKey("shift+up", vp(10)) // カーソルは rows[0] (目印つき) = 外す向き
	v.handleKey("n", vp(10))
	if !v.markNext.unmark || len(v.markNext.targets) != 2 {
		t.Fatalf("カーソル行の向きで揃っていない: %+v", v.markNext)
	}
	v.handleKey("y", vp(10))
	// 目印つきだけが動き、元から直下に居たものは対象外
	if _, err := os.Stat(filepath.Join(dir, "002-feat-b.md")); err != nil {
		t.Fatalf("目印つきが直下へ戻っていない: %v", err)
	}
	if _, err := os.Stat(plain); err != nil {
		t.Fatal("対象外のファイルが動いた")
	}
}

func TestIssuesViewTabChipCountsMatchRows(t *testing.T) {
	// チップの件数は「そのタブを選んだときに並ぶ行数」と一致する。issues.Tab.Count は done を
	// 含む全件なので、そのまま出すと done を伏せた既定表示で合計が All と食い違う。
	v := loadedView(sampleIssues()...)
	line := v.tabLine(issuesRenderOpts{width: 120})
	for _, want := range []string{"[All 2]", "[feat 2]", "[refactor 0]", "[other 0]"} {
		if !strings.Contains(line, want) {
			t.Fatalf("チップの件数が状態フィルタと合っていない (%s が無い): %q", want, line)
		}
	}
	for i := range v.tabs {
		v.tabIdx = i + 1
		v.refresh()
		if len(v.rows) != v.tabCount[i] {
			t.Fatalf("タブ %s の件数 %d が実際の行数 %d と違う", v.tabs[i].Name, v.tabCount[i], len(v.rows))
		}
	}
	v.tabIdx = 0
	v.handleKey("a", vp(10)) // + pending
	if v.allCount != 3 {
		t.Fatalf("pending 表示で All の件数が更新されない: %d", v.allCount)
	}
	v.handleKey("a", vp(10)) // + done
	if v.allCount != 5 {
		t.Fatalf("done 表示で All の件数が更新されない: %d", v.allCount)
	}
	if got := v.tabLine(issuesRenderOpts{width: 120}); !strings.Contains(got, "[other 1]") {
		t.Fatalf("done 表示で other の件数が更新されない: %q", got)
	}
}

func TestIssuesViewLinesAlwaysExactlyPageRows(t *testing.T) {
	// 幅は 1 から全数で振る: 狭い幅では固定部分 (溝・番号・バッジ・カテゴリ) だけで幅を
	// 超えるため、行のクリップが 1 経路でも抜けていると枠を突き破る。掃き始めが 20 だった
	// 頃は幅 1-2 の取りこぼしが眠っていた (issue 053: clipToWidth の width<=0 素通しと
	// tabLine のフィルタバッジ後置)。
	for width := 1; width <= 80; width++ {
		for _, page := range []int{3, 5, 20, 40} {
			for _, v := range []*issuesView{loadedView(sampleIssues()...), loadedView(), {shown: true, scanning: true}} {
				o := renderOpts(page)
				o.width = width
				got := v.lines(o)
				if len(got) != page {
					t.Fatalf("width=%d page=%d なのに %d 行返った", width, page, len(got))
				}
				for i, ln := range got {
					if w := dispWidth(ln); w > width {
						t.Fatalf("width=%d page=%d 行 %d が幅を超えた (w=%d): %q", width, page, i, w, ln)
					}
				}
			}
		}
	}
}

func TestIssuesViewRowShowsNumberBadgeCategoryTitle(t *testing.T) {
	v := loadedView(sampleIssues()...)
	v.filter = issues.FilterPending // pending のバッジ ⏸ も見たいので 1 段進める
	v.refresh()
	lines := v.lines(renderOpts(20))
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"030", "○", "feat", "028", "⏸", "refactor"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("一覧に %q が出ない:\n%s", want, joined)
		}
	}
	// 矢印はカーソル行だけに 1 本出る (先頭行 = 030)
	marked := make([]string, 0, 1)
	for _, ln := range lines {
		if strings.HasPrefix(ln, cursorGutterMark) {
			marked = append(marked, ln)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "030") {
		t.Fatalf("カーソル行の矢印が想定と違う: %q", marked)
	}
}

func TestIssuesViewCursorScrollsWithinWindow(t *testing.T) {
	many := make([]*issues.Issue, 0, 40)
	for i := 40; i > 0; i-- {
		many = append(many, fakeIssue(fmt.Sprintf("%03d", i), "feat", "x", issues.StatusOpen))
	}
	v := loadedView(many...)
	const rows = 10
	for range 20 {
		v.handleKey("j", vp(rows))
	}
	if v.cursor != 20 {
		t.Fatalf("カーソルが動いていない: %d", v.cursor)
	}
	if v.offset == 0 || v.cursor < v.offset || v.cursor >= v.offset+rows {
		t.Fatalf("カーソルが画面内に収まっていない: cursor=%d offset=%d", v.cursor, v.offset)
	}
	v.handleKey("G", vp(rows))
	if v.cursor != len(v.rows)-1 {
		t.Fatalf("G で末尾へ行かない: %d", v.cursor)
	}
	v.handleKey("g", vp(rows))
	if v.cursor != 0 || v.offset != 0 {
		t.Fatalf("g で先頭へ戻らない: cursor=%d offset=%d", v.cursor, v.offset)
	}
	// 端で止まる (負のインデックスにならない)
	v.handleKey("k", vp(rows))
	if v.cursor != 0 {
		t.Fatalf("先頭で k がはみ出した: %d", v.cursor)
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
	v.handleKey("enter", vp(10))
	if v.open == nil || v.body == nil {
		t.Fatal("Enter で本文モードに入らない")
	}
	// 本文は引き出しとして左から開く (issues_drawer.go)。開いた直後は幅 0 なので、中身の検証は
	// 演出を着地させてから行う (実機では 450ms で着地する)。
	v.drawer.finish()
	out := strings.Join(v.lines(renderOpts(20)), "\n")
	for _, want := range []string{"028-refactor-x.md", "タイトル", "本文の段落", "• 箇条書き"} {
		if !strings.Contains(out, want) {
			t.Fatalf("本文表示に %q が出ない:\n%s", want, out)
		}
	}
	v.handleKey("h", vp(10))
	// 閉じる演出のあいだ本文は生きている (逆再生に中身が映る必要がある)
	if v.open == nil {
		t.Fatal("h の直後に本文が消えた (閉じる演出に何も映らない)")
	}
	origNow := timeNow
	timeNow = func() time.Time { return origNow().Add(issuesDrawerDuration + time.Millisecond) }
	defer func() { timeNow = origNow }()
	v.lines(renderOpts(20)) // 演出の着地を反映させる
	if v.open != nil {
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
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-long.md", Number: "001", Category: "feat"})
	v.handleKey("enter", vp(10))
	// 描画で行数が確定してからスクロールする (Body は幅ごとに整形結果をキャッシュする)
	v.lines(renderOpts(20))
	const page = 17
	for range 100 {
		v.handleKey("ctrl+d", vp(page))
	}
	// handleKey に渡すのは画面行数 (page)。実際にスクロールに使える行数はヘッダーを
	// 差し引いた visibleRows で、これが描画側とずれると末尾に届かなくなる
	if v.bodyOff != max(v.body.Len()-v.visibleRows(vp(page)), 0) {
		t.Fatalf("末尾を超えてスクロールした: off=%d len=%d rows=%d", v.bodyOff, v.body.Len(), v.visibleRows(vp(page)))
	}
	v.handleKey("g", vp(page))
	if v.bodyOff != 0 {
		t.Fatalf("g で先頭へ戻らない: %d", v.bodyOff)
	}
}

// hasCursorMark はカーソル行の矢印が描かれている行があるか。
func hasCursorMark(lines []string) bool {
	for _, ln := range lines {
		if strings.HasPrefix(ln, cursorGutterMark) {
			return true
		}
	}
	return false
}

func TestIssuesViewCursorStaysVisibleAfterRowSetChanges(t *testing.T) {
	// ⚠️ 回帰防止: カーソルは常に窓の中にいる。行数やヘッダー高が変わる経路 (a / Tab /
	// 通知行の増加) で窓だけ先頭へ飛ぶと、カーソル行がどこにも描かれないまま
	// current() が見えない行を返し、Enter / v (nvim) / y が別の issue を対象にする。
	stubClipboardFunc(t, func(string) error { return nil })

	list := make([]*issues.Issue, 0, 50)
	for i := 50; i > 0; i-- {
		st := issues.StatusOpen
		if i <= 10 {
			st = issues.StatusDone
		}
		list = append(list, fakeIssue(fmt.Sprintf("%03d", i), "feat", "x", st))
	}
	const page = 12
	// ⚠️ tab は 2 回押す: All の右には常設の human タブ (この fixture では 0 件) が居るので、
	// 1 回だけだと「行が無いタブ」に入ってカーソル行そのものが存在しなくなる
	for _, keys := range [][]string{{"a"}, {"tab", "tab"}, {"y"}} {
		v := loadedView(list...)
		v.handleKey("G", vp(page)) // カーソルを末尾へ (窓は下端に張り付く)
		if !hasCursorMark(v.lines(renderOpts(page))) {
			t.Fatalf("前提が崩れた: G の直後にカーソル行が描かれていない (keys=%v)", keys)
		}
		for _, key := range keys {
			v.handleKey(key, vp(page))
		}
		if !hasCursorMark(v.lines(renderOpts(page))) {
			t.Fatalf("%v の後にカーソル行が窓の外へ出た:\n%s", keys, strings.Join(v.lines(renderOpts(page)), "\n"))
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
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-long.md", Number: "001", Category: "feat"})
	const page = 12
	v.handleKey("enter", vp(page))
	v.drawer.finish() // 引き出しを開ききった状態で幅の検証をする

	narrow := issuesRenderOpts{width: 40, page: page}
	v.lines(narrow)
	v.handleKey("G", vp(page)) // 狭い幅での末尾へ
	v.lines(narrow)
	tail := v.bodyOff

	wide := issuesRenderOpts{width: 100, page: page}
	v.lines(wide) // 幅が広がって行数が減る
	if v.bodyOff >= tail {
		t.Fatalf("幅を広げても bodyOff が縮まっていない: %d -> %d", tail, v.bodyOff)
	}
	before := v.bodyOff
	v.handleKey("k", vp(page))
	if v.bodyOff != before-1 {
		t.Fatalf("幅変更後の k が 1 行戻していない: %d -> %d", before, v.bodyOff)
	}
}

func TestIssuesViewGReachesLastLine(t *testing.T) {
	// キー操作側と描画側で使う行数が一致していることの担保 (ずれると G が末尾に届かない)。
	// 一覧と本文の両方で「G を押したら最後の行が実際に描かれる」ことを見る。
	many := make([]*issues.Issue, 0, 40)
	for i := 40; i > 0; i-- {
		many = append(many, fakeIssue(fmt.Sprintf("%03d", i), "feat", "x", issues.StatusOpen))
	}
	v := loadedView(many...)
	const page = 12
	v.handleKey("G", vp(page))
	if out := strings.Join(v.lines(renderOpts(page)), "\n"); !strings.Contains(out, "001") {
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
	v2 := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-long.md", Number: "001", Category: "feat"})
	v2.handleKey("enter", vp(page))
	v2.lines(renderOpts(page)) // 幅ごとの整形を確定させてから G
	v2.handleKey("G", vp(page))
	if out := strings.Join(v2.lines(renderOpts(page)), "\n"); !strings.Contains(out, "最終行マーカー") {
		t.Fatalf("本文で G が末尾に届いていない:\n%s", out)
	}
}

// realIssue はディスク上に実体を持つ issue を 1 件作る。editCmd は起動前に実体を確認するので、
// エディタ起動が絡むテストは合成パスの Issue では「起動しない」側に倒れてしまう。
func realIssue(t *testing.T) *issues.Issue {
	t.Helper()
	dir := t.TempDir()
	rel := "001-feat-real.md"
	path := filepath.Join(dir, rel)
	if err := os.WriteFile(path, []byte("# 001 feat: real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &issues.Issue{Path: path, Dir: dir, Rel: rel, Number: "001", Category: "feat"}
}

func TestIssuesViewCopyPathAndEditor(t *testing.T) {
	copied := stubClipboard(t)
	cmds := stubEditorCapture(t)
	pinFallbackEditor(t)

	// ⚠️ 実ファイルを置く: editCmd は起動前に実体を確認するので、合成パスだと起動しない
	// (stale なパスでエディタを開かせない guard。editCmd の doc 参照)
	iss := realIssue(t)
	v := loadedView(iss)
	v.handleKey("y", vp(10))
	if *copied != v.rows[0].Path {
		t.Fatalf("カーソル行のパスがコピーされていない: %q", *copied)
	}
	// 結果はヘッダーに出さず、browseModel がトーストへ流すために取り出す (takeNotice)
	if text, ok := v.takeNotice(); !ok || !strings.Contains(text, "コピーしました") {
		t.Fatalf("コピーの結果が通知に載らない: %q ok=%v", text, ok)
	}
	if cmd := v.handleKey("v", vp(10)); cmd == nil || len(*cmds) != 1 {
		t.Fatalf("v でエディタ起動の Cmd が返らない: cmd=%v 起動数=%d", cmd != nil, len(*cmds))
	}
	// ⚠️ 「開いた」だけでなく「何を開いたか」まで見る (対象の取り違えを通さない)
	if args, want := (*cmds)[0].Args, []string{editorFallback, iss.Path}; !slices.Equal(args, want) {
		t.Errorf("v の起動コマンドが違う: args=%v want=%v", args, want)
	}
}

func TestIssuesViewActionKeysWorkInBothModes(t *testing.T) {
	// v / y / p / Y / N は一覧でも本文でも同じ対象 (target) に効く。モードごとの switch へ
	// 写すと、追加時に片方へ入れ忘れても「そのモードでだけ効かない」形で静かに壊れる。
	copied := stubClipboard(t)
	cmds := stubEditorCapture(t)
	pinFallbackEditor(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	if err := os.WriteFile(path, []byte("# 028 refactor: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &issues.Issue{Path: path, Dir: dir, Rel: "028-refactor-x.md", Number: "028", Category: "refactor"}
	for _, mode := range []string{"一覧", "本文"} {
		v := loadedView(iss)
		if mode == "本文" {
			v.handleKey("enter", vp(10))
			if v.open == nil {
				t.Fatal("本文モードに入れていない")
			}
		}
		for _, key := range []string{"y", "p", "Y", "N"} {
			*copied = ""
			v.handleKey(key, vp(10))
			if *copied == "" {
				t.Fatalf("%s モードで %q がコピーしていない", mode, key)
			}
		}
		// v は先にあった割当、e は git log 一覧の e と語彙を揃えた別名。どちらのモードでも
		// 両方が nvim を起動する (hint が案内するのは e だけ)。
		for _, key := range []string{"v", "e"} {
			before := len(*cmds)
			if cmd := v.handleKey(key, vp(10)); cmd == nil || len(*cmds) != before+1 {
				t.Fatalf("%s モードで %q がエディタを起動しない", mode, key)
			}
			// 対象は「その issue の実ファイル」。一覧モードと本文モードで target() が
			// 切り替わるので、どちらでも同じファイルを指すことまで見る
			if args, want := (*cmds)[before].Args, []string{editorFallback, path}; !slices.Equal(args, want) {
				t.Errorf("%s モードの %q の起動コマンドが違う: args=%v want=%v", mode, key, args, want)
			}
		}
	}
}

func TestIssuesViewRescanReturnsCmd(t *testing.T) {
	v := loadedView(sampleIssues()...)
	cmd := v.handleKey("r", vp(10))
	if cmd == nil {
		t.Fatal("r で再スキャンの Cmd が返らない")
	}
	if !v.loading() {
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
	v.shown = true
	v.receive(issuesScanMsg{})
	if !strings.Contains(strings.Join(v.lines(renderOpts(10)), "\n"), "見つかりません") {
		t.Fatal("issues ディレクトリ不在の案内が出ない")
	}
	// タブに open が無い (done だけ): 既定は open のみなので「pending も表示」を案内する
	v2 := loadedView(fakeIssue("001", "feat", "a", issues.StatusDone))
	if !strings.Contains(strings.Join(v2.lines(renderOpts(10)), "\n"), "a: pending も表示") {
		t.Fatal("open が無いときの案内が出ない")
	}
	// 1 段進めても空なら done の案内へ進む
	v2.filter = issues.FilterPending
	v2.refresh()
	if !strings.Contains(strings.Join(v2.lines(renderOpts(10)), "\n"), "a: done も表示") {
		t.Fatal("pending まで見ても空のときの案内が出ない")
	}
}

func TestIssuesViewShowsScanWarning(t *testing.T) {
	v := newTestIssuesView()
	v.shown = true
	v.receive(issuesScanMsg{
		dirs:     []string{"/repo/issues"},
		issues:   sampleIssues(),
		warnings: []string{"同じファイル名が複数の状態ディレクトリにあります: 028-x.md / done/028-x.md"},
	})
	if !strings.Contains(strings.Join(v.lines(renderOpts(10)), "\n"), "同じファイル名") {
		t.Fatal("スキャンの警告が表示されない")
	}
}

func TestIssuesViewNoticeIsTransientAndDoesNotHideWarning(t *testing.T) {
	// ⚠️ 回帰防止: notice に寿命が無いと、コピーを 1 回した時点でスキャン警告
	// (同名ファイルの二重化 = conflict も error も出ない静かな内容喪失) が二度と出なくなる。
	stubClipboardFunc(t, func(string) error { return nil })

	v := newTestIssuesView()
	v.shown = true
	v.receive(issuesScanMsg{
		dirs:     []string{"/repo/issues"},
		issues:   sampleIssues(),
		warnings: []string{"同じファイル名が複数の状態ディレクトリにあります: 028-x.md / done/028-x.md"},
	})
	v.handleKey("y", vp(10))
	text, ok := v.takeNotice()
	if !ok || !strings.Contains(text, "コピーしました") {
		t.Fatalf("コピーの結果が通知に載らない: %q ok=%v", text, ok)
	}
	// ⚠️ 操作結果はヘッダーを占めない (トーストへ移した) ので、スキャン警告を一瞬も隠さない。
	// 以前は通知が警告と同じ 1 行を奪い合っていた (同名ファイルの二重化 = 静かな内容喪失の警告)。
	out := strings.Join(v.lines(renderOpts(10)), "\n")
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
	stubClipboardFunc(t, func(string) error { return errors.New("pbcopy が見つかりません") })

	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	if err := os.WriteFile(path, []byte("# 028 refactor: タイトル\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "028-refactor-x.md", Number: "028", Category: "refactor"})
	v.handleKey("enter", vp(10))
	v.handleKey("p", vp(10))
	text, ok := v.takeNotice()
	if ok || !strings.Contains(text, "コピーに失敗しました") {
		t.Fatalf("本文モードのコピー失敗が通知に載らない: %q ok=%v", text, ok)
	}
}

func TestIssuesViewHintChangesByMode(t *testing.T) {
	v := loadedView(sampleIssues()...)
	if !strings.Contains(v.hint(), "カテゴリ") {
		t.Fatalf("一覧の hint が想定と違う: %q", v.hint())
	}
	v.open = v.rows[0]
	if !strings.Contains(v.hint(), "一覧へ") {
		t.Fatalf("本文の hint が想定と違う: %q", v.hint())
	}
	// URL ピッカーは本文の上に重なる 3 つ目のモード。本文の案内を出したままにすると、案内した
	// キー (j/k/g/G/p/u/v/h/q) が全部 urlPicker の検索語に化けて 1 つも案内どおりに動かない。
	v.urlPick.open([]string{"https://example.com/"})
	if h := v.hint(); !strings.Contains(h, "絞り込み") || strings.Contains(h, "一覧へ") {
		t.Fatalf("URL ピッカーの hint が想定と違う: %q", h)
	}
}

func TestIssuesViewCopyActions(t *testing.T) {
	copied := stubClipboard(t)

	v := loadedView(sampleIssues()...)
	v.root = "/repo"
	v.all[0].Title = "030 feat: 新機能"

	v.handleKey("p", vp(10))
	if *copied != "030" {
		t.Fatalf("p が番号をコピーしていない: %q", *copied)
	}
	if !strings.Contains(v.notice, "番号をコピーしました: 030") {
		t.Fatalf("通知が出ていない: %q", v.notice)
	}
	// Y = 番号 + タイトル + repo 相対パス (H1 の先頭番号は Display が落とす)
	v.handleKey("Y", vp(10))
	if *copied != "issue 030 feat: 新機能 (issues/030-feat-a.md)" {
		t.Fatalf("Y の参照が想定と違う: %q", *copied)
	}
	// N = 次に採番すべき番号 (fixture の最大は 030)
	v.handleKey("N", vp(10))
	if *copied != "031" {
		t.Fatalf("N が次番号をコピーしていない: %q", *copied)
	}
	// 番号なし issue ではファイル名に落として理由を通知する
	v2 := loadedView(&issues.Issue{Path: "/repo/issues/resource-leaks.md", Dir: "/repo/issues", Rel: "resource-leaks.md", Slug: "resource-leaks"})
	v2.handleKey("p", vp(10))
	if *copied != "resource-leaks.md" || !strings.Contains(v2.notice, "番号が無い") {
		t.Fatalf("番号なしの扱いが想定と違う: copied=%q notice=%q", *copied, v2.notice)
	}
}

func TestCategoryColorIsStableAndReservesRed(t *testing.T) {
	if categoryColor("bug") != catRed || categoryColor("fix") != catRed {
		t.Fatal("bug / fix が赤になっていない")
	}
	if categoryColor("feat") != catGreen {
		t.Fatal("feat が緑になっていない")
	}
	if categoryColor("") != ansiDim {
		t.Fatal("カテゴリ無しが dim になっていない")
	}
	// 表に無い語: 同じ語なら常に同じ色 (起動ごとに変わらない)
	first := categoryColor("waveform")
	for range 5 {
		if categoryColor("waveform") != first {
			t.Fatal("未知カテゴリの色が安定していない")
		}
	}
	// ⚠️ 未知語に赤を割らない (意味の無い語が「失敗」の色で出ると誤読される)
	for _, name := range []string{"waveform", "videoviewmodel", "dfs", "share", "auth", "acl", "codec", "thumbnail", "tabmanager"} {
		if categoryColor(name) == catRed {
			t.Fatalf("未知カテゴリ %q に赤を割った", name)
		}
	}
}

func TestIssuesViewRowPaintsCategoryColor(t *testing.T) {
	v := loadedView(fakeIssue("001", "bug", "x", issues.StatusOpen))
	o := renderOpts(80)
	o.colored = true
	joined := strings.Join(v.lines(o), "\n")
	if !strings.Contains(joined, catRed+"bug") {
		t.Fatalf("カテゴリ列に色が塗られていない:\n%q", joined)
	}
}

func TestIssuesViewSlideInAnimation(t *testing.T) {
	v := loadedView(sampleIssues()...)
	v.shown = true

	// 開始直後: まだほとんど画面外 (先頭行に右オフセットが入る)
	atProgress(v, 0.05)
	if !v.animating() {
		t.Fatal("演出中と判定されない")
	}
	early := v.lines(renderOpts(12))
	if len(early) != 12 {
		t.Fatalf("演出中も page 行を返すべき: %d", len(early))
	}
	shifted := false
	for _, ln := range early {
		if w := dispWidth(ln); w > 80 {
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
	mid := v.lines(renderOpts(12))
	if offsetOf(mid[2]) > offsetOf(mid[len(mid)-1]) {
		t.Fatalf("上の行が先に着地していない: %q", mid)
	}

	// 着地後: 演出なしの静的描画と一致する
	v.finishAnim()
	if v.animating() {
		t.Fatal("finishAnim 後も演出中のまま")
	}
	want := v.lines(renderOpts(12))
	atProgress(v, 1.2) // duration を超えた進み
	if got := v.lines(renderOpts(12)); strings.Join(got, "\n") != strings.Join(want, "\n") {
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
	v := loadedView(sampleIssues()...)
	v.shown = true
	atProgress(v, 0.1)
	v.handleKey("j", vp(12))
	if v.animating() {
		t.Fatal("キー入力で演出が着地していない (演出中は操作を待たせない契約)")
	}
}

func TestIssuesViewHintFitsPopupWidth(t *testing.T) {
	// hint は 1 行で、超過分は末尾から黙って切られる。popup の実幅 (testPopupWidth) に収める
	const popupWidth = testPopupWidth
	v := loadedView(sampleIssues()...)
	if w := dispWidth(v.hint()); w > popupWidth {
		t.Fatalf("一覧の hint が %d 桁に収まらない (w=%d): %q", popupWidth, w, v.hint())
	}
	// a の巡回で文言が変わるので全段階を見る
	for _, f := range []issues.StatusFilter{issues.FilterPending, issues.FilterAll} {
		v.filter = f
		if w := dispWidth(v.hint()); w > popupWidth {
			t.Fatalf("filter=%d の hint が収まらない (w=%d): %q", f, w, v.hint())
		}
	}
	// 複数選択の hint (件数が桁上がりしても収まるよう 3 桁で見る)
	v.filter = issues.FilterAll
	v.refresh()
	v.marked, v.markAt, v.cursor = true, 0, len(v.rows)-1
	if w := dispWidth(v.hint()); w > popupWidth {
		t.Fatalf("選択中の hint が収まらない (w=%d): %q", w, v.hint())
	}
	v.clearMark()
	v.open = v.rows[0]
	if w := dispWidth(v.hint()); w > popupWidth {
		t.Fatalf("本文の hint が収まらない (w=%d): %q", w, v.hint())
	}
	v.urlPick.open([]string{"https://example.com/"})
	if w := dispWidth(v.hint()); w > popupWidth {
		t.Fatalf("URL ピッカーの hint が収まらない (w=%d): %q", w, v.hint())
	}
}

// a キーは 3 段の巡回 (open → +pending → +done → open)。既定は open だけ
// (ユーザー要望 2026-07-31: 既定で pending を伏せる)。
func TestIssuesStatusFilterCycle(t *testing.T) {
	v := loadedView(sampleIssues()...)
	want := []struct {
		filter issues.StatusFilter
		rows   int
		badges string
	}{
		{issues.FilterOpen, 2, "○"},
		{issues.FilterPending, 3, "○⏸"},
		{issues.FilterAll, 5, "○⏸✓"},
		{issues.FilterOpen, 2, "○"}, // 巡回して既定へ戻る
	}
	for i, w := range want {
		if v.filter != w.filter {
			t.Fatalf("%d 打目: filter=%d want %d", i, v.filter, w.filter)
		}
		if len(v.rows) != w.rows {
			t.Errorf("%d 打目: 行数=%d want %d", i, len(v.rows), w.rows)
		}
		if got := v.filter.Badges(); got != w.badges {
			t.Errorf("%d 打目: バッジ=%q want %q", i, got, w.badges)
		}
		// hint は「次に何が増えるか」を出し、現在地は出さない (幅が 1 行しかない)
		if !strings.Contains(v.hint(), "a: ") {
			t.Errorf("%d 打目: hint に a の案内が無い: %q", i, v.hint())
		}
		v.handleKey("a", vp(10))
	}
}

// 0 件のカテゴリはタブ行の右へ寄る。件数 > 0 / 0 の各群の中では元の順序を保ち、
// 選択中のタブは並べ替えを跨いでも同じカテゴリを指し続ける (位置で持つ tabIdx の張り替え)。
func TestIssuesZeroCountTabsGoRight(t *testing.T) {
	// feat: open 2 / refactor: done 2 / bug: done 2 → 既定 (open のみ) では feat だけ非 0。
	// issues.Tabs は done 込みの件数降順なので、並べ替えが無いと bug/refactor が feat より左に来る。
	v := loadedView(
		fakeIssue("030", "feat", "a", issues.StatusOpen),
		fakeIssue("029", "feat", "b", issues.StatusOpen),
		fakeIssue("028", "refactor", "c", issues.StatusDone),
		fakeIssue("027", "refactor", "d", issues.StatusDone),
		fakeIssue("026", "bug", "e", issues.StatusDone),
		fakeIssue("025", "bug", "f", issues.StatusDone),
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
	v.refresh()
	v.handleKey("a", vp(10)) // + pending
	v.handleKey("a", vp(10)) // + done (件数が変わり並びも変わる)
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
	stubBrowserFunc(t, func(url string) error { opened = append(opened, url); return nil })

	v := newTestIssuesView()
	v.shown, v.loaded = true, true
	v.body = issues.NewBody("A https://example.com/alpha\nB https://example.com/beta\nC https://other.test/gamma\n")
	v.open = fakeIssue("001", "feat", "a", issues.StatusOpen)

	if cmd := v.handleKey("u", vp(20)); cmd != nil {
		t.Fatal("u はピッカーを開くだけで Cmd を返さない")
	}
	if !v.urlPick.active {
		t.Fatal("u でピッカーが開かない")
	}
	// 絞り込み前は全件、先頭が選択されている
	if got := v.urlPick.selected(); got != "https://example.com/alpha" {
		t.Errorf("初期選択 = %q", got)
	}
	// インクリメンタルサーチ: "beta" で 1 件に絞れる
	for _, k := range []string{"b", "e", "t", "a"} {
		v.handleKey(k, vp(20))
	}
	if len(v.urlPick.match) != 1 || v.urlPick.selected() != "https://example.com/beta" {
		t.Fatalf("検索で絞れない: match=%d selected=%q", len(v.urlPick.match), v.urlPick.selected())
	}
	cmd := v.handleKey("enter", vp(20))
	if cmd == nil {
		t.Fatal("Enter で開く Cmd が返らない")
	}
	if msg, ok := cmd().(openURLMsg); !ok || msg.err != nil {
		t.Fatalf("openURLMsg でない/失敗: %#v", msg)
	}
	if len(opened) != 1 || opened[0] != "https://example.com/beta" {
		t.Errorf("開いた URL = %q", opened)
	}
	if v.urlPick.active {
		t.Error("確定後もピッカーが開いたまま")
	}
	if !strings.Contains(v.notice, "https://example.com/beta") {
		t.Errorf("通知に URL が無い: %q", v.notice)
	}
}

// ピッカーは他のどの割当よりも先にキーを飲む (印字文字は全部検索語)。
// e/v (エディタ) や y (コピー) が横取りすると、その文字を含む URL を検索できない。
func TestIssuesViewURLPickerSwallowsActionKeys(t *testing.T) {
	cmds := stubEditorCapture(t)

	// ⚠️ エディタを開くキーは e と v の 2 本あるので両方回す。現行コードでは飲み込みを早期
	// return 1 箇所が支配していて片方で足りるが、キー別の分岐を作る変異には 1 キーだと効かない
	// (実際に番号入力側は v の 1 マスだけ穴が空いていた)。
	for _, key := range []string{"e", "v"} {
		before := len(*cmds)
		p := newTestIssuesView()
		p.shown, p.loaded = true, true
		p.body = issues.NewBody("https://example.com/very-vivid\nhttps://example.com/plain\n")
		// ⚠️ 実ファイル: 実体が無いと editCmd の guard で止まり、飲み込みの検証が空振りする
		p.open = realIssue(t)
		p.handleKey("u", vp(20))
		p.handleKey(key, vp(20)) // 検索語になるべき (エディタを起動してはいけない)
		if len(*cmds) != before {
			t.Errorf("ピッカー中の %q がエディタを起動した: %v", key, (*cmds)[before].Args)
		}
		if p.urlPick.query != key {
			t.Errorf("%q が検索語にならない: query=%q", key, p.urlPick.query)
		}
	}

	v := newTestIssuesView()
	v.shown, v.loaded = true, true
	v.body = issues.NewBody("https://example.com/very-vivid\nhttps://example.com/plain\n")
	v.open = fakeIssue("001", "feat", "a", issues.StatusOpen)
	v.handleKey("u", vp(20))
	v.handleKey("v", vp(20))
	if len(v.urlPick.match) != 1 || v.urlPick.selected() != "https://example.com/very-vivid" {
		t.Errorf("v で絞り込めない: %v", v.urlPick.match)
	}
}

// 番号入力中も URL ピッカーと同じ理由でアクションキーより先に飲む (打った文字が検索語)。
// ⚠️ URL ピッカー側にしかテストが無いと、actionKey を numFilter より前に動かしても何も落ちない。
// 順序を守るものをここで作る。キーは e と v の両方を回す — 「支配しているのは早期 return 1 箇所
// だから 1 キーで足りる」は現行コードの記述であって、キー別の分岐を作る変異には効かない。
func TestIssuesViewNumberFilterSwallowsActionKeys(t *testing.T) {
	cmds := stubEditorCapture(t)

	// ⚠️ 実ファイルを置く: 実体が無いと editCmd の guard だけで起動が止まり、
	// 「順序が守られているから起動しない」を検証できない (空振りする)
	v := loadedView(realIssue(t))
	v.handleKey("/", vp(10))
	if !v.numFilter.typing {
		t.Fatal("/ で番号入力に入れていない")
	}
	// e/v はエディタを開くキーだが、入力中は数字以外として捨てられるだけ (画面を動かさない)
	for _, key := range []string{"e", "v"} {
		before := len(*cmds)
		v.handleKey(key, vp(10))
		if len(*cmds) != before {
			t.Errorf("番号入力中の %q がエディタを起動した: %v", key, (*cmds)[before].Args)
		}
		if !v.numFilter.typing {
			t.Errorf("番号入力中の %q が入力モードを抜けさせた", key)
		}
	}
}

// hint が案内するキーと実際の割当を繋ぐ。案内だけ書き換えて割当を忘れる (逆も) と、
// spec 5 節が既知の事故として挙げる「案内どおりに動かないキー」が静かに生まれる。
// ⚠️ 幅は TestIssuesViewHintFitsPopupWidth が別に見る (ここは対応だけを見る)。
func TestIssuesViewBodyHintAdvertisedEditorKeyWorks(t *testing.T) {
	// ⚠️ 回数だけ数える stubEditor では「渡す対象の取り違え」(iss.Path → iss.Dir 等) が通るので、
	// Args を捕まえる stubEditorCapture を使う。
	cmds := stubEditorCapture(t)
	pinFallbackEditor(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	if err := os.WriteFile(path, []byte("# 028 refactor: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "028-refactor-x.md", Number: "028", Category: "refactor"})
	v.handleKey("enter", vp(10)) // 本文モードへ
	if v.open == nil {
		t.Fatal("本文モードに入れていない")
	}
	const advertised = "e: 編集"
	if h := v.hint(); !strings.Contains(h, advertised) {
		t.Fatalf("本文の hint が %q を案内していない: %q", advertised, h)
	}
	if cmd := v.handleKey("e", vp(10)); cmd == nil {
		t.Fatal("hint が案内した e でエディタが開かない (cmd == nil)")
	}
	// 開く対象は「その issue の実ファイル」。末尾引数がパスであることまで見る
	if len(*cmds) != 1 {
		t.Fatalf("エディタの起動回数が 1 でない: %d", len(*cmds))
	}
	if args, want := (*cmds)[0].Args, []string{editorFallback, path}; !slices.Equal(args, want) {
		t.Errorf("e の起動コマンドが違う: args=%v want=%v", args, want)
	}
}

// e は実体が無いパスではエディタを起動しない。⚠️ 一覧が握る Path は n (next/ へ移動) や
// 別プロセスの rename/削除で stale になる。stale なまま渡すと nvim は黙って新規バッファを開き、
// 保存すると旧位置にファイルが復活して「同じ basename が 2 箇所」を viewer 自身が作る
// (issues/move.go が宣言している不変条件の違反)。
func TestIssuesViewEditSkipsMissingFile(t *testing.T) {
	cmds := stubEditorCapture(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"})

	// 前提: 実体があるうちは開く
	if cmd := v.handleKey("e", vp(10)); cmd == nil || len(*cmds) != 1 {
		t.Fatalf("実体があるのに開かない (cmd==nil: %v, 起動数=%d)", cmd == nil, len(*cmds))
	}

	// 実体が動いた (n の next/ 移動・別プロセスの rename と同じ状態) 後は開かず取り直す
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cmd := v.handleKey("e", vp(10))
	if len(*cmds) != 1 {
		t.Errorf("実体が無いのにエディタを起動した: %v", (*cmds)[len(*cmds)-1].Args)
	}
	if cmd == nil {
		t.Error("取り直しの Cmd を返していない (古い一覧が残り続ける)")
	}
	// ⚠️ スキャン飛行中でも取りこぼさない (一覧が古いと分かっている経路なので予約に回す)
	v.scanning, v.rescanPending = true, false
	if cmd := v.handleKey("e", vp(10)); cmd != nil {
		t.Error("飛行中に 2 本目の探索を発行した (single-flight が効いていない)")
	}
	if !v.rescanPending {
		t.Error("飛行中の取り直しが予約されていない (実体なしのまま古い一覧が残る)")
	}
	if text, ok := v.takeNotice(); text == "" || ok {
		t.Errorf("理由を通知していない: text=%q ok=%v", text, ok)
	}
}

// 本文モードで開いている issue が再スキャンで移動していたら、新しい場所へ繋ぎ直す。
// ⚠️ 追わないと、読んでいる最中に n / 別プロセスで done/ へ移された issue が「実体から外れた本文」
// になり、y が消えたパスをコピーし e は実体確認で弾かれる (編集も取り直しもできない)。
func TestIssuesViewRebindOpenFollowsMove(t *testing.T) {
	dir := t.TempDir()
	rel := "001-feat-x.md"
	before := &issues.Issue{Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: "001", Category: "feat"}
	if err := os.WriteFile(before.Path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(before)
	v.handleKey("enter", vp(10))
	if v.open == nil {
		t.Fatal("本文モードに入れていない")
	}

	// done/ へ移動した状態のスキャン結果が届く (パスが変わる = 旧パスは見つからない)
	doneDir := filepath.Join(dir, "done")
	if err := os.MkdirAll(doneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	after := &issues.Issue{Path: filepath.Join(doneDir, rel), Dir: dir, Rel: "done/" + rel, Number: "001", Category: "feat"}
	v.receive(issuesScanMsg{dirs: []string{dir}, issues: []*issues.Issue{after}})

	if v.open == nil {
		t.Fatal("移動しただけで本文モードが畳まれた (一覧へ引き戻している)")
	}
	if v.open.Path != after.Path {
		t.Errorf("移動先へ繋ぎ直していない: open=%q want=%q", v.open.Path, after.Path)
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
	v.handleKey("enter", vp(10))
	if v.open == nil {
		t.Fatal("本文モードに入れていない")
	}

	v.receive(issuesScanMsg{dirs: []string{dir}}) // 1 件も無い = 消えた

	if v.open != nil {
		t.Errorf("消えた issue の本文モードに留まっている: open=%q", v.open.Path)
	}
	if v.body != nil {
		t.Error("消えた issue の本文を保持し続けている")
	}
	if text, ok := v.takeNotice(); text == "" || ok {
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

	if len(msg.issues) != 1 {
		t.Fatalf("issue が 1 件見つかるはず: %d 件", len(msg.issues))
	}
	if read == 0 {
		t.Fatal("読んだバイト数が観測できていない (LoadMeta が観測点を通っていない)")
	}
	if read >= size {
		t.Errorf("scan 経路が全文を読んでいる: read=%d size=%d", read, size)
	}
}

// Msg 経路 (issuesScanMsg) からの notice 配達の回帰テスト (issue 059)。
// rebindOpen が置く「開いていた issue が見つかりません」は、打鍵を待たずスキャン結果を
// 受けたフレームで配達される (トースト + w でコピーできる lastWarning)。
// 以前は takeNotice が打鍵経路にしか無く、本文が無言で畳まれて見え、直後に q を押すと
// 理由が 1 度も描かれないままプロセスが終わった。
// ⚠️ v.takeNotice() を直接 assert しない: それは配達経路 (browseModel の Update) を通らず、
// 配達ブロックを丸ごと削っても green のままになる (この issue 自身が見つけた false green)。
func TestIssuesScanMsgDeliversRebindNotice(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n\n本文の行\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"}
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	m.issuesOv.cwd = root
	m.Update(issuesScanMsg{root: root, dirs: []string{dir}, issues: []*issues.Issue{iss}})
	m.issuesOv.finishAnim()
	m.handleKey("enter")
	m.issuesOv.drawer.finish() // 本文モードへ

	// 別プロセスがファイルを消したスキャン結果が Msg 経路で届く (キーは押さない)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.Update(issuesScanMsg{root: root, dirs: []string{dir}})

	if !strings.Contains(m.lastWarning, "見つかりません") {
		t.Errorf("畳んだ理由が lastWarning に届いていない (q で恒久喪失する): %q", m.lastWarning)
	}
	if !m.toast.visible() {
		t.Error("畳んだ理由のトーストが積まれていない")
	}
	if text, _ := m.issuesOv.takeNotice(); text != "" {
		t.Errorf("notice が取り出されずに残っている (配達漏れ): %q", text)
	}
}

// 同名が複数ある異常 (spec 3 節が警告する状態) では、どれが本人か決められないので繋ぎ直さない。
// ⚠️ 畳むのは「どこにも無い」ときだけなので、ここでは本文モードを維持する。
func TestIssuesViewRebindOpenKeepsOpenOnAmbiguousBase(t *testing.T) {
	dir := t.TempDir()
	rel := "001-feat-x.md"
	iss := &issues.Issue{Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: "001", Category: "feat"}
	if err := os.WriteFile(iss.Path, []byte("# 001 feat: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(iss)
	v.handleKey("enter", vp(10))

	// 同名が 2 箇所 (done/ と next/) にある結果が届く
	dup := []*issues.Issue{
		{Path: filepath.Join(dir, "done", rel), Dir: dir, Rel: "done/" + rel, Number: "001", Category: "feat"},
		{Path: filepath.Join(dir, "next", rel), Dir: dir, Rel: "next/" + rel, Number: "001", Category: "feat"},
	}
	v.receive(issuesScanMsg{dirs: []string{dir}, issues: dup})

	if v.open == nil {
		t.Fatal("同名が複数あるだけで本文モードを畳んだ (どれが本人か決められないだけで実体はある)")
	}
	if v.open.Path != iss.Path {
		t.Errorf("曖昧なのに繋ぎ替えた: open=%q", v.open.Path)
	}
}

// n の next/ 移動は、別のスキャンが飛行中でも一覧へ反映されなければならない。
// ⚠️ scanCmd は single-flight で飛行中は nil を返す。markNextKey がそれをそのまま返すと、
// 実ファイルは動いたのに一覧は旧位置を出し続ける (しかも飛行中のスキャンは移動より前に
// 開始されているので、その結果が届いても移動は映らない)。
func TestIssuesViewMarkNextReflectsWhileScanning(t *testing.T) {
	iss := realIssue(t)
	v := loadedView(iss)

	// 別経路のスキャンを飛ばした状態にする (r 連打・外部編集の自動反映などで普通に起きる)
	if cmd := v.scanCmd(v.cwd); cmd == nil {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}
	if !v.scanning {
		t.Fatal("前提が崩れた: scanning が立っていない")
	}

	v.handleKey("n", vp(10)) // 確認モーダル
	if !v.markNext.active {
		t.Fatal("n で確認モーダルが出ない")
	}
	v.handleKey("y", vp(10)) // 実行 (実ファイルが next/ へ動く)

	if _, err := os.Stat(filepath.Join(iss.Dir, issues.NextDirName, iss.Rel)); err != nil {
		t.Fatalf("実ファイルが next/ へ動いていない: %v", err)
	}

	// 飛行中スキャンの結果が届く。⚠️ これは移動より前に始まったので旧位置しか含まない
	stale := issuesScanMsg{dirs: []string{iss.Dir}, issues: []*issues.Issue{iss}}
	cmd := v.receive(stale)
	if cmd == nil {
		t.Fatal("移動を反映する取り直しが張られない (一覧が旧位置のまま残る)")
	}
	// ⚠️ 張り直しは 1 回だけ。予約を消さないと receive ごとに scan を張り続け、loading() が
	// 永久 true になってスピナーも止まらない (無限スキャン)。
	if v.rescanPending {
		t.Error("予約が消費されていない (次の receive でも張り直して無限に続く)")
	}
	v.receive(stale) // 1 本目の張り直しの結果が届いた形。ここで scanning は落ちる
	if again := v.receive(stale); again != nil {
		t.Error("2 回目以降の receive でも取り直しが張られる (予約が sticky)")
	}
}

// エディタ復帰後の取り直しも同じ保証が要る。⚠️ 落とすと「保存したのに一覧・本文が古い」に
// なり、しかも飛行中のスキャンは編集より前に始まっているのでその結果でも直らない。
func TestIssuesViewReloadAfterEditReflectsWhileScanning(t *testing.T) {
	iss := realIssue(t)
	v := loadedView(iss)
	if cmd := v.scanCmd(v.cwd); cmd == nil || !v.scanning {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}

	if cmd := v.reloadAfterEdit(); cmd != nil {
		t.Fatal("飛行中なのに 2 本目の探索を発行した (single-flight が効いていない)")
	}
	cmd := v.receive(issuesScanMsg{dirs: []string{iss.Dir}, issues: []*issues.Issue{iss}})
	if cmd == nil {
		t.Fatal("編集を反映する取り直しが張られない (保存したのに古い内容が残る)")
	}
}

// 閉じたら取り直しの予約も捨てる (片付けは finishClose に一本化されている)。
// ⚠️ 残すと非表示の viewer のためにスキャンが 1 回走り、その間スピナーの tick が昂進する。
func TestIssuesViewCloseDropsRescanReservation(t *testing.T) {
	iss := realIssue(t)
	v := loadedView(iss)
	if cmd := v.scanCmd(v.cwd); cmd == nil {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}
	if cmd := v.reloadAfterEdit(); cmd != nil || !v.rescanPending {
		t.Fatalf("前提が崩れた: 予約が立っていない (cmd=%v pending=%v)", cmd != nil, v.rescanPending)
	}

	v.close() // closeAnimOff なので即座に finishClose まで通る
	if v.rescanPending {
		t.Error("閉じたのに取り直しの予約が残っている")
	}
	if cmd := v.receive(issuesScanMsg{dirs: []string{iss.Dir}, issues: []*issues.Issue{iss}}); cmd != nil {
		t.Error("閉じた viewer のためにスキャンを張り直した")
	}
}

// browseModel の配線: issuesScanMsg の case が receive の戻り値 (取り直しの予約) を返すこと。
// ⚠️ ここを捨てても issuesView 単体のテストは全部通り、lint も通らない (errcheck 系は error 以外の
// 戻り値放棄を見ない)。予約機構の可視効果はこの 1 行に乗っているので model 層で固定する。
func TestBrowseIssuesScanMsgPropagatesRescanCmd(t *testing.T) {
	iss := realIssue(t)
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(iss)

	if cmd := m.issuesOv.scanCmd(m.issuesOv.cwd); cmd == nil {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}
	if cmd := m.issuesOv.reloadAfterEdit(); cmd != nil || !m.issuesOv.rescanPending {
		t.Fatalf("前提が崩れた: 予約が立っていない (cmd=%v pending=%v)", cmd != nil, m.issuesOv.rescanPending)
	}

	_, cmd := m.Update(issuesScanMsg{dirs: []string{iss.Dir}, issues: []*issues.Issue{iss}})
	if cmd == nil {
		t.Error("issuesScanMsg の case が取り直しの Cmd を捨てている (予約が失われる)")
	}
}

func TestIssuesViewURLPickerNone(t *testing.T) {
	v := newTestIssuesView()
	v.shown, v.loaded = true, true
	v.body = issues.NewBody("URL の無い本文\n")
	v.open = fakeIssue("001", "feat", "a", issues.StatusOpen)
	v.handleKey("u", vp(20))
	if v.urlPick.active {
		t.Error("URL が無いのにピッカーが開いた")
	}
	if !strings.Contains(v.notice, "URL はありません") {
		t.Errorf("URL 不在の案内が出ない: %q", v.notice)
	}
}

// 一覧モードでは u は効かない (本文を読んでいないため)。
func TestIssuesViewOpenURLListModeIsNoop(t *testing.T) {
	stubBrowserFunc(t, func(string) error { t.Fatal("一覧モードでブラウザを開いた"); return nil })

	v := loadedView(sampleIssues()...)
	if cmd := v.handleKey("u", vp(20)); cmd != nil {
		t.Error("一覧モードで u が Cmd を返した")
	}
	if v.urlPick.active {
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
		&issues.Issue{Path: p1, Dir: dir, Rel: "001-feat-a.md", Number: "001", Category: "feat"},
		&issues.Issue{Path: p2, Dir: dir, Rel: "002-feat-b.md", Number: "002", Category: "feat"},
	)
	v.handleKey("enter", vp(20))
	v.handleKey("u", vp(20))
	v.handleKey("1", vp(20)) // 検索語を入れた状態で
	if !v.urlPick.active || v.urlPick.query == "" {
		t.Fatal("ピッカーの前提が崩れている")
	}
	v.handleKey("esc", vp(20)) // 閉じて一覧へ
	v.handleKey("h", vp(20))
	v.handleKey("j", vp(20))
	v.handleKey("enter", vp(20))
	if v.urlPick.active || v.urlPick.query != "" {
		t.Errorf("別の issue へ状態が持ち越された: active=%v query=%q", v.urlPick.active, v.urlPick.query)
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
		many = append(many, fakeIssue(fmt.Sprintf("%03d", i+1), "feat", "x", issues.StatusOpen))
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
					v.handleKey("/", vp(20))
					for _, r := range c.query {
						v.handleKey(string(r), vp(20))
					}
				}
				o := issuesRenderOpts{width: w, page: 20}
				// 描画が実際に出した行数 (ヘッダーを除く) とキー側の行数を突き合わせる
				body := len(v.listLines(o)) - len(v.listHeadLines(w, false))
				if got := v.visibleRows(o.viewport()); got != body {
					t.Fatalf("幅 %d: キー側 %d 行 / 描画が出した %d 行", w, got, body)
				}
			}
		})
	}
	// 本文は引き出しの内側幅で組む。全体幅で数えると引き出しを開いている間だけずれるので、
	// 描画側が呼ぶ関数 (bodyHeadLines + bodyWidth) をそのまま並べて一致を見る。
	t.Run("本文 (引き出し)", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "001-feat-x.md")
		if err := os.WriteFile(path, []byte("# 001 feat: x\n\n"+strings.Repeat("本文。\n", 60)), 0o644); err != nil {
			t.Fatal(err)
		}
		iss := &issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"}
		for _, w := range []int{24, 40, 84, 200} {
			v := loadedView(iss)
			v.handleKey("enter", vp(20))
			if v.open == nil {
				t.Fatal("本文モードに入れていない")
			}
			o := issuesRenderOpts{width: w, page: 20}
			want := max(o.page-len(v.bodyHeadLines(v.bodyWidth(w), false)), 1)
			if got := v.visibleRows(o.viewport()); got != want {
				t.Fatalf("幅 %d: キー側 %d 行 / 描画側 %d 行", w, got, want)
			}
		}
	})
}

// 再スキャンをまたいで選択を保つ (錨をパスで張り替える)。
//
// 外部編集の即時反映が入って、選択している最中に取り直しが走るのが普通になった。畳むと
// Claude Code が issue を書くたびに選択が消えて実用にならない。
func TestIssuesViewKeepsSelectionAcrossRescan(t *testing.T) {
	v := loadedView(sampleIssues()...)
	v.handleKey("a", vp(20)) // pending も出して行を増やす
	v.handleKey("J", vp(20)) // 2 行選択 (錨 = 先頭行)
	lo, hi, ok := v.selection()
	if !ok || hi-lo != 1 {
		t.Fatalf("前提が崩れた: lo=%d hi=%d ok=%v", lo, hi, ok)
	}
	anchor, head := v.rows[lo].Path, v.rows[hi].Path

	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: sampleIssues()})

	lo2, hi2, ok2 := v.selection()
	if !ok2 {
		t.Fatal("再スキャンで選択が消えた")
	}
	if v.rows[lo2].Path != anchor || v.rows[hi2].Path != head {
		t.Fatalf("選択が別の issue を指した: %q..%q", v.rows[lo2].Path, v.rows[hi2].Path)
	}
	// タブ・フィルタの切り替えは行集合の意味が変わるので畳んだまま
	v.handleKey("tab", vp(20))
	if _, _, ok := v.selection(); ok {
		t.Error("タブ切り替えで選択が残った (別の行集合へ範囲を持ち越している)")
	}
}

// 通知文は issue のファイル名・本文由来の URL を素で埋め込むので、setNotice 自体を関門にする
// (呼び出しごとに包む方式だと必ずどこかが漏れる)。status viewer 側と対の規律。
func TestIssuesNoticeIsSanitized(t *testing.T) {
	const esc, bel = "\x1b", "\a"
	v := newTestIssuesView()
	v.setNotice("URL を開きます: https://example.com/"+esc+"]0;pwned"+bel+"x", true)
	if hasTerminalControl(v.notice) {
		t.Errorf("通知に制御シーケンスが残った: %q", v.notice)
	}
	if strings.Contains(v.notice, "pwned") {
		t.Errorf("OSC の中身が通知に残った: %q", v.notice)
	}
}

// newTestIssuesView は閉じる演出を切った viewer。⚠️ 既存テストは「close したら即座に畳まれて
// いる」前提で書かれているので、演出を挟むと全て 1 拍待ちになって読めなくなる。演出そのものは
// issues_close_anim_test.go が明示的に on にして検査する (newTestBrowse の zoom.off と同じ)。
func newTestIssuesView() issuesView {
	v := newIssuesView()
	v.closeAnimOff = true
	return v
}

// hint が案内する本文モードのキーは、全部が実際に効かなければならない。
//
// ⚠️ spec 5 節が「hint が案内するキーが 1 つも案内どおりに動かない」を既知の事故として挙げて
// いるのに、これまでその不変条件はテストで固定されていなかった (案内キーを未割当の z に
// 書き換えても全テストが green だった)。ここで hint 側から駆動して閉じる。
//
// ⚠️ 「案内された集合」と「効果を検証している表」の一致まで見る。片方だけ増やせないので、
// hint にキーを足したら効果の検証も必ず書くことになる。
// 本文 pager の半ページ送り・端ジャンプが glide の配線ごと効くことを**キー経路**で固定する。
//
// ⚠️ TestHalfPageScrollGlidesOnAllSurfaces の本文サブテストは bodyGlide.start を直接呼ぶ
// (body が nil の workaround) ため、handleBodyKey が glide を配線し忘れても green のままだった。
// pagerScrollKey への委譲 (2026-08-19) で glide を落とす退行はこのテストだけが検出する。
func TestIssuesViewBodyHalfPageGlidesViaKeyPath(t *testing.T) {
	e := newBodyKeyEnv(t)
	before := e.v.bodyOff
	e.press(" ")
	if !e.v.bodyGlide.active {
		t.Fatal("Space の半ページ送りが glide に載っていない (キー経路)")
	}
	if got := e.v.bodyGlide.offset(e.v.bodyOff); got != before {
		t.Errorf("glide 開始位置 = %d, want %d (移動前の offset)", got, before)
	}
	e.press("G")
	if e.v.bodyGlide.active {
		t.Error("G の端ジャンプで glide が残っている (端ジャンプは即時)")
	}
}

func TestIssuesViewBodyHintKeysAllRespond(t *testing.T) {
	// 案内キーごとの「押したら何が変わるか」。⚠️ 効果を観測できる状態を各自で作る
	// (本文の途中まで送ってから k を押す等。端で押して「変わらない」を通すと空振りする)
	probes := map[string]func(t *testing.T, e *bodyKeyEnv){
		"j": func(t *testing.T, e *bodyKeyEnv) {
			before := e.v.bodyOff
			e.press("j")
			if e.v.bodyOff <= before {
				t.Errorf("j で本文が下へ進まない: %d → %d", before, e.v.bodyOff)
			}
		},
		"k": func(t *testing.T, e *bodyKeyEnv) {
			// ⚠️ G を押して状態を作らない。G が壊れると k の probe も red になり、
			// 壊れていないキーを名指しして原因追跡を遅らせる
			e.v.bodyOff = 5
			before := e.v.bodyOff
			e.press("k")
			if e.v.bodyOff >= before {
				t.Errorf("k で本文が上へ戻らない: %d → %d", before, e.v.bodyOff)
			}
		},
		"Space": func(t *testing.T, e *bodyKeyEnv) {
			before := e.v.bodyOff
			e.press(" ")
			// ⚠️ 方向だけでなく「半ページ」まで見る (1 行送りの実装を通さない)
			if want := before + e.rows()/2; e.v.bodyOff != want {
				t.Errorf("Space が半ページ送りでない: %d → %d (want %d)", before, e.v.bodyOff, want)
			}
		},
		"g": func(t *testing.T, e *bodyKeyEnv) {
			e.press("G")
			e.press("g")
			if e.v.bodyOff != 0 {
				t.Errorf("g で先頭へ戻らない: bodyOff=%d", e.v.bodyOff)
			}
		},
		"G": func(t *testing.T, e *bodyKeyEnv) {
			e.press("G")
			// ⚠️ 「0 でない」ではなく末尾そのものを見る (途中で止まる実装を通さない)
			if want := e.v.body.Len() - e.rows(); e.v.bodyOff != want {
				t.Errorf("G が末尾へ飛ばない: bodyOff=%d want %d", e.v.bodyOff, want)
			}
		},
		"p": func(t *testing.T, e *bodyKeyEnv) {
			*e.copied = ""
			e.press("p")
			if *e.copied == "" {
				t.Error("p で番号がコピーされない")
			}
		},
		"u": func(t *testing.T, e *bodyKeyEnv) {
			e.press("u")
			if !e.v.urlPick.active {
				t.Error("u で URL ピッカーが開かない")
			}
		},
		"e": func(t *testing.T, e *bodyKeyEnv) {
			before := len(*e.cmds)
			e.press("e")
			if len(*e.cmds) != before+1 {
				t.Fatal("e でエディタが起動しない")
			}
			// ⚠️ 起動しただけでなく対象まで見る (専用テストと同じ強さに揃える。方向だけの
			// 弱い二重化にすると「壊れても片方しか落ちない」状態になる)
			if args, want := (*e.cmds)[before].Args, []string{editorFallback, e.v.open.Path}; !slices.Equal(args, want) {
				t.Errorf("e の起動コマンドが違う: args=%v want=%v", args, want)
			}
		},
		"Enter": func(t *testing.T, e *bodyKeyEnv) { e.assertClosesBody(t, "enter") },
		"h":     func(t *testing.T, e *bodyKeyEnv) { e.assertClosesBody(t, "h") },
		"q":     func(t *testing.T, e *bodyKeyEnv) { e.assertClosesBody(t, "q") },
	}

	advertised := advertisedHintKeys(t)
	if len(advertised) == 0 {
		t.Fatal("本文モードの hint から案内キーを取り出せていない (パースが壊れている)")
	}
	for _, name := range advertised {
		run, ok := probes[name]
		if !ok {
			t.Errorf("hint が案内している %q の効果を誰も検証していない (probes に足すこと)", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			run(t, newBodyKeyEnv(t))
		})
	}
	for name := range probes {
		if !slices.Contains(advertised, name) {
			t.Errorf("hint が案内していない %q を検証している (hint から消えたなら probes も消す)", name)
		}
	}
}

// advertisedHintKeys は本文モードの hint から案内キーを取り出す ("j/k/Space: 説明" → j, k, Space)。
func advertisedHintKeys(t *testing.T) []string {
	t.Helper()
	e := newBodyKeyEnv(t)
	var keys []string
	for _, tok := range strings.Split(e.v.hint(), "  ") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		label, _, ok := strings.Cut(tok, ": ")
		if !ok {
			// ⚠️ 捨てずに落とす。黙って continue すると「コロン無しトークンでキーを案内する」
			// 書き方 (他モードの hint に "文字入力で絞り込み" 等が実在する) を body に持ち込んだ
			// 瞬間に、そのキーが検証から漏れて集合一致もすり抜ける。
			t.Errorf("hint のトークン %q を \"キー: 説明\" として読めない (案内キーが検証から漏れる)", tok)
			continue
		}
		keys = append(keys, strings.Split(label, "/")...)
	}
	return keys
}

// bodyKeyEnv は本文モードのキーを押せる状態 (長い本文 + URL + 実ファイル + 各種スタブ)。
type bodyKeyEnv struct {
	v      *issuesView
	cmds   *[]*exec.Cmd
	copied *string
	page   int // キー処理へ渡す窓の高さ (実際の pager 行数は rows())
}

func newBodyKeyEnv(t *testing.T) *bodyKeyEnv {
	t.Helper()
	cmds := stubEditorCapture(t)
	copied := stubClipboard(t)
	pinFallbackEditor(t)

	dir := t.TempDir()
	rel := "001-feat-hintkeys.md"
	path := filepath.Join(dir, rel)
	// ⚠️ 本文は窓より十分長くする (短いと j / Space / G が「動かない」ので効果を観測できない)。
	// URL を 1 つ入れるのは u (ピッカー) の観測のため。
	// ⚠️ 空行で区切る。連続行は markdown で 1 段落に畳まれ、body.Len() が窓より短くなって
	// j / Space / G の効果が観測できない (実測: 80 行が 1 段落になり bodyOff が 0 のまま)
	body := "# 001 feat: hint keys\n\nhttps://example.com/x\n\n" + strings.Repeat("本文の行。\n\n", 60)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: rel, Number: "001", Category: "feat"})
	v.handleKey("enter", vp(10)) // 本文モードへ
	if v.open == nil {
		t.Fatal("本文モードに入れていない")
	}
	v.drawer.finish()
	// ⚠️ 行数は描画で確定する (未描画だと body.Len() = 0 でスクロール上限が 0 になり、
	// j / Space / G が「動かない」ので効果を観測できない)
	v.lines(renderOpts(20))
	if v.body.Len() <= 10 {
		t.Fatalf("前提が崩れた: 本文が窓より短くスクロールできない (Len=%d)", v.body.Len())
	}
	return &bodyKeyEnv{v: v, cmds: cmds, copied: copied, page: 10}
}

func (e *bodyKeyEnv) press(key string) { e.v.handleKey(key, vp(e.page)) }

// rows は本文 pager の実際の行数 (ヘッダー行数を引いた値)。⚠️ page とは違うので、
// 「半ページ」「末尾」を語義で検証する probe はこちらを使う。
func (e *bodyKeyEnv) rows() int { return e.v.visibleRows(vp(e.page)) }

func (e *bodyKeyEnv) assertClosesBody(t *testing.T, key string) {
	t.Helper()
	e.press(key)
	// ⚠️ 本文は「閉じる演出の着地後」に捨てられるので、open == nil を待つと時刻の進め方に
	// 依存する。キーの直接の効果である「閉じる演出に入ったか」を見る。
	if e.v.drawer.phase != drawerClosing {
		t.Errorf("%q で本文が閉じ始めない: phase=%v", key, e.v.drawer.phase)
	}
	// 次の打鍵で実際に畳まれる (handleKey 冒頭の drawer.finish → discardBody)
	e.press("j")
	if e.v.open != nil {
		t.Errorf("%q の後に本文が捨てられない", key)
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

// URL ピッカー入力中は、外側の U 横取りを止めて viewer へ委譲する (issue 113)。
//
// urlPicker は doc で「印字文字はすべて検索語に流す。ここで個別のキーを先に横取りすると、
// その文字を含む URL を検索できなくなる」と宣言しているのに、tui.go が委譲の 3 行前で
// 大文字 U だけを奪っていた。`github.com/Ueno/...` のような URL を絞り込めない (silent)。
func TestIssuesViewerOwnsKeysDuringURLPicker(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(realIssue(t))
	if !m.issuesOv.urlPick.open([]string{"https://github.com/Ueno/repo", "https://example.com/x"}) {
		t.Fatal("前提が崩れた: URL ピッカーが開かない")
	}
	// ⚠️ 起動時グランスの既定を継承しないこと。継承すると片方向で vacuous になる (敵対的レビュー)
	m.usageOv.visible = false

	m.handleKey("U")

	// 本命の主張はこちら: キーが viewer へ届いているか
	if q := m.issuesOv.urlPick.query; q != "U" {
		t.Errorf("U が検索語に届いていない (外側が横取りしている): query=%q (期待 \"U\")", q)
	}
	// ⚠️ こちらは別の主張 (残量モーダルを開かない)。混ぜて 1 つの非難文にすると、dismiss の
	// 位置を変えただけで「キーを奪っている」と誤診する (敵対的レビューが実測)
	if m.usageOv.visible {
		t.Error("URL ピッカー入力中の U が残量モーダルを開いた")
	}
}

// ⚠️ 述語の 2 節目 (numFilter.typing) を pin する。これが無いと節ごと削る変異も、
// typing→active の 1 語変異も**全テスト green のまま通る** (敵対的レビューが実測)。
//
// active でなく typing を見る理由: 絞り込みを確定した後 (typing=false, active=true) は通常の
// ナビゲーションなので、そこで U を殺すと**絞り込みを解くまで U が恒久的に死ぬ**。
func TestIssuesViewerOwnsKeysWhileTypingNumberFilter(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(realIssue(t))
	m.issuesOv.numFilter.start()
	if !m.issuesOv.ownsKeys() {
		t.Fatal("番号入力中に ownsKeys() が false")
	}
	m.usageOv.visible = false
	m.handleKey("U")
	if m.usageOv.visible {
		t.Error("番号入力中の U が残量モーダルを開いた (入力モードのキーを外側が奪っている)")
	}

	// 確定後は通常のナビゲーションに戻る = U は外側で受ける
	// ⚠️ 数字を打ってから確定すること。空入力の確定は clear() に落ちて active も false になり、
	//   typing→active の変異を素通りさせる (敵対的レビューの再現手順は / → 0 → Enter だった)
	if !m.issuesOv.numFilter.edit("0") {
		t.Fatal("前提が崩れた: 番号フィルタが数字を受け付けない")
	}
	m.issuesOv.numFilter.confirm()
	if !m.issuesOv.numFilter.active {
		t.Fatal("前提が崩れた: 確定後に絞り込みが残っていない")
	}
	if m.issuesOv.ownsKeys() {
		t.Fatal("絞り込み確定後も ownsKeys() が true (U が恒久的に死ぬ)")
	}
	releaseKey(m) // 指を離す (上の U とキーリピート判定を跨ぐ)
	m.handleKey("U")
	if !m.usageOv.visible {
		t.Error("絞り込み確定後に U が効かない (typing でなく active を見ている)")
	}
}

// ⚠️ 述語の 3 節目 (markNext.active) を pin する。
//
// 変更前は確認中の U で usage が開き、**確認が armed のまま裏に残った** (続く y で実ファイル移動)。
// 変更後は U が「y/Enter 以外 = 取り消し」に落ちる (spec の「知らないキーを押したら実ファイルが
// 動いた、を作らない」と一致)。安全側の変化なので固定する。
func TestIssuesViewerOwnsKeysDuringMarkNextConfirm(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(realIssue(t))
	m.issuesOv.markNext = issuesMarkConfirm{active: true}
	if !m.issuesOv.ownsKeys() {
		t.Fatal("確認中に ownsKeys() が false")
	}
	m.usageOv.visible = false

	m.handleKey("U")

	if m.usageOv.visible {
		t.Error("確認中の U が残量モーダルを開いた (確認が armed のまま裏に残る)")
	}
	if m.issuesOv.markNext.active {
		t.Error("確認が取り消されていない (知らないキーで実ファイルが動く側に倒れている)")
	}
}

// ⚠️ ガードが広すぎないこと: viewer を開いているだけ (どのモードにも入っていない) なら
// U は従来どおり外側で受ける (ユーザー要望 2026-08-01 の「viewer の上でも U は効く」)。
// これが無いと「ownsKeys() を常に true にする」変異が素通りする。
func TestIssuesViewerUsageStillWorksWhenNotOwningKeys(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(realIssue(t))
	if m.issuesOv.ownsKeys() {
		t.Fatal("前提が崩れた: 一覧モードで ownsKeys() が true")
	}
	m.usageOv.visible = false

	m.handleKey("U")

	if !m.usageOv.visible {
		t.Error("viewer の一覧モードで U が効かない (横取りを止めすぎている)")
	}
}

// 本文モードでも i で viewer が閉じる (issue 122)。
//
// --help と README は i を「viewer 内のキー」として案内しているのに、handleBodyKey に case が
// 無く default の pagerScrollKey へ落ちて offset 不変で返っていた = **押しても何も起きず理由も
// 出ない**。同じ関数の case "s" には「本文だけ沈黙すると案内が嘘になる」という同型の根拠が
// 既に書かれている。
func TestIssuesViewerBodyModeIClosesViewer(t *testing.T) {
	v := loadedView(realIssue(t))
	v.openBody() // 本文モードへ
	if v.body == nil {
		t.Fatal("前提が崩れた: 本文が開いていない")
	}

	v.handleBodyKey("i", 20)

	if !v.closing && v.shown {
		t.Error("本文モードの i で viewer が閉じない (--help の案内が嘘になる)")
	}
}

// R は一覧モードでも本文モードでも ratelimit ダッシュボードへ横断する (ユーザー要望 2026-09-01)。
// s (status への横断) と同じ扱いで、viewer は閉じてから browseModel が開く (全画面は同時に 1 枚)。
// ⚠️ 本文モードも見る: case が無いと default の pagerScrollKey へ落ちて無音になる (issue 122 と同型)。
func TestIssuesViewerRSwitchesToRatelimitDash(t *testing.T) {
	t.Run("一覧モード", func(t *testing.T) {
		v := loadedView(realIssue(t))
		if v.body != nil {
			t.Fatal("前提が崩れた: 一覧モードでない")
		}
		v.handleKey("R", vp(20))
		if !v.takeWantRatelimit() {
			t.Error("一覧モードの R でダッシュボードへの横断を要求しない")
		}
		if !v.closing && v.shown {
			t.Error("一覧モードの R で viewer が閉じない")
		}
	})
	t.Run("本文モード", func(t *testing.T) {
		v := loadedView(realIssue(t))
		v.openBody()
		if v.body == nil {
			t.Fatal("前提が崩れた: 本文が開いていない")
		}
		v.handleBodyKey("R", 20)
		if !v.takeWantRatelimit() {
			t.Error("本文モードの R でダッシュボードへの横断を要求しない (--help の案内が嘘になる)")
		}
		if !v.closing && v.shown {
			t.Error("本文モードの R で viewer が閉じない")
		}
	})
	// 一度きりの信号 (takeNotice と同じ語彙。取り出した後は落ちている)
	v := loadedView(realIssue(t))
	v.handleKey("R", vp(20))
	v.takeWantRatelimit()
	if v.takeWantRatelimit() {
		t.Error("takeWantRatelimit が 2 回 true を返す (横断が二重に起きる)")
	}
}

// 一覧モードの u は無音で消えず、理由を返す (issue 122)。
//
// u は git log 一覧では pull、本文では URL ピッカー、status viewer では「pull は p です」を
// 返すのに、issues 一覧だけが無音だった。効かせない理由は openURLPicker の doc にあるが、
// それが画面に出ていなかった。
func TestIssuesViewerListModeUReturnsReason(t *testing.T) {
	v := loadedView(realIssue(t))
	if v.body != nil {
		t.Fatal("前提が崩れた: 一覧モードでない")
	}

	v.handleKey("u", vp(20))

	notice, _ := v.takeNotice()
	if notice == "" {
		t.Error("一覧モードの u が無音 (押したのに何も起きない = 壊れて見える)")
	}
	if !strings.Contains(notice, "本文") {
		t.Errorf("理由が「本文を開いてから」を案内していない: %q", notice)
	}
}
