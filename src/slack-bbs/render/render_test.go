package render

import (
	"strings"
	"testing"
	"time"

	"slack-bbs/board"
)

// 外から来た文字列 (本文・名前・見出し・要点・ラベルの理由・注記・リンクの根) は端末の制御文字を落としてから出す。
func TestMarkdownStripsTerminalControls(t *testing.T) {
	esc := "\x1b]0;pwned\x07\x1b[2J"
	u := board.Unit{ID: "1.000000", Thread: true, Msgs: []board.Msg{{Ts: "1.000000", Name: "n" + esc, Text: "本文" + esc + "\n2 行目" + esc, New: true}}}
	p := Page{
		ChannelName: "dev" + esc, Channel: "C1", Base: "https://ws.slack.com/" + esc, Notes: []string{"注記" + esc},
		Topics: []board.Topic{{Title: "見出し" + esc, State: board.StateOpen, Units: []board.Unit{u},
			Points: []board.Point{{Section: board.SectionConclusion, Text: "要点" + esc, Refs: []string{"1.000000"}}},
			Labels: []board.Label{{Kind: board.KindMustRead, Source: board.SourceLLM, Reason: "理由" + esc, Refs: []string{"1.000000"}}}}},
		Since: time.Unix(0, 0), Until: time.Unix(10, 0), Lookback: time.Unix(0, 0),
	}
	var sb strings.Builder
	if err := Markdown(&sb, p); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(sb.String(), "\x1b\x07") {
		t.Errorf("制御文字が出た: %q", sb.String())
	}
	for _, want := range []string{"本文", "2 行目", "見出し", "要点", "理由", "注記"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("%q が消えた", want)
		}
	}
}

// 省いた文脈・切れたスレッド・読めなかったスレッドは、画面に印を出す。
func TestMarkdownShowsOmittedAndUnreadable(t *testing.T) {
	u := board.Unit{ID: "1.000000", Thread: true, Continuing: true, Omitted: 3, Truncated: true, Unreadable: true,
		Msgs: []board.Msg{{Ts: "1.000000", Name: "a", Text: "親"}, {Ts: "2.000000", Name: "b", Text: "新", New: true}}}
	var sb strings.Builder
	if err := Markdown(&sb, Page{Topics: []board.Topic{{Title: "t", State: board.StateInfo, Units: []board.Unit{u}}}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"期間前の返信 3 件を省略", "途中で切れている", "スレッドを読めなかった", "🔁 続いているスレッド", "(前) "} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("%q が無い:\n%s", want, sb.String())
		}
	}
}

// 複数行の発言の続きの行は、時刻の頭から 2 桁下げる (期間前の発言の「(前)」は全角を含むので、表示幅で揃える)。
func TestContinuationLinesIndentFromTimestamp(t *testing.T) {
	u := board.Unit{ID: "1.000000", Thread: true, Msgs: []board.Msg{
		{Ts: "1.000000", Name: "a", Text: "親\n親の 2 行目"},
		{Ts: "2.000000", Name: "b", Text: "返信\n返信の 2 行目", New: true},
	}}
	lines := Page{}.Body(board.Topic{Title: "t", State: board.StateInfo, Units: []board.Unit{u}}, false)
	checked := 0
	for i, l := range lines[:len(lines)-1] {
		at := strings.Index(l, tsTime("1.000000").Format("01/02"))
		if at < 0 {
			continue
		}
		next := lines[i+1]
		got := len(next) - len(strings.TrimLeft(next, " "))
		want := 6 // "    " の 4 桁 + 2
		if strings.Contains(l, "(前)") {
			want = 11 // "    (前) " の表示幅 9 + 2
		}
		if got != want {
			t.Errorf("続きの行の字下げ %d, want %d:\n%q\n%q", got, want, l, next)
		}
		checked++
	}
	if checked != 2 {
		t.Errorf("確かめた発言 %d, want 2:\n%s", checked, strings.Join(lines, "\n"))
	}
}

// 要約は欄 (結論 → 経緯 → 次の動き) の順に、見出しを付けて空行で区切って出す。読む画面は見出しを HeadingMark で始め、
// Markdown は太字にする。項目の無い欄は見出しも出さない。
func TestBodySections(t *testing.T) {
	u := board.Unit{ID: "1.000000", Msgs: []board.Msg{{Ts: "1.000000", Name: "a", Text: "本文", New: true}}}
	tp := board.Topic{Title: "t", State: board.StateOpen, Units: []board.Unit{u}, Points: []board.Point{
		{Section: board.SectionNext, Text: "次", Refs: []string{"1.000000"}},
		{Section: board.SectionConclusion, Text: "結論の文", Refs: []string{"1.000000"}},
	}}
	got := strings.Join(Page{}.Body(tp, false), "\n")
	want := HeadingMark + "結論\n  結論の文\n\n" + HeadingMark + "次の動き\n  • 次\n\n" + HeadingMark + "原文\n"
	if !strings.HasPrefix(got, want) || strings.Contains(got, "経緯") {
		t.Errorf("読む画面の形:\n%s\nwant の頭:\n%s", got, want)
	}
	md := strings.Join(Page{}.Body(tp, true), "\n")
	if !strings.HasPrefix(md, "**結論**\n- 結論の文\n\n**次の動き**\n- 次\n\n**原文**\n\n") {
		t.Errorf("Markdown の形:\n%s", md)
	}
}

// 要約の項目の頭の空白は落とす (読む画面で原文の発言の行と同じ字下げにならない)。
func TestBodyTrimsPointText(t *testing.T) {
	u := board.Unit{ID: "1.000000", Msgs: []board.Msg{{Ts: "1.000000", Name: "a", Text: "x", New: true}}}
	tp := board.Topic{Title: "t", State: board.StateInfo, Units: []board.Unit{u}, Points: []board.Point{
		{Section: board.SectionConclusion, Text: "  11/09 15:07 tanaka: 了解", Refs: []string{"1.000000"}}}}
	if got := (Page{}).Body(tp, false)[1]; got != "  11/09 15:07 tanaka: 了解" {
		t.Errorf("結論の行 %q (頭の空白を落としていない)", got)
	}
}
