package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
	"ja-writing/visibility"
)

const goldenDir = "../testdata/golden"

// outcome は呼び出し 1 つの結果か例外。
type outcome struct {
	value pyjson.Value
	exc   *oracle.PyException
}

// unserializable は JSON にできない値 (datetime、または datetime を含む tuple) を返した呼び出しの、本家の driver が記録する例外 (型の名前と、default が投げた TypeError の文)。
func unserializable(typeName string) outcome {
	return outcome{exc: &oracle.PyException{Type: "Unserializable", Message: typeName + ": datetime"}}
}

type interp struct {
	t     *testing.T
	fx    *oracle.Fixture
	saved map[string]any
	files ToolFiles
}

func (e *interp) sub(s string) string {
	if e.fx == nil {
		return s
	}
	return e.fx.Sub(s)
}

func (e *interp) str(a any) string {
	e.t.Helper()
	switch v := a.(type) {
	case string:
		return e.sub(v)
	case oracle.Cps:
		return pyunicode.FromRunes(v)
	}
	e.t.Fatalf("文字列でない引数 %T", a)
	return ""
}

// optStr は nil か文字列の引数。
func (e *interp) optStr(a any) *string {
	e.t.Helper()
	if a == nil {
		return nil
	}
	s := e.str(a)
	return &s
}

func optValue(s string, ok bool) pyjson.Value {
	if ok {
		return s
	}
	return nil
}

// ref は Ref か {"$ref", "path"} が指す、覚えた値。
func (e *interp) ref(a any) any {
	e.t.Helper()
	switch v := a.(type) {
	case oracle.Ref:
		return e.saved[string(v)]
	case map[string]any:
		picked := e.saved[v["$ref"].(string)]
		for _, step := range v["path"].([]any) {
			picked = picked.([]any)[step.(int)]
		}
		return picked
	}
	e.t.Fatalf("参照でない引数 %T", a)
	return nil
}

func pair(a, b pyjson.Value) pyjson.Value { return []pyjson.Value{a, b} }

func nilIfEmpty(s string) pyjson.Value { return optValue(s, s != "") }

func (e *interp) call(c oracle.Call) outcome {
	e.t.Helper()
	switch {
	case c.Module == "builtins":
		return e.builtin(c)
	case c.Get:
		return outcome{value: e.constant(c.Function)}
	}
	a := c.Args
	switch c.Function {
	case "_sha256":
		return outcome{value: SHA256([]byte(a[0].(oracle.Bytes)))}
	case "_text_sha256":
		return outcome{value: TextSHA256(e.str(a[0]))}
	case "_parse_iso":
		s, isStr := a[0].(string)
		if !isStr {
			e.save(c, nil)
			return outcome{}
		}
		moment, ok := ParseISO(s)
		if !ok {
			e.save(c, nil)
			return outcome{}
		}
		e.save(c, moment)
		return unserializable("datetime")
	case "_iso":
		return outcome{value: ISO(e.ref(a[0]).(time.Time))}
	case "_stamp":
		return outcome{value: Stamp(e.ref(a[0]).(time.Time))}
	case "_since":
		moment, problem := Since(e.optStr(a[0]))
		if moment != nil {
			e.save(c, []any{*moment, nil})
			return unserializable("tuple")
		}
		e.save(c, []any{nil, problem})
		return outcome{value: pair(nil, nilIfEmpty(problem))}
	case "_run_id_problem":
		return outcome{value: nilIfEmpty(RunIDProblem(e.str(a[0])))}
	case "_run_id_case_problem":
		return outcome{value: nilIfEmpty(RunIDCaseProblem(e.str(a[0]), e.str(a[1])))}
	case "_record_digest":
		v, err := pyjson.Loads(string(a[0].(json.RawMessage)))
		if err != nil {
			e.t.Fatal(err)
		}
		digest, err := RecordDigest(v.(*pyjson.Object))
		if err != nil {
			e.t.Fatal(err)
		}
		return outcome{value: digest}
	case "_status_line":
		return outcome{value: StatusLine(e.status(c))}
	case "_host_value":
		out := HostValue(e.optStr(a[0]))
		if out == nil {
			return outcome{}
		}
		return outcome{value: *out}
	case "_fail_on":
		found, problem := FailOn(a[0].([]string))
		var list pyjson.Value
		if problem == "" {
			list = pyjson.Strings(found)
		}
		return outcome{value: pair(list, nilIfEmpty(problem))}
	case "_timeout":
		value, problem := Timeout(e.optStr(a[0]))
		return outcome{value: pair(value, nilIfEmpty(problem))}
	case "_compact":
		return outcome{value: Compact(e.str(a[0]))}
	case "_similarity":
		return outcome{value: Similarity(e.str(a[0]), e.str(a[1]))}
	case "_source_like":
		var texts []string
		for _, x := range a[0].([]any) {
			texts = append(texts, e.str(x))
		}
		best, values := SourceLike(texts, e.str(a[1]), a[2].(bool))
		floats := make([]pyjson.Value, len(values))
		for i, v := range values {
			floats[i] = v
		}
		var index pyjson.Value
		if best != 0 {
			index = int64(best)
		}
		return outcome{value: pair(index, floats)}
	case "_tool_versions":
		factGaps := len(a) > 0 && a[0].(bool)
		return outcome{value: ToolVersions(e.files, factGaps)}
	case "_list_sha256":
		v, err := pyjson.Loads(string(a[0].(json.RawMessage)))
		if err != nil {
			e.t.Fatal(err)
		}
		return outcome{value: ListSHA256(v.(*pyjson.Object), e.str(a[1]))}
	case "_skill_info":
		return outcome{value: SkillInfo()}
	}
	return e.pathCall(c)
}

