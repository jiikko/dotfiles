package jsonout

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteAndLineKeepAngleBracketsAndAmpersands(t *testing.T) {
	v := map[string]string{"sig": "f(_ a: Array<Int>) -> Bool", "body": "a && b"}
	var w bytes.Buffer
	if err := Write(&w, v); err != nil {
		t.Fatal(err)
	}
	line, err := Line(v)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{"Write": w.String(), "Line": string(line)} {
		for _, want := range []string{"Array<Int>", "-> Bool", "a && b"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q を含まない: %s", name, want, got)
			}
		}
		var back map[string]string
		if err := json.Unmarshal([]byte(got), &back); err != nil || back["sig"] != v["sig"] || back["body"] != v["body"] {
			t.Errorf("%s: 読み戻した値が違う: %v err=%v", name, back, err)
		}
	}
	if !strings.HasSuffix(w.String(), "}\n") || !strings.Contains(w.String(), "\n  \"") {
		t.Errorf("Write は字下げして末尾に改行を 1 つ付ける: %q", w.String())
	}
	if strings.Contains(string(line), "\n") {
		t.Errorf("Line は改行を含まない: %q", line)
	}
}
