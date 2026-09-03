// 新規予約フォームの描画。状態 (form.go) を読むだけで、状態は変えない。
//
// 🚨 画面は popup の内側 (既定 70x14) に必ず収める。溢れると端末が流れて表示が二重になり、
//
//	本物のカーソル位置 (= IME の未確定文字が出る場所) も行数分ずれる (2026-08-28 の報告)。
//	幅・高さ・カーソルの保証は frame (layout.go) が持つので、行は必ず frame 経由で足す。
package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// 欄の見出し幅。行頭の "> " (2) + 見出し (全角 3 文字 = 6) + 区切りの空白 (1)。
// 揃えることで入力欄の左端が縦に並ぶ。
const labelCol = 9

// view はフォームを描き、フォーカス中の入力欄に置く本物のカーソルを返す
// (bubbletea v2 の View.Cursor。これが無いと IME の未確定文字が別の場所に出る)。
func (f *formState) view(label string, now time.Time, width, height int) (string, *tea.Cursor) {
	fr := newFrame(width, height)
	fr.add(sgr(fgDim, "新規予約") + "  " + sgr(fgAccent, truncate(label, maxInt(width-10, 0))))
	fr.add("")

	// 「いつ」: 見出し + 候補。選択中は色付きの [ ] で囲む (太字だけだと分からない、の指摘 2026-08-28)。
	// 🚨 候補は折り返さず左右にずらす (折り返すと行数が増えて popup から溢れる)
	fr.add(fieldLabel("いつ", f.focus == focusWhen) + chips(f.whenIdx, f.focus == focusWhen, maxInt(width-labelCol, 0)))

	// 入力欄 (時刻 / 自由) は選んだときだけ出す
	if f.needsSpec() {
		f.addField(fr, specLabel(f.current().kind), &f.spec, f.focus == focusSpec, width)
	}

	fr.add("")
	f.addField(fr, "何を", &f.text, f.focus == focusText, width)
	fr.add("")

	// 結果 (発火時刻) かエラーを 1 行だけ出す。両方出すと同じ文言が二重に並ぶ
	if msg := f.err; msg != "" {
		fr.add(sgr(fgErr, "  "+msg))
	} else if at, err := f.resolve(now); err != "" {
		fr.add(sgr(fgErr, "  "+err))
	} else {
		ok := fmt.Sprintf("  %s に送る", at.Format("15:04"))
		fr.add(sgr(fgOK, ok) + sgr(fgDim, fmt.Sprintf("  (%s後)", formatRemaining(at.Sub(now)))))
	}

	enterHelp := "Enter 次へ"
	if f.focus == focusText {
		enterHelp = "Enter 予約"
	}
	fr.add(sgr(fgDim, help(width, "Tab/C-n 欄移動", "h/l C-f/C-b 候補", enterHelp, "Esc 戻る")))
	return fr.render()
}

// addField は「見出し + 入力欄」の 1 行を足す。フォーカス中なら本物のカーソルもその行に置く
// (bubbletea v2 の View.Cursor。これが無いと IME の未確定文字が別の場所に出る)。
func (f *formState) addField(fr *frame, name string, ed *editor, focused bool, width int) {
	prefix := fieldLabel(name, focused)
	col := ansi.StringWidth(stripSGR(prefix))
	val, cur := ed.viewport(maxInt(width-col-1, 0), focused)
	if focused {
		fr.addAt(prefix+val, col+cur)
		return
	}
	fr.add(prefix + val)
}

// fieldLabel は見出しを labelCol 幅に揃える。フォーカス中は色を変え、行頭に > を置く
// (記号は ASCII に限る: 端末によって幅が変わる記号を混ぜると桁がずれる)。
func fieldLabel(name string, focused bool) string {
	mark := "  "
	if focused {
		mark = sgr(fgAccent, "> ")
	}
	pad := labelCol - 2 - ansi.StringWidth(name)
	if pad < 0 {
		pad = 0
	}
	body := sgr(fgDim, name)
	if focused {
		body = sgr(fgAccent+";"+bold, name)
	}
	return mark + body + strings.Repeat(" ", pad)
}

// chips は候補を 1 行に並べる。幅に入らない分は左右にずらして「選択中が必ず見える」ようにする
// (折り返すと行数が増えて popup から溢れるため、1 行に固定する)。
func chips(sel int, focused bool, width int) string {
	labels := make([]string, len(presets))
	for i, p := range presets {
		labels[i] = p.label
	}
	// 選択中の候補だけは必ず出す。それすら入らない幅なら候補名を切り詰める
	// (切り詰めないと行が幅を超え、端末が折り返して行数が増える = カーソルがずれる)
	if w := ansi.StringWidth(labels[sel]) + 2; w > width {
		labels[sel] = truncate(labels[sel], maxInt(width-2, 0))
	}
	lo, hi := chipRange(labels, sel, width)
	// 端の省略記号を出す余地が無いなら出さない (選択中の候補の閉じ括弧を押し出してしまう)
	showLeft := lo > 0 && chipWidth(labels, lo, hi) <= width
	showRight := hi < len(labels)-1 && chipWidth(labels, lo, hi) <= width
	var out strings.Builder
	if showLeft {
		out.WriteString(sgr(fgDim, "< "))
	}
	for i := lo; i <= hi; i++ {
		if i == sel {
			style := fgAccent
			if focused {
				style = revAccent + ";" + bold
			}
			out.WriteString(sgr(style, "["+labels[i]+"]"))
		} else {
			out.WriteString(sgr(fgDim, " "+labels[i]+" "))
		}
		if i < hi {
			out.WriteString(" ")
		}
	}
	if showRight {
		out.WriteString(sgr(fgDim, " >"))
	}
	return out.String()
}

// chipWidth は候補 lo..hi を描いたときの表示幅 (端の省略記号ぶんを含む)。
func chipWidth(labels []string, lo, hi int) int {
	w := 0
	for i := lo; i <= hi; i++ {
		w += ansi.StringWidth(labels[i]) + 2
		if i < hi {
			w++
		}
	}
	if lo > 0 {
		w += 2
	}
	if hi < len(labels)-1 {
		w += 2
	}
	return w
}

// chipRange は幅 width に収まる候補の範囲を選ぶ (sel を必ず含む)。
// 🚨 幅は「実際に描く形」で数える: 各候補は括弧か空白で挟んで +2、候補の間に区切りの空白 +1、
//
//	端の省略記号 "< " / " >" が +2 ずつ。ここを近似すると行が幅を超え、端末が折り返して
//	行数が増える (= popup から溢れてカーソルがずれる。2026-08-28 の回帰テストで検出)。
func chipRange(labels []string, sel, width int) (int, int) {
	total := func(lo, hi int) int { return chipWidth(labels, lo, hi) }
	lo, hi := sel, sel
	for {
		grew := false
		if hi < len(labels)-1 && total(lo, hi+1) <= width {
			hi++
			grew = true
		}
		if lo > 0 && total(lo-1, hi) <= width {
			lo--
			grew = true
		}
		if !grew {
			return lo, hi
		}
	}
}

func specLabel(k presetKind) string {
	if k == kindClock {
		return "時刻"
	}
	return "あと"
}
