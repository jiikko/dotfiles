package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// inspect は events (行ごとの値。map は JSON にし、文字列はそのまま) と応答の本文で inspectEvents を通す。
func inspect(t *testing.T, stream []any, response string) (*metadata, string) {
	t.Helper()
	dir := t.TempDir()
	var lines []string
	for _, v := range stream {
		if s, ok := v.(string); ok {
			lines = append(lines, s)
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(b))
	}
	must(t, os.WriteFile(filepath.Join(dir, "events"), []byte(strings.Join(lines, "\n")), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "response"), []byte(response), 0o644))
	md, resp, err := inspectEvents(filepath.Join(dir, "events"), filepath.Join(dir, "response"))
	must(t, err)
	return md, resp
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type obj = map[string]any

func TestCompletionRecordsThreadUsageAndCommands(t *testing.T) {
	md, resp := inspect(t, []any{
		obj{"type": "thread.started", "thread_id": "thread-a"},
		obj{"type": "item.completed", "item": obj{"type": "command_execution", "command": "false", "exit_code": 1}},
		obj{"type": "turn.completed", "usage": obj{"input_tokens": 12, "cached_input_tokens": 8, "output_tokens": 3}},
	}, "A failing test was found")
	if !md.Complete || string(md.ThreadID) != `"thread-a"` || string(md.Commands[0].ExitCode) != "1" || resp != "A failing test was found" {
		t.Errorf("got %+v %q", md, resp)
	}
	var usage map[string]int
	must(t, json.Unmarshal(md.Usage[0], &usage))
	if usage["cached_input_tokens"] != 8 {
		t.Errorf("usage が違う: %v", usage)
	}
}

func TestEventMessageCanSupplyMissingReviewOutput(t *testing.T) {
	md, resp := inspect(t, []any{obj{"type": "item.completed", "item": obj{"type": "agent_message", "text": "finding"}}, obj{"type": "turn.completed"}}, "")
	// usage の無い turn.completed は {} を記録する (Python 版の event.get("usage", {}))
	if !md.Complete || resp != "finding" || md.ResponseSource == nil || *md.ResponseSource != "agent_message_event" || len(md.Usage) != 1 || string(md.Usage[0]) != "{}" {
		t.Errorf("got %+v %q", md, resp)
	}
}

// text が null の agent_message は本文の補いに使わない (1 つ前の文字列の本文で補う。Go は null を空の string に読むので、型を見て除く)
func TestNullMessageTextIsSkipped(t *testing.T) {
	md, resp := inspect(t, []any{obj{"type": "item.completed", "item": obj{"type": "agent_message", "text": "hi"}},
		obj{"type": "item.completed", "item": obj{"type": "agent_message", "text": nil}}, obj{"type": "turn.completed"}}, "")
	if !md.Complete || resp != "hi" {
		t.Errorf("complete=%v 本文=%q (want true, hi)", md.Complete, resp)
	}
}

func TestIncompleteFailureEmptyAndMalformedAreNotSuccess(t *testing.T) {
	done := obj{"type": "turn.completed"}
	for name, tc := range map[string]struct {
		stream   []any
		response string
	}{
		"空":           {nil, "partial reply"},
		"応答が無い":       {[]any{done}, ""},
		"turn.failed": {[]any{obj{"type": "turn.failed"}, done}, "reply"},
		"error":       {[]any{obj{"type": "error"}, done}, "reply"},
		"壊れた JSON":    {[]any{"broken JSON", done}, "reply"},
		"item が配列":    {[]any{obj{"type": "item.completed", "item": []any{}}, done}, "reply"},
		"完了の後に始まった":   {[]any{done, obj{"type": "turn.started"}}, "old reply"},
		"オブジェクトでない行":  {[]any{"[1]", done}, "reply"},
	} {
		if md, _ := inspect(t, tc.stream, tc.response); md.Complete {
			t.Errorf("%s: 完了と判定した", name)
		}
	}
}

