package source

import (
	"bytes"
	"context"
	"testing"
)

// 読む画面では、端末へ stderr を流さない runner に替える (値でもポインタでも)。偽物はそのまま。
func TestQuietDropsTee(t *testing.T) {
	var buf bytes.Buffer
	for name, r := range map[string]Runner{
		"値":    ExecRunner{Bin: "slack", Tee: &buf},
		"ポインタ": &ExecRunner{Bin: "slack", Tee: &buf},
	} {
		q, ok := Quiet(r).(ExecRunner)
		if !ok || q.Tee != nil || q.Bin != "slack" {
			t.Errorf("%s: Quiet = %#v", name, Quiet(r))
		}
	}
}

// 端末へ流す stderr から制御文字を落とす (改行は残す)。
func TestTeeStripsControls(t *testing.T) {
	var buf bytes.Buffer
	r := ExecRunner{Bin: "/bin/sh", Tee: &buf}
	if _, _, err := r.Run(context.Background(), "-c", `printf '警告\033]0;x\007\n次の行\n' >&2`); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "警告\n次の行\n" {
		t.Errorf("端末へ流した文 = %q", got)
	}
}