func (e *interp) save(c oracle.Call, v any) {
	if c.Save != "" {
		e.saved[c.Save] = v
	}
}

func (e *interp) status(c oracle.Call) Status {
	e.t.Helper()
	s := Status{State: e.str(c.Args[0]), Via: e.str(c.Args[1])}
	if len(c.Args) > 2 {
		s.RunID = e.str(c.Args[2])
	}
	if len(c.Args) > 3 {
		s.Attempt = c.Args[3].(int)
	}
	if len(c.Args) > 4 {
		s.Final = e.str(c.Args[4])
	}
	text := func(v any) *string {
		switch x := v.(type) {
		case nil:
			return nil
		case int:
			t := strconv.Itoa(x)
			return &t
		}
		t := e.str(v)
		return &t
	}
	for key, v := range c.Kwargs {
		switch key {
		case "run_id":
			if p := text(v); p != nil {
				s.RunID = *p
			}
		case "attempt":
			if v != nil {
				s.Attempt = v.(int)
			}
		case "final":
			if p := text(v); p != nil {
				s.Final = *p
			}
		case "expected":
			s.Expected = text(v)
		case "mode":
			s.Mode = text(v)
		case "doc_type":
			s.DocType = text(v)
		case "reader":
			s.Reader = text(v)
		case "checks":
			s.Checks = text(v)
		case "meaning_risk":
			s.MeaningRisk = text(v)
		case "replay":
			s.Replay = text(v)
		case "failed":
			s.Failed = text(v)
		case "reason":
			s.Reason = text(v)
		default:
			e.t.Fatalf("知らない引数 %s", key)
		}
	}
	return s
}

func (e *interp) constant(name string) pyjson.Value {
	switch name {
	case "_DEFAULT_TIMEOUT":
		return int64(DefaultTimeout)
	case "_SIMILAR_TO_SOURCE":
		return SimilarToSource
	case "_TOOL_NAMES":
		return pyjson.Strings(ToolNames)
	case "_LIST_NAMES":
		return pyjson.Strings(ListNames)
	case "_FACT_GAPS_KEY":
		return FactGapsKey
	case "_GITIGNORE":
		return GitignoreText
	case "_HERE":
		return nil // 覚えるだけ (結果は quiet で比べない)
	}
	e.t.Fatalf("Go に持っていない定数 %s", name)
	return nil
}

// builtin は _HERE を差し替える呼び出し (builtins.__import__・setattr)。_HERE の Get は差し替える前の値を覚えるだけ。
func (e *interp) builtin(c oracle.Call) outcome {
	switch c.Function {
	case "__import__":
		return outcome{}
	case "setattr":
		switch v := c.Args[2].(type) {
		case string:
			e.files = DirFiles{Scripts: e.sub(v)}
		case oracle.Ref:
			e.files = Released{}
		default:
			e.t.Fatalf("setattr の値 %T", v)
		}
		return outcome{}
	}
	e.t.Fatalf("Go に持っていない builtins.%s", c.Function)
	return outcome{}
}

func (e *interp) pathCall(c oracle.Call) outcome {
	e.t.Helper()
	a := c.Args
	wrap := func(v string, err error) outcome {
		if err != nil {
			return pathError(err)
		}
		return outcome{value: v}
	}
	boolean := func(v bool, err error) outcome {
		if err != nil {
			return pathError(err)
		}
		return outcome{value: v}
	}
	switch c.Function {
	case "_getcwd":
		return wrap(Getcwd())
	case "_abspath":
		return wrap(AbsPath(e.str(a[0])))
	case "_disk_path":
		return wrap(DiskPath(e.str(a[0])))
	case "_case_insensitive":
		return outcome{value: CaseInsensitive(e.str(a[0]))}
	case "_place":
		return wrap(Place(e.str(a[0])))
	case "_same_path":
		return boolean(SamePath(e.str(a[0]), e.str(a[1])))
	case "_same_file":
		return boolean(SameFile(e.str(a[0]), e.str(a[1])))
	case "_home_path":
		return outcome{value: HomePath(e.str(a[0]))}
	case "state_dir_for":
		return wrap(StateDirFor(e.str(a[0]), optionArg(e, a[1])))
	case "_state_dir":
		return wrap(StateDir(optionArg(e, a[0])))
	case "_state_dir_given":
		return outcome{value: StateDirGiven(optionArg(e, a[0]))}
	}
	e.t.Fatalf("Go に持っていない関数 %s", c.Function)
	return outcome{}
}

