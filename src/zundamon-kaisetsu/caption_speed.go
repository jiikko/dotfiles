package main

import (
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// captionCPSMax は字幕を読み切れる速さの上限 (1 秒あたりの字数)。YES (yuya-takeyama/agent-plugins) の video の
// CPS_WARN と同じ値。音声を聞きながら読む前提の目安。build は、超えた行があれば書き出しの前に止まる
// (--allow-fast-captions で続ける)。警告だけで続けていたころは、mp4 を撮り終えてから気づいて撮り直していた (issue 676)。
const captionCPSMax = 7.5

// fastCaption は字幕を読み切れないおそれのある行。
type fastCaption struct {
	Line int     // 台本の行番号 (0 始まり)
	CPS  float64 // 1 秒あたりの字数
	Text string
}

// fastCaptions は、字幕が出ている間 (その行の開始から次の行の開始まで。最後の行は動画の終わりまで) に読む字数が
// captionCPSMax を超える行を返す。字数は空白を除いた文字の数。
func fastCaptions(lines []timelineLine, duration float64) []fastCaption {
	var out []fastCaption
	for i, l := range lines {
		end := duration
		if i+1 < len(lines) {
			end = lines[i+1].Start
		}
		shown := end - l.Start
		n := 0
		for _, r := range l.Text {
			if !unicode.IsSpace(r) {
				n++
			}
		}
		if shown <= 0 {
			continue // 字幕が出ない行 (timeline の上ではありえない) は数えない
		}
		if cps := float64(n) / shown; cps > captionCPSMax {
			out = append(out, fastCaption{Line: i, CPS: cps, Text: l.Text})
		}
	}
	return out
}

// warnFastCaptions は字幕の速すぎる行を、行番号と先頭の文字つきで warn に書き (無ければ何も書かない)、その行数を返す。
func warnFastCaptions(w io.Writer, lines []timelineLine, duration float64) int {
	fast := fastCaptions(lines, duration)
	if len(fast) == 0 {
		return 0
	}
	fmt.Fprintf(w, "build: 字幕が速くて読み切れないおそれのある行が %d 行 (1 秒 %.1f 字を超える。行を分けるか、その行の pause_after で間を足す):\n",
		len(fast), captionCPSMax)
	for _, f := range fast {
		text := f.Text
		if utf8.RuneCountInString(text) > 24 {
			text = string([]rune(text)[:24]) + "…"
		}
		fmt.Fprintf(w, "  lines[%d] %.1f 字/秒 「%s」\n", f.Line, f.CPS, text)
	}
	return len(fast)
}
