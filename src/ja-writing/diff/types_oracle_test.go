package diff

import (
	"slices"
	"testing"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 F の Go の表と関数を、本家の記録と突き合わせる道具。定数は本家の値そのもの (re.compile の flags は str のパターンで re.UNICODE (32) が付く)。

const pyUnicodeFlag int64 = 32

// typeConstantValue は群 F の本家の定数 (oracle.diffTypeConstants) の、Go の表から作った値。
func typeConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "_END":
		return typeEnd
	case "_TYPE_PATTERNS":
		return replay.MapList(typeKinds, func(k typeKind) pyjson.Value { return replay.List(k.name, k.pattern.Pattern()) })
	case "TYPE_MARKERS":
		o := pyjson.NewObject()
		for _, k := range typeKinds {
			o.Set(k.name, replay.Pattern(pyUnicodeFlag|int64(k.pattern.Flags()), k.pattern.Pattern()))
		}
		return replay.SortKeys(o)
	case "_TYPE_LITERALS":
		o := pyjson.NewObject()
		for _, k := range typeKinds {
			o.Set(k.name, replay.Strings(k.literals))
		}
		return replay.SortKeys(o)
	case "_SIMILE_HEADS":
		return replay.Strings(simileHeads)
	case "_SIMILE_LIKE":
		return replay.Pattern(pyUnicodeFlag|int64(simileLike.Flags()), simileLike.Pattern())
	case "_STOPS":
		return stopsChars
	case "_STOP":
		return replay.Pattern(pyUnicodeFlag|int64(stopPattern.Flags()), stopPattern.Pattern())
	case "_NOT_BODY":
		var chars []rune
		for c := range notBody {
			chars = append(chars, c)
		}
		slices.Sort(chars)
		return replay.MapList(chars, func(c rune) pyjson.Value { return pyunicode.FromRunes([]rune{c}) })
	case "_SENTENCE_PIECE":
		return replay.Pattern(pyUnicodeFlag|int64(sentencePiece.Flags()), sentencePiece.Pattern())
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func kindHitsValue(list []KindHits) pyjson.Value {
	o := pyjson.NewObject()
	for _, e := range list {
		o.Set(e.Kind, replay.Strings(e.Hits))
	}
	return replay.SortKeys(o)
}

// typeLostValue は種類と印の一覧を [{"kind", "hits"}] にした値 (_type_changes の lost と added)。
func typeLostValue(list []KindHits) pyjson.Value {
	return replay.MapList(list, func(e KindHits) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("hits", replay.Strings(e.Hits))
		o.Set("kind", e.Kind)
		return o
	})
}

func typeChangesValue(changes []TypeChange) pyjson.Value {
	return replay.MapList(changes, func(c TypeChange) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("added", typeLostValue(c.Added))
		o.Set("lost", typeLostValue(c.Lost))
		o.Set("orig", c.Orig)
		o.Set("rewrite", c.Rewrite)
		return o
	})
}

// refName は引数の {"$ref": 名前} の名前。
func refName(t *testing.T, a any) string {
	t.Helper()
	m, ok := a.(map[string]any)
	if !ok {
		t.Fatalf("$ref でない引数 %T", a)
	}
	return m["$ref"].(string)
}

// invokeTypeChanges は _type_changes の呼び出し。記録は normalize した 2 つの文を、_ExactSequenceMatcher の get_opcodes と一緒に渡している
// (本家の _rule_candidates の「o != r のときだけ」は、記録の呼び出しでは付けず、いつも照合する)。
func invokeTypeChanges(t *testing.T, c oracle.Call, saved map[string]string) pyjson.Value {
	t.Helper()
	o, r := saved[refName(t, c.Args[0])], saved[refName(t, c.Args[1])]
	ops := difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	return typeChangesValue(TypeChanges(o, r, ops, nil))
}
