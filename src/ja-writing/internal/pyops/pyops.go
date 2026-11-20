// Package pyops は、json.loads で読んだ値に Python の演算 (d[key]・d.get・for・len・+・join・format) を当てた結果と、例外になるかを写す。
//
// 本家の check は子の --json を読んだ dict を lint と diff の人が読む出力の関数に渡し、例外になったら
// 「子の JSON から人が読む形を作れません」の誤りにする。Go の写しが欠けた欄を 0 や空として読むと、本家が誤りにする入力で文を出す。
//
// 例外は panic で運び、口の関数が defer Catch(&err) で error に戻す (読む所ごとに error を返すと、写した関数の形が本家から離れる)。
// 例外の型の名前は本家に合わせるが、文までは合わせない (check は型も文も見ない)。
package pyops

import (
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// Exception は本家が投げる例外 (Type は TypeError などの名前)。
type Exception struct{ Type, Msg string }

func (e *Exception) Error() string { return e.Type + ": " + e.Msg }

type raised struct{ e *Exception }

// Raise は例外を投げる (Catch が受ける)。
func Raise(typ, format string, args ...any) {
	panic(raised{&Exception{Type: typ, Msg: fmt.Sprintf(format, args...)}})
}

// Catch は Raise の panic を *err にする (ほかの panic はそのまま)。口の関数で defer Catch(&err) と呼ぶ。
func Catch(err *error) {
	if r := recover(); r != nil {
		x, ok := r.(raised)
		if !ok {
			panic(r)
		}
		*err = x.e
	}
}

// TypeName は Python の type(v).__name__。
func TypeName(v pyjson.Value) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case int64, *big.Int:
		return "int"
	case float64:
		return "float"
	case string:
		return "str"
	case []pyjson.Value:
		return "list"
	case *pyjson.Object:
		return "dict"
	}
	return fmt.Sprintf("%T", v)
}

func dict(v pyjson.Value) (*pyjson.Object, bool) {
	o, ok := v.(*pyjson.Object)
	return o, ok && o != nil
}

// Index は v[key] (dict の鍵。無ければ KeyError、dict でなければ TypeError)。
func Index(v pyjson.Value, key string) pyjson.Value {
	o, ok := dict(v)
	if !ok {
		Raise("TypeError", "'%s' object is not subscriptable by str", TypeName(v))
	}
	x, ok := o.Get(key)
	if !ok {
		Raise("KeyError", "%s", pystr.Repr(key))
	}
	return x
}

// At は v[i] (i は 0 以上。list の要素、str の字。dict は鍵 i を探して KeyError)。
func At(v pyjson.Value, i int) pyjson.Value {
	switch x := v.(type) {
	case []pyjson.Value:
		if i < len(x) {
			return x[i]
		}
	case string:
		if rs := pyunicode.Runes(x); i < len(rs) {
			return pyunicode.FromRunes(rs[i : i+1])
		}
	case *pyjson.Object:
		Raise("KeyError", "%d", i)
	default:
		Raise("TypeError", "'%s' object is not subscriptable", TypeName(v))
	}
	Raise("IndexError", "%s index out of range", TypeName(v))
	return nil
}

// Slice は v[lo:hi] (list と str。hi < 0 は終わりまで)。dict は Python 3.12 からの slice を鍵にした KeyError (鍵の repr は slice(lo, hi, None))。
// 本家の v[:n] は slice(None, n, None) だが、引数では v[:n] と v[0:n] を見分けられないので slice(0, n, None) を出す。
// 呼び出し元 (diff の new_lines[:3]・shortened、check の text[:40]) は内部で作った list と str しか渡さず、dict は届かない。
func Slice(v pyjson.Value, lo, hi int) pyjson.Value {
	clip := func(n int) (int, int) {
		l, h := min(lo, n), n
		if hi >= 0 {
			h = min(hi, n)
		}
		return l, max(l, h)
	}
	switch x := v.(type) {
	case []pyjson.Value:
		l, h := clip(len(x))
		return x[l:h]
	case string:
		rs := pyunicode.Runes(x)
		l, h := clip(len(rs))
		return pyunicode.FromRunes(rs[l:h])
	case *pyjson.Object:
		end := "None"
		if hi >= 0 {
			end = strconv.Itoa(hi)
		}
		Raise("KeyError", "slice(%d, %s, None)", lo, end)
	}
	Raise("TypeError", "'%s' object is not subscriptable", TypeName(v))
	return nil
}

