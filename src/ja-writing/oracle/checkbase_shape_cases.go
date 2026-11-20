package oracle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// 記録の形の検査 (_record_shape・_fact_gaps_shape・_words_shape) の case。run の書く形の記録 2 つ (推敲と新規作成) を基準にし、
// 欄を 1 か所ずつ別の値に替える・消す・list に要素を足した記録を全部、json.loads で読んでから渡す (鍵の順と NaN を本家と同じに持つため)。

const (
	shapeHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func shapeInput(role string, index int, path string, extra string) string {
	via, p := "file", `"`+path+`"`
	if path == "" {
		via, p = "stdin", "null"
	}
	return fmt.Sprintf(`{"role": %q, "index": %d, "path": %s, "via": %q, "bytes": 3, "sha256": %q, "text_sha256": %q, "bom": false, "eol": "lf", "snapshot": "inputs/%s.bin"%s}`,
		role, index, p, via, shapeHex, shapeHex, shapeHex, extra)
}

func shapeCheck(name string) string {
	return fmt.Sprintf(`{"name": %q, "status": "ran", "exit_code": 0, "duration_ms": 12, "target": {}, "report": "r", "report_sha256": %q, "text": null, "text_sha256": null, "summary": {}, "stderr": "", "error": null}`,
		name, shapeHex)
}

func shapeTools() string {
	var parts []string
	for _, name := range []string{"yomiyasu_check.py", "yomiyasu_lint.py", "yomiyasu_diff.py", "markdown_visibility.py"} {
		parts = append(parts, fmt.Sprintf(`%q: {"version": "3.1.0", "sha256": %q}`, name, shapeHex))
	}
	parts = append(parts, fmt.Sprintf(`"references/lists/hedges.txt": {"version": null, "sha256": %q}`, shapeHex),
		`"references/lists/fact-gaps.txt": {"version": null, "sha256": null}`)
	return "{" + strings.Join(parts, ", ") + "}"
}

// shapeBaseRecords は形の合う記録 (推敲で資料と語の一覧 7 つのキーと fact_gaps がある記録、新規作成で資料を原文とみなした記録)。
func shapeBaseRecords() []string {
	common := `"format": "yomiyasu-check-record", "format_version": 1, "run_id": "20261120T000000Z-abcd1234", "attempt": 2, "fix_count": 1, ` +
		`"previous": {"attempt": 1, "final_sha256": null}, "created_at": "2026-11-20T00:00:00Z", `
	tail := `"cwd": "/w", "host": {"session": null, "codex_thread_id": "t", "armed_by": null}, ` +
		`"skill": {"found": false, "dir": null, "skill_md_sha256": null, "version": null}, "tools": ` + shapeTools() + `, ` +
		`"python": {"version": "3.15.0", "implementation": "CPython", "unicode_version": "17.0.0"}, `
	result := `"result": {"status": "executed", "token": "t", "next": "n", "fail_on_hit": false, "meaning_risk_count": 0, ` +
		`"final_unchanged_during_run": true, "not_judged": ["meaning_preservation"]}, "record_digest": "sha256:x"}`
	rewrite := "{" + common + `"mode": "rewrite", ` + tail +
		`"inputs": {"source": ` + shapeInput("source", 0, "/w/s.md", "") + `, "materials": [` +
		shapeInput("material", 1, "/w/m.md", `, "label": "出典", "treated_as_source": false, "similarity": null, "treated_by": null`) + `], ` +
		`"final": ` + shapeInput("final", 0, "", "") + `}, ` +
		`"options": {"stance": null, "fail_on": ["warn"], "timeout": 90, "doc_type": "pr", "reader": "general", "fact_gaps": true, ` +
		`"words": {"path": "/w/.yomiyasu-words", "allow_lines": 1, "flag_lines": 0, "allow_sha256": "` + shapeHex + `", "flag_sha256": null, ` +
		`"flag_patterns_lines": 0, "flag_patterns_sha256": "` + shapeHex + `"}}, ` +
		`"required_checks": ["lint", "diff"], "checks": [` + shapeCheck("lint") + `, ` + shapeCheck("diff") + `], "materials_summary": {}, ` +
		`"fact_gaps": {"count": 2, "skipped": null, "by_kind": {"degree": 1, "figure": 1}, "items": [` +
		`{"kind": "degree", "line": 1, "sentence": "s", "match": "m", "need": "n"}, {"kind": "figure", "line": 3, "sentence": "t", "match": "m", "need": "n"}]}, ` +
		result
	compose := "{" + common + `"mode": "compose", ` + tail +
		`"inputs": {"source": null, "materials": [` +
		shapeInput("material", 1, "", `, "label": null, "treated_as_source": true, "similarity": 0.6, "treated_by": "similarity"`) + `, ` +
		shapeInput("material", 2, "/w/m.md", `, "label": "b", "treated_as_source": false, "similarity": 0.1, "treated_by": null`) + `], ` +
		`"final": ` + shapeInput("final", 0, "/w/f.md", "") + `}, ` +
		`"options": {"stance": "keep", "fail_on": [], "timeout": 1.5, ` +
		`"words": {"path": "/w/.yomiyasu-words", "allow_lines": 0, "flag_lines": 2, "allow_sha256": null, "flag_sha256": "` + shapeHex + `"}}, ` +
		`"required_checks": [], "checks": [` + shapeCheck("lint") + `, ` + shapeCheck("diff") + `, ` + shapeCheck("materials") + `], "materials_summary": null, ` +
		`"fact_gaps": null, ` + result
	return []string{rewrite, compose}
}

// jraw は JSON の数・真偽をそのままの綴りで持つ (1.0 を float のまま、大きい整数を桁のまま本家に渡すため)。
// oracle は internal/pyjson を import できない (pyjson のテストが oracle を import する) ので、鍵の順を持つ小さな木をここで持つ。
type jraw string

// jobj は鍵の順を持つ JSON の object (keys[i] の値が vals[i])。書き換えは with・without で写しを作る。
type jobj struct {
	keys []string
	vals []any
}

// with は i 番目の値を v に替えた写し。
func (o *jobj) with(i int, v any) *jobj {
	vals := append([]any{}, o.vals...)
	vals[i] = v
	return &jobj{keys: o.keys, vals: vals}
}

// without は i 番目の鍵を除いた写し。
func (o *jobj) without(i int) *jobj {
	return &jobj{keys: append(o.keys[:i:i], o.keys[i+1:]...), vals: append(o.vals[:i:i], o.vals[i+1:]...)}
}

// get は鍵の値 (無ければ nil)。
func (o *jobj) get(k string) any {
	for i, key := range o.keys {
		if key == k {
			return o.vals[i]
		}
	}
	return nil
}

// jparse は JSON の文字列を jobj・[]any・string・jraw・nil の木にする。
func jparse(text string) any {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var walk func() any
	walk = func() any {
		tok, err := dec.Token()
		if err != nil {
			panic(err)
		}
		switch t := tok.(type) {
		case json.Delim:
			if t == '{' {
				o := &jobj{}
				for dec.More() {
					k, err := dec.Token()
					if err != nil {
						panic(err)
					}
					o.keys = append(o.keys, k.(string))
					o.vals = append(o.vals, walk())
				}
				_, _ = dec.Token()
				return o
			}
			list := []any{}
			for dec.More() {
				list = append(list, walk())
			}
			_, _ = dec.Token()
			return list
		case json.Number:
			return jraw(t.String())
		case bool:
			return jraw(strconv.FormatBool(t))
		case string:
			return t
		}
		return nil
	}
	return walk()
}

// jdump は木を JSON の文字列にする (NaN と Infinity は jraw の綴りのまま。本家の json.loads は読める)。
func jdump(v any) string {
	var b bytes.Buffer
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case nil:
			b.WriteString("null")
		case jraw:
			b.WriteString(string(x))
		case string:
			enc, err := json.Marshal(x)
			if err != nil {
				panic(err)
			}
			b.Write(enc)
		case []any:
			b.WriteString("[")
			for i, item := range x {
				if i > 0 {
					b.WriteString(", ")
				}
				walk(item)
			}
			b.WriteString("]")
		case *jobj:
			b.WriteString("{")
			for i, k := range x.keys {
				if i > 0 {
					b.WriteString(", ")
				}
				walk(k)
				b.WriteString(": ")
				walk(x.vals[i])
			}
			b.WriteString("}")
		}
	}
	walk(v)
	return b.String()
}

