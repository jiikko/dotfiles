package report

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TruncationHint は text の結果を件数の上限で打ち切ったときに、全件の大きさと次の手 (rg) を先頭に示すための問い合わせの情報。
// dupscan 自身は rg を実行しない。出すのは実行できる 1 行の提示だけで、構文木の結果ではなく文字列の一致になる
// (コメント・文字列にも当たる) ことを文に書く。Label が空なら案内を出さない。
type TruncationHint struct {
	Label   string   // 件数の呼び方 ("該当" / "呼び出し")
	Pattern string   // rg に渡す正規表現 (メタ文字は済ませる)。空なら rg を案内しない
	Ignore  bool     // 大文字小文字を無視して探す (-i)
	Globs   []string // 走査した言語の拡張子の glob (-g)
	Dirs    []string // 走査の対象。空なら .
}

// header は打ち切ったときだけ出す先頭の 1 行。total は全件、shown は表示する件数、allBytes は全件を text で出したときの byte 数。
func (h TruncationHint) header(shown, total, allBytes int) string {
	line := fmt.Sprintf("%s %d 件 (%d 件表示 / 全部で約 %s)。", h.Label, total, shown, humanBytes(allBytes))
	if h.Pattern == "" {
		return strings.TrimSuffix(line, "。")
	}
	// -e でパターンを、-- で走査の対象を明示する: - で始まる名前や dir が rg のオプションに読まれない
	cmd := "rg -n"
	if h.Ignore {
		cmd += " -i"
	}
	cmd += " -e " + shellQuote(h.Pattern)
	for _, g := range h.Globs {
		cmd += " -g " + shellQuote(g)
	}
	dirs := h.Dirs
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	cmd += " --"
	for _, d := range dirs {
		cmd += " " + shellQuote(d)
	}
	return line + "絞るか、文字列 (コメント含む) で探すなら: " + cmd
}

func humanBytes(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1000)
}

// shellQuote は sh で 1 語になる単引用符の形にする。安全な文字だけなら引用符を付けない。
func shellQuote(s string) string {
	safe := s != ""
	for _, r := range s {
		letterOrDigit := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !letterOrDigit && !strings.ContainsRune("_-./", r) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShortDoc は doc の先頭行を、最初の文の終わりまで、かつ maxBytes まで (rune の途中で切らない) に縮める。maxBytes が 0 以下なら縮めない。
// 文の終わりは `。` か、後ろが空白で次の語が大文字か非 ASCII の `.` (略語 e.g. / Dr. などの後ろでは切らない)。上限で切ったときだけ … を付ける。
// 2 文目に理由や条件がある doc (`Do not use X. It is unsafe.`) も最初の文で切れる。全文が要るときは -doc-bytes 0 か JSON。
func ShortDoc(doc string, maxBytes int) string {
	if maxBytes <= 0 {
		return doc
	}
	if end := sentenceEnd(doc); end >= 0 {
		doc = doc[:end]
	}
	if len(doc) <= maxBytes {
		return doc
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(doc[cut]) {
		cut--
	}
	if cut == 0 {
		_, cut = utf8.DecodeRuneInString(doc) // 上限が最初の文字より小さくても、1 文字は残す
	}
	return strings.TrimRightFunc(doc[:cut], unicode.IsSpace) + "…"
}

// sentenceEnd は最初の文の終わり (含めて切る位置) を返す。無ければ -1。
func sentenceEnd(doc string) int {
	for i, r := range doc {
		switch r {
		case '。':
			return i + len("。")
		case '.':
			rest := doc[i+1:]
			if rest == "" {
				return i + 1
			}
			trimmed := strings.TrimLeft(rest, " ")
			if len(trimmed) == len(rest) {
				continue // 後ろが空白でない (v1.2・a.b)
			}
			next, _ := utf8.DecodeRuneInString(trimmed)
			if (trimmed == "" || unicode.IsUpper(next) || next > unicode.MaxASCII) && !endsWithAbbreviation(doc[:i]) {
				return i + 1
			}
		}
	}
	return -1
}

// abbreviations は文の終わりでない `.` の前の語 (小文字)。
var abbreviations = map[string]bool{"e.g": true, "i.e": true, "etc": true, "vs": true, "cf": true, "dr": true, "mr": true, "mrs": true, "ms": true, "approx": true}

// endsWithAbbreviation は text の末尾の語 (空白で区切る) が略語か。
func endsWithAbbreviation(text string) bool {
	start := strings.LastIndexAny(text, " \t(")
	return abbreviations[strings.ToLower(text[start+1:])]
}
