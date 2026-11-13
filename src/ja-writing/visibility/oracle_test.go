package visibility_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

const (
	goldenDir = "../testdata/golden"
	corpusDir = "../testdata/corpus"
)

// このファイルは、本家 (Python) の記録 (testdata/golden/visibility.json) と Go の実装を、公開面の全関数について突き合わせる。
// 記録は oracle.VisibilityCases の呼び出しを本家に通した結果の digest (正規の JSON の SHA-256 の先頭) で、Go も同じ呼び出しを通して同じ正規の JSON を作る。

type env struct {
	reg  map[string]any
	fx   *oracle.Fixture
	root string
}

func obj(kv ...any) *pyjson.Object {
	o := pyjson.NewObject()
	for i := 0; i < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func strOrNil(p *string) pyjson.Value {
	if p == nil {
		return nil
	}
	return *p
}

func lineValue(l *visibility.Line) pyjson.Value {
	return obj("line", int64(l.Line), "raw", l.Raw, "start", int64(l.Start), "ending", l.Ending, "content_start", int64(l.ContentStart),
		"content", l.Content, "quote_depth", int64(l.QuoteDepth), "list_item", l.ListItem, "kind", l.Kind)
}

func spansValue(spans []visibility.Span) pyjson.Value {
	out := make([]pyjson.Value, len(spans))
	for i, s := range spans {
		out[i] = []pyjson.Value{int64(s.Start), int64(s.End), s.Kind}
	}
	return out
}

func analysisValue(a *visibility.Analysis) pyjson.Value {
	lines := make([]pyjson.Value, len(a.Lines))
	for i, l := range a.Lines {
		lines[i] = lineValue(l)
	}
	blocks := make([]pyjson.Value, len(a.Blocks))
	for i, b := range a.Blocks {
		bl := make([]pyjson.Value, len(b.Lines))
		for j, l := range b.Lines {
			bl[j] = lineValue(l)
		}
		blocks[i] = obj("kind", b.Kind, "lines", bl)
	}
	masks := make([]pyjson.Value, len(a.MaskSpans))
	for i, m := range a.MaskSpans {
		masks[i] = []pyjson.Value{int64(m[0]), int64(m[1])}
	}
	ends := make([]pyjson.Value, len(a.MaskEnds))
	for i, e := range a.MaskEnds {
		ends[i] = int64(e)
	}
	return obj("text", a.Text, "lines", lines, "blocks", blocks, "protected_spans", spansValue(a.ProtectedSpans), "mask_spans", masks, "mask_ends", ends)
}

func checkedValue(c *visibility.CheckedPattern) pyjson.Value {
	req := make([]pyjson.Value, len(c.Required))
	for i, r := range c.Required {
		req[i] = pyjson.Strings(r)
	}
	return []pyjson.Value{patternValue(c), req, int64(c.Cost)}
}

func patternValue(c *visibility.CheckedPattern) pyjson.Value {
	return obj("pattern", c.Compiled.Pattern(), "flags", int64(c.Compiled.Flags()))
}

func wordsValue(w *visibility.Words) pyjson.Value {
	repl := pyjson.NewObject()
	keys := make([]string, 0, len(w.FlagReplacements))
	for k := range w.FlagReplacements {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		repl.Set(k, w.FlagReplacements[k])
	}
	o := obj("path", w.Path, "flag_replacements", repl, "allow", pyjson.Strings(w.Allow), "allow_lines", int64(w.AllowLines), "allow_sha256", strOrNil(w.AllowSHA256),
		"flag", pyjson.Strings(w.Flag), "flag_lines", int64(w.FlagLines), "flag_sha256", strOrNil(w.FlagSHA256))
	if w.Patterns != nil {
		entries := make([]pyjson.Value, len(w.Patterns.Entries))
		for i, e := range w.Patterns.Entries {
			req := make([]pyjson.Value, len(e.Compiled.Required))
			for j, r := range e.Compiled.Required {
				req[j] = pyjson.Strings(r)
			}
			entries[i] = []pyjson.Value{e.Name, e.Pattern, patternValue(e.Compiled), req}
		}
		o.Set("flag_patterns", entries)
		o.Set("flag_patterns_lines", int64(w.Patterns.Lines))
		o.Set("flag_patterns_sha256", w.Patterns.SHA256)
	}
	return o
}

func contextValue(c visibility.WordsContext) pyjson.Value {
	o := obj("path", c.Path, "allow_lines", int64(c.AllowLines), "flag_lines", int64(c.FlagLines), "allow_sha256", strOrNil(c.AllowSHA256), "flag_sha256", strOrNil(c.FlagSHA256))
	if c.PatternsSHA256 != nil {
		o.Set("flag_patterns_lines", int64(c.PatternsLines))
		o.Set("flag_patterns_sha256", *c.PatternsSHA256)
	}
	return o
}

func constantValue(name string) (pyjson.Value, bool) {
	switch name {
	case "DOC_TYPES":
		out := make([]pyjson.Value, len(visibility.DocTypes))
		for i, d := range visibility.DocTypes {
			out[i] = []pyjson.Value{d.Value, d.Name, pyjson.Strings(d.Domains)}
		}
		return out, true
	case "READER_LEVELS":
		out := make([]pyjson.Value, len(visibility.ReaderLevels))
		for i, r := range visibility.ReaderLevels {
			out[i] = []pyjson.Value{r.Value, r.Name}
		}
		return out, true
	case "WORDS_FILES":
		out := make([]pyjson.Value, len(visibility.WordsFiles))
		for i, f := range visibility.WordsFiles {
			out[i] = []pyjson.Value{f[0], f[1]}
		}
		return out, true
	case "WORDS_FOLDER":
		return visibility.WordsFolder, true
	case "WORDS_MAX_LINES":
		return int64(visibility.WordsMaxLines), true
	case "FLAG_REPLACEMENT_MAX":
		return int64(visibility.FlagReplacementMax), true
	case "PATTERNS_FILE":
		return visibility.PatternsFile, true
	case "PATTERNS_MAX":
		return int64(visibility.PatternsMax), true
	case "PATTERN_NAME_MAX":
		return int64(visibility.PatternNameMax), true
	case "PATTERN_MAX_LENGTH":
		return int64(visibility.PatternMaxLength), true
	case "PATTERN_MAX_REPEAT":
		return int64(visibility.PatternMaxRepeat), true
	case "PATTERN_MAX_COST":
		return int64(visibility.PatternMaxCost), true
	case "PATTERNS_MAX_COST":
		return int64(visibility.PatternsMaxCost), true
	case "INPUT_PATH_OUTSIDE":
		return visibility.InputPathOutside, true
	case "INPUT_PATH_HIDDEN":
		return visibility.InputPathHidden, true
	case "INPUT_PATH_NO_CWD":
		return visibility.InputPathNoCwd, true
	case "__version__":
		return visibility.Version, true
	}
	return nil, false
}

// 引数の読み (case の組み立てが渡す型だけ)。
func (e *env) argString(t *testing.T, a any) string {
	t.Helper()
	switch v := a.(type) {
	case string:
		if e.fx != nil {
			return e.fx.Sub(v)
		}
		return v
	case oracle.Cps:
		return pyunicode.FromRunes(v)
	}
	t.Fatalf("文字列でない引数: %T", a)
	return ""
}

func (e *env) argOptString(t *testing.T, a any) *string {
	t.Helper()
	if a == nil {
		return nil
	}
	s := e.argString(t, a)
	return &s
}

func argInt(t *testing.T, a any) int {
	t.Helper()
	v, ok := a.(int)
	if !ok {
		t.Fatalf("整数でない引数: %T", a)
	}
	return v
}

func (e *env) argKinds(t *testing.T, a any) []string {
	t.Helper()
	if a == nil {
		return nil
	}
	list, ok := a.([]any)
	if !ok {
		t.Fatalf("種類の一覧でない引数: %T", a)
	}
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = e.argString(t, x)
	}
	return out
}

