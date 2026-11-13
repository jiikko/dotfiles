package diff

import (
	"errors"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/lists"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 R の Go の関数を、本家の記録と突き合わせる道具。

func listPatternValue(regex string) pyjson.Value {
	p := pyre.NewLazy(regex, 0)
	return replay.Pattern(pyUnicodeFlag|int64(p.Flags()), p.Pattern())
}

func listItemValue(item ListItem, compiled bool) pyjson.Value {
	regex := func(s string) pyjson.Value {
		if compiled {
			return listPatternValue(s)
		}
		return s
	}
	out := replay.List(item.Name, regex(item.Regex))
	if item.Third != "" {
		out = append(out.([]pyjson.Value), regex(item.Third))
	}
	return out
}

func listItemsValue(items []ListItem, compiled bool) pyjson.Value {
	return replay.MapList(items, func(i ListItem) pyjson.Value { return listItemValue(i, compiled) })
}

// invokeList は群 R の呼び出し 1 つの結果。本家が例外を投げる呼び出しは、型と文面を返す (ok は偽)。
func invokeList(t *testing.T, c oracle.Call) (v pyjson.Value, excType, excMessage string, raised bool) {
	t.Helper()
	switch c.Function {
	case "parse_list":
		text := argString(t, c.Args[0])
		if !argBool(t, c.Args[1]) {
			return replay.Strings(lists.Words(text)), "", "", false
		}
		items, err := ParsePatternList(text)
		var listErr *ListError
		if errors.As(err, &listErr) {
			return nil, "ValueError", listErr.Message, true
		}
		if err != nil {
			t.Fatalf("parse_list: %v", err)
		}
		return listItemsValue(items, false), "", "", false
	case "_load_list":
		name := argString(t, c.Args[0])
		l := LoadList(name)
		if wordLists[name].pattern {
			return replay.List(listItemsValue(l.Items, true), ListSource(name)), "", "", false
		}
		return replay.List(replay.Strings(l.Words), ListSource(name)), "", "", false
	case "_list_info":
		name := argString(t, c.Args[0])
		o := pyjson.NewObject()
		o.Set("list", ListSource(name))
		o.Set("list_problem", nil)
		return o, "", "", false
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil, "", "", false
}

// listDigest は invokeList の結果の digest (本家が例外を投げたら例外の digest)。
func listDigest(t *testing.T, c oracle.Call) (canon, digest string, raised bool) {
	t.Helper()
	v, typ, msg, raised := invokeList(t, c)
	var err error
	if raised {
		canon, digest, err = replay.ExceptionDigest(typ, msg)
	} else {
		canon, digest, err = replay.ResultDigest(v)
	}
	if err != nil {
		t.Fatal(err)
	}
	return canon, digest, raised
}

// 埋め込んだ 4 本の一覧の正規表現は、全部 pyre で組める (本家の _compile_item が re.compile するのと同じ)。
func TestEmbeddedListPatternsCompile(t *testing.T) {
	for _, name := range oracle.DiffListNames {
		l := LoadList(name)
		if wordLists[name].pattern {
			if len(l.Items) == 0 {
				t.Errorf("%s の項目が 0 件", name)
			}
			for _, item := range l.Items {
				for _, regex := range []string{item.Regex, item.Third} {
					if regex == "" {
						continue
					}
					if _, err := pyre.Compile(regex, 0); err != nil {
						t.Errorf("%s の %q の正規表現 %q を組めない: %v", name, item.Name, regex, err)
					}
				}
			}
		} else if len(l.Words) == 0 {
			t.Errorf("%s の語が 0 件", name)
		}
	}
}
