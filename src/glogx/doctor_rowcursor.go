package main

import "strings"

// rowCursor は「行の列の上でどこを選んでいるか」だけを知る状態機械。
//
// **disk / svc / brew を一切知らない**のが要点。doctorView に同居していたときは、
// 不変条件を試すのに合成した disk.Result を作る必要があり、実際そのテストは
// production では起こらない縮小を捏造していた (敵対レビュー 2026-09-03)。
// 行の列を直接与えれば、寄せ・key 保持・巻き戻しをそのまま検査できる。
//
// 不変条件 (どれも敵対レビューで実測した退行の跡):
//   - 選択は **index ではなく行の key** で覚える。走査中にカーソルより上へ行が挿入されると
//     index は別の行を指す (issue 210)
//   - **選べる行が 0 件のフレームでは key を捨てない**。捨てると index 保持へ退行する
//   - key が消えて寄せたときは、**実際に動いたときだけ**「寄せた」と言う
//   - 動いた先の key を**必ず**覚える (覚えないと次の描画で古い key へ巻き戻り、G が効かない)
type rowCursor struct {
	index  int
	key    string
	offset int
	// fellBack は「描画中に選択行が消えて寄せた」印。表示は呼び出し側が 1 回だけ取り出す
	// (描画から直接トーストを出せないため)。
	fellBack bool
}

// reset は世代の切り替え (開き直し / 再スキャン) で全部捨てる。
func (c *rowCursor) reset() { *c = rowCursor{} }

// restore は「前に選んでいた行」を key で探し直す。key が消えていたら index の近傍へ寄せる。
func (c *rowCursor) restore(rows []doctorRow) {
	if len(rows) == 0 {
		c.index, c.key = 0, ""
		return
	}
	if c.key != "" {
		for i, r := range rows {
			if r.selectable && r.key == c.key {
				c.index = i
				return
			}
		}
		// key が消えた (エントリが落ちた / 本文が変わった)。近傍へ寄せて、その事実を伝える
		old := c.key
		if c.index >= len(rows) {
			c.index = len(rows) - 1
		}
		c.move(rows, 0) // 🚨 move は remember するので c.key はここで書き換わる
		// 🚨 判定は「**別の選べる行に着いたか**」。
		//   - clamp 後の index と比べる形だと、行が縮んだだけで着地点が同じとき (c が消えて
		//     b に着く) に**報告されない** (2026-09-03 に実測: この経路が丸ごと死んでいた)
		//   - key の有無で見る形だと、選べる行が 0 件のフレーム (key は保持する) で
		//     動いていないのに寄せたと言う
		if c.index < len(rows) && rows[c.index].selectable && rows[c.index].key != old {
			c.fellBack = true
		}
		return
	}
	if c.index >= len(rows) {
		c.index = max(0, len(rows)-1)
	}
	c.move(rows, 0)
	c.remember(rows)
}

// remember は今の index が指す行の key を覚える (次の描画で復元する材料)。
func (c *rowCursor) remember(rows []doctorRow) {
	if c.index >= 0 && c.index < len(rows) && rows[c.index].selectable {
		c.key = rows[c.index].key
		return
	}
	// 🚨 選べる行が無いフレームでは既存の key を保持する (捨てると index 保持へ退行する)
}

// move は選べる行の間を dir 方向に 1 つ動く (0 = 今の位置を選べる行へ寄せる)。
func (c *rowCursor) move(rows []doctorRow, dir int) {
	if len(rows) == 0 {
		c.index = 0
		return
	}
	// 🚨 **関数の先頭で登録する**。dir==0 のブロックより後に置くと、その経路 (G / 寄せ直し) が
	//    key を覚えず、次の描画で restore が古い key の行へ巻き戻す
	defer c.remember(rows)
	i := c.index
	if dir == 0 {
		for j := i; j < len(rows); j++ {
			if rows[j].selectable {
				c.index = j
				return
			}
		}
		for j := i; j >= 0; j-- {
			if rows[j].selectable {
				c.index = j
				return
			}
		}
		return
	}
	for j := i + dir; j >= 0 && j < len(rows); j += dir {
		if rows[j].selectable {
			c.index = j
			return
		}
	}
}

// jumpTo は prefix で始まる key を持つ最初の選べる行へ移る (見つからなければ何もしない)。
func (c *rowCursor) jumpTo(rows []doctorRow, prefix string) bool {
	for i, r := range rows {
		if r.selectable && strings.HasPrefix(r.key, prefix) {
			c.index, c.key = i, r.key
			return true
		}
	}
	return false
}

// takeFellBack は「寄せた」印を 1 回だけ取り出す。
func (c *rowCursor) takeFellBack() bool {
	if !c.fellBack {
		return false
	}
	c.fellBack = false
	return true
}

// window は窓の先頭 (offset) を、index が room 行の中に入るよう寄せる。
func (c *rowCursor) window(rowCount, room int) int {
	if c.index < c.offset {
		c.offset = c.index
	}
	if c.index >= c.offset+room {
		c.offset = c.index - room + 1
	}
	if c.offset > max(0, rowCount-room) {
		c.offset = max(0, rowCount-room)
	}
	return c.offset
}