func (e *env) argRef(t *testing.T, a any) any {
	t.Helper()
	r, ok := a.(oracle.Ref)
	if !ok {
		t.Fatalf("Ref でない引数: %T", a)
	}
	v, ok := e.reg[string(r)]
	if !ok {
		panic(missingRef(r))
	}
	return v
}

// missingRef は保存できなかった (その呼び出しが例外で終わった) Ref を読んだこと。Python の driver は registry[名前] の KeyError になる。
type missingRef string

// invoke は call を Go の実装で評価する。*visibility.Error は例外として返す。
func (e *env) invoke(t *testing.T, c oracle.Call) (v pyjson.Value, exc *visibility.Error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			name, ok := r.(missingRef)
			if !ok {
				panic(r)
			}
			v, exc = nil, &visibility.Error{Type: "KeyError", Message: "'" + string(name) + "'"}
		}
	}()
	a := c.Args
	fail := func(err error) (pyjson.Value, *visibility.Error) {
		var pe *visibility.Error
		if errors.As(err, &pe) {
			return nil, pe
		}
		t.Fatalf("%s が本家の例外でない error を返した: %v", c.Function, err)
		return nil, nil
	}
	if c.Get {
		v, ok := constantValue(c.Function)
		if !ok {
			t.Fatalf("定数 %s を Go に持っていない", c.Function)
		}
		return v, nil
	}
	save := func(v any) {
		if c.Save != "" {
			e.reg[c.Save] = v
		}
	}
	switch c.Function {
	case "analyze_markdown":
		skip := true
		if len(a) > 1 {
			skip = a[1].(bool)
		}
		an, err := visibility.AnalyzeMarkdown(e.argString(t, a[0]), skip)
		if err != nil {
			return fail(err)
		}
		save(an)
		return analysisValue(an), nil
	case "line_visible_text":
		an := e.argRef(t, a[0]).(*visibility.Analysis)
		var kinds []string
		if len(a) > 2 {
			kinds = e.argKinds(t, a[2])
		}
		s, err := an.LineVisibleText(argInt(t, a[1]), kinds)
		if err != nil {
			return fail(err)
		}
		return s, nil
	case "range_overlaps_protected":
		an := e.argRef(t, a[0]).(*visibility.Analysis)
		var kinds []string
		if len(a) > 3 {
			kinds = e.argKinds(t, a[3])
		}
		return an.RangeOverlapsProtected(argInt(t, a[1]), argInt(t, a[2]), kinds), nil
	case "inline_protected_spans":
		return spansValue(visibility.InlineProtectedSpans(e.argString(t, a[0]))), nil
	case "reference_definition_end":
		list := a[0].([]any)
		lines := make([]string, len(list))
		for i, l := range list {
			lines[i] = e.argString(t, l)
		}
		paragraph := len(a) > 2 && a[2].(bool)
		end, ok, err := visibility.ReferenceDefinitionEnd(lines, argInt(t, a[1]), paragraph)
		if err != nil {
			return fail(err)
		}
		if ok {
			return int64(end), nil
		}
		return nil, nil
	case "unicode_whitespace":
		return visibility.UnicodeWhitespace(e.argString(t, a[0])), nil
	case "decode_source":
		var text string
		var info visibility.SourceInfo
		if b, ok := a[0].(oracle.Bytes); ok {
			var err error
			if text, info, err = visibility.DecodeSourceBytes(b); err != nil {
				return fail(err)
			}
		} else {
			text, info = visibility.DecodeSource(e.argString(t, a[0]))
		}
		return []pyjson.Value{text, obj("bom", info.BOM, "eol", info.EOL)}, nil
	case "normalize_doc_type", "normalize_reader":
		fn := visibility.NormalizeDocType
		if c.Function == "normalize_reader" {
			fn = visibility.NormalizeReader
		}
		v, err := fn(e.argOptString(t, a[0]))
		if err != nil {
			return fail(err)
		}
		return strOrNil(v), nil
	case "context_label":
		s, err := visibility.ContextLabel(e.argOptString(t, a[0]), e.argOptString(t, a[1]))
		if err != nil {
			return fail(err)
		}
		return s, nil
	case "find_words_folder":
		folder, ok, err := visibility.FindWordsFolder(e.argOptString(t, a[0]), e.argOptString(t, a[1]))
		if err != nil {
			return fail(err)
		}
		if !ok {
			return nil, nil
		}
		return folder, nil
	case "load_words":
		w, ok, err := visibility.LoadWords(e.argString(t, a[0]))
		if err != nil {
			return fail(err)
		}
		if !ok {
			save(nil)
			return nil, nil
		}
		save(w)
		return wordsValue(w), nil
	case "resolve_words":
		w, ok, err := visibility.ResolveWords(e.argOptString(t, a[0]), e.argOptString(t, a[1]), a[2].(bool))
		if err != nil {
			return fail(err)
		}
		if !ok {
			return nil, nil
		}
		return wordsValue(w), nil
	case "current_directory":
		if cwd, ok := visibility.CurrentDirectory(); ok {
			return cwd, nil
		}
		return nil, nil
	case "words_context":
		w, ok := e.argRef(t, a[0]).(*visibility.Words)
		if !ok || w == nil {
			t.Fatalf("words_context に渡す Ref が語の一覧でない")
		}
		ctx := visibility.WordsContextOf(w)
		save(ctx)
		return contextValue(ctx), nil
	case "words_label":
		return visibility.WordsLabel(e.argRef(t, a[0]).(visibility.WordsContext)), nil
	case "check_pattern":
		p, err := visibility.CheckPattern(e.argString(t, a[0]))
		if err != nil {
			return fail(err)
		}
		return checkedValue(p), nil
	case "input_path_problem":
		if reason, ok := visibility.InputPathProblem(e.argString(t, a[0])); !ok {
			return reason, nil
		}
		return nil, nil
	case "absolute_path":
		p, err := visibility.AbsolutePath(e.argString(t, a[0]))
		if err != nil {
			return fail(err)
		}
		return p, nil
	}
	t.Fatalf("Go に dispatch の無い関数: %s", c.Function)
	return nil, nil
}

