package doctorview

import (
	"testing"

	"doctor/disk"
)

// 走査中に行が増えても、選択は同じエントリに留まる (issue 210)。
//
// 🚨 **カーソルより上に挿入する** fixture を使う。disk 行は Size 降順に並べ替えて描くので、
// 大きい結果が後から届くと既存の行の上に入る。下に挿入する形では index が偶然一致して
// 素通りする (issue 210 のテスト観点)。
func TestCursorStaysOnSameRowWhenRowsGrowAbove(t *testing.T) {
	v := &DoctorView{Expanded: map[RowKey]bool{}, Selected: map[EntryID]bool{}, inspected: map[EntryID]bool{}}
	small := disk.Result{Entry: disk.Entry{ID: "small", Label: "小", Tier: 1}, Size: 100, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/small", Size: 100}}}
	mid := disk.Result{Entry: disk.Entry{ID: "mid", Label: "中", Tier: 1}, Size: 200, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/mid", Size: 200}}}
	huge := disk.Result{Entry: disk.Entry{ID: "huge", Label: "大", Tier: 1}, Size: 999999, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/huge", Size: 999999}}}

	o := DoctorRenderOpts{Page: 40, Width: 100}
	v.DiskResults = []disk.Result{small, mid}
	v.Lines(o) // 1 度描いて rows を組む

	// 一番上の選べる行から 1 つ下へ動く (= 2 番目のエントリを選ぶ)
	v.Cur.Move(v.Rows, 1)
	before := v.Rows[v.Cur.Index].Key
	if before == "" {
		t.Fatal("判定不能: 選択行に key が無い")
	}

	// 走査中に「もっと大きい結果」が届く = 並べ替えでカーソルより上に入る
	v.DiskResults = append(v.DiskResults, huge)
	v.Lines(o)

	after := v.Rows[v.Cur.Index].Key
	if after != before {
		t.Fatalf("走査中に選択が別の行へ移った: before=%s after=%s (y / Y が別エントリをコピーする)", before, after)
	}
}

// G (末尾へ) が次の描画で巻き戻らない (issue 210 の敵対レビュー P1-1。私が入れた回帰)。
//
// 🚨 `moveCursor(0)` を「移動」として使う経路 (G) が key を覚えないと、`restoreCursor` が
// 古い key の行へ cursor を戻す。実測された症状: after G cursor=6 / cursorKey="disk:b" →
// repaint で cursor=4 へ巻き戻る。
func TestCursorEndKeySurvivesRepaint(t *testing.T) {
	v := &DoctorView{Expanded: map[RowKey]bool{}, Selected: map[EntryID]bool{}, inspected: map[EntryID]bool{}}
	mk := func(id string, size int64) disk.Result {
		return disk.Result{Entry: disk.Entry{ID: id, Label: id, Tier: 1}, Size: size, Status: disk.StatusOK,
			Items: []disk.Item{{Path: "/tmp/" + id, Size: size}}}
	}
	o := DoctorRenderOpts{Page: 40, Width: 100}
	v.DiskResults = []disk.Result{mk("a", 300), mk("b", 200), mk("c", 100)}
	v.Lines(o)

	// G 相当: 末尾へ飛んで寄せる
	v.Cur.Index = len(v.Rows) - 1
	v.Cur.Move(v.Rows, 0)
	atEnd := v.Rows[v.Cur.Index].Key
	if atEnd == "" {
		t.Fatal("判定不能: 末尾の選べる行に key が無い")
	}

	v.Lines(o) // 次の描画
	if got := v.Rows[v.Cur.Index].Key; got != atEnd {
		t.Fatalf("G の後の描画で巻き戻った: at end=%s after repaint=%s", atEnd, got)
	}
}

// 選べる行が 0 件のフレームを挟んでも key を捨てない (issue 210 の敵対レビュー P2)。
// 捨てると index 保持へ退行する。
func TestCursorKeySurvivesFrameWithoutSelectableRows(t *testing.T) {
	v := &DoctorView{Expanded: map[RowKey]bool{}, Selected: map[EntryID]bool{}, inspected: map[EntryID]bool{}}
	a := disk.Result{Entry: disk.Entry{ID: "a", Label: "A", Tier: 1}, Size: 300, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/a", Size: 300}}}
	b := disk.Result{Entry: disk.Entry{ID: "b", Label: "B", Tier: 1}, Size: 200, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/b", Size: 200}}}
	o := DoctorRenderOpts{Page: 40, Width: 100}
	v.DiskResults = []disk.Result{a, b}
	v.Lines(o)
	v.Cur.Move(v.Rows, 1)
	want := v.Cur.Key
	if want == "" {
		t.Fatal("判定不能: key を覚えていない")
	}

	// 選べる行が 1 つも無いフレーム (結果が消えた瞬間)
	v.DiskResults = nil
	v.Lines(o)

	// 結果が戻ってきたら元の行へ復帰する。
	// 🚨 **戻すときに「上へ増える」形にする**。同じ 2 件で戻すと、key を捨てる退行でも
	//    index が偶然一致して緑になる (変異検証 2026-09-03 で実際に素通りした)
	huge := disk.Result{Entry: disk.Entry{ID: "huge", Label: "H", Tier: 1}, Size: 999999, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/huge", Size: 999999}}}
	v.DiskResults = []disk.Result{a, b, huge}
	v.Lines(o)
	if v.Rows[v.Cur.Index].Key != want {
		t.Fatalf("空フレームを挟んだら別の行へ移った: want=%s got=%s", want, v.Rows[v.Cur.Index].Key)
	}
}
