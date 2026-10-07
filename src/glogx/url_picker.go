package main

// urlPicker は issue 本文中の URL を絞り込んで選ぶピッカー (本文 pager で u)。
//
// なぜ「順に開く」でなくピッカーか: 実測で URL の本数は issue によって偏る (dotfiles の 6 件は
// 1 本 ×4 / 2 本 ×1 / 24 本 ×1)。順送りだと 24 本の research issue で 17 番目に辿り着くまでに
// 16 個のタブが開いてしまい実用にならない。一覧から選べれば本数に依らず 1 回の操作で済む。
//
// 入力の作法は fzf 流にする: 印字文字はすべて検索語になり、移動は ctrl+n/p と矢印、確定は
// Enter、取り消しは Esc。🚨 j/k を移動に使わない — インクリメンタルサーチでは j も k も
// 検索語の一部であり、両立させると「j を打つと勝手にカーソルが動く」か「移動できない」の
// どちらかになる。この割り切りをやめるなら検索モードを別キー (/) に分ける必要がある。
//
// 検索語の編集 (カーソルの移動・語の削除・ctrl+u / ctrl+k) は tuikit/lineedit に任せる
// (docs/glogx-ui-guide.md §7)。

import (
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

type urlPicker struct {
	active bool
	urls   []string      // 元の並び (本文の出現順)
	line   lineedit.Line // インクリメンタルサーチの検索語
	match  []int         // query に一致する urls の index (絞り込み結果。並びは出現順)
	cursor int           // match 内の位置
	offset int           // 窓の先頭 (描画のたびに listnav.WindowOffset で更新する。issues 一覧と同じ規律)
}

// open はピッカーを開く。URL が 1 本も無ければ開かず false を返す (呼び出し側が通知を出す)。
func (p *urlPicker) open(urls []string) bool {
	if len(urls) == 0 {
		return false
	}
	p.active, p.urls, p.line, p.cursor, p.offset = true, urls, lineedit.Line{}, 0, 0
	p.refilter()
	return true
}

// query は今の検索語。
func (p *urlPicker) query() string { return p.line.String() }

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
	q := strings.ToLower(p.query())
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
// 🚨 印字文字はすべて検索語に流す (default 節)。ここで個別のキーを先に横取りすると、その文字を
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
	case " ":
		// Space は URL に現れないので絞り込みには不要。誤爆 (本文スクロールの癖) を無視する
	default:
		before := p.query()
		text := ""
		if isPrintableKey(key) {
			text = key // 1 文字の印字キーは、その文字を入れる (KeyPressMsg.String() は印字キーなら Text と同じ)
		}
		p.line.Key(key, text)
		if after := p.query(); after != before {
			if text != "" {
				p.cursor = 0 // 絞り込み直後は先頭を見せる (fzf と同じ)
			}
			p.refilter()
		}
	}
	return false, false
}

// paste は貼り付けた文字列を検索語のカーソルの位置に入れる (bracketed paste。tui.go の PasteMsg)。
// 先に termsafe で無害化する (ESC のシーケンスを丸ごと・BiDi の制御文字を落とす。lineedit.Insert は ESC 1 字しか
// 落とさず、`[31m` の残骸や RLO が検索語と画面に残る。敵対レビューが実測)。空白・改行・タブも落とす: URL に現れず、
// 打鍵の Space も入れていない (handleKey) ので、貼り付けた URL の末尾の改行が空白として残って何にも一致しなくなるのを避ける。
func (p *urlPicker) paste(s string) {
	if !p.active {
		return
	}
	s = strings.Join(strings.Fields(sanitizePlainLine(s)), "")
	if s == "" {
		return
	}
	p.line.Insert(s)
	p.cursor = 0 // 絞り込み直後は先頭を見せる (打鍵と同じ)
	p.refilter()
}

// isPrintableKey は検索語に足してよい 1 文字か。修飾キー付き ("ctrl+x") や名前付きキー
// ("pgdown") を弾くため、1 ルーンで制御文字でないものだけを通す。
func isPrintableKey(key string) bool {
	r := []rune(key)
	return len(r) == 1 && r[0] >= 0x20 && r[0] != 0x7f
}

// urlPickerPrompt は検索行の頭。キャレットの桁はこの幅から数える (caretCol)。
const urlPickerPrompt = "URL 検索: "

// caretCol は検索行 (lines の 0 行目) のキャレットの桁。端末のカーソルはここに置く (issuesView.caretCol)。
func (p *urlPicker) caretCol(width int) int {
	_, caret := promptWindow(urlPickerPrompt, &p.line, width)
	return caret
}

// promptWindow は「見出し + 入力欄」の 1 行について、入力欄に見せる部分 (行の幅に収まるよう
// lineedit.Line.Window で前を切る) と、行頭から数えたキャレットの桁を返す。描画とキャレットの
// 両方がこれを通る (別々に組むと、見出しを変えたときにキャレットだけ取り残されて IME の変換中の
// 文字が欄からずれる。URL ピッカーと番号の入力欄が共有する。issue 666)。
func promptWindow(prompt string, line *lineedit.Line, width int) (text string, caret int) {
	text, col := line.Window(max(width-termwidth.Of(prompt), 1))
	return text, termwidth.Of(prompt) + col
}

// lines はピッカーの描画行 (ヘッダー + 一覧)。width/page は呼び出し側の領域。
// 選択行はカーソル溝 (→) で示し、cursorPaint があれば強調も乗せる (一覧と同じ語彙)。
func (p *urlPicker) lines(o issuesRenderOpts) []string {
	text, _ := promptWindow(urlPickerPrompt, &p.line, o.width)
	head := []string{
		paint(clipToWidth(urlPickerPrompt+text, o.width), ansiBold, o.colored),
		paint(clipToWidth(fmt.Sprintf("%d/%d 件  ctrl+n/p: 移動  ctrl+h: 1 字消す  Enter: 開く  Esc: 戻る",
			len(p.match), len(p.urls)), o.width), ansiDim, o.colored),
		"",
	}
	rows := max(o.page-len(head), 1)
	if len(p.match) == 0 {
		return append(head, paint(clipToWidth("一致する URL がありません", o.width), ansiDim, o.colored))
	}
	// 窓は状態 (offset) で持ち、カーソルが窓を出たときだけ動かす (issues 一覧と同じ規律。以前は
	// カーソルから毎回導出していて、下端を越えた後に上へ戻すと窓が 1 行ずつ戻った。issue 666)
	p.offset = listnav.WindowOffset(p.offset, p.cursor, len(p.match), rows)
	out := head
	for i := p.offset; i < min(p.offset+rows, len(p.match)); i++ {
		text := p.urls[p.match[i]]
		if i != p.cursor {
			out = append(out, clipToWidth(cursorGutterBlank+text, o.width))
			continue
		}
		out = append(out, o.paintCursorRow(text, o.width, ansiBold))
	}
	return out
}
