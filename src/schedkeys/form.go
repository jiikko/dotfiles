// 新規予約フォーム。1 画面で「いつ送るか」と「何を送るか」を決め、発火時刻をその場で見せる
// (確認画面まで待たずに打ち間違いに気づけるようにする)。
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

func (f *formState) handleKey(key, text string, now time.Time) {
	f.err = ""
	switch key {
	case "tab", "down":
		f.moveFocus(1)
		return
	case "shift+tab", "up":
		f.moveFocus(-1)
		return
	}
	if f.focus == focusWhen {
		switch key {
		case "left", "h":
			f.whenIdx = (f.whenIdx - 1 + len(presets)) % len(presets)
			f.fixFocus()
			return
		case "right", "l", "space":
			f.whenIdx = (f.whenIdx + 1) % len(presets)
			f.fixFocus()
			return
		}
		// 候補行で数字などを打ったら入力欄へ送らない (誤爆を避ける)
		return
	}
	ed := &f.text
	if f.focus == focusSpec {
		ed = &f.spec
	}
	ed.handle(key, text)
}

// fixFocus は候補を動かして自由入力欄が消えたときにフォーカスを戻す。
func (f *formState) fixFocus() {
	if f.focus == focusSpec && !f.needsSpec() {
		f.focus = focusWhen
	}
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

// view はフォームを描き、フォーカス中の入力欄に置く本物のカーソルを返す
// (bubbletea v2 の View.Cursor。これが無いと IME の未確定文字が別の場所に出る)。
func (f *formState) view(label string, now time.Time, width int) (string, *tea.Cursor) {
	var b strings.Builder
	b.WriteString(dim("新規予約  対象: ") + label + "\n\n")

	// 候補行 (幅で折り返す)
	b.WriteString(dim("いつ") + "\n")
	line := "  "
	lineW := 2
	for i, p := range presets {
		chip := " " + p.label + " "
		w := ansi.StringWidth(chip)
		if lineW+w > width-2 && lineW > 2 {
			b.WriteString(line + "\n")
			line, lineW = "  ", 2
		}
		if i == f.whenIdx {
			mark := "\x1b[7m" + chip + "\x1b[0m"
			if f.focus == focusWhen {
				mark = "\x1b[7;1m" + chip + "\x1b[0m"
			}
			line += mark
		} else {
			line += chip
		}
		lineW += w
	}
	b.WriteString(line + "\n")

	var cur *tea.Cursor
	row := func(prefix string, ed *editor, focused bool) {
		// 行頭からの列 = プレフィックスの表示幅 + 入力済みの表示幅
		col := ansi.StringWidth(prefix) + ed.cursorCol()
		curLine := strings.Count(b.String(), "\n")
		b.WriteString(prefix + ed.value() + "\n")
		if focused {
			c := tea.NewCursor(col, curLine)
			cur = c
		}
	}
	if f.needsSpec() {
		b.WriteString("\n")
		p := "  " + specLabel(f.current().kind) + " "
		row(p, &f.spec, f.focus == focusSpec)
	}
	b.WriteString("\n" + dim("文字列") + "\n")
	row("  > ", &f.text, f.focus == focusText)

	b.WriteString("\n")
	if at, err := f.resolve(now); err != "" {
		b.WriteString("  " + err + "\n")
	} else {
		b.WriteString(fmt.Sprintf("  %s に送る (%s)\n", at.Format("15:04"), formatRemaining(at.Sub(now))))
	}
	if f.err != "" {
		b.WriteString("  " + f.err + "\n")
	}
	b.WriteString("\n" + dim("Tab 欄移動   左右 候補   Enter 予約   Esc 戻る"))
	return b.String(), cur
}

func specLabel(k presetKind) string {
	if k == kindClock {
		return "時刻 (HH:MM)"
	}
	return "どれくらい後 (90 / 1h30m)"
}
