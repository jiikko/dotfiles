// Package replay は、本家の関数の記録 (結果は digest だけ) と Go の実装の結果を突き合わせるテストの道具。
// 記録の形は oracle の VisibilityGolden。
// oracle 本体に置かないのは、internal/pyjson などの標準ライブラリの写しの試験が oracle を import しているため (oracle が pyjson を import すると、試験で循環する)。
package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
)

// ResultDigest は関数の結果 v の digest (結果を {"result": v} にして正規の JSON (sort_keys・ensure_ascii・separators) にした SHA-256 の先頭 oracle.DigestWidth 字)。
// canon はその正規の JSON (食い違ったときの表示用)。sort_keys の代わりに、組む側が鍵を昇順に並べて入れる (整数の鍵の dict は、本家は整数の大小で並べてから文字にする)。
func ResultDigest(v pyjson.Value) (canon, digest string, err error) {
	rec := pyjson.NewObject()
	rec.Set("result", v)
	s, err := pyjson.Dumps(rec, pyjson.Options{Separators: &pyjson.Separators{Item: ",", Key: ":"}})
	if err != nil {
		return "", "", fmt.Errorf("正規の JSON にできません: %w", err)
	}
	sum := sha256.Sum256([]byte(s))
	return s, hex.EncodeToString(sum[:])[:oracle.DigestWidth], nil
}

// SortKeys は本家の sort_keys=True: 入れ子の dict の鍵を文字の昇順に並べ直した値。CLI の出力と同じく挿入順で作った値を、ResultDigest に渡す前に通す。
// 整数の鍵の dict (本家は整数の大小で並べてから文字にする) は、数の順に入れた Object をそのまま渡し、これを通さない。
func SortKeys(v pyjson.Value) pyjson.Value {
	switch x := v.(type) {
	case *pyjson.Object:
		keys := x.Keys()
		sort.Strings(keys)
		out := pyjson.NewObject()
		for _, k := range keys {
			val, _ := x.Get(k)
			out.Set(k, SortKeys(val))
		}
		return out
	case []pyjson.Value:
		out := make([]pyjson.Value, len(x))
		for i, e := range x {
			out[i] = SortKeys(e)
		}
		return out
	}
	return v
}

// List は pyjson の配列 (本家の list) を値から組む。
func List(vs ...pyjson.Value) pyjson.Value { return append([]pyjson.Value{}, vs...) }

// MapList は items の各要素を f で値にした配列 (本家の [f(x) for x in items])。
func MapList[T any](items []T, f func(T) pyjson.Value) pyjson.Value {
	out := make([]pyjson.Value, 0, len(items))
	for _, item := range items {
		out = append(out, f(item))
	}
	return out
}

// Strings は文字列の配列 (本家の list[str])。
func Strings(items []string) pyjson.Value {
	return MapList(items, func(s string) pyjson.Value { return s })
}

// Pattern は本家の re.Pattern の JSON ({"flags", "pattern"}。鍵は昇順)。flags は re.compile が付ける値 (str のパターンは re.UNICODE の 32 を含む)。
func Pattern(flags int64, pattern string) pyjson.Value {
	o := pyjson.NewObject()
	o.Set("flags", flags)
	o.Set("pattern", pattern)
	return o
}
