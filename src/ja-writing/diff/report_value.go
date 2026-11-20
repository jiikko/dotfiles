package diff

import (
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
)

// 群 AD: 人が読む出力 (本家の report) は diff() の dict だけを読む。Go でも diffDocument の値 (*pyjson.Object) か、pyjson.Loads で読んだ本家の --json の値だけを読み、
// 検出の型付きの値には触れない。読み方は本家の Python の式 (d[key]・d.get(key) or {}・for・len・"、".join・"{}".format) の写しで、本家が例外になる所は
// pyops が例外を投げ、口の関数 (Report・ReportParts など) が error にする。check は子の --json を読んだ dict を渡し、その error を誤りとして出す。

// idx は本家の d[key]。
func idx(d pyjson.Value, key string) pyjson.Value { return pyops.Index(d, key) }

// get は本家の d.get(key)。
func get(d pyjson.Value, key string) pyjson.Value { return pyops.Get(d, key) }

// getDict は本家の d.get(key) or {}。
func getDict(d pyjson.Value, key string) pyjson.Value {
	return pyops.Or(pyops.Get(d, key), pyops.EmptyDict())
}

// getList は本家の d.get(key) or []。
func getList(d pyjson.Value, key string) pyjson.Value {
	return pyops.Or(pyops.Get(d, key), pyops.EmptyList())
}

// pyStr は "{}".format(v)。
func pyStr(v pyjson.Value) string { return pyops.Format(v) }

// joinList は "、".join(v)。
func joinList(v pyjson.Value) string { return pyops.Join("、", v) }

// joinOr は "、".join(v) or def。
func joinOr(v pyjson.Value, def string) string {
	if s := joinList(v); s != "" {
		return s
	}
	return def
}

// concat は本家の "…" + v (v が str でなければ TypeError)。
func concat(prefix string, v pyjson.Value) string { return prefix + pyops.Str(v) }
