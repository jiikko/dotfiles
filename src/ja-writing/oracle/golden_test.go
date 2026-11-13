package oracle

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	corpusDir = "../testdata/corpus"
	goldenDir = "../testdata/golden"
)

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func loadPatterns(t *testing.T) PatternsGolden {
	t.Helper()
	var g PatternsGolden
	if err := ReadGolden(goldenDir, PatternsFile, &g); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("パターンの記録がありません。make golden で作る: %v", err)
		}
		t.Fatal(err)
	}
	return g
}

// Python の hashlib で同じ並べ方をして得た値。Digest の並べ方が Python の driver とずれたら落ちる。
func TestDigestMatchesPythonOrdering(t *testing.T) {
	d := NewDigest()
	d.Unit("a.md#doc")
	d.Match(0, 3)
	d.Match(5, 6, 5, 6, -1, -1)
	d.Unit("x.md#2")
	d.Match(1, 2, 1, 2)
	if got, want := d.Sum(), "5724de23ef15800b85d4c835500d61a90d5017277e14d5605c7ac47fcc85487e"; got != want {
		t.Fatalf("digest %s、期待 %s", got, want)
	}
	if got := NewDigest().Sum(); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("空の digest %s", got)
	}
}

func TestCorpusIsNotEmpty(t *testing.T) {
	c, err := LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) < 100 || c.Units() < 10000 {
		t.Fatalf("入力の集まりが小さすぎます: %d ファイル・%d 単位", len(c.Files), c.Units())
	}
}

func TestPatternGoldenMatchesCorpusAndUpstream(t *testing.T) {
	g := loadPatterns(t)
	c, err := LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	if g.Header.UpstreamCommit != Commit {
		t.Fatalf("記録の本家の commit %s が固定の %s と違う。make golden", g.Header.UpstreamCommit, Commit)
	}
	if len(g.Header.Corpus) != len(c.Files) {
		t.Fatalf("入力 %d 件に対して記録の入力は %d 件。make golden で作り直す", len(c.Files), len(g.Header.Corpus))
	}
	for i, f := range c.Files {
		if g.Header.Corpus[i] != f {
			t.Fatalf("入力 %s が記録と違う (記録 %+v、今 %+v)。make golden で作り直す", f.Path, g.Header.Corpus[i], f)
		}
	}
}

func TestPatternGoldenIsNotVacuous(t *testing.T) {
	g := loadPatterns(t)
	if len(g.Patterns) == 0 {
		t.Fatal("パターンの記録が 0 件")
	}
	matchedPatterns, matchedUnits := 0, 0
	for _, p := range g.Patterns {
		if !hexDigest.MatchString(p.Digest) {
			t.Fatalf("%q の digest が SHA-256 の 16 進でない: %q", p.Pattern, p.Digest)
		}
		if p.MatchedUnits > 0 {
			matchedPatterns++
			matchedUnits += p.MatchedUnits
		}
		if (p.MatchedUnits == 0) != (p.Matches == 0) || p.Matches < p.MatchedUnits || len(p.Sample) > p.MatchedUnits {
			t.Fatalf("%q の件数が食い違う: 単位 %d・一致 %d・見本 %d", p.Pattern, p.MatchedUnits, p.Matches, len(p.Sample))
		}
		if p.MatchedUnits == 0 && p.Digest != NewDigest().Sum() {
			t.Fatalf("%q は一致 0 件なのに digest が空のものでない", p.Pattern)
		}
	}
	if matchedPatterns == 0 || matchedUnits == 0 {
		t.Fatal("どのパターンも入力に当たっていない (入力か記録の取り方が空振り)")
	}
}

// (パターン, フラグ) は記録の鍵。重複があると、1 つを別の 1 つの写しに置き換えても件数の下限は満たされたままで、失われたパターンが検査から外れる。
func TestPatternGoldenKeysAreUnique(t *testing.T) {
	g := loadPatterns(t)
	type key struct {
		pattern string
		flags   int
	}
	seen := map[key]bool{}
	for _, p := range g.Patterns {
		k := key{p.Pattern, p.Flags}
		if seen[k] {
			t.Errorf("(パターン, フラグ) = (%q, %d) が記録に 2 回ある", p.Pattern, p.Flags)
		}
		seen[k] = true
	}
}

func TestPatternGoldenSamplesPointAtRealUnits(t *testing.T) {
	g := loadPatterns(t)
	lines := map[string]int{}
	for _, f := range g.Header.Corpus {
		lines[f.Path] = f.Lines
	}
	for _, p := range g.Patterns {
		for _, s := range p.Sample {
			path, unit, ok := strings.Cut(s.Unit, "#")
			n, known := lines[path]
			if !ok || !known {
				t.Fatalf("%q の見本の単位 %s が入力に無い", p.Pattern, s.Unit)
			}
			if unit != "doc" && !validLine(unit, n) {
				t.Fatalf("%q の見本の単位 %s の行が範囲外 (行数 %d)", p.Pattern, s.Unit, n)
			}
			for _, m := range s.Matches {
				if len(m) != 2+2*p.Groups {
					t.Fatalf("%q の一致の要素数 %d、グループ %d", p.Pattern, len(m), p.Groups)
				}
			}
		}
	}
}