// unroot は結果の中の文字列の作業フォルダを RootToken に戻す (driver の unroot と同じ)。
func unroot(fx *oracle.Fixture, v pyjson.Value) pyjson.Value {
	if fx == nil {
		return v
	}
	switch x := v.(type) {
	case string:
		return fx.Unroot(x)
	case []pyjson.Value:
		out := make([]pyjson.Value, len(x))
		for i, e := range x {
			out[i] = unroot(fx, e)
		}
		return out
	case *pyjson.Object:
		out := pyjson.NewObject()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			out.Set(k, unroot(fx, val))
		}
		return out
	}
	return v
}

var canonical = pyjson.Options{SortKeys: true, Separators: &pyjson.Separators{Item: ",", Key: ":"}}

// record は結果か例外から、本家の driver と同じ正規の JSON (と、その digest) を作る。
func record(t *testing.T, fx *oracle.Fixture, v pyjson.Value, exc *visibility.Error) (canon, digest string, raised bool) {
	t.Helper()
	var rec *pyjson.Object
	if exc != nil {
		msg := exc.Message
		if fx != nil {
			msg = fx.Unroot(msg)
		}
		rec = obj("exception", obj("type", exc.Type, "message", msg))
	} else {
		rec = obj("result", unroot(fx, v))
	}
	s, err := pyjson.Dumps(rec, canonical)
	if err != nil {
		t.Fatalf("正規の JSON にできません: %v", err)
	}
	sum := sha256.Sum256([]byte(s))
	return s, hex.EncodeToString(sum[:])[:oracle.DigestWidth], exc != nil
}

func realTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 本家の使い捨ての dir と同じ前提: 上に .git・.yomiyasu-words・.yomiyasu があると、探索が上まで届いて結果が置き場所で変わる。
	for cur := root; ; cur = filepath.Dir(cur) {
		for _, name := range []string{".git", ".yomiyasu-words", ".yomiyasu"} {
			if _, err := os.Lstat(filepath.Join(cur, name)); err == nil {
				t.Fatalf("テストの作業フォルダ %s の上 (%s) に %s があります。TMPDIR を、上に .git・.yomiyasu-words・.yomiyasu が無い場所に向けてください", root, cur, name)
			}
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return root
}

func loadGolden(t *testing.T) (oracle.VisibilityGolden, oracle.Corpus, []oracle.VisCase) {
	t.Helper()
	var g oracle.VisibilityGolden
	if err := oracle.ReadGolden(goldenDir, oracle.VisibilityFile, &g); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("関数の記録がありません。make golden で作る: %v", err)
		}
		t.Fatal(err)
	}
	corpus, err := oracle.LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	if g.Header.UpstreamCommit != oracle.Commit {
		t.Fatalf("記録の本家の commit %s が固定の %s と違う。make golden", g.Header.UpstreamCommit, oracle.Commit)
	}
	if len(g.Header.Corpus) != len(corpus.Files) {
		t.Fatalf("入力 %d 件に対して記録の入力は %d 件。make golden で作り直す", len(corpus.Files), len(g.Header.Corpus))
	}
	for i, f := range corpus.Files {
		if g.Header.Corpus[i] != f {
			t.Fatalf("入力 %s が記録と違う (記録 %+v、今 %+v)。make golden で作り直す", f.Path, g.Header.Corpus[i], f)
		}
	}
	cases, err := oracle.VisibilityCases(corpus)
	if err != nil {
		t.Fatal(err)
	}
	return g, corpus, cases
}

func hasOp(c oracle.VisCase) bool {
	for _, call := range c.Calls {
		if call.Op != nil {
			return true
		}
	}
	return false
}

type stat struct{ calls, raised int }