// Tail は v[-n:] (最後の n 個。list と str。dict は slice(-n, None, None) を鍵にした KeyError、ほかは TypeError)。
func Tail(v pyjson.Value, n int) pyjson.Value {
	switch v.(type) {
	case []pyjson.Value, string:
		return Slice(v, max(0, Len(v)-n), -1)
	case *pyjson.Object:
		Raise("KeyError", "slice(%d, None, None)", -n)
	}
	Raise("TypeError", "'%s' object is not subscriptable", TypeName(v))
	return nil
}

// Get は v.get(key) (鍵が無ければ nil。dict でなければ AttributeError)。
func Get(v pyjson.Value, key string) pyjson.Value { return GetOr(v, key, nil) }

// GetOr は v.get(key, def)。
func GetOr(v pyjson.Value, key string, def pyjson.Value) pyjson.Value {
	o, ok := dict(v)
	if !ok {
		Raise("AttributeError", "'%s' object has no attribute 'get'", TypeName(v))
	}
	if x, ok := o.Get(key); ok {
		return x
	}
	return def
}

// Or は v or def (v が偽なら def)。
func Or(v, def pyjson.Value) pyjson.Value {
	if Truthy(v) {
		return v
	}
	return def
}

// EmptyDict は {} (d.get(key) or {} の右辺)。
func EmptyDict() pyjson.Value { return pyjson.NewObject() }

// EmptyList は []。
func EmptyList() pyjson.Value { return []pyjson.Value{} }

// Iter は for x in v の並び (list は要素、str は字、dict は鍵。ほかは TypeError)。
func Iter(v pyjson.Value) []pyjson.Value {
	switch x := v.(type) {
	case []pyjson.Value:
		return x
	case string:
		rs := pyunicode.Runes(x)
		out := make([]pyjson.Value, len(rs))
		for i := range rs {
			out[i] = pyunicode.FromRunes(rs[i : i+1])
		}
		return out
	case *pyjson.Object:
		if x == nil {
			break
		}
		keys := x.Keys()
		out := make([]pyjson.Value, len(keys))
		for i, k := range keys {
			out[i] = k
		}
		return out
	}
	Raise("TypeError", "'%s' object is not iterable", TypeName(v))
	return nil
}

// Unpack2 は a, b = v (要素がちょうど 2 つでなければ ValueError)。
func Unpack2(v pyjson.Value) (pyjson.Value, pyjson.Value) {
	items := Iter(v)
	if len(items) != 2 {
		Raise("ValueError", "expected 2 values to unpack, got %d", len(items))
	}
	return items[0], items[1]
}

// Len は len(v)。
func Len(v pyjson.Value) int {
	switch x := v.(type) {
	case []pyjson.Value:
		return len(x)
	case string:
		return pyunicode.RuneCount(x)
	case *pyjson.Object:
		if x != nil {
			return x.Len()
		}
	}
	Raise("TypeError", "object of type '%s' has no len()", TypeName(v))
	return 0
}

// Str は str が要る所の値 ("…" + v・v.split など。str でなければ TypeError。本家は v.split などでは AttributeError)。
func Str(v pyjson.Value) string {
	s, ok := v.(string)
	if !ok {
		Raise("TypeError", "expected str, got '%s'", TypeName(v))
	}
	return s
}