func validLine(s string, lines int) bool {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
	}
	return s != "" && n < lines
}

func TestPatternGoldenRunsRanTheTools(t *testing.T) {
	g := loadPatterns(t)
	ok := map[string]int{}
	for _, r := range g.Runs {
		if r.RC < 0 || r.RC > 2 {
			t.Errorf("%s %v が rc=%d (内部の誤りか落ちた run)", r.Module, r.Argv, r.RC)
		}
		if r.RC == 0 {
			ok[r.Module]++
		}
	}
	for _, m := range []string{"yomiyasu_lint", "yomiyasu_diff", "yomiyasu_check", "yomiyasu_hook"} {
		if ok[m] == 0 {
			t.Errorf("%s は rc=0 で終わった run が 1 件も無い (道具が動いていない)", m)
		}
	}
	// words の一覧つきの run が全部 rc=2 だと、語の一覧のパターンの経路を通っていない。
	wordsOK := 0
	for _, r := range g.Runs {
		if r.RC == 0 && slices.Contains(r.Argv, "--words") {
			wordsOK++
		}
	}
	if wordsOK == 0 {
		t.Error("--words つきで rc=0 の run が無い (語の一覧が読まれていない)")
	}
}

func TestSmokeGoldenShowsDriversRan(t *testing.T) {
	var g SmokeGolden
	if err := ReadGolden(goldenDir, SmokeFile, &g); err != nil {
		t.Fatalf("make golden で作る: %v", err)
	}
	if g.Header.UpstreamCommit != Commit || len(g.Functions) == 0 || len(g.CLIs) == 0 {
		t.Fatalf("smoke の記録が空か commit が違う: %+v", g.Header)
	}
	versions := 0
	for _, c := range g.CLIs {
		if len(c.Spec.Args) == 1 && c.Spec.Args[0] == "--version" {
			versions++
			if c.Result.RC != 0 || !strings.Contains(c.Result.Stdout, "3.1.0") {
				t.Errorf("%s --version の記録が想定と違う: %+v", c.Spec.Script, c.Result)
			}
		}
	}
	if versions != 4 {
		t.Errorf("--version の記録が %d 件 (期待 4)", versions)
	}
	var sawResult, sawException bool
	for _, f := range g.Functions {
		sawResult = sawResult || (f.Result.Exception == nil && len(f.Result.Result) > 0)
		sawException = sawException || f.Result.Exception != nil
	}
	if !sawResult || !sawException {
		t.Error("関数の記録に戻り値と例外の両方が要る (driver がどちらも持つことの確認)")
	}
}

func TestPlanRunsCoversTools(t *testing.T) {
	c := Corpus{Files: []CorpusFile{
		{Path: "upstream/raw_ai/x.md", Lines: 1}, {Path: "upstream/yomiyasu_rewritten/x.md", Lines: 1},
		{Path: "upstream/blacklist_ai/x.md", Lines: 1}, {Path: "notes.txt", Lines: 1},
	}}
	seen := map[string]int{}
	for _, r := range planRuns(c) {
		seen[r.Module]++
	}
	for m, min := range map[string]int{"yomiyasu_lint": 4, "yomiyasu_diff": 4, "yomiyasu_check": 1, "yomiyasu_hook": 3} {
		if seen[m] < min {
			t.Errorf("%s の run が %d 件 (最低 %d)", m, seen[m], min)
		}
	}
}

// 記録は 1 回の make golden で同じ環境から作る。1 つでも本家の commit・Python・unicodedata のバージョンが違えば、混ざった環境の記録になる。
func TestGoldenFilesShareOneEnvironment(t *testing.T) {
	var first Header
	for i, name := range []string{PatternsFile, PyreFile, UnicodeFile, JSONFile, PystrFile, SmokeFile, DifflibFile, VisibilityFile, PauseCommaFile, LintBaseFile, LintCLIFile, LintRawFile, LintPostFile} {
		var file struct{ Header Header }
		if err := ReadGoldenHeader(goldenDir, name, &file); err != nil {
			t.Fatal(err)
		}
		h := file.Header
		if h.UpstreamCommit != Commit || h.PythonVersion == "" || h.UnicodedataVersion == "" {
			t.Errorf("%s の header が想定と違う: %+v", name, h)
		}
		if i == 0 {
			first = h
			continue
		}
		if diff := first.EnvMismatch(h); diff != "" {
			t.Errorf("%s の環境が %s と違う (%s)", name, PatternsFile, diff)
		}
	}
}

func TestReadGoldenRejectsTrailingData(t *testing.T) {
	dir := t.TempDir()
	good := `{"header":{"upstream_commit":"x","python_version":"3","unicodedata_version":"1"}}`
	for name, body := range map[string]string{"one.json": good + "\n", "two.json": good + "\n" + good + "\n", "junk.json": good + " x"} {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var v struct{ Header Header }
	if err := ReadGolden(dir, "one.json", &v); err != nil {
		t.Fatalf("1 つだけの JSON は読める: %v", err)
	}
	for _, name := range []string{"two.json", "junk.json"} {
		if err := ReadGolden(dir, name, &v); err == nil {
			t.Errorf("%s: 後ろに余りのある記録を受けた", name)
		}
	}
}
