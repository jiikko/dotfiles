package view

import (
	"slices"
	"strings"
	"time"

	"slack-bbs/board"

	"github.com/jiikko/dotfiles/src/tuikit/listnav"
)

// 読み直しの後の一覧の窓。glogx の git log の一覧 (browse_log.go の applyLogData / startPullAnim) にならう。カーソルはどちらでも同じ話題に置く:
//   - 一覧を下へ送って見ている間は、カーソルの話題を同じ画面の行に出す (増えた話題は窓の上の外に積む)
//   - 一覧の先頭を見ている間は、窓を先頭に置いたまま (カーソルが窓の下に出るときだけ、カーソルが一番下の行に来る位置)。
//     一覧の先頭に新しい話題が増えたら、読み直しの直後は前の見え方に一番近い窓の位置から始め、着地の位置まで上から 1 行ずつ降らせる
//     (元からある行が下へずれて見える)。前の見え方に一番近い位置は、前の窓の各行 (話題と見出し) と新しい一覧を照合して決める
//
// 窓を作り直さない: 作り直すと Fit がカーソルを窓の一番下に寄せ、読み直しのたびに一覧がスクロールしたように見える。
// 窓の一番上の話題を錨にしない (glogx はそうする): slack-bbs の話題は読み直しで並びが変わる (新着のタブで既読にした話題は
// 既読の組へ移る・返信が付くと未読の組へ戻る)。錨が動いただけで「上へ増えた」と読み違え、増えていない行を降らせ、窓を跳ばした。
// 先頭を見ている間にカーソルの行を保とうともしない: 既にある話題がカーソルより上へ移ると窓が下がり、先頭の見出しと話題が窓の上の外に隠れる。
// 降らせる行の数をカーソルのずれで決めない: カーソルの話題が自分で下へ移っただけで、増えていない既存の行を何行も降らせる。
// 降らせ始めの位置を、増えた行の数を数える式で決めない: 「着地の位置 + 増えた話題の数」「前の一番上の話題のずれ」「新しい一覧で最初の
// 前からの話題の行 − 前の一番上の行」と 3 通り試し、どれも話題が沈む・消える・上へ移る・見出しが増える場面で数え違えて、
// 元からある行を上へ跳ねさせてから降らせた。照合なら、最初の 1 枚は定義から前の見え方に一番近い。

const (
	// listDropStep は上から降らせる 1 行の間隔、listDropMax は降らせる行の上限 (超えた分は最初から所定の位置に置く)。
	// どちらも glogx の pull の演出と同じ (spinnerInterval の tick ごとに 1 行・pullAnimMaxLines)。
	// 行の数は壁時計で決める: 読み直しと同時に知らせが滑り込み、その間は tick が frameInterval で回るので、tick の回数で数えると速くなる
	listDropStep = 80 * time.Millisecond
	listDropMax  = 8
)

// listView は読み直しの前の一覧の窓。
type listView struct {
	cursor    string          // カーソルの話題 (keyOf)
	cursorRow int             // カーソルの画面の行 (窓の中の何行目か)
	offset    int             // 窓の位置
	known     map[string]bool // 一覧にあった話題の発言 (チャンネル|発言の ID)。読み直しで増えた話題を見分ける
	window    []rowID         // 窓に出ていた行 (降らせ始めの位置を、新しい一覧と照合して決める)
}

// listDrop は一覧の先頭へ増えた行を上から降らせる演出。窓の位置を from から to へ、listDropStep ごとに 1 行ずつ戻す。
type listDrop struct {
	from, to int
	start    time.Time
	active   bool
}

// keyOf は一覧の行の話題の鍵 (selKey と同じ形)。
func (m *Model) keyOf(r row) string {
	c := m.chans[m.ownerOf(r)]
	return c.ch.ID + "|" + c.page.Topics[r.topic].Units[0].ID
}

