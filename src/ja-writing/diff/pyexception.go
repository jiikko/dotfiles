package diff

import (
	"fmt"

	"ja-writing/visibility"
)

// pyMaxIntDigits は Python の int と文字列の変換の桁数の上限 (sys.get_int_max_str_digits() の初期値)。本家は数字の並びを int にする所
// (漢数字の位取りの書き方・3 桁区切りのない算用数字の千倍) で、この上限を超えると ValueError で止まる。Go は big.Int で計算できるが、
// 本家と同じ答え (rc 3 と標準エラーの 1 行目) にするため、同じ条件で止める。
const pyMaxIntDigits = 4300

// pyException は本家が投げる例外を、数値の取り出しの深い所から diffDocument まで運ぶ値 (取り出しの関数に error を通すと、呼び出し元が多い群 M・Q・Y の
// 全部の署名が変わる)。recoverPyException で error に戻す。
type pyException struct{ err error }

// raiseIntLimit は int と文字列の変換の桁数の上限を超えた ValueError を投げる。fromString が真なら文字列から int への変換 (本家の int(str)。メッセージに桁数が付く)、
// 偽なら int から文字列への変換 (str(int))。
func raiseIntLimit(digits string, fromString bool) {
	msg := "Exceeds the limit (4300 digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit"
	if fromString {
		msg = fmt.Sprintf("Exceeds the limit (4300 digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", len(digits))
	}
	panic(pyException{&visibility.Error{Type: visibility.TypeValueError, Message: msg}})
}

// recoverPyException は pyException の panic を err にする (ほかの panic はそのまま)。defer で呼ぶ。
func recoverPyException(err *error) {
	if r := recover(); r != nil {
		e, ok := r.(pyException)
		if !ok {
			panic(r)
		}
		*err = e.err
	}
}
