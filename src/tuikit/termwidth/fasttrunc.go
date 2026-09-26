package termwidth

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// ansiTruncate は ansi.Truncate と同じ結果を返す。s が fastDispWidth の受理集合 (印字可能 ASCII・SGR・幅を表に持つ記号) だけで
// できているときは、書記素を 1 つずつ走査せずに切る位置を求める (pro-con の 1 フレームの CPU の約 2 割が切り詰め・切り出しの
// 書記素の走査だった。epic 523)。受理しない行は ansi.Truncate に任せる。
//
// 受理集合の字はどれも単独で 1 書記素になり、幅は ASCII が 1、記号が symbolWidth (fastDispWidthGeneric の doc)。そのため
// ansi.truncate の走査を、書記素の境を探さずに同じ順で辿れる。🚨 ansi.truncate の規則を写している: 収まる行はそのまま返す /
// tail の幅を引いた残りに収まるところまでを残し、はみ出す字の位置で tail を 1 回だけ書く / 切った後ろの SGR は全部残す。
// x/ansi を上げたら TestAnsiTruncateMatchesAnsi (総当たり) と FuzzAnsiTruncateMatchesAnsi で一致を確かめ直す
func ansiTruncate(s string, length int, tail string) string {
	total, ok := fastDispWidth(s)
	if !ok {
		return ansi.Truncate(s, length, tail)
	}
	if total <= length {
		return s
	}
	limit := length - Of(tail)
	if limit < 0 {
		return ""
	}
	// 収まるところまで (切る位置 cut) の byte はそのまま残る。はみ出す字から後ろは SGR だけが残る
	cut, cur := len(s), 0
scan:
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			i = sgrEnd(s, i)
		case c < 0x80: // 印字可能 ASCII (受理済みなので C0 は来ない)
			if cur+1 > limit {
				cut = i
				break scan
			}
			cur++
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if cur+symbolWidth(r) > limit {
				cut = i
				break scan
			}
			cur += symbolWidth(r)
			i += n
		}
	}
	return s[:cut] + tail + sgrOnly(s[cut:]) // 空でない側が 1 つだけなら連結は確保しない (TestAnsiTruncateNoAllocWithoutSGR)
}

// ansiTruncateLeft は ansi.TruncateLeft と同じ結果を返す (速い道は ansiTruncate と同じ理由・同じ受理集合)。
// 🚨 ansi.truncateLeft の規則を写している: n <= 0 ならそのまま / 左から幅 n を捨て、幅の累計が n を超えた字の位置で prefix を
// 1 回だけ書いて、その字から末尾までを残す (跨いだ幅 2 の字は残る) / 捨てた側にある SGR は prefix の前に全部残す
func ansiTruncateLeft(s string, n int, prefix string) string {
	if n <= 0 {
		return s
	}
	if _, ok := fastDispWidth(s); !ok {
		return ansi.TruncateLeft(s, n, prefix)
	}
	cur := 0
	for i := 0; i < len(s); {
		c := s[i]
		w, size := 1, 1
		switch {
		case c == 0x1b:
			i = sgrEnd(s, i)
			continue
		case c >= 0x80:
			var r rune
			r, size = utf8.DecodeRuneInString(s[i:])
			w = symbolWidth(r)
		}
		if cur+w > n {
			return sgrOnly(s[:i]) + prefix + s[i:]
		}
		cur += w
		i += size
	}
	return sgrOnly(s) // 全部捨てた: 残るのは SGR だけ
}

// sgrEnd は s[i] (ESC) から始まる SGR の直後の位置。s は fastDispWidth が受理した文字列 (ESC は必ず整った SGR) の前提
func sgrEnd(s string, i int) int {
	j := i + 2 // ESC [
	for s[j] != 'm' {
		j++
	}
	return j + 1
}

// sgrOnly は s (受理済み) から SGR だけを取り出して並べる。SGR が 1 つも無ければ "" (確保しない)
func sgrOnly(s string) string {
	first := strings.IndexByte(s, 0x1b)
	if first < 0 {
		return ""
	}
	var b strings.Builder
	for i := first; i < len(s); {
		if s[i] != 0x1b {
			i++
			continue
		}
		j := sgrEnd(s, i)
		b.WriteString(s[i:j])
		i = j
	}
	return b.String()
}