// runCase は case を Go の実装で通し、呼び出しごとの digest を返す。fixture を使う case は、作業フォルダを新しく作って chdir する。
func runCase(t *testing.T, c oracle.VisCase, rec oracle.VisCaseRecord, stats map[string]*stat) (mismatches []string) {
	t.Helper()
	e := &env{reg: map[string]any{}}
	if hasOp(c) {
		e.root = realTempDir(t)
		fx, err := oracle.NewFixture(e.root)
		if err != nil {
			t.Fatal(err)
		}
		e.fx = fx
		if err := os.Chdir(e.root); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := fx.Restore(); err != nil {
				t.Fatalf("環境を戻せません: %v", err)
			}
		}()
	}
	kept := map[int]oracle.VisKept{}
	for _, k := range rec.Kept {
		kept[k.Index] = k
	}
	for i, call := range c.Calls {
		var canon, digest string
		var raised bool
		if call.Op != nil {
			if err := e.fx.Apply(context.Background(), *call.Op); err != nil {
				t.Fatalf("case %s の %d 番目の操作 %s が失敗: %v", c.Name, i, call.Op.Kind, err)
			}
			canon, digest, _ = record(t, e.fx, nil, nil)
		} else {
			v, exc := e.invoke(t, call)
			canon, digest, raised = record(t, e.fx, v, exc)
			s := stats[call.Function]
			if s == nil {
				s = &stat{}
				stats[call.Function] = s
			}
			s.calls++
			if raised {
				s.raised++
			}
		}
		want := rec.Digests[i*oracle.DigestWidth : (i+1)*oracle.DigestWidth]
		if digest != want {
			msg := fmt.Sprintf("case %s の %d 番目 (%s): digest %s、記録 %s", c.Name, i, call.Function, digest, want)
			if k, ok := kept[i]; ok {
				expected := string(k.Result)
				if k.Exception != nil {
					expected = fmt.Sprintf("exception %s: %s", k.Exception.Type, k.Exception.Message)
				}
				msg += fmt.Sprintf("\n  Go:   %s\n  記録: %s", clip(canon, 600), clip(expected, 600))
			} else {
				msg += "\n  Go: " + clip(canon, 300)
			}
			mismatches = append(mismatches, msg)
		}
	}
	return mismatches
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func TestVisibilityMatchesPythonRecording(t *testing.T) {
	g, _, cases := loadGolden(t)
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。make golden で作り直す", len(cases), len(g.Cases))
	}
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。make golden で作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
	}
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Errorf("cwd を戻せません: %v", err)
		}
	})
	stats := map[string]*stat{}
	failed := 0
	for i, c := range cases {
		for _, m := range runCase(t, c, g.Cases[i], stats) {
			failed++
			if failed <= 15 {
				t.Error(m)
			}
		}
	}
	if failed > 15 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-15)
	}
	// 関数ごとの件数が記録と同じ。
	for _, f := range g.Functions {
		s := stats[f.Name]
		if s == nil || s.calls != f.Calls || s.raised != f.Raised {
			t.Errorf("%s の呼び出し数・例外の数が記録と違う: Go %+v、記録 %+v", f.Name, s, f)
		}
	}
}

// 空振りを止める: 公開面の関数ごとに記録の件数が 0 でなく、例外を投げうる関数は、例外になる呼び出しと成功する呼び出しの両方が記録にある。
func TestRecordingCoversEveryPublicFunction(t *testing.T) {
	g, _, _ := loadGolden(t)
	byName := map[string]oracle.VisFunctionRecord{}
	for _, f := range g.Functions {
		byName[f.Name] = f
	}
	for _, f := range oracle.VisFunctions {
		rec, ok := byName[f.Name]
		if !ok || rec.Calls == 0 {
			t.Errorf("%s の記録が 0 件 (空振り)", f.Name)
			continue
		}
		if f.Raises && (rec.Raised == 0 || rec.Raised == rec.Calls) {
			t.Errorf("%s は例外になる呼び出しと成功する呼び出しの両方が要る: 呼び出し %d 件のうち例外 %d 件", f.Name, rec.Calls, rec.Raised)
		}
	}
	known := map[string]bool{}
	for _, f := range oracle.VisFunctions {
		known[f.Name] = true
	}
	for _, f := range g.Functions {
		if !known[f.Name] {
			t.Errorf("記録に、公開面の一覧 (oracle.VisFunctions) にない名前 %s がある", f.Name)
		}
	}
	// 記録のどの case も、呼び出しが 1 件以上。操作 (fixture を作る・環境を戻す) の失敗は記録に入らない。記録の作り直し (oracle.RecordVisibility) が case 名と操作の位置を出して止まる。
	for _, c := range g.Cases {
		if c.Calls == 0 {
			t.Errorf("case %s の呼び出しが 0 件", c.Name)
		}
	}
	if !strings.Contains(strings.Join(caseNames(g), "\n"), "corpus/") {
		t.Error("入力の集まりの case が記録に無い")
	}
}

func caseNames(g oracle.VisibilityGolden) []string {
	names := make([]string, len(g.Cases))
	for i, c := range g.Cases {
		names[i] = c.Name
	}
	return names
}