// optionArg は --state-dir の値 (None は空文字列。本家は「if option:」で両方を同じに扱う)。
func optionArg(e *interp, a any) string {
	if a == nil {
		return ""
	}
	return e.str(a)
}

func pathError(err error) outcome {
	var gone *NoWorkingDirectoryError
	var vis *visibility.Error
	switch {
	case errors.As(err, &gone):
		return outcome{exc: &oracle.PyException{Type: "_NoWorkingDirectory", Message: gone.Message}}
	case errors.As(err, &vis):
		return outcome{exc: &oracle.PyException{Type: vis.Type, Message: vis.Message}}
	}
	return outcome{exc: &oracle.PyException{Type: "?", Message: err.Error()}}
}

// unrootValue は結果の文字列の中の作業フォルダを RootToken に戻す (folded なら、NFC にして casefold した作業フォルダを戻す)。
func unrootValue(root string, folded bool, v pyjson.Value) pyjson.Value {
	needle := root
	if folded {
		needle = pyunicode.Casefold(pyunicode.Normalize(pyunicode.NFC, root))
	}
	var walk func(v pyjson.Value) pyjson.Value
	walk = func(v pyjson.Value) pyjson.Value {
		switch x := v.(type) {
		case string:
			return strings.ReplaceAll(x, needle, oracle.RootToken)
		case []pyjson.Value:
			out := make([]pyjson.Value, len(x))
			for i, el := range x {
				out[i] = walk(el)
			}
			return out
		case *pyjson.Object:
			out := pyjson.NewObject()
			for _, k := range x.Keys() {
				val, _ := x.Get(k)
				out.Set(k, walk(val))
			}
			return out
		}
		return v
	}
	return walk(v)
}

func (e *interp) digest(c oracle.Call, out outcome) (canon, digest string, raised bool) {
	e.t.Helper()
	var err error
	if out.exc != nil {
		msg := out.exc.Message
		if e.fx != nil {
			msg = e.fx.Unroot(msg)
		}
		canon, digest, err = replay.ExceptionDigest(out.exc.Type, msg)
		raised = true
	} else {
		v := out.value
		if e.fx != nil {
			v = unrootValue(e.fx.Root, c.Folded, v)
		}
		canon, digest, err = replay.ResultDigest(replay.SortKeys(v))
	}
	if err != nil {
		e.t.Fatal(err)
	}
	return canon, digest, raised
}

