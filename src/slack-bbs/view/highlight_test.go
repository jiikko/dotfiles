package view

import (
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/board"
	"slack-bbs/render"
)

func hlFor(names map[string]bool) highlighter {
	var msgs []board.Msg
	for n, mine := range names {
		msgs = append(msgs, board.Msg{Ts: "1.000000", Name: n, Mine: mine, New: true})
	}
	return newHighlighter(render.Page{Topics: []board.Topic{{Units: []board.Unit{{ID: "1.000000", Msgs: msgs}}}}})
}

// 見出し・原文の時刻と名前・本文の名前 (自分は別の色)・「」・番号に色を付け、表示の幅は変えない。
// 名前と番号は語の境目でだけ当てる (英単語・URL・時刻の途中に当てない)。
func TestHighlightLine(t *testing.T) {
	h := hlFor(map[string]bool{"hiroyuki.sato": false, "kawaguchi.koji": true, "Bach": false})
	for _, c := range []struct {
		name, in string
		want     []string
		not      []string
	}{
		{"見出し", "▍結論", []string{sgrHeading + "▍結論" + sgrOff}, nil},
		{"原文の頭", "    (前) 11/09 15:07 hiroyuki.sato: PR 24010 を見た",
			[]string{"(前) " + sgrDim + "11/09 15:07" + sgrOff, sgrName + "hiroyuki.sato" + sgrOff + ": ", sgrNumber + "24010" + sgrOff}, nil},
		{"自分の発言", "    11/09 15:07 kawaguchi.koji: 了解", []string{sgrMe + "kawaguchi.koji" + sgrOff}, nil},
		{"本文の名前と「」と番号", "  • kawaguchi.koji に「テストはやったほうがいい」と PR(#1047, 24058〜24060) を依頼。",
			[]string{sgrMe + "kawaguchi.koji" + sgrOff, sgrQuote + "「テストはやったほうがいい」" + sgrOff, sgrNumber + "#1047" + sgrOff, sgrNumber + "24058〜24060" + sgrOff}, nil},
		{"文末の名前", "  • 依頼先は hiroyuki.sato.", []string{sgrName + "hiroyuki.sato" + sgrOff + "."}, nil},
		{"英字の直後の番号", "  PR6600・6601 を入れる", []string{sgrNumber + "6600" + sgrOff, sgrNumber + "6601" + sgrOff}, nil},
		{"語の途中・長い数字・版", "  Bachelor と abc24010x と 11/09 15:07 と 12 件と p1794275725841219 と 2.48.0", nil, []string{sgrName, sgrNumber}},
	} {
		got := h.line(c.in, true)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q に %q が無い", c.name, got, w)
			}
		}
		for _, w := range c.not {
			if strings.Contains(got, w) {
				t.Errorf("%s: %q に色 %q が付いた", c.name, got, w)
			}
		}
		if termwidth.Of(got) != termwidth.Of(c.in) {
			t.Errorf("%s: 幅が変わった %d → %d", c.name, termwidth.Of(c.in), termwidth.Of(got))
		}
	}
}

// 閉じていない「 (折り返しで割れた) には色を付けない (行の残りを全部緑にしない)。
func TestHighlightUnclosedQuote(t *testing.T) {
	if got := hlFor(nil).line("  「折り返しで割れた", true); strings.Contains(got, sgrQuote) {
		t.Errorf("閉じていない「に色を付けた: %q", got)
	}
}

// 原文の 2 行目から (深く字下げされた行) は、本文が「▍」や「時刻 名前: 」で始まっても見出し・発言の頭の色にしない。
func TestHighlightIgnoresLookalikesInMessageBody(t *testing.T) {
	h := hlFor(map[string]bool{"kawaguchi.koji": true})
	for _, in := range []string{"      ▍見出しのふり", "      11/09 15:07 kawaguchi.koji: 発言のふり", "  ▍字下げした見出し"} {
		got := h.line(in, true)
		if strings.Contains(got, sgrHeading) || strings.Contains(got, sgrDim) {
			t.Errorf("%q を見出し・発言の頭にした: %q", in, got)
		}
	}
}

// 折り返しの続きの行 (first が偽) は、見出し・発言の頭の形をしていても色を付けない。
func TestHighlightContinuationIsNotHead(t *testing.T) {
	h := hlFor(map[string]bool{"kawaguchi.koji": true})
	for _, in := range []string{"▍見出しのふり", "    11/09 15:07 kawaguchi.koji: 発言のふり"} {
		if got := h.line(in, false); strings.Contains(got, sgrHeading) || strings.Contains(got, sgrDim) {
			t.Errorf("折り返しの続き %q を見出し・発言の頭にした: %q", in, got)
		}
	}
}
