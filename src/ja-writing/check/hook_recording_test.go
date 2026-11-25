package check

import (
	"encoding/base64"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyshlex"
	"ja-writing/oracle"
)

// hookCall は本家の hook の中の関数と shlex の呼び出しを、Go の写しで動かす。対象でなければ ok が偽。
func (e *interp) hookCall(c oracle.Call) (outcome, bool) {
	e.t.Helper()
	switch c.Module {
	case "shlex":
		switch c.Function {
		case "split":
			words, err := pyshlex.Split(e.str(c.Args[0]))
			if err != nil {
				return pyError(err), true
			}
			return outcome{value: pyjson.Strings(words)}, true
		case "quote":
			return outcome{value: pyshlex.Quote(e.str(c.Args[0]))}, true
		case "join":
			return outcome{value: pyshlex.Join(c.Args[0].([]string))}, true
		}
	case oracle.HookModule:
		h := &hookRun{}
		switch c.Function {
		case "_shell_segments":
			return outcome{value: pyjson.Strings(shellSegments(e.str(c.Args[0])))}, true
		case "_bash_command":
			return outcome{value: bashCommand(jsonValue(e.t, c.Args[0]))}, true
		case "_paths":
			h.cwd = e.str(c.Args[1])
			return outcome{value: pyjson.Strings(h.paths(e.str(c.Args[0])))}, true
		case "_bodies":
			out := []pyjson.Value{}
			for _, b := range hookBodies(jsonValue(e.t, c.Args[0]).(*pyjson.Object), e.str(c.Args[1])) {
				var body pyjson.Value
				if b.body != nil {
					o := pyjson.NewObject()
					o.Set("bytes_b64", base64.StdEncoding.EncodeToString(b.body))
					body = o
				}
				out = append(out, []pyjson.Value{body, pyjson.OptionalString(b.path), b.command})
			}
			return outcome{value: out}, true
		case "_invocation":
			got := hookInvocation(jsonValue(e.t, c.Args[0]).(*pyjson.Object), e.str(c.Args[1]), e.str(c.Args[2]))
			return outcome{value: pyjson.OptionalString(got)}, true
		}
	}
	return outcome{}, false
}
