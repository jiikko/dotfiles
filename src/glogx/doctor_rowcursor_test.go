package main

import "testing"

// rowCursor は行の列だけを知る機械なので、**行を直接与えて**不変条件を試せる。
// (doctorView に同居していたときは、合成した disk.Result を作らないと試せなかった。
// その結果、fellBack のテストは production では起こらない縮小を捏造していた。
// 敵対レビュー 2026-09-03)

func rows(spec ...string) []doctorRow {
	out := make([]doctorRow, 0, len(spec))
	for _, s := range spec {
		if s == "" {
			out = append(out, doctorRow{text: "(見出し)"}) // 選べない行
			continue
		}
		out = append(out, doctorRow{key: s, selectable: true, text: s})
	}
	return out
}

// 選択は index ではなく key で覚える: 上に行が挿入されても同じ行に留まる (issue 210)。
func TestRowCursorKeepsRowWhenRowsInsertedAbove(t *testing.T) {
	var c rowCursor
	before := rows("", "a", "b")
	c.move(before, 0)
	c.move(before, +1) // b へ
	if c.key != "b" {
		t.Fatalf("key = %q", c.key)
	}
	after := rows("", "z", "a", "b") // 上に 1 行増えた
	c.restore(after)
	if got := after[c.index].key; got != "b" {
		t.Errorf("挿入で別の行を指した: %q", got)
	}
}

// key が消えたら近傍へ寄せ、**実際に動いたときだけ**「寄せた」と言う。
func TestRowCursorFallsBackAndTellsOnlyWhenMoved(t *testing.T) {
	var c rowCursor
	before := rows("", "a", "b", "c")
	c.move(before, 0)
	c.move(before, +1)
	c.move(before, +1)          // c へ
	after := rows("", "a", "b") // c が消えた
	c.restore(after)
	if got := after[c.index].key; got != "b" {
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
	c.move(full, 0)
	c.move(full, +1) // b
	empty := rows("", "", "")
	c.restore(empty)
	if c.key != "b" {
		t.Fatalf("選べる行が無いフレームで key を捨てた: %q", c.key)
	}
	if c.takeFellBack() {
		t.Error("動いていないのに寄せたと言った")
	}
	c.restore(full)
	if got := full[c.index].key; got != "b" {
		t.Errorf("戻ってきたら元の行に付かない: %q", got)
	}
}

// 動いた先の key を必ず覚える (覚えないと次の描画で古い key へ巻き戻り、G が効かない)。
func TestRowCursorRemembersAfterEveryMove(t *testing.T) {
	var c rowCursor
	rs := rows("", "a", "b", "c")
	c.move(rs, 0)
	for _, want := range []string{"b", "c"} {
		c.move(rs, +1)
		if c.key != want {
			t.Fatalf("key = %q, want %q", c.key, want)
		}
	}
	c.index = len(rs) - 1 // G 相当
	c.move(rs, 0)
	if c.key != "c" {
		t.Errorf("寄せ直しで key を覚えていない: %q", c.key)
	}
	c.restore(rs)
	if got := rs[c.index].key; got != "c" {
		t.Errorf("次の描画で巻き戻った: %q", got)
	}
}

func TestRowCursorJumpTo(t *testing.T) {
	var c rowCursor
	rs := rows("", "disk:a", "diskitem:a\x00/x", "diskitem:a\x00/y", "disk:b")
	if !c.jumpTo(rs, "diskitem:a\x00") {
		t.Fatal("見つからない")
	}
	if got := rs[c.index].key; got != "diskitem:a\x00/x" {
		t.Errorf("最初の子へ移らない: %q", got)
	}
	if c.jumpTo(rs, "diskitem:zzz") {
		t.Error("無い prefix で動いた")
	}
	if got := rs[c.index].key; got != "diskitem:a\x00/x" {
		t.Errorf("見つからないときに位置が変わった: %q", got)
	}
}

// 窓は index が room 行の中に入るまで寄せる。
func TestRowCursorWindow(t *testing.T) {
	var c rowCursor
	c.index = 30
	if got := c.window(50, 10); got != 21 {
		t.Errorf("下へスクロールしない: offset = %d", got)
	}
	c.index = 5
	if got := c.window(50, 10); got != 5 {
		t.Errorf("上へスクロールしない: offset = %d", got)
	}
	c.index = 49
	if got := c.window(50, 10); got != 40 {
		t.Errorf("末尾で窓が余る: offset = %d", got)
	}
	c.index = 0
	if got := c.window(3, 10); got != 0 {
		t.Errorf("行が窓より少ないのに offset が動いた: %d", got)
	}
}

func TestRowCursorResetDropsEverything(t *testing.T) {
	c := rowCursor{index: 5, key: "x", offset: 3, fellBack: true}
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
	c.move(full, 0)
	c.move(full, +1) // b
	c.restore(nil)
	if c.key != "b" {
		t.Fatalf("0 行のフレームで key を捨てた: %q", c.key)
	}
	if c.takeFellBack() {
		t.Error("動いていないのに寄せたと言った")
	}
	c.restore(full)
	if got := full[c.index].key; got != "b" {
		t.Errorf("戻ってきたら元の行に付かない: %q", got)
	}
}