// Join は sep.join(v) (要素は str でなければ TypeError)。
func Join(sep string, v pyjson.Value) string {
	items := Iter(v)
	parts := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			Raise("TypeError", "sequence item %d: expected str instance, %s found", i, TypeName(item))
		}
		parts[i] = s
	}
	return strings.Join(parts, sep)
}

// Truthy は bool(v)。
func Truthy(v pyjson.Value) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int64:
		return x != 0
	case *big.Int:
		return x.Sign() != 0
	case float64:
		return x != 0
	case []pyjson.Value:
		return len(x) > 0
	case *pyjson.Object:
		return x != nil && x.Len() > 0
	}
	return true
}

// Format は "{}".format(v) と str(v) (None は "None"、真偽は "True" と "False"、float は repr、list と dict は repr)。
func Format(v pyjson.Value) string { return format(v, false) }

func format(v pyjson.Value, nested bool) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		if nested {
			return pystr.Repr(x)
		}
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case *big.Int:
		return x.String()
	case float64:
		return pyjson.FloatRepr(x)
	case []pyjson.Value:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = format(item, true)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *pyjson.Object:
		var parts []string
		for _, k := range x.Keys() {
			item, _ := x.Get(k)
			parts = append(parts, pystr.Repr(k)+": "+format(item, true))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprint(v)
}

// Hashable は v が dict の鍵や set の要素になれるか確かめる (list と dict は TypeError)。
func Hashable(v pyjson.Value) {
	switch v.(type) {
	case []pyjson.Value, *pyjson.Object:
		Raise("TypeError", "unhashable type: '%s'", TypeName(v))
	}
}

// Lookup は表 (鍵が str の dict) の table.get(v, def)。str でない鍵は当たらない (list と dict は TypeError)。
func Lookup(table map[string]string, v pyjson.Value, def pyjson.Value) pyjson.Value {
	Hashable(v)
	if s, ok := v.(string); ok {
		if name, ok := table[s]; ok {
			return name
		}
	}
	return def
}

func isNumber(v pyjson.Value) bool {
	switch v.(type) {
	case bool, int64, *big.Int, float64:
		return true
	}
	return false
}

// Number は数として使う値 (int・float・bool) を float にした値 (本家の float(x))。数でなければ ok が偽。
// float に収まらない int は inf にせず OverflowError (本家は int を float に変える所で OverflowError になる)。
func Number(v pyjson.Value) (float64, bool) {
	if x, ok := v.(float64); ok {
		return x, true
	}
	b, ok := bigOf(v)
	if !ok {
		return 0, false
	}
	f, _ := new(big.Float).SetInt(b).Float64()
	if math.IsInf(f, 0) {
		Raise("OverflowError", "int too large to convert to float")
	}
	return f, true
}

// exactFloat は数を丸めずに big.Float にする (nan は nil)。
func exactFloat(v pyjson.Value) *big.Float {
	if x, ok := v.(float64); ok {
		if math.IsNaN(x) {
			return nil
		}
		return new(big.Float).SetFloat64(x)
	}
	b, _ := bigOf(v)
	return new(big.Float).SetInt(b)
}

// compareNumbers は数どうしの大小 (-1・0・1)。本家と同じく int と float も float に変えずに比べる (9007199254740993 と 9007199254740992.0 は違う、
// 10**400 は inf より小さい)。ok が偽なら片方が数でない。unordered が真なら片方が nan で、== も < も >= も偽。
func compareNumbers(a, b pyjson.Value) (cmp int, unordered, ok bool) {
	if !isNumber(a) || !isNumber(b) {
		return 0, false, false
	}
	if x, ok := bigOf(a); ok {
		if y, ok := bigOf(b); ok {
			return x.Cmp(y), false, true
		}
	}
	x, y := exactFloat(a), exactFloat(b)
	if x == nil || y == nil {
		return 0, true, true
	}
	return x.Cmp(y), false, true
}

