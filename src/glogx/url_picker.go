package main

// urlPicker は issue 本文中の URL を絞り込んで選ぶピッカー (本文 pager で u)。
//
// なぜ「順に開く」でなくピッカーか: 実測で URL の本数は issue によって偏る (dotfiles の 6 件は
// 1 本 ×4 / 2 本 ×1 / 24 本 ×1)。順送りだと 24 本の research issue で 17 番目に辿り着くまでに
// 16 個のタブが開いてしまい実用にならない。一覧から選べれば本数に依らず 1 回の操作で済む。
//
// 入力の作法は fzf 流にする: 印字文字はすべて検索語になり、移動は ctrl+n/p と矢印、確定は
// Enter、取り消しは Esc。⚠️ j/k を移動に使わない — インクリメンタルサーチでは j も k も
// 検索語の一部であり、両立させると「j を打つと勝手にカーソルが動く」か「移動できない」の
// どちらかになる。この割り切りをやめるなら検索モードを別キー (/) に分ける必要がある。

import (
	"fmt"
	"strings"
)

type urlPicker struct {
	active bool
	urls   []string // 元の並び (本文の出現順)
	query  string   // インクリメンタルサーチの検索語
	match  []int    // query に一致する urls の index (絞り込み結果。並びは出現順)
	cursor int      // match 内の位置
}

// open はピッカーを開く。URL が 1 本も無ければ開かず false を返す (呼び出し側が通知を出す)。
func (p *urlPicker) open(urls []string) bool {
	if len(urls) == 0 {
		return false
	}
	p.active, p.urls, p.query, p.cursor = true, urls, "", 0
	p.refilter()
	return true
}

// close はピッカーを閉じる (状態は捨てる。次に開いたときは検索語なしから始める)。
func (p *urlPicker) close() { *p = urlPicker{} }

// selected は今選んでいる URL ("" = 一致なし)。
func (p *urlPicker) selected() string {
	if !p.active || p.cursor < 0 || p.cursor >= len(p.match) {
		return ""
	}
	return p.urls[p.match[p.cursor]]
}

// refilter は query で match を作り直し、カーソルを範囲内へ収める。
// 大文字小文字を無視した部分一致 (URL はホスト名も path も小文字が多く、入力で shift を
// 押させる意味がない)。
func (p *urlPicker) refilter() {
	q := strings.ToLower(p.query)
	p.match = p.match[:0]
	for i, u := range p.urls {
		if q == "" || strings.Contains(strings.ToLower(u), q) {
			p.match = append(p.match, i)
		}
	}
	p.cursor = max(min(p.cursor, len(p.match)-1), 0)
}

// handleKey はピッカー表示中のキーを処理する。open=true なら選択を確定した (呼び出し側が
// selected() を開く)。closed=true なら閉じた。どちらも false なら絞り込み/移動を続行中。
//
// ⚠️ 印字文字はすべて検索語に流す (default 節)。ここで個別のキーを先に横取りすると、その文字を
// 含む URL を検索できなくなる。
func (p *urlPicker) handleKey(key string) (open, closed bool) {
	if !p.active {
		return false, false
	}
	switch key {
	case "esc", "ctrl+g":
		p.close()
		return false, true
	case "enter":
		if p.selected() == "" {
			return false, false // 一致なしでの Enter は無視 (閉じない = 検索語を直せる)
		}
		return true, false
	case "down", "ctrl+n":
		if len(p.match) > 0 {
			p.cursor = (p.cursor + 1) % len(p.match)
		}
	case "up", "ctrl+p":
		if len(p.match) > 0 {
			p.cursor = (p.cursor - 1 + len(p.match)) % len(p.match)
		}
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.refilter()
		}
	case "ctrl+u":
		p.query = ""
		p.refilter()
	case " ":
		// Space は URL に現れないので絞り込みには不要。誤爆 (本文スクロールの癖) を無視する
	default:
		if isPrintableKey(key) {
			p.query += key
			p.cursor = 0 // 絞り込み直後は先頭を見せる (fzf と同じ)
			p.refilter()
		}
	}
	return false, false
}

// isPrintableKey は検索語に足してよい 1 文字か。修飾キー付き ("ctrl+x") や名前付きキー
// ("pgdown") を弾くため、1 ルーンで制御文字でないものだけを通す。
func isPrintableKey(key string) bool {
	r := []rune(key)
	return len(r) == 1 && r[0] >= 0x20 && r[0] != 0x7f
}

// lines はピッカーの描画行 (ヘッダー + 一覧)。width/page は呼び出し側の領域。
// 選択行はカーソル溝 (→) で示し、cursorPaint があれば強調も乗せる (一覧と同じ語彙)。
func (p *urlPicker) lines(o issuesRenderOpts) []string {
	head := []string{
		paint(clipToWidth(fmt.Sprintf("URL 検索: %s_", p.query), o.width), ansiBold, o.colored),
		paint(clipToWidth(fmt.Sprintf("%d/%d 件  ctrl+n/p: 移動  Enter: 開く  Esc: 戻る",
			len(p.match), len(p.urls)), o.width), ansiDim, o.colored),
		"",
	}
	rows := max(o.page-len(head), 1)
	if len(p.match) == 0 {
		return append(head, paint(clipToWidth("一致する URL がありません", o.width), ansiDim, o.colored))
	}
	// 窓はカーソルから導出する (issues 一覧と同じ規律。offset を状態で持たない)。
	offset := max(min(p.cursor-rows+1, len(p.match)-rows), 0)
	offset = min(offset, p.cursor)
	out := head
	for i := offset; i < min(offset+rows, len(p.match)); i++ {
		text := p.urls[p.match[i]]
		if i != p.cursor {
			out = append(out, clipToWidth(cursorGutterBlank+text, o.width))
			continue
		}
		line := clipToWidth(cursorGutterMark+text, o.width)
		if o.cursorPaint != nil {
			line = o.cursorPaint(line)
		} else {
			line = clipToWidth(cursorGutterMark+paint(text, ansiBold, o.colored), o.width)
		}
		out = append(out, line)
	}
	return out
}
