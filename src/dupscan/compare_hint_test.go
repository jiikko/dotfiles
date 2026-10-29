package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// writerGo は、メッセージのリテラルだけが違う L2 の写し (msg が同じなら L1)。
func writerGo(name, msg string) string {
	return "package a\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc " + name + "(path, body string) error {\n" +
		"\tif err := os.WriteFile(path, []byte(body), 0o644); err != nil {\n\t\treturn fmt.Errorf(\"" + msg + ": %w\", err)\n\t}\n\treturn nil\n}\n"
}

func TestReportMembersCarryEndLine(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": writerGo("writeA", "書けない"), "a/b.go": writerGo("writeB", "書けない")})
	rc, out, errs := runReport(t, root)
	if rc != 0 || !strings.Contains(out, "a/a.go:8-13 writeA") || !strings.Contains(out, "a/b.go:8-13 writeB") {
		t.Fatalf("text のメンバーは Read の範囲に渡せる `file:開始-終了`: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, _ = runReport(t, "-format", "json", root)
	var s struct {
		Groups []struct {
			Members []struct {
				Line    int `json:"line"`
				EndLine int `json:"end_line"`
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &s); rc != 0 || err != nil || len(s.Groups) != 1 {
		t.Fatalf("rc=%d err=%v out=%q", rc, err, out)
	}
	for _, m := range s.Groups[0].Members {
		if m.Line != 8 || m.EndLine != 13 {
			t.Errorf("json のメンバーに end_line: %+v", m)
		}
	}
}

func TestVerboseL1GroupSaysOnlyLocalNamesDiffer(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": writerGo("writeA", "書けない"), "a/b.go": writerGo("writeB", "書けない")})
	rc, out, errs := runReport(t, "-v", root)
	if rc != 0 || !strings.Contains(out, "L1 ") || !strings.Contains(out, "  違い: 本体で局所の名前として番号にした綴りだけ") || strings.Contains(out, "違いを見る") {
		t.Fatalf("L1 の組は比べる argv ではなく「本体は局所の名前だけが違う」と言う: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	if _, out, _ = runReport(t, root); strings.Contains(out, "違い") {
		t.Errorf("-v の無い text には出さない: %q", out)
	}
}

// receiver と引数の型はハッシュに入らないので、型だけが違うメソッドも L1 の組になる。L1 の行はそこを同じと言い切らない。
func TestVerboseL1LineDoesNotClaimSignaturesMatch(t *testing.T) {
	method := func(recv, param string) string {
		return "package a\n\nimport \"os\"\n\ntype " + recv + " struct{}\n\nfunc (s *" + recv + ") save(p " + param + ") error {\n" +
			"\tif err := os.WriteFile(\"x\", []byte(p), 0o644); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n"
	}
	root := repoWith(t, map[string]string{"a/a.go": method("SuiteA", "string"), "a/b.go": method("SuiteB", "[]byte")})
	rc, out, errs := runReport(t, "-v", "-min-nodes", "1", root)
	if rc != 0 || !strings.Contains(out, "L1 ") || !strings.Contains(out, "引数と receiver の型は比べていない") {
		t.Fatalf("型だけが違う L1 の組で、署名を比べていないと書く: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

// realRoot は report が出す root (symlink を解いた実パス。macOS の一時 dir は /var → /private/var)。
func realRoot(t *testing.T, root string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// 番号は綴りで振るので、局所の名前と同じ綴りの package も番号になり、違う package を呼ぶ 2 つが L1 の組になる。L1 の行はそれを言う。
func TestVerboseL1LineSaysWhichShadowedOuterNamesStayNumbered(t *testing.T) {
	// 外側の名前を番号にしない扱い (Go は名前解決、Swift は近似) で残る形を、L1 の行が断る。別の package を呼ぶ写しは組にならない (l1_shadow_test.go)
	trim := func(local, param string) string {
		return "package a\n\nimport \"bytes\"\n\nfunc f(" + param + " []byte) bool {\n\t" + local + " := bytes.TrimSpace(" + param + ")\n" +
			"\tif len(" + local + ") > 0 {\n\t\treturn len(" + local + ") > 1\n\t}\n\treturn false\n}\n"
	}
	root := repoWith(t, map[string]string{"a/a.go": trim("t", "v"), "b/b.go": trim("u", "w")})
	rc, out, errs := runReport(t, "-v", "-min-nodes", "1", root)
	if rc != 0 || !strings.Contains(out, "L1 ") || !strings.Contains(out, "Swift は名前を解決しないので、局所の名前と同じ綴りの外側の名前が番号になる形が残る (block・closure・ループの中や if / guard の条件で宣言した綴りを、束縛の届かない所 (その外・else の中) で使う所)") {
		t.Fatalf("L1 の組で、外側の名前が番号になる形が残る範囲を書く: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestVerboseL2GroupPrintsCompareArgvThatResolvesToTheMembers(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": writerGo("writeA", "書けない"), "a/b.go": writerGo("writeB", "保存できない")})
	rc, out, errs := runReport(t, "-v", root)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errs)
	}
	const prefix = "  違いを見る: dupscan explain -compare "
	var argv []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			argv = strings.Fields(rest)
		}
	}
	if len(argv) != 2 || argv[0] != filepath.Join(realRoot(t, root), "a/b.go")+":8" || argv[1] != filepath.Join(realRoot(t, root), "a/a.go")+":8" || strings.Contains(out, "  違い: 本体は") {
		t.Fatalf("L2 の組は先頭のメンバーと他のメンバーを比べる argv: %q", out)
	}
	// 出した argv をそのまま (cwd はこの package の dir で repo の外) 打つと、指したメンバーどうしのリテラルの違いが返る
	rc, cmp, errs := runReport(t, "explain", "-compare", argv[0], argv[1])
	if rc != 0 || !strings.Contains(cmp, `"a/a.go":8 vs "a/b.go":8 differences=1`) || !strings.Contains(cmp, "保存できない") {
		t.Fatalf("argv の比較が違うリテラルを返す: rc=%d out=%q stderr=%q", rc, cmp, errs)
	}
}

func TestGateCompareArgvOnlyForWorktreeHead(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": writerGo("writeA", "書けない")})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "a/b.go", writerGo("writeB", "保存できない"))
	rc, out, errs := runReport(t, "gate", "-v", "-base", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "  違いを見る: dupscan explain -compare "+filepath.Join(realRoot(t, root), "a/a.go")+":8 "+filepath.Join(realRoot(t, root), "a/b.go")+":8") {
		t.Fatalf("作業ツリーを head にした gate の違反にも argv を出す: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "copy")
	writeFile(t, root, "a/b.go", "package a\n")
	rc, out, errs = runReport(t, "gate", "-v", "-base", "HEAD~1", "-head", "HEAD", root)
	if rc != 1 || !strings.Contains(out, "a/b.go:8-13 writeB") || strings.Contains(out, "違いを見る") {
		t.Fatalf("head が commit なら argv を出さない (explain は作業ツリーを読む): rc=%d out=%q stderr=%q", rc, out, errs)
	}
}