func bigOf(v pyjson.Value) (*big.Int, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return big.NewInt(1), true
		}
		return big.NewInt(0), true
	case int64:
		return big.NewInt(x), true
	case *big.Int:
		return x, true
	}
	return nil, false
}

// IntValue は Python の int の値 (int64 に収まれば int64、収まらなければ *big.Int。pyjson が読む整数と同じ形)。
func IntValue(b *big.Int) pyjson.Value {
	if b.IsInt64() {
		return b.Int64()
	}
	return b
}

// Add は a + b (数どうし・str どうし・list どうし。ほかは TypeError)。
func Add(a, b pyjson.Value) pyjson.Value { return arith(a, b, "+") }

// Sub は a - b (数どうし)。
func Sub(a, b pyjson.Value) pyjson.Value { return arith(a, b, "-") }

func arith(a, b pyjson.Value, op string) pyjson.Value {
	if op == "+" {
		if x, ok := a.(string); ok {
			y, ok := b.(string)
			if !ok {
				Raise("TypeError", "can only concatenate str (not \"%s\") to str", TypeName(b))
			}
			return x + y
		}
		if x, ok := a.([]pyjson.Value); ok {
			y, ok := b.([]pyjson.Value)
			if !ok {
				Raise("TypeError", "can only concatenate list (not \"%s\") to list", TypeName(b))
			}
			return append(slices.Clip(x), y...)
		}
	}
	if x, ok := bigOf(a); ok {
		if y, ok := bigOf(b); ok {
			if op == "+" {
				return IntValue(new(big.Int).Add(x, y))
			}
			return IntValue(new(big.Int).Sub(x, y))
		}
	}
	if !isNumber(a) || !isNumber(b) {
		Raise("TypeError", "unsupported operand type(s) for %s: '%s' and '%s'", op, TypeName(a), TypeName(b))
	}
	x, _ := Number(a)
	y, _ := Number(b)
	if op == "+" {
		return x + y
	}
	return x - y
}

// Mul は a * b (数どうし。本家の 100.0 * x だけに使う。str や list に float を掛けると TypeError)。
func Mul(a, b pyjson.Value) float64 {
	if !isNumber(a) || !isNumber(b) {
		Raise("TypeError", "unsupported operand type(s) for *: '%s' and '%s'", TypeName(a), TypeName(b))
	}
	x, _ := Number(a)
	y, _ := Number(b)
	return x * y
}

// Div は a / b (真の割り算。0 で割ると ZeroDivisionError)。int どうしは本家と同じく商を丸めて 1 回で float にし (10**400 / 10**399 は 10.0)、
// 収まらなければ OverflowError。
func Div(a, b pyjson.Value) float64 {
	if !isNumber(a) || !isNumber(b) {
		Raise("TypeError", "unsupported operand type(s) for /: '%s' and '%s'", TypeName(a), TypeName(b))
	}
	if x, ok := bigOf(a); ok {
		if y, ok := bigOf(b); ok {
			if y.Sign() == 0 {
				Raise("ZeroDivisionError", "division by zero")
			}
			q, _ := new(big.Rat).SetFrac(x, y).Float64()
			if math.IsInf(q, 0) {
				Raise("OverflowError", "integer division result too large for a float")
			}
			if q == 0 && (x.Sign() < 0) != (y.Sign() < 0) {
				// 有理数の 0 は符号を持たないが、本家の 0 / -2 と 1 / -10**400 は -0.0 (format すると "-0.00")
				return math.Copysign(0, -1)
			}
			return q
		}
	}
	x, _ := Number(a)
	y, _ := Number(b)
	if y == 0 {
		Raise("ZeroDivisionError", "division by zero")
	}
	return x / y
}