// Python 版の str.splitlines / str.strip / str(Path) と同じ区切り方 (行番号とログの表記を Python 版とそろえる。issue 670)
func TestPythonCompat(t *testing.T) {
	if got := pySplitlines("a\vb\fc\x1cd\u0085e\u2028f\u2029g\nh\n"); !slices.Equal(got, []string{"a", "b", "c", "d", "e", "f", "g", "h"}) {
		t.Errorf("pySplitlines = %q", got)
	}
	if got := pySplitlines(""); len(got) != 0 {
		t.Errorf("空の splitlines = %q", got)
	}
	if got := pyStrip(" \t\x1f 　x\x1c"); got != "x" {
		t.Errorf("pyStrip = %q", got)
	}
	for in, want := range map[string]string{"meta.json": "meta.json", "./a//b/./c/": "a/b/c", "//a": "//a", "///a": "/a", "": ".", ".": ".", "/": "/", "a/../b": "a/../b"} {
		if got := pyPathStr(in); got != want {
			t.Errorf("pyPathStr(%q) = %q, want %q", in, got, want)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	must(t, os.WriteFile(p, []byte("a\r\nb\rc"), 0o644))
	if got, err := readText(p); err != nil || got != "a\nb\nc" {
		t.Errorf("readText = %q %v (改行を \\n にそろえる)", got, err)
	}
	must(t, os.WriteFile(p, []byte{0xff}, 0o644))
	if _, err := readText(p); err == nil {
		t.Error("UTF-8 でないのに読めた")
	}
}

// 入口: meta.json とログの書き方・rc・読めない入力・is-object
func TestInspectMain(t *testing.T) {
	dir := t.TempDir()
	at := func(n string) string { return filepath.Join(dir, n) }
	must(t, os.WriteFile(at("ev"), []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"<b>"}}`+"\n"+`{"type":"turn.completed"}`), 0o644))
	must(t, os.WriteFile(at("log"), []byte("pre\n"), 0o644))
	var stderr strings.Builder
	if rc := run([]string{"inspect", at("ev"), at("missing"), at("meta.json"), at("log")}, &stderr, &stderr); rc != 0 {
		t.Fatalf("rc=%d %s", rc, stderr.String())
	}
	if b, _ := os.ReadFile(at("log")); string(b) != "pre\n\n--- Codex JSONL result ---\n<b>\n" {
		t.Errorf("log = %q", b)
	}
	if b, _ := os.ReadFile(at("meta.json")); !strings.HasPrefix(string(b), "{\n  \"thread_id\": null,\n") || !strings.HasSuffix(string(b), "\"complete\": true\n}\n") {
		t.Errorf("meta.json の形が違う: %s", b)
	}
	// 読めない events は read_error の meta と rc=1、ログには未完了の案内
	must(t, os.WriteFile(at("log"), nil, 0o644))
	if rc := run([]string{"inspect", at("none"), at("missing"), at("m2.json"), at("log")}, &stderr, &stderr); rc != 1 {
		t.Errorf("読めない events で rc=%d", rc)
	}
	var m map[string]any
	b, _ := os.ReadFile(at("m2.json"))
	must(t, json.Unmarshal(b, &m))
	if m["complete"] != false || m["read_error"] == nil || len(m) != 2 {
		t.Errorf("read_error の meta が違う: %s", b)
	}
	if b, _ := os.ReadFile(at("log")); !strings.HasSuffix(string(b), "Incomplete JSONL run; see "+pyPathStr(at("m2.json"))+"\n") {
		t.Errorf("未完了の案内が無い: %q", b)
	}
	for body, want := range map[string]int{`{"type":"object"}`: 0, `[]`: 1, `nope`: 1, "{\"a\":\"\xff\"}": 1} {
		must(t, os.WriteFile(at("s.json"), []byte(body), 0o644))
		if rc := run([]string{"is-object", at("s.json")}, &stderr, &stderr); rc != want {
			t.Errorf("is-object %s: rc=%d want %d", body, rc, want)
		}
	}
	if rc := run([]string{"inspect", "a"}, &stderr, &stderr); rc != 2 {
		t.Errorf("引数の誤りで rc=%d", rc)
	}
}
