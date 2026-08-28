// 新規予約フォームの状態機械。「いつ送るか」と「何を送るか」を持ち、キーで遷移し、
// 確定できるかを判定する。**描き方は持たない** (それは form_view.go)。
package main

import (
	"fmt"
	"strings"
	"time"
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

// 開いた直後は「いつ」にフォーカスする。この画面の流れは「いつ → (時刻/自由) → 何を → 予約」で、
// 最初に決めるのは送る時刻だから (ユーザー要望 2026-08-28)。Enter が次の欄へ進むので、
// 開いてから予約まで Enter だけで辿れる。
func newForm() formState { return formState{focus: focusWhen} }

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

// focusedEditor は今フォーカスしている入力欄。候補行のときは文字列欄を返す
// (呼び出し側は候補行かどうかを先に見てから使う)。
func (f *formState) focusedEditor() *editor {
	if f.focus == focusSpec {
		return &f.spec
	}
	return &f.text
}

// paste は貼り付けられた文字列を、フォーカス中の入力欄へ入れる。受け付けない文字は落として
// 残りを入れる (全部拒否すると「1 文字混ざっていたので何も貼れない」になる)。
func (f *formState) paste(s string) {
	if f.focus == focusWhen {
		return
	}
	ed := f.focusedEditor()
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
	f.focusedEditor().handle(key, text)
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
			return time.Time{}, fmt.Sprintf("90 / 1h30m / 1:30 の形で (0 と %d 日超は不可)", int(maxDuration.Hours()/24))
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
		return time.Time{}, "", "何を送るかを入れる"
	}
	return at, text, ""
}
