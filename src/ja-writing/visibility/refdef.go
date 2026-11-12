package visibility

// rowCursor は _RowCursor。行は改行を持たず、行の終わりを越えると、次の行があれば '\n'、無ければ終わり (noChar) を返す。
type rowCursor struct {
	lines    [][]rune
	row, col int
}

const noChar = -1

func (c *rowCursor) char() int {
	line := c.lines[c.row]
	if c.col < len(line) {
		return int(line[c.col])
	}
	if c.row+1 < len(c.lines) {
		return '\n'
	}
	return noChar
}

func (c *rowCursor) following() int {
	line := c.lines[c.row]
	if c.col+1 < len(line) {
		return int(line[c.col+1])
	}
	return noChar
}

func (c *rowCursor) advance() {
	if c.col < len(c.lines[c.row]) {
		c.col++
	} else if c.row+1 < len(c.lines) {
		c.row++
		c.col = 0
	}
}

// nextRowBlank は cursor.lines[cursor.row + 1].strip(' \t') が空か。行の中に改行の字があると最後の行でも char が '\n' を返し、
// 本家はここで IndexError になる。analyze_markdown は改行を含まない行だけを渡すので届かないが、呼び出し側 (lint など) が
// 改行つきの行を渡しても panic にせず、本家と同じ例外にする (ReferenceDefinitionEnd が recover する)。
func (c *rowCursor) nextRowBlank() bool {
	if c.row+1 >= len(c.lines) {
		panic(newError(TypeIndexError, "list index out of range"))
	}
	return len(stripSpaceTab(c.lines[c.row+1])) == 0
}

func (c *rowCursor) escape() bool {
	if c.char() == '\\' && c.following() != noChar && isASCIIPunct(rune(c.following())) {
		c.advance()
		c.advance()
		return true
	}
	return false
}

func isSpaceTabChar(ch int) bool { return ch == ' ' || ch == '\t' }

// referenceGap は _reference_gap (空白・タブと高々 1 つの行の区切りを進め、あったかを返す)。
func referenceGap(c *rowCursor) bool {
	seen := false
	for isSpaceTabChar(c.char()) {
		c.advance()
		seen = true
	}
	if c.char() == '\n' {
		c.advance()
		seen = true
		for isSpaceTabChar(c.char()) {
			c.advance()
		}
	}
	return seen
}

func referenceDestination(c *rowCursor) bool {
	if c.char() == '<' {
		c.advance()
		for c.char() != noChar {
			if c.escape() {
				continue
			}
			if c.char() == '>' {
				c.advance()
				return true
			}
			if c.char() == '<' || c.char() == '\n' {
				return false
			}
			c.advance()
		}
		return false
	}
	count, depth := 0, 0
	for ch := c.char(); ch != noChar && ch != ' ' && ch != '\t' && ch != '\n'; ch = c.char() {
		if c.escape() {
			count += 2
			continue
		}
		if ch < 32 || ch == 127 || ch == '<' || ch == '>' {
			return false
		}
		switch ch {
		case '(':
			depth++
			if depth > 32 {
				return false // 入れ子の上限は決めた方針 (3 以上であること)。
			}
		case ')':
			if depth == 0 {
				return false
			}
			depth--
		}
		c.advance()
		count++
	}
	return count > 0 && depth == 0
}

// referenceTitleEnd は _reference_title_end (タイトルを閉じた行。閉じなければ ok が偽)。
func referenceTitleEnd(c *rowCursor) (int, bool) {
	opener := c.char()
	closer := opener
	if opener == '(' {
		closer = ')'
	}
	c.advance()
	for c.char() != noChar {
		if c.escape() {
			continue
		}
		ch := c.char()
		if ch == closer {
			c.advance()
			for isSpaceTabChar(c.char()) {
				c.advance()
			}
			if c.char() == noChar || c.char() == '\n' {
				return c.row, true
			}
			return 0, false
		}
		if opener == '(' && ch == '(' {
			return 0, false
		}
		if ch == '\n' && c.nextRowBlank() {
			return 0, false
		}
		c.advance()
	}
	return 0, false
}

// ReferenceDefinitionEnd は reference_definition_end。定義の最後の行 (その行を含む) の添字を返し、不透明な境界が無ければ ok が偽。
// 行は改行を持たず、行ごとの同一性を保つ。呼び出し側がコンテナの所有と通常の段落が開いているかを決めてある前提で、
// 段落の途中 (paragraphContext) なら定義にならない。次の行のタイトルが失敗しても、宛先だけの行は残る。同じ行の余計な文字は定義全体を無効にする。
func ReferenceDefinitionEnd(lines []string, start int, paragraphContext bool) (end int, ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			e, isErr := r.(*Error)
			if !isErr {
				panic(r)
			}
			end, ok, err = 0, false, e
		}
	}()
	rows := make([][]rune, len(lines))
	for i, l := range lines {
		rows[i] = runesOf(l)
	}
	end, ok = referenceDefinitionEnd(rows, start, paragraphContext)
	return end, ok, nil
}

func referenceDefinitionEnd(lines [][]rune, start int, paragraphContext bool) (int, bool) {
	if paragraphContext || start < 0 || start >= len(lines) {
		return 0, false
	}
	c := &rowCursor{lines: lines, row: start}
	spaces := 0
	for c.char() == ' ' && spaces < 3 {
		c.advance()
		spaces++
	}
	if c.char() != '[' {
		return 0, false
	}
	c.advance()
	labelLength := 0
	meaningful := false
	for ch := c.char(); ch != noChar && ch != ']'; ch = c.char() {
		if c.escape() {
			labelLength += 2
			meaningful = true
		} else {
			if ch == '[' {
				return 0, false
			}
			if ch == '\n' && c.nextRowBlank() {
				return 0, false
			}
			meaningful = meaningful || (ch != ' ' && ch != '\t' && ch != '\n')
			labelLength++
			c.advance()
		}
		if labelLength > 999 {
			return 0, false
		}
	}
	if c.char() != ']' || !meaningful {
		return 0, false
	}
	c.advance()
	if c.char() != ':' {
		return 0, false
	}
	c.advance()
	referenceGap(c)
	if c.char() == noChar || c.char() == '\n' || !referenceDestination(c) {
		return 0, false
	}
	destinationRow := c.row
	separated := referenceGap(c)
	if c.char() == noChar || c.char() == '\n' {
		return destinationRow, true
	}
	if !separated || (c.char() != '"' && c.char() != '\'' && c.char() != '(') {
		if c.row > destinationRow {
			return destinationRow, true
		}
		return 0, false
	}
	titleOnLaterRow := c.row > destinationRow
	titleRow, ok := referenceTitleEnd(c)
	if !ok && titleOnLaterRow {
		return destinationRow, true
	}
	return titleRow, ok
}
