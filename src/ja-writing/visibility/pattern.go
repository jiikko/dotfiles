package visibility

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// flag-patterns.txt の正規表現の上限 (本家の PATTERN_* 定数と同じ値)。
const (
	PatternNameMax     = 40  // 名前の字数
	PatternMaxLength   = 200 // 正規表現の字数
	PatternMaxRepeat   = 30  // {m,n} の n
	PatternMaxCost     = 512 // 1 つのパターンが 1 か所で試す数 (繰り返しの幅と選択肢の数の積)
	PatternsMax        = 500 // パターンの数
	PatternsMaxCost    = 512 // 全パターンの試す数の合計
	FlagReplacementMax = 80  // flag.txt の「使う語」の字数
)

const patternMeta = ".^$*+?{}[]\\|()"

func patternErr(msg string) *Error { return newError(TypePatternError, msg) }

// patternEscape は _pattern_escape。index の \ のあと。(次の位置, 種類 ("lit"・"cls"・"zero"), 字)。字は "lit" のときだけ意味を持つ。
func patternEscape(pattern []rune, index int, inClass bool) (int, string, rune, *Error) {
	if index+1 >= len(pattern) {
		return 0, "", 0, patternErr("行の終わりに \\ だけがあります")
	}
	char := pattern[index+1]
	switch {
	case strings.ContainsRune("dDwWsS", char):
		return index + 2, "cls", 0, nil
	case char == 't':
		return index + 2, "lit", '\t', nil
	case char == 'n':
		return index + 2, "lit", '\n', nil
	case strings.ContainsRune("bBAZ", char) && !inClass:
		return index + 2, "zero", 0, nil
	case char < 128 && pyunicode.IsAlnum(char):
		return 0, "", 0, patternErr("\\" + string(char) + " は使えません（使えるのは \\d \\D \\w \\W \\s \\S \\b \\B \\A \\Z \\t \\n と、記号の前の \\）")
	}
	return index + 2, "lit", char, nil
}

// patternClass は _pattern_class。index の [ から ] までの字の組の、次の位置。入れ子の [ と、--、&&、~~、|| は断る (Python が FutureWarning を出す)。
func patternClass(pattern []rune, index int) (int, *Error) {
	position := index + 1
	if position < len(pattern) && pattern[position] == '^' {
		position++
	}
	first := true
	var previous rune
	havePrevious := false
	for {
		if position >= len(pattern) {
			return 0, patternErr("[ が閉じていません")
		}
		char := pattern[position]
		if char == ']' && !first {
			return position + 1, nil
		}
		if char == '\\' {
			next, _, _, err := patternEscape(pattern, position, true)
			if err != nil {
				return 0, err
			}
			position = next
			havePrevious = false
		} else {
			if char == '[' {
				return 0, patternErr("[ ] の中の [ は \\[ と書いてください")
			}
			if strings.ContainsRune("-&~|", char) && havePrevious && previous == char {
				return 0, patternErr("[ ] の中の " + string(char) + string(char) + " は使えません（\\" + string(char) + " と書いてください）")
			}
			previous, havePrevious = char, true
			position++
		}
		first = false
	}
}

func tooCostly() *Error {
	return patternErr("試す組み合わせが多すぎます（繰り返しの幅と選択肢の数の積は " + commaInt(PatternMaxCost) + " までです）")
}