// Sum は sum(items)。CPython 3.12 以降の builtin_sum_impl と同じ 3 段で足す: int (bool を含む) が C long に収まるうちは整数で足し、
// float に移ったら float と int (float に変えて。収まらない int は OverflowError) を補正つきの和 (Neumaier) で足し、それ以外の要素が来たら
// そこから先は Add を順に当てる。0 から順に Add するだけにしない: float の和の丸めが本家と変わる (sum([0.1, 0.2, 0.3]) は本家 0.6、順の + は 0.6000000000000001)。
func Sum(items []pyjson.Value) pyjson.Value {
	var acc int64
	i := 0
	for ; i < len(items); i++ {
		if b, ok := longOf(items[i]); ok && ((acc >= 0 && b <= math.MaxInt64-acc) || (acc < 0 && b >= math.MinInt64-acc)) {
			acc += b
			continue
		}
		break
	}
	if i == len(items) {
		return acc
	}
	result := Add(acc, items[i])
	i++
	if f, ok := result.(float64); ok {
		hi, lo := f, 0.0
		for ; i < len(items); i++ {
			if !isNumber(items[i]) {
				break
			}
			x, _ := Number(items[i])
			hi, lo = compensatedAdd(hi, lo, x)
		}
		if i == len(items) {
			return compensatedTotal(hi, lo)
		}
		result = Add(compensatedTotal(hi, lo), items[i])
		i++
	}
	for ; i < len(items); i++ {
		result = Add(result, items[i])
	}
	return result
}

// longOf は v が C long (64 ビット) に収まる int か bool なら、その値を返す (sum が整数のまま足す要素)。
func longOf(v pyjson.Value) (int64, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case int64:
		return x, true
	case *big.Int:
		if x.IsInt64() {
			return x.Int64(), true
		}
	}
	return 0, false
}

// compensatedAdd は CPython の cs_add (和 hi と補正 lo に x を足す)。
func compensatedAdd(hi, lo, x float64) (float64, float64) {
	t := hi + x
	if math.Abs(hi) >= math.Abs(x) {
		lo += (hi - t) + x
	} else {
		lo += (x - t) + hi
	}
	return t, lo
}

// compensatedTotal は CPython の cs_to_double。補正が有限でないなら足さない (inf の和を nan にしない)。CPython の「補正が 0 なら足さない」は
// 和が -0.0 のときの符号のためで、写さない: Sum は start を取らず 0 から足すので、float の段の和が -0.0 になる入力が無い。
func compensatedTotal(hi, lo float64) float64 {
	if !math.IsInf(lo, 0) && !math.IsNaN(lo) {
		return hi + lo
	}
	return hi
}

// Values は d.values() (dict でなければ AttributeError)。
func Values(v pyjson.Value) []pyjson.Value {
	o, ok := dict(v)
	if !ok {
		Raise("AttributeError", "'%s' object has no attribute 'values'", TypeName(v))
	}
	keys := o.Keys()
	out := make([]pyjson.Value, len(keys))
	for i, k := range keys {
		out[i], _ = o.Get(k)
	}
	return out
}

