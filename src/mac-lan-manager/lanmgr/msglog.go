package lanmgr

// メッセージ一覧 (e で開く)。知らせ (toast) は数秒で消え、長い理由 (ssh の stderr など) は切れて読めないので、履歴として残して
// 選んでコピーできるようにする (ユーザー要望 2026-10-16「エラーが出たら、メッセージ一覧からログを y でコピー」)。

import (
	"fmt"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// logCap は覚えておく件数 (古いものから捨てる)。
const logCap = 100

// logEntry は 1 件の知らせ。text は切らずに持つ (コピーは全文)。
type logEntry struct {
	at   time.Time
	text string
	ok   bool
}

// record は履歴に足す (toast には出さない)。知らせ (note) はこれも通る。
func (m *Model) record(text string, ok bool) {
	m.logf("note %s %s", map[bool]string{true: "✓", false: "✗"}[ok], text)
	m.log = append(m.log, logEntry{at: m.o.Now(), text: text, ok: ok})
	if len(m.log) > logCap {
		m.log = m.log[len(m.log)-logCap:]
	}
	if m.ov == ovLog { // 開いている間に来た知らせ (y の「コピーした」も) は上に足される: 選んでいる 1 件がずれないように付いていく
		m.logC = min(m.logC+1, len(m.log)-1)
	}
}

// line は 1 件をコピーする形 (時刻・成否・本文を 1 行)。本文は相手の ssh の stderr を含みうる (相手が書ける文字列) ので、改行・
// 制御文字を落とす (生のまま貼ると複数の行が走り、ESC が bracketed paste を抜けた。敵対レビュー)。1 件 1 行なので Y も行で割れない
func (e logEntry) line() string {
	mark := "✗"
	if e.ok {
		mark = "✓"
	}
	return e.at.Format("15:04:05") + " " + mark + " " + termsafe.PlainLine(e.text)
}

// openLog はメッセージ一覧を開く (新しいものが上。カーソルは最新)。
func (m *Model) openLog() {
	m.ov, m.logC = ovLog, 0
}

// logAt は一覧の i 行目 (0 が最新) の 1 件。
func (m *Model) logAt(i int) logEntry { return m.log[len(m.log)-1-i] }

func (m *Model) keyLog(key string) Action {
	n := len(m.log)
	if n > 0 && move(&m.logC, n, key, max(m.logRows, 1)) {
		return None
	}
	switch key {
	case "esc", "q", "e":
		m.ov = ovNone
	case "y": // 選んだ 1 件を全文で
		if n == 0 {
			m.note("メッセージがまだ無い", false)
			return None
		}
		m.copyAs(m.logAt(m.logC).line(), "メッセージ 1 件")
	case "Y": // 全部 (古い順)
		if n == 0 {
			m.note("メッセージがまだ無い", false)
			return None
		}
		lines := make([]string, 0, n)
		for _, e := range m.log {
			lines = append(lines, e.line())
		}
		m.copyAs(strings.Join(lines, "\n"), fmt.Sprintf("メッセージ %d 件", n))
	}
	return None
}

// logBox はメッセージ一覧の板。行は板の幅で切る (全文は y でコピーする)。
func (m *Model) logBox() string {
	inner := max(min(m.w-8, 110), 20)
	m.logRows = max(m.bodyHeight()-10, 3)
	var body []string
	if len(m.log) == 0 {
		body = []string{sDim.Render("まだ無い")}
	} else {
		off := 0
		if len(m.log) > m.logRows {
			off = listnav.WindowOffset(m.logOffPrev, m.logC, len(m.log), m.logRows)
			m.logOffPrev = off
		}
		for i := off; i < min(off+m.logRows, len(m.log)); i++ {
			e := m.logAt(i)
			st := sOK
			if !e.ok {
				st = sWarn
			}
			row := sDim.Render(e.at.Format("15:04:05")+" ") + st.Render(termwidth.Truncate(termsafe.PlainLine(e.text), inner-9, "…"))
			if i == m.logC {
				row = selRow(row, inner)
			}
			body = append(body, row)
		}
	}
	return modal(fmt.Sprintf("メッセージ一覧  %d 件", len(m.log)), body, "j/k 選ぶ   y 1 件コピー   Y 全部コピー   e/esc 閉じる", m.w)
}