// shapeAlternatives は欄を替える値。
func shapeAlternatives() []any {
	sub := &jobj{keys: []string{"a"}, vals: []any{jraw("1")}}
	return []any{
		nil, jraw("true"), jraw("false"), jraw("0"), jraw("1"), jraw("2"), jraw("-1"), jraw("123456789012345678901234567890"),
		jraw("0.0"), jraw("1.0"), jraw("1.5"), jraw("-0.5"), jraw("NaN"), jraw("Infinity"),
		"", "x", "a\n", "lint", "diff", "materials", "rewrite", "compose", "file", "stdin", "lf", "executed", "similarity", "degree", "essay",
		"/abs", "rel/x", shapeHex, shapeHex + "\n", strings.ToUpper(shapeHex), "inputs/" + shapeHex + ".bin", "2026-11-20T00:00:00Z\n",
		"x>>>y", "a\rb", strings.Repeat("長", 200), strings.Repeat("長", 201), strings.Repeat("文", 81),
		[]any{}, []any{"lint"}, []any{"lint", "lint"}, []any{[]any{"x"}}, &jobj{}, sub,
	}
}

// shapeMutants は v (dict か list) の中の欄を 1 か所ずつ替えた・消した・足したものの全部 (v そのものは替えない)。
func shapeMutants(v any) []any {
	var out []any
	switch x := v.(type) {
	case *jobj:
		for i := range x.keys {
			out = append(out, x.without(i))
			for _, r := range append(shapeAlternatives(), shapeMutants(x.vals[i])...) {
				out = append(out, x.with(i, r))
			}
		}
	case []any:
		if len(x) > 0 {
			out = append(out, append(append([]any{}, x...), x[len(x)-1]), x[1:])
		}
		for i, child := range x {
			for _, r := range append(shapeAlternatives(), shapeMutants(child)...) {
				c := append([]any{}, x...)
				c[i] = r
				out = append(out, c)
			}
		}
	}
	return out
}

