// 新規予約フォーム。1 画面で「いつ送るか」と「何を送るか」を決め、発火時刻をその場で見せる
// (確認画面まで待たずに打ち間違いに気づけるようにする)。
//
// ⚠️ 画面は popup の内側 (既定 70x14) に必ず収める。溢れると非 alt-screen の描画では画面が
//
//	流れて表示が二重になり、本物のカーソル位置 (= IME の未確定文字が出る場所) も行数分ずれる
//	(2026-08-28 のユーザー報告。alt-screen 化と合わせてこのレイアウトで潰した)。
//	行数は model_test.go の TestFormFitsInPopup が上限で固定している。
package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// preset は「いつ送る」の選択肢。secs=0 の 2 つは自由入力欄を開く。
type preset struct {
	label string
	secs  int
	kind  presetKind
}

type presetKind int

const (
	kindFixed presetKind = iota
	kindClock            // HH:MM
	kindFree             // 90 / 1h30m / 1:30
)

// presets は候補の並び。ここが「表示名と秒」の単一の出典。
var presets = []preset{
	{"5分後", 300, kindFixed},
	{"10分後", 600, kindFixed},
	{"15分後", 900, kindFixed},
	{"30分後", 1800, kindFixed},
	{"1時間後", 3600, kindFixed},
	{"2時間後", 7200, kindFixed},
	{"4時間後", 14400, kindFixed},
	{"8時間後", 28800, kindFixed},
	{"時刻", 0, kindClock},
	{"自由", 0, kindFree},
}

type focusField int

const (
	focusWhen focusField = iota
	focusSpec            // 時刻 / 自由 のときだけ現れる入力欄
	focusText
)

type formState struct {
	whenIdx int
	spec    editor // HH:MM または 1h30m
	text    editor
	focus   focusField
	err     string
}

func newForm() formState { return formState{focus: focusText} }

func (f *formState) current() preset { return presets[f.whenIdx] }

// needsSpec は選択中の候補が自由入力欄を要るか。
func (f *formState) needsSpec() bool { return f.current().kind != kindFixed }

// fields は今フォーカスを回せる欄。
func (f *formState) fields() []focusField {
	if f.needsSpec() {
		return []focusField{focusWhen, focusSpec, focusText}
	}
	return []focusField{focusWhen, focusText}
}

func (f *formState) moveFocus(delta int) {
	fs := f.fields()
	cur := 0
	for i, x := range fs {
		if x == f.focus {
			cur = i
		}
	}
	f.focus = fs[(cur+delta+len(fs))%len(fs)]
}

// paste は貼り付けられた文字列を、フォーカス中の入力欄へ入れる。受け付けない文字は落として
// 残りを入れる (全部拒否すると「1 文字混ざっていたので何も貼れない」になる)。
func (f *formState) paste(s string) {
	if f.focus == focusWhen {
		return
	}
	ed := &f.text
	if f.focus == focusSpec {
		ed = &f.spec
	}
	var keep []rune
	for _, r := range s {
		if acceptable(string(r)) {
			keep = append(keep, r)
		}
	}
	if len(keep) > 0 {
		ed.insert(string(keep))
	}
}

func (f *formState) handleKey(key, text string) {
	f.err = ""
	switch key {
	case "tab", "down", "ctrl+n":
		f.moveFocus(1)
		return
	case "shift+tab", "up", "ctrl+p":
		f.moveFocus(-1)
		return
	}
	if f.focus == focusWhen {
		// 候補行では ctrl+f / ctrl+b も左右に割り当てる (入力欄での文字移動と同じ向き)
		switch key {
		case "left", "h", "ctrl+b":
			f.whenIdx = (f.whenIdx - 1 + len(presets)) % len(presets)
		case "right", "l", "space", "ctrl+f":
			f.whenIdx = (f.whenIdx + 1) % len(presets)
		}
		// 候補行で数字などを打っても入力欄へは送らない (誤爆を避ける)
		return
	}
	ed := &f.text
	if f.focus == focusSpec {
		ed = &f.spec
	}
	ed.handle(key, text)
}

// advance は Enter で次の欄へ進む。入力欄 (時刻 / 自由) から出るときだけ内容を検査し、
// 直せていないまま先へ行かせない (戻り値は表示用の日本語。空なら進んだ)。
func (f *formState) advance(now time.Time) string {
	if f.focus == focusSpec {
		if _, err := f.resolve(now); err != "" {
			return err
		}
	}
	f.moveFocus(1)
	return ""
}

// resolve は今の入力から発火時刻を出す。エラーは表示用の日本語。
func (f *formState) resolve(now time.Time) (time.Time, string) {
	p := f.current()
	switch p.kind {
	case kindFixed:
		return now.Add(time.Duration(p.secs) * time.Second), ""
	case kindClock:
		v := strings.TrimSpace(f.spec.value())
		if v == "" {
			return time.Time{}, "時刻を HH:MM で入れる (例 15:30)"
		}
		at, err := parseClock(v, now)
		if err != nil {
			return time.Time{}, "時刻は HH:MM で (例 15:30)"
		}
		return at, ""
	default:
		v := strings.TrimSpace(f.spec.value())
		if v == "" {
			return time.Time{}, "どれくらい後かを入れる (90 = 90分 / 1h30m / 1:30)"
		}
		d, err := parseDuration(v)
		if err != nil {
			return time.Time{}, fmt.Sprintf("90 / 1h30m / 1:30 の形で (0 と %d 桁超は不可)", maxDigits)
		}
		return now.Add(d), ""
	}
}

// submit は Enter で確定できるかを判定する。err が空でなければフォームに留まる。
func (f *formState) submit(now time.Time) (time.Time, string, string) {
	at, err := f.resolve(now)
	if err != "" {
		return time.Time{}, "", err
	}
	text := f.text.value()
	if strings.TrimSpace(text) == "" {
		return time.Time{}, "", "送る文字列を入れる"
	}
	return at, text, ""
}

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
	// ⚠️ 候補は折り返さず左右にずらす (折り返すと行数が増えて popup から溢れる)
	fr.add(fieldLabel("いつ", f.focus == focusWhen) + chips(f.whenIdx, f.focus == focusWhen, maxInt(width-labelCol, 0)))

	// 入力欄 (時刻 / 自由) は選んだときだけ出す
	if f.needsSpec() {
		f.addField(fr, specLabel(f.current().kind), &f.spec, f.focus == focusSpec, width)
	}

	fr.add("")
	f.addField(fr, "文字列", &f.text, f.focus == focusText, width)
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
// ⚠️ 幅は「実際に描く形」で数える: 各候補は括弧か空白で挟んで +2、候補の間に区切りの空白 +1、
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
