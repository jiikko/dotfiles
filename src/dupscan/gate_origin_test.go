package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// recorderGo は records に積んで数を返すメソッド。tag だけが違う写しは L2 で、tag も同じなら L1 で同じ形。
func recorderGo(typ, tag string) string {
	return "package a\n\ntype " + typ + " struct{ records []string }\n\nfunc (r *" + typ + ") Insert(id, name string) bool {\n" +
		"\tr.records = append(r.records, \"" + tag + ":\"+id)\n\tr.records = append(r.records, name)\n" +
		"\tif len(r.records) > 100 {\n\t\tr.records = r.records[1:]\n\t}\n\treturn len(r.records) > 0\n}\n"
}

func gateNew(t *testing.T, root string) (string, []map[string]any) {
	out, v := gateViolation(t, root)
	origins, _ := v["origins"].([]any)
	var got []map[string]any
	for _, o := range origins {
		got = append(got, o.(map[string]any))
	}
	return out, got
}

func gateViolation(t *testing.T, root string) (string, map[string]any) {
	t.Helper()
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	if rc != 1 {
		t.Fatalf("違反の前提: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	_, js, _ := runGateCmd(t, "-base", "HEAD", "-format", "json", root)
	var s struct{ Violations []map[string]any }
	if err := json.Unmarshal([]byte(js), &s); err != nil || len(s.Violations) != 1 {
		t.Fatalf("err=%v json=%s", err, js)
	}
	return out, s.Violations[0]
}

// 寄せ先に置いた正本が、消した写しの出どころに当たり、本体を変えて既存の別の写しと同じ形になった。違反のまま、出どころを名指しする。
func TestGateNamesOriginOfConsolidatedUnit(t *testing.T) {
	root := repoWith(t, map[string]string{"a/p.go": recorderGo("P", "ins"), "a/q.go": recorderGo("Q", "ins"),
		"a/r.go": recorderGo("R", "upd"), "a/s.go": recorderGo("S", "upd")})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "base")
	gitRun(t, root, "rm", "-q", "a/p.go", "a/q.go")
	writeFile(t, root, "a/shared.go", recorderGo("Shared", "upd"))
	out, news := gateNew(t, root)
	if !strings.Contains(out, "新しい: a/shared.go:5-12 Shared.Insert(id, name string) bool (base の a/") || !strings.Contains(out, "から。本体を変えた)") {
		t.Fatalf("寄せる側に出た単位に、base の出どころと本体を変えたことを添える: %q", out)
	}
	if len(news) != 1 || news[0]["key"] != "a/shared.go|Shared|Insert(id, name string) bool" || news[0]["body_changed"] != true || news[0]["sure"] != true {
		t.Fatalf("json の origins に寄せる側の鍵・出どころ・body_changed・sure: %v", news)
	}
	if from, _ := news[0]["from"].(map[string]any); from == nil || !strings.HasPrefix(from["key"].(string), "a/") {
		t.Errorf("出どころは base の単位: %v", news[0])
	}
}

func TestGateNewlyWrittenUnitHasNoOrigin(t *testing.T) {
	root := repoWith(t, map[string]string{"a/r.go": recorderGo("R", "upd")})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "base")
	writeFile(t, root, "a/s.go", recorderGo("S", "upd"))
	out, v := gateViolation(t, root)
	if strings.Contains(out, "base の") || strings.Contains(out, "本体を変えた") {
		t.Errorf("新しく書いた単位には出どころを付けない: %q", out)
	}
	if origins, ok := v["origins"].([]any); !ok || len(origins) != 0 {
		t.Errorf("json の origins は空の配列: %v", v["origins"])
	}
}

// その場で本体を変えて既存の写しと同じ形になった単位は、同じ単位から来たと言う。
func TestGateEditedInPlaceSaysSameUnit(t *testing.T) {
	root := repoWith(t, map[string]string{"a/r.go": recorderGo("R", "upd"), "a/s.go": recorderGo("S", "other")})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "base")
	writeFile(t, root, "a/s.go", recorderGo("S", "upd"))
	out, _ := gateNew(t, root)
	if !strings.Contains(out, "新しい: a/s.go:5-12 S.Insert(id, name string) bool (本体を変えた。base にも同じ単位)") {
		t.Errorf("同じ鍵なら名指しせず、本体を変えたと言う: %q", out)
	}
}
