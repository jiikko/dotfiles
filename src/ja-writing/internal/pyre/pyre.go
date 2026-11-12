// Package pyre は Python 3.15.0 の re (str のパターン) を regexp2 の上に写す。
//
// Python のパターンを自前の構文木に読み (CPython の re._parser の写し)、regexp2 (.NET 流の構文) のパターンに直してから照合する。
// 直す理由は、regexp2 と Python の re で意味が違う所を、そのまま渡すと本家より多く・少なく当たるため。
//   - 名前付きのグループ (?P<名前>...) は名前の無いグループに直し、名前と番号の対応は自分で持つ (Python の番号の振り方を保つ)
//   - \s \S \w \W \d \D \b \B と文字の集合 [...] は、Python の定義の字の集合 (pyunicode の表) を範囲の列に開いて渡す。
//     regexp2 の \w は Mn・Pc などにも当たり、Python の \w は No・Nl や Unicode 16 / 17 の字に当たるので、集合が違う
//   - re.I は Python の規則 (_sre.unicode_tolower と re._casefix の対応) で字の集合に開く。regexp2 の IgnoreCase には任せない
//   - ^ $ . は regexp2 のオプションに頼らず、フラグごとに字の集合・先読みで書く
//   - 後読みが固定幅でないパターンは、Python と同じくエラーにする (.NET は受けるので、断らないと本家より多く当たる)
//   - 空の一致の規則 (Python 3.7 以降) は、次の探索を (?:パターン)(?!\G) で書いて再現する
//
// 位置は文字列の添字 (コードポイント単位)。文字列は pyunicode の WTF-8 として読み、単独のサロゲートも 1 字として数える。
// 照合に時間の上限は付けない (本家に無い)。止まらない形 (入れ子の量指定子の破局的な後戻り) は本家と同じく戻らない。
package pyre

import (
	"fmt"
	"strings"

	"ja-writing/internal/pyunicode"
)

// re のフラグ (re.RegexFlag の値と同じ)。
const (
	IGNORECASE = 2
	LOCALE     = 4
	MULTILINE  = 8
	DOTALL     = 16
	UNICODE    = 32
	VERBOSE    = 64
	DEBUG      = 128
	ASCII      = 256
)

// 型を決めるフラグ。1 つの (部分) パターンで同時に 1 つしか立たない。
const typeFlags = ASCII | LOCALE | UNICODE

// 失敗の種類。Python の例外の型名に合わせる。
const (
	ClassError    = "PatternError"
	ClassValue    = "ValueError"
	ClassOverflow = "OverflowError"
	ClassIndex    = "IndexError"
)

// Error は re が投げる例外。Class は Python の例外の型名、Msg は位置を足す前の文、Pos は文の中の位置 (無いときは -1)。
type Error struct {
	Class   string
	Msg     string
	Pattern string
	Pos     int
	Line    int
	Col     int
}

func (e *Error) Error() string {
	if e.Pos < 0 {
		return e.Msg
	}
	msg := fmt.Sprintf("%s at position %d", e.Msg, e.Pos)
	if strings.Contains(e.Pattern, "\n") {
		msg += fmt.Sprintf(" (line %d, column %d)", e.Line, e.Col)
	}
	return msg
}

func newError(class, msg string) *Error { return &Error{Class: class, Msg: msg, Pos: -1} }

func newPatternError(msg string, pattern []rune, pos int) *Error {
	e := &Error{Class: ClassError, Msg: msg, Pattern: pyunicode.FromRunes(pattern), Pos: pos}
	before := pattern[:min(max(pos, 0), len(pattern))]
	e.Line = 1
	last := -1
	for i, r := range before {
		if r == '\n' {
			e.Line++
			last = i
		}
	}
	e.Col = pos - last
	return e
}
