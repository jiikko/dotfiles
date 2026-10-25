package exclude

import (
	"strings"
	"testing"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

func ent(key, file, h1 string) *group.Entry {
	return &group.Entry{Unit: &tree.Unit{Key: key, File: file}, Result: norm.Result{Hash: map[norm.Level]string{norm.L1: h1}}}
}

func TestPinDropsOrdinals(t *testing.T) {
	// クロージャの出現の順番と多重定義の #n は釘に入れない (前に足すとずれる)。
	a := ent("F.swift|V|var body#closure1#closure3", "F.swift", "0123456789abcdef")
	b := ent("F.swift|V|var body#closure2#closure1", "F.swift", "0123456789abcdef")
	if Pin(a) != Pin(b) || Pin(a) != "F.swift|V|var body@0123456789ab" {
		t.Errorf("順番が釘に残った: %q / %q", Pin(a), Pin(b))
	}
	if Pin(ent("A#1.swift|T|foo()", "A#1.swift", "ff")) != "A#1.swift|T|foo()@ff" {
		t.Errorf("ファイル名の #数字 を釘から削った")
	}
	if Pin(ent("F.swift|T|foo()#2", "F.swift", "ff")) != "F.swift|T|foo()@ff" {
		t.Errorf("多重定義の #n が釘に残った")
	}
}

func TestAmbiguousPinIsNotApplied(t *testing.T) {
	// 同じ囲む単位に同じ本体のクロージャが 2 つあると釘が決まらない: 除外を効かせず、名指しできない除外に出す。
	x, y := ent("F|V|body#closure1", "F", "h"), ent("F|V|body#closure2", "F", "h")
	g := ent("G|W|g", "G", "h")
	c := &Config{Version: 1, Exclude: []Rule{{Group: []string{Pin(x), Pin(g)}, Level: "L1", Kind: "intentional", Reason: "r"}}}
	if err := c.Exclude[0].check(); err != nil {
		t.Fatal(err)
	}
	s := New(c, []*group.Entry{x, y, g}, nil)
	if s.GroupExcluded(norm.L1, []*group.Entry{x, y, g}) || len(s.Ambiguous()) != 1 || len(s.Stale()) != 0 {
		t.Errorf("名指しできない除外が効いた: ambiguous=%d stale=%d", len(s.Ambiguous()), len(s.Stale()))
	}
}

func TestSiblingClosurePinCountedInGroupRule(t *testing.T) {
	// 同じ囲む単位に並んだ同じ本体のクロージャは、釘を人数分書いた組の除外で外せる (報告の除外の 1 行がその形)。
	x, y := ent("F|V|body#closure1", "F", "h"), ent("F|V|body#closure2", "F", "h")
	c := &Config{Version: 1, Exclude: []Rule{{Group: []string{Pin(x), Pin(y)}, Level: "L1", Kind: "intentional", Reason: "r"}}}
	if err := c.Exclude[0].check(); err != nil {
		t.Fatal(err)
	}
	s := New(c, []*group.Entry{x, y}, nil)
	if !s.GroupExcluded(norm.L1, []*group.Entry{x, y}) || len(s.Ambiguous()) != 0 || len(s.Stale()) != 0 {
		t.Errorf("人数分の釘を書いた除外が効かない: ambiguous=%d stale=%d", len(s.Ambiguous()), len(s.Stale()))
	}
	// 同じ本体のクロージャが 3 つ目に増えたら、書いた回数を超えるので効かせない (組が再び出る)。
	z := ent("F|V|body#closure3", "F", "h")
	s = New(c, []*group.Entry{x, y, z}, nil)
	if s.GroupExcluded(norm.L1, []*group.Entry{x, y, z}) || len(s.Ambiguous()) != 1 {
		t.Errorf("書いた回数を超えて当たる釘の除外が効いた: ambiguous=%d", len(s.Ambiguous()))
	}
}

func TestMatchGlob(t *testing.T) {
	for _, c := range []struct {
		glob, file string
		want       bool
	}{
		{"Generated/**", "Generated/A.swift", true},
		{"Generated/**", "Generated/x/y/A.swift", true},
		{"Generated/**", "Sources/Generated/A.swift", false},
		{"**/*Tests.swift", "FooTests.swift", true},
		{"**/*Tests.swift", "a/b/FooTests.swift", true},
		{"Sources/*.swift", "Sources/x/A.swift", false},
	} {
		if got := Match(c.glob, c.file); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.glob, c.file, got)
		}
	}
}

func TestNewElementsAreItemized(t *testing.T) {
	// 並べ替えと reason の直しは新しい要素に出ず、既存の組に足した釘だけが出る。
	parse := func(s string) *Config {
		c, err := Parse([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	base := parse(`{"version":1,"exclude":[{"group":["a@1","b@2"],"level":"L1","kind":"intentional","reason":"r"}]}`)
	head := parse(`{"version":1,"exclude":[{"group":["c@3","b@2","a@1"],"level":"L1","kind":"intentional","reason":"直した"}]}`)
	var got []string
	for _, el := range NewElements(base, head) {
		got = append(got, el.Key)
	}
	if strings.Join(got, ",") != "group:L1:c@3" {
		t.Errorf("新しい要素: %v", got)
	}
}