// Equal は a == b (数は値で比べる。1 == 1.0 == True)。
//
// nan は本家と揃えない所がある: list の == は本家では要素の identity を先に見る (同じ nan の object を持つ list どうしは等しい) が、ここは値だけで比べる。
// 並べ替え (SortedBy) の nan を含む鍵の順も本家の timsort とは揃えていない。どちらも子の JSON に nan が出ないので届かない: 子の JSON は pyjson.Dumps で
// NaN を書けるが、本家にも Go の子にも nan を作る式が無い (割り算は分母 0 を先に除き、float() は check の引数で 0<v<inf に限る)。
// 再提起するなら、nan を含む子の JSON が実際に出る経路を示すこと。
func Equal(a, b pyjson.Value) bool {
	if c, unordered, ok := compareNumbers(a, b); ok {
		return !unordered && c == 0
	}
	if isNumber(a) {
		return false
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []pyjson.Value:
		y, ok := b.([]pyjson.Value)
		return ok && slices.EqualFunc(x, y, Equal)
	case *pyjson.Object:
		y, ok := b.(*pyjson.Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.Keys() {
			xv, _ := x.Get(k)
			yv, ok := y.Get(k)
			if !ok || !Equal(xv, yv) {
				return false
			}
		}
		return true
	}
	return false
}

// Less は a < b (数どうし・str どうし・list どうし。ほかの組は TypeError)。
func Less(a, b pyjson.Value) bool {
	if c, unordered, ok := compareNumbers(a, b); ok {
		return !unordered && c < 0
	}
	if x, ok := a.(string); ok {
		if y, ok := b.(string); ok {
			return slices.Compare(pyunicode.Runes(x), pyunicode.Runes(y)) < 0
		}
	}
	if x, ok := a.([]pyjson.Value); ok {
		if y, ok := b.([]pyjson.Value); ok {
			for i := 0; i < len(x) && i < len(y); i++ {
				if !Equal(x[i], y[i]) {
					return Less(x[i], y[i])
				}
			}
			return len(x) < len(y)
		}
	}
	Raise("TypeError", "'<' not supported between instances of '%s' and '%s'", TypeName(a), TypeName(b))
	return false
}

// GreaterEq は a >= b (数どうしは丸めずに比べ、nan があれば偽。ほかは Less の否定で、比べられない組は TypeError)。
func GreaterEq(a, b pyjson.Value) bool {
	if c, unordered, ok := compareNumbers(a, b); ok {
		return !unordered && c >= 0
	}
	return !Less(a, b)
}

// SortedBy は sorted(items, key=key) (安定。鍵はすべて先に作る。比べられない鍵の組は TypeError)。
func SortedBy(items []pyjson.Value, key func(pyjson.Value) pyjson.Value) []pyjson.Value {
	keys := make([]pyjson.Value, len(items))
	for i, item := range items {
		keys[i] = key(item)
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	// 2 つ以上あれば、本家の並べ替えはどの要素も少なくとも 1 回ほかと比べる。比べられない組を含む鍵は、並べ替えの順によらず TypeError になる
	for i := 1; i < len(keys); i++ {
		Less(keys[i], keys[0])
		Less(keys[0], keys[i])
	}
	slices.SortStableFunc(order, func(i, j int) int {
		switch {
		case Less(keys[i], keys[j]):
			return -1
		case Less(keys[j], keys[i]):
			return 1
		}
		return 0
	})
	out := make([]pyjson.Value, len(items))
	for i, k := range order {
		out[i] = items[k]
	}
	return out
}

// FormatFixed は format(v, ".Nf") (int・float・bool。str は ValueError、ほかは TypeError)。
func FormatFixed(v pyjson.Value, prec int) string {
	x := formatNumber(v, "f")
	return fixed(x, prec)
}

// FormatPercent は format(v, ".N%")。
func FormatPercent(v pyjson.Value, prec int) string {
	x := formatNumber(v, "%")
	return fixed(x*100, prec) + "%"
}

func formatNumber(v pyjson.Value, code string) float64 {
	if _, ok := v.(string); ok {
		Raise("ValueError", "Unknown format code '%s' for object of type 'str'", code)
	}
	if !isNumber(v) {
		Raise("TypeError", "unsupported format string passed to %s.__format__", TypeName(v))
	}
	x, _ := Number(v)
	return x
}

func fixed(x float64, prec int) string {
	switch {
	case math.IsNaN(x):
		return "nan"
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	}
	return strconv.FormatFloat(x, 'f', prec, 64)
}

// Int は int(s) (10 進の整数の文字列。読めなければ ValueError)。本家の int("{:.0f}".format(x)) に使う。
func Int(s string) *big.Int {
	n, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
	if !ok {
		Raise("ValueError", "invalid literal for int() with base 10: %s", pystr.Repr(s))
	}
	return n
}