// shapeTexts は基準の記録と、その全部の書き換えの JSON の文字列。
func shapeTexts(base string) []string {
	texts := []string{base}
	for _, m := range shapeMutants(jparse(base)) {
		texts = append(texts, jdump(m))
	}
	return texts
}

func cloads(text, save string) Call {
	return Call{Module: "json", Function: "loads", Args: []any{text}, Save: save, Digest: true, Quiet: true}
}

func checkShapeCases() []VisCase {
	var cases []VisCase
	for i, base := range shapeBaseRecords() {
		var calls []Call
		for _, text := range shapeTexts(base) {
			calls = append(calls, cloads(text, "r"), ccall("_record_shape", Ref("r")))
		}
		cases = append(cases, VisCase{Name: fmt.Sprintf("check/record-shape-%d", i), Calls: calls})
	}
	var calls []Call
	for i, base := range shapeBaseRecords() {
		v := jparse(base).(*jobj)
		options := v.get("options")
		for _, key := range []string{"fact_gaps", "words"} {
			fn := "_fact_gaps_shape"
			var from any = v
			if key == "words" {
				fn, from = "_words_shape", options
			}
			part := from.(*jobj).get(key)
			if part == nil {
				continue
			}
			for _, m := range append([]any{part}, append(shapeAlternatives(), shapeMutants(part)...)...) {
				text := jdump(m)
				name := fmt.Sprintf("p%d", i)
				calls = append(calls, cloads(text, name), ccall(fn, Ref(name)))
			}
		}
	}
	for i, extra := range shapeExtraParts() {
		name := fmt.Sprintf("x%d", i)
		calls = append(calls, cloads(extra[1], name), ccall(extra[0], Ref(name)))
	}
	return append(cases, VisCase{Name: "check/part-shape", Calls: calls})
}

// shapeExtraParts は 1 か所の書き換えでは作れない部分 ([関数, JSON])。鍵の順だけが違う語の一覧と、20 件を超える fact_gaps (items は 20 件まで)。
func shapeExtraParts() [][2]string {
	item := `{"kind": "degree", "line": 1, "sentence": "s", "match": "m", "need": "n"}`
	items := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat(item+", ", n), ", ") + "]" }
	gaps := func(count, n int) string {
		return fmt.Sprintf(`{"count": %d, "skipped": null, "by_kind": {"degree": %d}, "items": %s}`, count, count, items(n))
	}
	h := `"` + shapeHex + `"`
	return [][2]string{
		{"_fact_gaps_shape", gaps(20, 20)}, {"_fact_gaps_shape", gaps(25, 20)}, {"_fact_gaps_shape", gaps(25, 25)}, {"_fact_gaps_shape", gaps(21, 21)},
		{"_fact_gaps_shape", `{"items": [], "by_kind": {}, "skipped": "essay", "count": 0}`},
		{"_words_shape", `{"path": "/w", "allow_lines": 0, "flag_lines": 0, "allow_sha256": ` + h + `, "flag_sha256": null}`},
		{"_words_shape", `{"allow_lines": 0, "path": "/w", "flag_lines": 0, "allow_sha256": ` + h + `, "flag_sha256": null}`},
		{"_words_shape", `{"path": "/w", "allow_lines": 0, "flag_lines": 0, "allow_sha256": null, "flag_sha256": null, "flag_patterns_lines": 0, "flag_patterns_sha256": ` + h + `}`},
		{"_words_shape", `{"path": "/w", "allow_lines": 0, "flag_lines": 0, "allow_sha256": null, "flag_sha256": null, "flag_patterns_sha256": ` + h + `, "flag_patterns_lines": 0}`},
	}
}
