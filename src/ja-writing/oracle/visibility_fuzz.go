package oracle

import "strings"

// xorshift は乱数の種から決まった並びを作る (本家に通す入力が、Go のバージョンや実行ごとに変わらない)。
type xorshift uint64

func (x *xorshift) next() uint64 {
	v := uint64(*x)
	v ^= v >> 12
	v ^= v << 25
	v ^= v >> 27
	*x = xorshift(v)
	return v * 0x2545F4914F6CDD1D
}

func (x *xorshift) intn(n int) int { return int(x.next()>>33) % n }

// 解析の状態遷移 (柵・HTML ブロック・リスト・引用・表・参照定義) を組み合わせで通すための行の部品。
var fuzzLines = []string{
	"", "", "   ", "text line", "\U00003082\U00003044\U00003061\U00003069\U0000306e\U00006587\U00007ae0\U00003067\U00003059\U00003002", "# heading", "## h ##", "#", "===", "---", "- - -", "***", "___",
	"- item", "* item", "+ item", "-", "1. item", "2) item", "  - nested", "    code", "\tcode", "> quote", ">> deep", "> ", ">", "> > a", "-   > q",
	"```", "```go", "~~~", "````", "``` x`y", "   ```", "~~~~ info",
	"| a | b |", "|---|---|", "| 1 | 2 |", "a | b", "-|-", "|:-:|:-:|", "| x |", "|---|",
	"<div>", "</div>", "<!-- c", "-->", "<!-- c -->", "<script>", "</script>", "<pre>", "</pre>", "<style>", "</style>", "<?php", "?>", "<![CDATA[", "]]>", "<!DOCTYPE x>", "<span>x</span>", "<custom a=b>", "<b", "<textarea>", "</textarea>", "<p>",
	"[ref]: /url", "[ref]: /url \"t\"", "\"title\"", "'title", "  /url", "[a]:", "[a]: <u v>", "[a b", "c]: /x",
	"[a](u) and `c`", "text `unterminated", "<a href='x'> y", "http://x.com/a). www.b.com", "\\* esc", "![i](p)", "[l](<a b> 'c')", "**b** _i_ ~~s~~", "a\\", "``x`` `y",
	"   cont", "  cont2", "1.", "10. ten", "  1. two", "    - deep", "  > q2",
}

func fuzzDoc(rng *xorshift) string {
	n := 3 + rng.intn(14)
	var b strings.Builder
	if rng.intn(8) == 0 {
		b.WriteString("---\nk: v\n---\n")
	}
	for i := range n {
		b.WriteString(fuzzLines[rng.intn(len(fuzzLines))])
		if i == n-1 && rng.intn(2) == 0 {
			break
		}
		switch rng.intn(12) {
		case 0:
			b.WriteString("\r\n")
		case 1:
			b.WriteString("\r")
		default:
			b.WriteString("\n")
		}
	}
	return b.String()
}

var fuzzInlineParts = []string{
	"`", "``", "```", "\\", "[", "]", "(", ")", "<", ">", "!", "a", "b", " ", "  ", "\t", "\n", "\r\n", "http://", "https://", "www.", "x.com", ".", ",", "'", "\"", "*", "_", "~", "-->", "<!--", "<!-->", "<?", "?>", "<![CDATA[", "]]>",
	"<a", "<b>", "</b>", "=", "/", "1", "-", ":", "@", "x@y.z", "<!X", "<http://a>", "(a)", "[a]", "![", "](", "](x)", "](<", "\\\\", "\\*", "\U00003042", "\U0000ff1c", "\U000000a0",
}

func fuzzInline(rng *xorshift) string {
	n := 1 + rng.intn(24)
	var b strings.Builder
	for range n {
		b.WriteString(fuzzInlineParts[rng.intn(len(fuzzInlineParts))])
	}
	return b.String()
}

func fuzzCases() []VisCase {
	rng := xorshift(0x9E3779B97F4A7C15)
	var cases []VisCase
	for i := range 400 {
		text := fuzzDoc(&rng)
		c := snippetCase("", text)
		c.Name = "fuzz/doc-" + itoa3(i)
		// 結果そのものは残さない (digest だけ)。
		for j := range c.Calls {
			c.Calls[j].Keep = false
		}
		cases = append(cases, c)
	}
	var inline []Call
	for range 4000 {
		s := fuzzInline(&rng)
		inline = append(inline, vcall("inline_protected_spans", s), vcall("analyze_markdown", s))
	}
	cases = append(cases, VisCase{Name: "fuzz/inline", Calls: inline})
	var refs []Call
	for range 3000 {
		n := 1 + rng.intn(4)
		lines := make([]any, n)
		for i := range n {
			lines[i] = fuzzRefLine(&rng)
		}
		refs = append(refs, vcall("reference_definition_end", lines, rng.intn(n)))
	}
	cases = append(cases, VisCase{Name: "fuzz/reference-definition-end", Calls: refs})
	return cases
}

var fuzzRefParts = []string{"[", "]", "a", ":", " ", "  ", "/url", "<", ">", "\"", "'", "(", ")", "\\", "t", "", "\t", "x y", "[a]:", "[a]: /u", " \"t\"", "\U000000a0"}

func fuzzRefLine(rng *xorshift) string {
	n := 1 + rng.intn(6)
	var b strings.Builder
	for range n {
		b.WriteString(fuzzRefParts[rng.intn(len(fuzzRefParts))])
	}
	return b.String()
}

func itoa3(i int) string {
	return string([]byte{byte('0' + i/100%10), byte('0' + i/10%10), byte('0' + i%10)})
}
