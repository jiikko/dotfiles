package main

import (
	"strings"
	"testing"

	"glogx/issues"
)

// numbered は番号だけが違う issue 群 (カテゴリ・状態はばらけさせる)。
func numbered() []*issues.Issue {
	return []*issues.Issue{
		fakeIssue("415", "feat", "a", issues.StatusOpen),
		fakeIssue("141", "bug", "b", issues.StatusDone),
		fakeIssue("041", "refactor", "c", issues.StatusPending),
		fakeIssue("500", "feat", "d", issues.StatusOpen),
	}
}

// filteringView は番号を打ち込んだ状態の viewer を返す。
func filteringView(t *testing.T, query string, list ...*issues.Issue) *issuesView {
	t.Helper()
	v := loadedView(list...)
	v.handleKey("/", vp(20))
	for _, r := range query {
		v.handleKey(string(r), vp(20))
	}
	return v
}

func numbersOf(rows []*issues.Issue) string {
	out := make([]string, 0, len(rows))
	for _, iss := range rows {
		out = append(out, iss.Number)
	}
	return strings.Join(out, ",")
}

// 部分一致で絞り込む。🚨 タブ (カテゴリ) と状態フィルタの両方を無視する: 番号で引くのは
// 「その issue へ飛びたい」ときで、done だから出てこないのでは用を成さない。
func TestIssuesNumberFilterMatchesAcrossTabsAndStatuses(t *testing.T) {
	v := filteringView(t, "41", numbered()...)

	if got := numbersOf(v.rows); got != "415,141,041" {
		t.Fatalf("41 の絞り込み結果が %q (415,141,041 を期待)", got)
	}
	if v.filter != issues.FilterOpen {
		t.Fatal("前提が崩れた: 既定の状態フィルタが open のみでない")
	}
}

// 検索語を消すと元に戻る (1 字消しが効く)。
func TestIssuesNumberFilterBackspaceWidensAgain(t *testing.T) {
	v := filteringView(t, "415", numbered()...)
	if got := numbersOf(v.rows); got != "415" {
		t.Fatalf("415 の絞り込み結果が %q", got)
	}

	v.handleKey("backspace", vp(20))
	if got := numbersOf(v.rows); got != "415,141,041" {
		t.Fatalf("1 字消した後の結果が %q (41 相当を期待)", got)
	}
}

// 入力中は数字以外の印字文字を捨てる。🚨 一覧のキーとして実行しない: 打った文字で画面が動くと
// 「検索語を打っただけで何かが起きた」状態になる (将来タイトル検索を足すと j/k も検索語になる)。
func TestIssuesNumberFilterTypingSwallowsListKeys(t *testing.T) {
	v := filteringView(t, "4", numbered()...)
	before := numbersOf(v.rows)

	for _, key := range []string{"j", "a", "G", "tab"} {
		v.handleKey(key, vp(20))
		if v.cursor != 0 {
			t.Fatalf("%q でカーソルが動いた (入力中は飲むこと)", key)
		}
		if got := numbersOf(v.rows); got != before {
			t.Fatalf("%q で行集合が変わった: %q → %q", key, before, got)
		}
	}
	if v.numFilter.query != "4" {
		t.Fatalf("数字以外が検索語に混ざった: %q", v.numFilter.query)
	}
}

// Enter は入力を終えるだけで絞り込みは残す (残さないと y/p/n を結果へ効かせられない)。
func TestIssuesNumberFilterEnterKeepsRowsAndFreesKeys(t *testing.T) {
	v := filteringView(t, "41", numbered()...)
	v.handleKey("enter", vp(20))

	if v.numFilter.typing || !v.numFilter.active {
		t.Fatalf("Enter 後の状態が typing=%v active=%v (入力だけ終える)", v.numFilter.typing, v.numFilter.active)
	}
	if got := numbersOf(v.rows); got != "415,141,041" {
		t.Fatalf("Enter で絞り込みが解けた: %q", got)
	}
	v.handleKey("j", vp(20)) // 一覧のキーが戻っている
	if v.cursor != 1 {
		t.Fatalf("確定後に j が効かない (cursor=%d)", v.cursor)
	}
}

// 空のまま確定したら絞り込み自体をやめる (ヘッダーだけ残ると「絞り込み中」の嘘になる)。
func TestIssuesNumberFilterEmptyConfirmStopsFiltering(t *testing.T) {
	v := filteringView(t, "", numbered()...)
	v.handleKey("enter", vp(20))

	if v.numFilter.active {
		t.Fatal("空入力の確定で絞り込みが残った")
	}
}

