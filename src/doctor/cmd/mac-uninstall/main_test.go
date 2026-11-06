package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doctor/exitcode"
)

func TestRunRejectsBadArgsWithoutTouchingAnything(t *testing.T) {
	plan := filepath.Join(t.TempDir(), "plan-x.json")
	if err := os.WriteFile(plan, []byte(`{"id":"x","items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"plan"},                 // -out が無い
		{"apply", "-plan", plan}, // -approve が無い
		{"apply", "-plan", plan, "-approve", "other"}, // ID が違う
	} {
		var out, errb bytes.Buffer
		if rc := run(args, &out, &errb); rc != exitcode.Undiagnosed {
			t.Errorf("%v: rc=%d (want %d) %s", args, rc, exitcode.Undiagnosed, errb.String())
		}
		if strings.Contains(out.String(), "済") {
			t.Errorf("%v: 何かを片付けた: %s", args, out.String())
		}
	}
}
