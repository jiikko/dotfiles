package visibility

// UnicodeWhitespace は unicode_whitespace。空文字列は真、2 字以上は全部の字が空白のとき真。空白は \t \n \r \f と一般カテゴリ Zs。
func UnicodeWhitespace(ch string) bool {
	rs := runesOf(ch)
	for _, c := range rs {
		if !unicodeWhitespaceRune(c) {
			return false
		}
	}
	return true
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

// CurrentDirectory は current_directory。作業ディレクトリか、読めない (消えた) ときは ok が偽。
func CurrentDirectory() (string, bool) { return getcwd() }

// WordsLabel は words_label (人が読む出力の「語の一覧: <パス>（allow N語、flag N語）。」。flag-patterns.txt があれば「、flag-patterns N行」を足す)。
func WordsLabel(c WordsContext) string { return WordsLabelOf(c.Value()) }
