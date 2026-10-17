package doctorview

import "testing"

func rows(spec ...string) []DoctorRow {
	out := make([]DoctorRow, 0, len(spec))
	for _, s := range spec {
		if s == "" {
			out = append(out, DoctorRow{Text: "(見出し)"}) // 選べない行
			continue
		}
		out = append(out, DoctorRow{Key: RowKey(s), Selectable: true, Text: s})
	}
	return out
}

// 選択は index ではなく key で覚える: 上に行が挿入されても同じ行に留まる (issue 210)。
func TestRowCursorKeepsRowWhenRowsInsertedAbove(t *testing.T) {
	var c rowCursor
	before := rows("", "a", "b")
	c.Move(before, 0)
	c.Move(before, +1) // b へ
	if c.Key != "b" {
		t.Fatalf("key = %q", c.Key)
	}
	after := rows("", "z", "a", "b") // 上に 1 行増えた
	c.restore(after)
	if got := after[c.Index].Key; got != "b" {
		t.Errorf("挿入で別の行を指した: %q", got)
	}
}

// key が消えたら近傍へ寄せ、**実際に動いたときだけ**「寄せた」と言う。
func TestRowCursorFallsBackAndTellsOnlyWhenMoved(t *testing.T) {
	var c rowCursor
	before := rows("", "a", "b", "c")
	c.Move(before, 0)
	c.Move(before, +1)
	c.Move(before, +1)          // c へ
	after := rows("", "a", "b") // c が消えた
	c.restore(after)
	if got := after[c.Index].Key; got != "b" {
		t.Errorf("寄せ先 = %q (近傍の b のはず)", got)
	}
	if !c.takeFellBack() {
		t.Error("寄せたことを伝えていない")
	}
	if c.takeFellBack() {
		t.Error("2 回目でも立っている (1 回だけ取り出す)")
	}
}

// 🚨 選べる行が 0 件のフレームでは key を捨てない (捨てると index 保持へ退行する)。
func TestRowCursorKeepsKeyWhenNothingSelectable(t *testing.T) {
	var c rowCursor
	full := rows("", "a", "b")
	c.Move(full, 0)
	c.Move(full, +1) // b
	empty := rows("", "", "")
	c.restore(empty)
	if c.Key != "b" {
		t.Fatalf("選べる行が無いフレームで key を捨てた: %q", c.Key)
	}
	if c.takeFellBack() {
		t.Error("動いていないのに寄せたと言った")
	}
	c.restore(full)
	if got := full[c.Index].Key; got != "b" {
		t.Errorf("戻ってきたら元の行に付かない: %q", got)
	}
}

// 動いた先の key を必ず覚える (覚えないと次の描画で古い key へ巻き戻り、G が効かない)。
func TestRowCursorRemembersAfterEveryMove(t *testing.T) {
	var c rowCursor
	rs := rows("", "a", "b", "c")
	c.Move(rs, 0)
	for _, want := range []string{"b", "c"} {
		c.Move(rs, +1)
		if string(c.Key) != want {
			t.Fatalf("key = %q, want %q", c.Key, want)
		}
	}
	c.Index = len(rs) - 1 // G 相当
	c.Move(rs, 0)
	if c.Key != "c" {
		t.Errorf("寄せ直しで key を覚えていない: %q", c.Key)
	}
	c.restore(rs)
	if got := rs[c.Index].Key; got != "c" {
		t.Errorf("次の描画で巻き戻った: %q", got)
	}
}

func TestRowCursorJumpTo(t *testing.T) {
	var c rowCursor
	rs := rows("", "disk:a", "diskitem:a\x00/x", "diskitem:a\x00/y", "disk:b")
	if !c.JumpTo(rs, "diskitem:a\x00") {
		t.Fatal("見つからない")
	}
	if got := rs[c.Index].Key; got != "diskitem:a\x00/x" {
		t.Errorf("最初の子へ移らない: %q", got)
	}
	if c.JumpTo(rs, "diskitem:zzz") {
		t.Error("無い prefix で動いた")
	}
	if got := rs[c.Index].Key; got != "diskitem:a\x00/x" {
		t.Errorf("見つからないときに位置が変わった: %q", got)
	}
}

// 窓は index が room 行の中に入るまで寄せる。
func TestRowCursorWindow(t *testing.T) {
	var c rowCursor
	c.Index = 30
	if got := c.window(50, 10); got != 21 {
		t.Errorf("下へスクロールしない: offset = %d", got)
	}
	c.Index = 5
	if got := c.window(50, 10); got != 5 {
		t.Errorf("上へスクロールしない: offset = %d", got)
	}
	c.Index = 49
	if got := c.window(50, 10); got != 40 {
		t.Errorf("末尾で窓が余る: offset = %d", got)
	}
	c.Index = 0
	if got := c.window(3, 10); got != 0 {
		t.Errorf("行が窓より少ないのに offset が動いた: %d", got)
	}
}

func TestRowCursorResetDropsEverything(t *testing.T) {
	c := rowCursor{Index: 5, Key: "x", offset: 3, FellBack: true}
	c.reset()
	if c != (rowCursor{}) {
		t.Errorf("残っている: %+v", c)
	}
}

// 🚨 行そのものが 0 件のフレームでも key を捨てない (上と同じ規律を全経路で守る)。
// production では buildRows が必ず区切り行を積むので到達しないが、ここだけ規律を外すと、
// 次に 0 行のフレームを作った人が index 保持へ黙って退行する (issue 242 P3-5)。
func TestRowCursorKeepsKeyWhenNoRowsAtAll(t *testing.T) {
	var c rowCursor
	full := rows("", "a", "b")
	c.Move(full, 0)
	c.Move(full, +1) // b
	c.restore(nil)
	if c.Key != "b" {
		t.Fatalf("0 行のフレームで key を捨てた: %q", c.Key)
	}
	if c.takeFellBack() {
		t.Error("動いていないのに寄せたと言った")
	}
	c.restore(full)
	if got := full[c.Index].Key; got != "b" {
		t.Errorf("戻ってきたら元の行に付かない: %q", got)
	}
}
