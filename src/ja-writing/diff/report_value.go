package diff

import (
	"fmt"
	"strconv"
	"strings"

	"ja-writing/internal/pyjson"
)

// 群 AD: 人が読む出力 (本家の report) は diff() の dict だけを読む。Go でも diffDocument の値 (*pyjson.Object) か、pyjson.Loads で読んだ本家の --json の値だけを読み、
// 検出の型付きの値には触れない。ここは Python の dict の読み方 (d.get(key) or {}・真偽・"{}".format) の写し。

// field は o[key] (o が nil か鍵が無ければ nil。本家の d.get(key))。
func field(o *pyjson.Object, key string) pyjson.Value { return o.Field(key) }

func fieldObj(o *pyjson.Object, key string) *pyjson.Object {
	v, _ := field(o, key).(*pyjson.Object)
	return v
}

func fieldList(o *pyjson.Object, key string) []pyjson.Value {
	v, _ := field(o, key).([]pyjson.Value)
	return v
}

func fieldStr(o *pyjson.Object, key string) string {
	v, _ := field(o, key).(string)
	return v
}

func fieldInt(o *pyjson.Object, key string) int { return asInt(field(o, key)) }

func asInt(v pyjson.Value) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	}
	return 0
}

func asFloat(v pyjson.Value) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

// truthy は Python の bool(v)。
func truthy(v pyjson.Value) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []pyjson.Value:
		return len(x) > 0
	case *pyjson.Object:
		return x != nil && x.Len() > 0
	}
	return true
}

// pyStr は "{}".format(v) の中身 (None は "None"、真偽は "True" と "False"、float は repr)。
func pyStr(v pyjson.Value) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return pyjson.FloatRepr(x)
	}
	return fmt.Sprint(v)
}

// strList は文字列の list (本家の "、".join(list) に渡す形)。
func strList(v pyjson.Value) []string {
	list, _ := v.([]pyjson.Value)
	out := make([]string, len(list))
	for i, item := range list {
		out[i] = pyStr(item)
	}
	return out
}

// joinList は "、".join(v)。
func joinList(v pyjson.Value) string { return strings.Join(strList(v), "、") }