// fixtureRoot は新しい作業フォルダの実パスにして、そこに移る (macOS の syscall.Getwd は実パスを返す。テストが終わると cwd を戻す)。
func fixtureRoot(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	root, err := syscall.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// finish は fixture が変えた環境変数と cwd を戻す。
func (e *interp) finish() {
	if err := e.fx.Restore(); err != nil {
		e.t.Fatalf("環境を戻せません: %v", err)
	}
}

func hasOp(c oracle.VisCase) bool {
	return slices.ContainsFunc(c.Calls, func(call oracle.Call) bool { return call.Op != nil })
}

type stat struct{ calls, raised int }

func clip(s string, n int) string {
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

func runCase(t *testing.T, c oracle.VisCase, rec oracle.VisCaseRecord, stats map[string]*stat) (mismatches []string) {
	t.Helper()
	e := &interp{t: t, saved: map[string]any{}, files: Released{}}
	if hasOp(c) {
		fx, err := oracle.NewFixture(fixtureRoot(t))
		if err != nil {
			t.Fatal(err)
		}
		e.fx = fx
		defer e.finish()
	}
	kept := map[int]oracle.VisKept{}
	for _, k := range rec.Kept {
		kept[k.Index] = k
	}
	for i, call := range c.Calls {
		var canon, digest string
		if call.Op != nil {
			if err := e.fx.Apply(context.Background(), *call.Op); err != nil {
				t.Fatalf("case %s の %d 番目の操作 %s が失敗: %v", c.Name, i, call.Op.Kind, err)
			}
			canon, digest, _ = e.digest(call, outcome{})
		} else {
			out := e.call(call)
			var raised bool
			canon, digest, raised = e.digest(call, out)
			if call.Quiet {
				// 覚えるだけの呼び出し (結果の digest は 8 つの 0)
				canon, digest = "(quiet)", strings.Repeat("0", oracle.DigestWidth)
			}
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
			msg := fmt.Sprintf("case %s の %d 番目 (%s %.80v): digest %s、記録 %s", c.Name, i, call.Function, call.Args, digest, want)
			if k, ok := kept[i]; ok {
				expected := string(k.Result)
				if k.Exception != nil {
					expected = fmt.Sprintf("exception %s: %s", k.Exception.Type, k.Exception.Message)
				}
				msg += fmt.Sprintf("\n  Go:   %s\n  記録: %s", clip(canon, 700), clip(expected, 700))
			} else {
				msg += "\n  Go: " + clip(canon, 300)
			}
			mismatches = append(mismatches, msg)
		}
	}
	return mismatches
}

func loadBase(t *testing.T) (oracle.CheckBaseGolden, []oracle.VisCase) {
	t.Helper()
	var g oracle.CheckBaseGolden
	if err := oracle.ReadGolden(goldenDir, oracle.CheckBaseFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	cases := oracle.CheckBaseCases()
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。make golden で作り直す", len(cases), len(g.Cases))
	}
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。make golden で作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
	}
	return g, cases
}

func TestBaseMatchesPythonRecording(t *testing.T) {
	g, cases := loadBase(t)
	stats := map[string]*stat{}
	failed, total := 0, 0
	for i, c := range cases {
		for _, m := range runCase(t, c, g.Cases[i], stats) {
			failed++
			if failed <= 15 {
				t.Error(m)
			}
		}
		total += len(c.Calls)
	}
	if failed > 15 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-15)
	}
	for _, f := range g.Functions {
		s := stats[f.Name]
		if s == nil || s.calls != f.Calls || s.raised != f.Raised {
			t.Errorf("%s の呼び出し数・例外の数が記録と違う: Go %+v、記録 %+v", f.Name, s, f)
		}
	}
	t.Logf("check の部品の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}

// 空振りを止める: 関数ごとに記録の件数が 0 でなく、記録に一覧にない名前が無く、成功と失敗の両方を持つ関数は両方が記録にある。
func TestBaseRecordingCoversEveryFunction(t *testing.T) {
	g, _ := loadBase(t)
	byName := map[string]oracle.VisFunctionRecord{}
	for _, f := range g.Functions {
		byName[f.Name] = f
	}
	known := map[string]bool{"_HERE": true, "__import__": true, "setattr": true}
	for _, name := range append(slices.Clone(oracle.CheckBaseFunctions), oracle.CheckBaseConstants...) {
		known[name] = true
		if byName[name].Calls == 0 {
			t.Errorf("%s の記録が 0 件 (空振り)", name)
		}
	}
	for _, f := range g.Functions {
		if !known[f.Name] {
			t.Errorf("記録に、一覧 (oracle.CheckBaseFunctions) にない名前 %s がある", f.Name)
		}
	}
	// 例外になる呼び出しと、ならない呼び出しの両方が要る関数 (作業ディレクトリを読む関数と、時刻を作る関数)
	for _, name := range []string{"_parse_iso", "_since", "_abspath", "_state_dir", "state_dir_for", "_getcwd", "_same_path"} {
		if f := byName[name]; f.Raised == 0 || (name != "_getcwd" && f.Raised == f.Calls) {
			t.Errorf("%s は例外になる呼び出しと成功する呼び出しの両方が要る: 呼び出し %d 件のうち例外 %d 件", name, f.Calls, f.Raised)
		}
	}
}

// 埋め込んだ道具と一覧の SHA-256 が、固定した本家の commit の upstream.sha256 と同じ (記録の tools の欄が答えを合わせる相手)。
func TestReleasedToolSHA256MatchUpstreamManifest(t *testing.T) {
	manifest, err := oracle.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range ToolNames {
		if got, want := releasedToolSHA256[name], manifest[oracle.ScriptsDir+"/"+name]; got == "" || got != want {
			t.Errorf("%s の SHA-256 %q、upstream.sha256 は %q", name, got, want)
		}
	}
	for _, name := range append(slices.Clone(ListNames), FactGapsList) {
		if got, want := releasedListSHA256[name], manifest[oracle.ListsDir+"/"+name+".txt"]; got == "" || got != want {
			t.Errorf("一覧 %s の SHA-256 %q、upstream.sha256 は %q", name, got, want)
		}
	}
	if len(releasedToolSHA256) != len(ToolNames) || len(releasedListSHA256) != len(ListNames)+1 {
		t.Errorf("定数の数: 道具 %d・一覧 %d", len(releasedToolSHA256), len(releasedListSHA256))
	}
}
