// Package pyjson は Python 3.15.0 の json.dumps / json.loads と round() の振る舞いを Go で再現する。
//
// 本家 yomiyasu は JSON を ensure_ascii=True で書き (lint は indent=2、diff と check は indent=1、check の照合は sort_keys と separators=(",", ":")、
// hook は指定なしか indent=2)、hook と check は json.loads(bytes.decode("utf-8")) で読む。出力が 1 字でも違うと check の記録の SHA-256 や
// 利用者の diff が変わるので、Go の encoding/json (map の鍵の順・数の書き方・HTML のエスケープが違う) は使わない。
//
// 値は Python の型に対応する Value で持つ。
//
//	nil → None / bool → bool / int64 と *big.Int → int / float64 → float / string → str (WTF-8。サロゲートは pyunicode を参照)
//	[]Value → list / *Object → dict (挿入順を覚える)
package pyjson

import (
	"fmt"
	"math/big"
	"strings"
)

// Value は JSON の値。上の対応表の型だけを使う。
type Value = any

// Object は Python の dict (文字列の鍵だけ)。鍵は初めて Set した順に並び、同じ鍵への Set は位置を変えずに値だけを替える (json.loads の重複した鍵の扱い)。
type Object struct {
	keys []string
	vals map[string]Value
}

// NewObject は空の Object を作る。
func NewObject() *Object { return &Object{vals: map[string]Value{}} }

// Set は鍵に値を結びつける。
func (o *Object) Set(key string, v Value) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// Get は鍵の値を返す。
func (o *Object) Get(key string) (Value, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Len は鍵の数。
func (o *Object) Len() int { return len(o.keys) }

// Keys は鍵を挿入順で返す (呼び出し側が書き換えても Object は変わらない)。
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Error 種別 (Python の例外の型の名前)。
const (
	ClassJSONDecodeError    = "JSONDecodeError"
	ClassValueError         = "ValueError"
	ClassTypeError          = "TypeError"
	ClassOverflowError      = "OverflowError"
	ClassRecursionError     = "RecursionError"
	ClassUnicodeDecodeError = "UnicodeDecodeError"
)

// Error は Python が投げる例外に対応するエラー。Class は例外の型の名前。
// ClassJSONDecodeError のときは Msg に位置を含まないメッセージ、Pos・Line・Col に位置 (Pos は 0 から数えたコードポイントの添字、Line と Col は 1 から) が入る。
type Error struct {
	Class string
	Msg   string
	Pos   int
	Line  int
	Col   int
}

// Error は Python の str(exc) と同じ文字列。
func (e *Error) Error() string {
	if e.Class == ClassJSONDecodeError {
		return fmt.Sprintf("%s: line %d column %d (char %d)", e.Msg, e.Line, e.Col, e.Pos)
	}
	return e.Msg
}

// IsValueError は Python の `except (ValueError, UnicodeError)` が捕まえる例外かどうかを返す。
// JSONDecodeError と UnicodeDecodeError は ValueError の仲間で、RecursionError は捕まえられず、Python では処理が落ちる。
func (e *Error) IsValueError() bool {
	return e.Class == ClassJSONDecodeError || e.Class == ClassValueError || e.Class == ClassUnicodeDecodeError
}

// maxIntDigits は Python の int と文字列の変換の桁数の上限 (sys.get_int_max_str_digits() の初期値)。
const maxIntDigits = 4300

func digitsExceeded(digits int) bool { return digits > maxIntDigits }

// bigDigits は |v| の 10 進の桁数。
func bigDigits(v *big.Int) int { return len(strings.TrimPrefix(v.String(), "-")) }
