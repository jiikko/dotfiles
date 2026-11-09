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
			Points: []board.Point{{Text: "要点" + esc, Refs: []string{"1.000000"}}},
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