// patternSequence は _pattern_sequence。選択肢の並びを読み、(次の位置, 選択肢ごとの必ず当たる字の並び, 試す数 (選択肢の試す数の和)) を返す。
func patternSequence(pattern []rune, index, depth int) (int, [][]string, int, *Error) {
	var branches [][]string
	var runs []string
	var run strings.Builder
	cost, total := 1, 0
	flush := func() {
		if run.Len() > 0 {
			runs = append(runs, run.String())
			run.Reset()
		}
	}
	for index < len(pattern) && pattern[index] != ')' {
		char := pattern[index]
		if char == '|' {
			flush()
			branches = append(branches, runs)
			total += cost
			runs, cost = nil, 1
			index++
			continue
		}
		var innerRuns []string
		var end, innerCost int
		var atomKind string
		var literal rune
		var single bool
		switch {
		case char == '(':
			var kind string
			var start int
			switch {
			case hasPrefixAt(pattern, "(?:", index):
				kind, start = "group", index+3
			case hasPrefixAt(pattern, "(?=", index) || hasPrefixAt(pattern, "(?!", index):
				kind, start = "look", index+3
			case hasPrefixAt(pattern, "(?<=", index) || hasPrefixAt(pattern, "(?<!", index):
				kind, start = "look", index+4
			case hasPrefixAt(pattern, "(?", index):
				return 0, nil, 0, patternErr("(? の組は (?: (?= (?! (?<= (?<! だけです（印、名前、原子団、注釈、条件は使えません）")
			default:
				return 0, nil, 0, patternErr("取り込む組 ( ) は使えません。(?: ) と書いてください")
			}
			e, inner, ic, err := patternSequence(pattern, start, depth+1)
			if err != nil {
				return 0, nil, 0, err
			}
			if e >= len(pattern) {
				return 0, nil, 0, patternErr("( が閉じていません")
			}
			end, innerCost, single = e+1, ic, false
			if kind == "group" && len(inner) == 1 {
				innerRuns = inner[0]
			}
			atomKind = kind
		case char == '[':
			e, err := patternClass(pattern, index)
			if err != nil {
				return 0, nil, 0, err
			}
			end, atomKind, innerCost, single = e, "cls", 1, true
		case char == '\\':
			e, kind, lit, err := patternEscape(pattern, index, false)
			if err != nil {
				return 0, nil, 0, err
			}
			end, atomKind, literal, innerCost, single = e, kind, lit, 1, kind != "zero"
		case char == '.':
			end, atomKind, innerCost, single = index+1, "cls", 1, true
		case char == '^' || char == '$':
			end, atomKind, innerCost, single = index+1, "zero", 1, false
		case strings.ContainsRune(patternMeta, char):
			return 0, nil, 0, patternErr(string(char) + " は、字として使うなら \\" + string(char) + " と書いてください（繰り返しなら、前に字か組が要ります）")
		default:
			end, atomKind, literal, innerCost, single = index+1, "lit", char, 1, true
		}
		// 繰り返し
		var quantifier rune
		if end < len(pattern) {
			quantifier = pattern[end]
		}
		width, optional, repeated := 1, false, false
		if quantifier == '*' || quantifier == '+' {
			return 0, nil, 0, patternErr("上限のない繰り返し（* と +）は使えません。{0,30} のように上限を書いてください")
		}
		if quantifier == '{' {
			closeAt := -1
			for i := end; i < len(pattern); i++ {
				if pattern[i] == '}' {
					closeAt = i
					break
				}
			}
			var pieces []string
			if closeAt >= 0 {
				pieces = strings.Split(str(pattern[end+1:closeAt]), ",")
			}
			valid := closeAt >= 0 && len(pieces) <= 2
			for _, piece := range pieces {
				if !asciiDigits(piece) {
					valid = false
				}
			}
			if !valid {
				return 0, nil, 0, patternErr("{ の書き方は {m} か {m,n} です（上限のない {m,} と、{,n} は使えません）")
			}
			low, high := atoiDigits(pieces[0]), atoiDigits(pieces[len(pieces)-1])
			if high > PatternMaxRepeat || low > high {
				return 0, nil, 0, patternErr("繰り返しの上限は " + strconv.Itoa(PatternMaxRepeat) + " までです")
			}
			if !single {
				return 0, nil, 0, patternErr("{m,n} を付けられるのは1字の要素だけです（組に付けられるのは ? だけです）")
			}
			width, optional, repeated, end = high-low+1, low == 0, high > 1, closeAt+1
		} else if quantifier == '?' {
			if atomKind == "zero" || atomKind == "look" {
				return 0, nil, 0, patternErr("幅のない位置（^ $ \\b や先読み・後読み）には ? を付けられません")
			}
			width, optional, end = 2, true, end+1
		}
		if (quantifier == '{' || quantifier == '?') && end < len(pattern) {
			if pattern[end] == '+' {
				return 0, nil, 0, patternErr("所有の繰り返し（?+ など）は Python 3.9 で使えません")
			}
			if pattern[end] == '?' {
				end++ // 最短一致
			}
			if end < len(pattern) && strings.ContainsRune("*+?{", pattern[end]) {
				return 0, nil, 0, patternErr("繰り返しが重なっています")
			}
		}
		// 必ず当たる字 (? と {0,n} の外で、先読み・後読みの外の字)
		if atomKind == "lit" && !optional {
			run.WriteString(string(literal))
			if repeated {
				// x{m,n} の x は 2 回以上続くことがあるので、x の前と後で字の並びを分ける (ba{2}c は ba と ac)。
				runs = append(runs, run.String())
				run.Reset()
				run.WriteString(string(literal))
			}
		} else {
			flush()
			if innerRuns != nil && !optional {
				runs = append(runs, innerRuns...)
			}
		}
		cost *= width * innerCost
		if cost > PatternMaxCost {
			return 0, nil, 0, tooCostly()
		}
		index = end
	}
	flush()
	branches = append(branches, runs)
	total += cost
	if total > PatternMaxCost {
		return 0, nil, 0, tooCostly()
	}
	return index, branches, total, nil
}

