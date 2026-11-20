package check

import (
	"bytes"
	"encoding/base64"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
)

// 入力の読みの呼び出しを、本家の driver の objects と同じ形の値にする (_Input は record・label・text、_Target は as_json、bytes は bytes_b64)。

func (e *interp) inputValue(in *Input) pyjson.Value {
	e.t.Helper()
	if in == nil {
		return nil
	}
	record, err := in.Record()
	if err != nil {
		e.t.Fatal(err)
	}
	o := pyjson.NewObject()
	o.Set("record", record)
	o.Set("label", pyjson.OptionalString(in.Label))
	o.Set("text", in.Text)
	return o
}

func targetValue(t *Target) pyjson.Value {
	if t == nil {
		return nil
	}
	return t.JSON()
}

func bytesValue(b []byte) pyjson.Value {
	o := pyjson.NewObject()
	o.Set("bytes_b64", base64.StdEncoding.EncodeToString(b))
	return o
}

// stdinOp は op stdin を、このテストの標準入力の代わりに持つ (Name が "none" なら標準入力が無い)。
func (e *interp) stdinOp(op oracle.Op) {
	e.t.Helper()
	if op.Name == "none" {
		e.stdin = nil
		return
	}
	data, err := e.fx.WriteData(oracle.Op{Content: op.Content, BytesB64: op.BytesB64})
	if err != nil {
		e.t.Fatal(err)
	}
	e.stdin = bytes.NewReader(data)
}

func (e *interp) optBytes(a any) []byte {
	if a == nil {
		return nil
	}
	return []byte(a.(oracle.Bytes))
}

func (e *interp) namespace(a any) InputArgs {
	e.t.Helper()
	n, ok := a.(oracle.Namespace)
	if !ok {
		e.t.Fatalf("Namespace でない引数 %T", a)
	}
	args := InputArgs{Stdin: n["stdin"].(bool), Final: e.sub(n["final"].([]string)[0])}
	if source, ok := n["source"].([]string); ok {
		s := e.sub(source[0])
		args.Source = &s
	}
	if materials, ok := n["materials"].([]string); ok {
		for _, m := range materials {
			args.Materials = append(args.Materials, e.sub(m))
		}
	}
	return args
}

// inputCall は入力の読みの呼び出し。ok が偽なら、この関数たちの名前でない。
func (e *interp) inputCall(c oracle.Call) (out outcome, ok bool) {
	e.t.Helper()
	a := c.Args
	switch c.Function {
	case "_read_stdin_bytes":
		data, problem := ReadStdinBytes(e.stdin)
		if problem != "" {
			return outcome{value: pair(nil, problem)}, true
		}
		return outcome{value: pair(bytesValue(data), nil)}, true
	case "_read_named":
		item, problem, err := readNamed(e.str(a[0]), a[1].(int), e.str(a[2]), e.optBytes(a[3]))
		if err != nil {
			return pathError(err), true
		}
		return outcome{value: pair(e.inputValue(item), nilIfEmpty(problem))}, true
	case "_read_target":
		target, problem, err := ReadTarget(e.str(a[0]), e.optBytes(a[1]))
		if err != nil {
			return pathError(err), true
		}
		return outcome{value: pair(targetValue(target), nilIfEmpty(problem))}, true
	case "_path_problem":
		var paths []*string
		for _, p := range a[1].([]any) {
			paths = append(paths, e.optStr(p))
		}
		problem, err := PathProblem(e.str(a[0]), paths)
		if err != nil {
			return pathError(err), true
		}
		return outcome{value: nilIfEmpty(problem)}, true
	case "_read_inputs":
		inputs, problem, err := ReadInputs(e.namespace(a[0]), e.stdin)
		if err != nil {
			return pathError(err), true
		}
		if problem != "" {
			return outcome{value: pair(nil, problem)}, true
		}
		materials := make([]pyjson.Value, len(inputs.Materials))
		for i, m := range inputs.Materials {
			materials[i] = e.inputValue(m)
		}
		return outcome{value: pair([]pyjson.Value{e.inputValue(inputs.Source), materials, e.inputValue(inputs.Final)}, nil)}, true
	case "_split_stdin":
		parts, problem := splitStdin([]byte(a[0].(oracle.Bytes)))
		if problem != "" {
			return outcome{value: pair(nil, problem)}, true
		}
		rows := make([]pyjson.Value, len(parts))
		for i, p := range parts {
			rows[i] = []pyjson.Value{p.role, bytesValue(p.data), pyjson.OptionalString(p.label)}
		}
		return outcome{value: pair(rows, nil)}, true
	case "_material_marker":
		label, found := materialMarker([]byte(a[0].(oracle.Bytes)))
		if !found {
			return outcome{value: nil}, true
		}
		return outcome{value: []pyjson.Value{pyjson.OptionalString(label)}}, true
	}
	return outcome{}, false
}