// indexOf は鍵の話題の行 (無ければ -1)。話題の中のどの発言でも当たれば同じ話題とみなす (読み直しで話題の最初の発言が変わっても追える)。
func (m *Model) indexOf(key string) int {
	ch, unit, _ := strings.Cut(key, "|")
	for i, r := range m.rows() {
		if r.header() {
			continue
		}
		c := &m.chans[m.ownerOf(r)]
		if c.ch.ID == ch && c.page != nil && slices.ContainsFunc(c.page.Topics[r.topic].Units, func(u board.Unit) bool { return u.ID == unit }) {
			return i
		}
	}
	return -1
}

// captureListView は今の一覧の窓を控える (読み直しの板を入れる前に呼ぶ)。降らせている途中なら着地させてから控える:
// 途中の窓の位置を控えると、先頭を見ているのに「下へ送って見ている」と読み、演出が途中で止まる。
func (m *Model) captureListView() listView {
	m.finishListDrop()
	v := listView{cursor: m.selKey(), cursorRow: m.list.Cursor - m.list.Offset, offset: m.list.Offset, known: map[string]bool{}}
	rows := m.rows()
	for i, r := range rows {
		id := m.idOf(r)
		for k := range id.units {
			v.known[k] = true
		}
		if i >= m.list.Offset && i < m.list.Offset+m.listRows() {
			v.window = append(v.window, id)
		}
	}
	return v
}

// restoreListView は読み直しの板を入れた後に、控えた窓と同じ見え方へ戻す (このファイルの冒頭の扱い)。
// 見ていた話題が無くなったら、keepOnLost が偽なら先頭へ戻し、真なら窓をそのまま残す (新着のタブ。前から行の番号の付近に残していた)。
func (m *Model) restoreListView(v listView, keepOnLost bool) {
	m.drop = listDrop{}
	m.list.Stop()
	if !m.reselect(v.cursor) {
		if !keepOnLost {
			m.list = listnav.List{}
			m.fitList(1)
		}
		return
	}
	if v.offset > 0 {
		m.list.Offset = max(m.list.Cursor-v.cursorRow, 0)
		m.fitList(1)
		return
	}
	to := max(0, m.list.Cursor-m.listRows()+1)
	// 降らせ始めの位置は、読み直しの直後に前の見え方に一番近くなる位置 (closestStart)。探すのは、カーソルが窓の上に出ない所 (低い端末) と、
	// 窓が一覧の下に余らない所 (短い一覧では描画が窓を詰め、降らせても見えない) まで。降らせる行の上限 (listDropMax) は closestStart が掛ける
	from := m.closestStart(v, to, min(m.list.Cursor, max(0, len(m.rows())-m.listRows())))
	m.list.Offset = max(from, to)
	if from > to {
		m.drop = listDrop{from: from, to: to, start: m.now(), active: true}
	}
}

// rowID は一覧の行の見分け。見出しは文と日、話題は発言の集合 (チャンネル|発言の ID。1 つでも重なれば同じ話題とみなす。indexOf と同じ)。
type rowID struct {
	header string
	units  map[string]bool
}

func (m *Model) idOf(r row) rowID {
	if r.header() {
		return rowID{header: r.label + "|" + r.day.Format(time.DateOnly)}
	}
	c := m.chans[m.ownerOf(r)]
	id := rowID{units: map[string]bool{}}
	for _, u := range c.page.Topics[r.topic].Units {
		id.units[c.ch.ID+"|"+u.ID] = true
	}
	return id
}

func (a rowID) same(b rowID) bool {
	if a.header != "" || b.header != "" {
		return a.header == b.header
	}
	for k := range a.units {
		if b.units[k] {
			return true
		}
	}
	return false
}

