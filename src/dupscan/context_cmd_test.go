package main

import (
	"strings"
	"testing"
)

const contextSource = `struct Box {
    func combine(_ left: Int, named right: Int) -> Int { left + right }
}
struct Other {
    func combine(_ left: Int, named right: Int) -> Int { left - right }
    func combine(_ only: Int) -> Int { only }
}
func use(_ box: Box, _ other: Other) -> Int {
    let combine = 0
    return box.combine(1, named: 2) + other.combine(3) + combine
}
`

func contextCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"context"}, args...)...)
}

// contextSections は context の出力を、見出し (`## ` の行) ごとの本文に分ける。先頭の要約は "" に入る。
func contextSections(t *testing.T, out string) map[string]string {
	t.Helper()
	parts := strings.Split(out, "\n## ")
	got := map[string]string{"": parts[0] + "\n"}
	for _, p := range parts[1:] {
		title, body, _ := strings.Cut(p, "\n")
		key, _, _ := strings.Cut(title, " ")
		got[key] = body
	}
	return got
}

func TestContextSectionsEqualDefRefsAndCallers(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": contextSource})
	rc, out, errs := contextCmd(t, "-limit", "0", "combine(_:named:)", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	sec := contextSections(t, out)
	for _, c := range []struct{ section, cmd, query string }{
		{"定義", "def", "combine(_:named:)"},
		{"参照:", "refs", "combine"},
		{"呼び出し:", "callers", "combine(_:named:)"},
	} {
		_, want, _ := runReport(t, c.cmd, "-limit", "0", c.query, root)
		if sec[c.section] != want {
			t.Errorf("%s の節は `%s %s` と同じ中身:\ncontext=%q\n%s=%q", c.section, c.cmd, c.query, sec[c.section], c.cmd, want)
		}
	}
	if !strings.HasPrefix(out, "dupscan context: combine(_:named:) — 定義 2 件 / 参照 3 件 / 呼び出し 1 件\n") {
		t.Errorf("1 行目は 3 つの件数: %q", out)
	}
	if !strings.Contains(sec[""], "参照は名前 combine、呼び出しは combine(_:named:) で当てた近似") || !strings.Contains(sec[""], "定義が 2 件あり 1 つに決まっていない") {
		t.Errorf("近似であることと、定義が 1 つに決まっていないことを要約に出す: %q", sec[""])
	}
}

func TestContextTypeMemberDoesNotNarrowRefsOrCallers(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": contextSource})
	rc, out, errs := contextCmd(t, "Box.combine", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	if !strings.Contains(out, "定義 1 件 / 参照 3 件 / 呼び出し 2 件") || !strings.Contains(out, "Box では絞っていない") {
		t.Errorf("定義は Box で絞り、参照・呼び出しは名前だけで当てたと書く: %q", out)
	}
	if strings.Contains(out, "1 つに決まっていない") {
		t.Errorf("定義が 1 件なら曖昧の行を出さない: %q", out)
	}
}

func TestContextOmissionHintNamesOnlyItsOwnFlags(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": contextSource})
	rc, out, errs := contextCmd(t, "-limit", "1", "combine", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	if strings.Count(out, "… 他 ") != 3 || strings.Contains(out, "-format") {
		t.Errorf("3 つの節とも省略を書き、context に無い -format を案内しない: %q", out)
	}
	_, legacy, _ := def(t, "-limit", "1", "combine", root)
	if !strings.Contains(legacy, "(-limit 0 か -format json で全部)") {
		t.Errorf("def の省略の案内は変えない: %q", legacy)
	}
}

func TestContextNoDefinitionStillShowsRefsAndCallersWithRcZero(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": "func f() { missing() }\n"})
	rc, out, errs := contextCmd(t, "missing", root)
	if rc != 0 || errs != "" || !strings.Contains(out, "定義 0 件 / 参照 1 件 / 呼び出し 1 件") || !strings.Contains(out, "missing の定義なし") {
		t.Errorf("定義が無くても参照・呼び出しを出して rc=0: rc=%d stderr=%q out=%q", rc, errs, out)
	}
	if !strings.Contains(out, "定義が 0 件。参照・呼び出しは問い合わせた定義のものとは限らない") {
		t.Errorf("定義が 0 件なら、参照・呼び出しが別のものかもしれないと書く: %q", out)
	}
}

func TestContextInitializerLooksUpCallsByTypeName(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Header.swift": `struct Header {
    init(tag: Int) {}
    init() { self.init(tag: 0) }
}
struct Other {
    init(tag: Int) {}
}
func make() -> [Header] {
    [Header(tag: 1), Header(tag: 2), Header()]
}
`})
	rc, out, errs := contextCmd(t, "-limit", "0", "Header.init(tag:)", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	if !strings.Contains(out, "定義 1 件 / 参照 4 件 / 呼び出し 2 件") || !strings.Contains(out, "## 参照: Header (4 件)") || !strings.Contains(out, "初期化子なので型の名前で引いた") {
		t.Errorf("Type.init の参照・呼び出しは型の名前で引き (init で引くと self.init だけになる)、そう書く: %q", out)
	}
	_, want, _ := runReport(t, "callers", "-limit", "0", "Header(tag:)", root)
	if contextSections(t, out)["呼び出し:"] != want {
		t.Errorf("呼び出しの節は `callers Header(tag:)` と同じ: %q / %q", out, want)
	}
	if rc, _, errs := contextCmd(t, "Header..init", root); rc != 2 || !strings.Contains(errs, "引く名前が決まらない") {
		t.Errorf("型の名前が空の Type.init は入力の誤り: rc=%d stderr=%q", rc, errs)
	}
	_, out, _ = contextCmd(t, "Header<T>.init", root)
	if !strings.Contains(out, "## 呼び出し: Header (3 件)") {
		t.Errorf("generic の引数を書いた Type.init も型の名前で引く: %q", out)
	}
	if !strings.Contains(out, "`Header<…>(…)` と書いた呼び出しは数えない") || strings.Contains(out, "既定値のある引数を省いた") {
		t.Errorf("型の名前で引いたときの限界を書き、ラベルが無ければ既定値の注記は出さない: %q", out)
	}
	_, out, _ = contextCmd(t, "Header.init(tag:)", root)
	if !strings.Contains(out, "既定値のある引数を省いた呼び出しは数えない") {
		t.Errorf("ラベルで当てるときは既定値の省略を数えないと書く: %q", out)
	}
	_, out, _ = contextCmd(t, "init", root)
	if !strings.Contains(out, "型の呼び出しを見るなら Type.init で引く") {
		t.Errorf("型の無い init では、型の名前で引く方法を案内する: %q", out)
	}
}

func TestContextReportsUnreadableFilesAndSkippedLanguages(t *testing.T) {
	files := map[string]string{"Sources/Box.swift": contextSource, "tool/main.go": "package main\n\nfunc main() {}\n"}
	for _, name := range []string{"A", "B", "C", "D"} {
		files["Sources/Broken"+name+".swift"] = "func broken( {\n"
	}
	root := repoWith(t, files)
	rc, out, errs := contextCmd(t, "combine", root)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	if !strings.Contains(out, "読めない構文を含むファイルが 4 個") || !strings.Contains(out, "ほか 1 個") {
		t.Errorf("読めないファイルの数を出し、並べるのは先頭の 3 個まで: %q", out)
	}
	if !strings.Contains(errs, "go のファイル 1 個") || !strings.Contains(out, "go のファイル 1 個は def / refs の対象外") || !strings.Contains(out, "呼び出しも Swift の分だけ") {
		t.Errorf("読まなかった言語を stderr と要約に出す (呼び出しの節も Swift だけ): stdout=%q stderr=%q", out, errs)
	}
	goOnly := repoWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	if rc, _, errs := contextCmd(t, "main", goOnly); rc != 2 || !strings.Contains(errs, "def / refs の対象外") {
		t.Errorf("Go だけの repo は判定不能で、理由を添える: rc=%d stderr=%q", rc, errs)
	}
}

func TestContextRejectsBadArguments(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": contextSource})
	for _, args := range [][]string{{}, {" "}, {"-max-lines", "0", "combine"}, {"-limit", "-1", "combine"}, {"combine(", root}} {
		if rc, _, _ := contextCmd(t, args...); rc != 2 {
			t.Errorf("%q は rc=2: rc=%d", args, rc)
		}
	}
}
