package visibility

// 本家が投げる例外の型名 (Python の type(exc).__name__)。
const (
	TypeValueError         = "ValueError"
	TypeWordsError         = "WordsError"
	TypePatternError       = "_PatternError"
	TypeUnicodeDecodeError = "UnicodeDecodeError"
	TypeIndexError         = "IndexError"
	TypeAssertionError     = "AssertionError"
)

// Error は本家の関数が投げる例外。Type は Python の例外の型名、Message は str(exc)。
// 例外の種類と文は呼び出し側 (lint・diff・check) が標準エラーにそのまま書くので、本家と同じ文にする。
type Error struct {
	Type    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func newError(typ, msg string) *Error { return &Error{Type: typ, Message: msg} }