// closestStart は降らせ始めの窓の位置: to から hi までのうち、前の窓と同じ画面の行に同じ行が一番多く並ぶ位置 (同点なら小さい方)。
// その位置に降らせる行の上限 (to + listDropMax) を掛け、以下の判定は上限で切った位置 (実際の最初の 1 枚) で行う。
// 次のどちらかなら to (降らせない):
//   - 降らせて現れる行と、最初の 1 枚で窓の一番上に出て前の窓と一致しない行に、新しい話題が無い (前からある行が上へ移っただけ)。
//     一番上の一致しない行も見る: 見出しの下に 1 件だけ増えると、新しい話題は最初の 1 枚の一番上に出て、降らせて現れるのは見出しだけになる
//   - 跳ね返る行がある: 前からある行で、着地では元の画面の行かそれより下に出るのに、最初の 1 枚では元の行より上に出る (か窓の上に隠れる)
//     行。見出しも数えるが、前の窓の一番上の見出しが窓の上に隠れるのは数えない (受容)。一番上に残る話題 (要対応・返信が続くスレッド) の下に
//     新しい話題が 2 件以上入ると、その話題は最初の 1 枚で窓の上に隠れてから降りてくる。一番上の話題やカーソルの話題が窓の中で沈み、
//     同時に新しい話題が増えると、窓の下側がそろう位置が照合で勝ち、
//     沈んだ話題より上にあった行が跳ねる。読み直しで本当に上へ移った行 (着地でも元の行より上) は数えない (もともと上へ動く)。
//     着地の位置でそろう行だけを見ない: ずれが 1 行の行と 2 行の行が混ざると、どの行も着地の位置でそろわずに素通りする
//     その日の要対応の話題の下に新しい話題が入ると、要対応の話題がこの形になって (件数に依らず) 降らない。窓全体の位置を動かす作りでは、要対応の話題を
//     止めたまま下だけを降らせられない。前の作り (読み直しのたびに窓を作り直す) も降らせないので、悪化ではない
func (m *Model) closestStart(v listView, to, hi int) int {
	rows := m.rows()
	best, bestN := to, -1
	for s := to; s <= hi; s++ {
		n := 0
		for i, old := range v.window {
			if s+i < len(rows) && old.same(m.idOf(rows[s+i])) {
				n++
			}
		}
		if n > bestN {
			best, bestN = s, n
		}
	}
	// 降らせる行の上限は、見つけた位置に掛ける (探す範囲に掛けると、多く増えたときに一致する位置が範囲の外に出て降らせない)。
	// 以下の判定は上限で切った位置 (実際の最初の 1 枚) で行う
	best = min(best, to+listDropMax)
	top := best // 最初の 1 枚の窓の一番上から並ぶ、前の窓と一致しない行の終わり
	for i, old := range v.window {
		if best+i >= len(rows) || old.same(m.idOf(rows[best+i])) {
			break
		}
		top = best + i + 1
	}
	for j := to; j < min(best+len(v.window), len(rows)); j++ {
		id := m.idOf(rows[j])
		for i, old := range v.window {
			if j < best && i == 0 && rows[j].header() {
				continue // 最初の 1 枚で窓の上に隠れる、前の窓の一番上の見出し (見出しの下に行が増えるたびに隠れる。数えると主な場面が全部降らない)
			}
			if old.same(id) && j-to >= i && j-best < i {
				return to
			}
		}
	}
	for _, r := range rows[to:top] {
		if !r.header() && !m.known(r, v.known) {
			return best
		}
	}
	return to
}

// known は話題の行が、読み直しの前の一覧にあったか (どれか 1 つの発言が known にある)。
func (m *Model) known(r row, known map[string]bool) bool {
	c := m.chans[m.ownerOf(r)]
	return slices.ContainsFunc(c.page.Topics[r.topic].Units, func(u board.Unit) bool { return known[c.ch.ID+"|"+u.ID] })
}

// advanceListDrop は降らせる演出を時刻に合わせて進める (tick から呼ぶ)。
func (m *Model) advanceListDrop() {
	if !m.drop.active {
		return
	}
	off := max(m.drop.to, m.drop.from-int(m.now().Sub(m.drop.start)/listDropStep))
	m.list.Offset = off
	if off == m.drop.to {
		m.drop.active = false
	}
}

// finishListDrop は降らせる演出を着地させる (キーを押したとき。動いている窓の上でカーソルを動かさない)。
func (m *Model) finishListDrop() {
	if m.drop.active {
		m.list.Offset = m.drop.to
		m.drop.active = false
	}
}