func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// atoiDigits は int(s) (s は ASCII の数字だけ。長さは PatternMaxLength 以下なので 4300 桁の上限には届かない)。大きすぎる値は上限の検査で同じく断られるよう飽和させる。
func atoiDigits(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1 << 40
	}
	return n
}

// CheckedPattern は check_pattern の戻り値 (組んだ正規表現・一番上の選択肢ごとの必ず当たる字の並び・試す数)。
type CheckedPattern struct {
	Compiled *pyre.Pattern
	Required [][]string
	Cost     int
}

// CheckPattern は check_pattern。flag-patterns.txt の正規表現を確かめる。
// Python の版で意味の変わる書き方と、手間が行の長さに比例しない書き方を断る。使えない書き方は _PatternError (理由の文)。
// 本家はここで、compile が出す警告も断る (warnings.catch_warnings)。ここまでの走査が [ ] の中の入れ子と -- && ~~ || を先に断るので、
// 走査を通った書き方で 3.15.0 の re.compile が警告を出す形は見つかっていない (記録の突き合わせの入力に、警告の出る形を含めて確かめている)。
func CheckPattern(pattern string) (*CheckedPattern, error) {
	runes := runesOf(pattern)
	if len(runes) == 0 {
		return nil, patternErr("正規表現が空です")
	}
	if len(runes) > PatternMaxLength {
		return nil, patternErr("正規表現は " + strconv.Itoa(PatternMaxLength) + " 字までです")
	}
	end, branches, cost, err := patternSequence(runes, 0, 0)
	if err != nil {
		return nil, err
	}
	if end != len(runes) {
		return nil, patternErr(") が多すぎます")
	}
	required := make([][]string, 0, len(branches))
	for _, runs := range branches {
		kept := []string{}
		for _, r := range runs {
			if hasNonSpace(runesOf(r)) {
				kept = append(kept, r)
			}
		}
		if len(kept) == 0 {
			return nil, patternErr("どの選択肢にも、? や繰り返し、先読み・後読みの外に、必ず当たる字を1字以上書いてください")
		}
		required = append(required, kept)
	}
	compiled, cerr := pyre.Compile(pattern, 0)
	if cerr != nil {
		var pe *pyre.Error
		if errors.As(cerr, &pe) {
			if pe.Class == pyre.ClassError {
				return nil, patternErr(fmt.Sprintf("正規表現の誤り（%s）", pe.Error()))
			}
			return nil, newError(pe.Class, pe.Error())
		}
		return nil, fmt.Errorf("visibility: %q を internal/pyre が compile できません: %w", pattern, cerr)
	}
	if compiled.Groups() != 0 {
		return nil, patternErr("取り込む組 ( ) は使えません。(?: ) と書いてください")
	}
	return &CheckedPattern{Compiled: compiled, Required: required, Cost: cost}, nil
}