// Esc は 1 段戻る = 絞り込みを解いて viewer は閉じない。
func TestIssuesNumberFilterEscUnfiltersBeforeClosing(t *testing.T) {
	v := filteringView(t, "41", numbered()...)
	v.handleKey("enter", vp(20)) // 入力を終えてから Esc (確定後の Esc も解除であること)

	v.handleKey("esc", vp(20))
	if v.numFilter.active {
		t.Fatal("Esc で絞り込みが解けない")
	}
	if !v.visible() {
		t.Fatal("Esc で viewer ごと閉じた (1 段戻ること)")
	}
	if got := numbersOf(v.rows); got != "415,500" {
		t.Fatalf("解除後にタブの行集合へ戻っていない: %q (open のみ = 415,500)", got)
	}
}

// viewer を閉じ切ったら絞り込みは捨てる。🚨 q / Esc は 1 段戻るだけ (絞り込みを解いて viewer は
// 残る) だが、i は 1 段戻さず閉じる。ここで捨てないと次に開いた viewer が絞り込まれたまま始まる。
func TestIssuesNumberFilterIsDroppedWhenViewerCloses(t *testing.T) {
	v := filteringView(t, "41", numbered()...)
	v.handleKey("enter", vp(20))

	v.handleKey("i", vp(20))
	if v.visible() {
		t.Fatal("前提が崩れた: i で閉じていない")
	}
	if v.numFilter.active {
		t.Fatal("閉じても絞り込みが残った (次に開くと理由の分からない絞り込み一覧から始まる)")
	}
	if got := numbersOf(v.rows); got != "415,500" {
		t.Fatalf("行集合が絞り込まれたまま残った: %q (open のみ = 415,500)", got)
	}
}

// 🚨 再スキャン (r / 見張り) を跨いでも絞り込みが残る。行集合を作るのが refresh の 1 箇所で
// ないと、絞り込みヘッダーを出したままタブの行へ黙って戻る。
func TestIssuesNumberFilterSurvivesRescan(t *testing.T) {
	v := filteringView(t, "41", numbered()...)
	v.handleKey("enter", vp(20))

	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: numbered()})

	if !v.numFilter.active {
		t.Fatal("再スキャンで絞り込みが消えた")
	}
	if got := numbersOf(v.rows); got != "415,141,041" {
		t.Fatalf("再スキャン後の行集合が絞り込まれていない: %q", got)
	}
}

// 一致 0 件の案内は状態フィルタの話をしない (a を押しても 1 件も増えないため)。
func TestIssuesNumberFilterEmptyMessageDoesNotSuggestStatusKey(t *testing.T) {
	v := filteringView(t, "999", numbered()...)

	msg := v.emptyMessage(renderOpts(20))
	if !strings.Contains(msg, "999") {
		t.Fatalf("一致なしの案内に検索語が出ない: %q", msg)
	}
	if strings.Contains(msg, "a: ") {
		t.Fatalf("一致なしの案内が状態フィルタを勧めている: %q", msg)
	}
}

// 絞り込み中のヘッダーはタブ行を置き換え、タブ・状態を無視していることを明示する。
func TestIssuesNumberFilterHeaderReplacesTabLine(t *testing.T) {
	v := filteringView(t, "41", numbered()...)

	head := stripANSI(v.listHeadLines(80, false)[0])
	if !strings.Contains(head, "番号: 41") || !strings.Contains(head, "全カテゴリ・全状態") {
		t.Fatalf("絞り込みヘッダーの内容が足りない: %q", head)
	}
	if strings.Contains(head, "All") {
		t.Fatalf("タブ行が残っている: %q", head)
	}
}

// isDigitKey は番号フィルタ入力の**唯一の関門**。issues_view.go:handleKey は typing 中の
// 全キーを numberFilterKey へ流し、default が edit(key) を呼ぶので、ここが 1 文字ずれると
// `/` (フィルタを開くキー自身) や `:` が検索語に入り「番号に『12/』を含む issue はありません」
// になる。境界を両側で pin する (issue 287。`'0'` を `'/'` へずらす変異が全スイート緑だった)。
func TestIsDigitKeyBoundaries(t *testing.T) {
	for _, c := range []struct {
		key  string
		want bool
	}{
		{"0", true}, {"9", true}, {"5", true},
		{"/", false}, // 下端の 1 つ外。フィルタを開くキー自身なので特に重要
		{":", false}, // 上端の 1 つ外
		{"a", false}, {" ", false}, {"", false},
		{"12", false},    // 1 文字であることも契約
		{"０", false},     // 全角数字は受けない (doc「数字以外の印字文字は無視」)
		{"enter", false}, // bubbletea のキー名がそのまま来る経路
	} {
		if got := isDigitKey(c.key); got != c.want {
			t.Errorf("isDigitKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}
