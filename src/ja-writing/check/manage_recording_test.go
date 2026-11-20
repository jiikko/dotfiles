package check

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

// manageCall は 1078 の hook が呼ぶ本家の関数の呼び出しを、check の公開の関数で動かす。対象でなければ ok が偽。
func (e *interp) manageCall(c oracle.Call) (outcome, bool) {
	e.t.Helper()
	if c.Module != oracle.CheckModule {
		return outcome{}, false
	}
	rt := DefaultRuntime()
	if e.sim != nil {
		rt = e.sim.runtime()
	}
	kw := func(key string) (any, bool) { v, ok := c.Kwargs[key]; return v, ok }
	optional := func(key string) *string {
		if v, ok := kw(key); ok && v != nil {
			s := e.str(v)
			return &s
		}
		return nil
	}
	done := func(v pyjson.Value, err error) outcome {
		if err != nil {
			return pyError(err)
		}
		return outcome{value: v}
	}
	switch c.Function {
	case "armed_entries":
		entries, err := ArmedEntries(e.str(c.Args[0]))
		return done(objects(entries), err), true
	case "armed_update":
		var budget *float64
		if b, ok := c.Args[3].(float64); ok {
			budget = &b
		}
		change := c.Args[2].(oracle.ArmedChange)
		entry, err := ArmedUpdate(context.Background(), rt, e.str(c.Args[0]), e.str(c.Args[1]), func(entry *pyjson.Object) (bool, error) {
			for _, pair := range change.Set {
				steps := pair[0].([]any)
				node := pyjson.Value(entry)
				for _, step := range steps[:len(steps)-1] {
					node = pyops.Index(node, step.(string))
				}
				node.(*pyjson.Object).Set(steps[len(steps)-1].(string), jsonValue(e.t, pair[1]))
			}
			return change.Write, nil
		}, budget)
		return done(recordValue(entry), err), true
	case "start_run":
		o := StartOptions{FinalPath: optional("final_path"), RunID: optional("run_id"), Session: optional("session"), Host: optional("host")}
		if v, ok := kw("by"); ok {
			o.By = e.str(v)
		}
		if v, ok := kw("cwd"); ok {
			o.Cwd = e.str(v)
		}
		if v, ok := kw("default_place"); ok {
			o.DefaultPlace = v.(bool)
		}
		if v, ok := kw("limits"); ok {
			o.Limits = jsonValue(e.t, v).(*pyjson.Object)
		}
		entry, err := StartRun(context.Background(), rt, e.str(c.Args[0]), e.str(c.Args[1]), o)
		return done(recordValue(entry), err), true
	case "verify_quick":
		var data []byte
		if v, ok := kw("final_bytes"); ok {
			data = []byte(v.(oracle.Bytes))
			if data == nil {
				data = []byte{}
			}
		}
		result, err := VerifyQuick(func(name string) (string, bool) { return os.LookupEnv(name) }, e.str(c.Args[0]), optional("final_path"), data,
			optional("run_id"), optional("since"))
		return done(recordValue(result), err), true
	case "run_known_texts":
		texts, paths, err := RunKnownTexts(e.str(c.Args[0]), e.str(c.Args[1]))
		return done([]pyjson.Value{sortedSet(texts), sortedSet(paths)}, err), true
	case "latest_final_path":
		path, err := LatestFinalPath(e.str(c.Args[0]), e.str(c.Args[1]))
		return done(pyjson.OptionalString(path), err), true
	case "text_sha256_of":
		digest, err := TextSHA256Of([]byte(c.Args[0].(oracle.Bytes)))
		return done(digest, err), true
	}
	return outcome{}, false
}

// pyError は公開の関数の誤りを、本家の例外 (型の名前と文) にする。
func pyError(err error) outcome {
	var e *pyops.Exception
	if errors.As(err, &e) {
		return outcome{exc: &oracle.PyException{Type: e.Type, Message: e.Msg}}
	}
	var v *visibility.Error
	if errors.As(err, &v) {
		return outcome{exc: &oracle.PyException{Type: v.Type, Message: v.Message}}
	}
	return pyOSError(err)
}

func objects(items []*pyjson.Object) pyjson.Value {
	out := []pyjson.Value{}
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// sortedSet は集合を driver と同じく元の値の昇順の並びにする。
func sortedSet(set map[string]bool) pyjson.Value {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, pyStrCompare)
	return pyjson.Strings(keys)
}

// jsonValue は case の Go の値を、driver が JSON から読んだ Python の値と同じ pyjson の値にする (dict の鍵は JSON にしたときの順)。
func jsonValue(t *testing.T, v any) pyjson.Value {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	value, err := pyjson.LoadsBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
