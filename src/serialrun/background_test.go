package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBackgroundDenialStopsLongVerificationBehindBareAmpersand(t *testing.T) {
	cases := []string{
		"make test > log 2>&1 &",
		"make test&",
		"( make -C src/dupscan lint test > /x/log 2>&1; echo $? > /x/rc ) &",
		"(cd src/x && go test ./... > log 2>&1) &",
		"{ make test > log 2>&1; echo $? > rc; } &",
		"make test && echo ok > done &",
		"make lint || echo ng &",
		"make test 2>&1 | tee log &",
		"echo start; make test > log &",
		"cd a\nmake test > log &\necho next",
		"for m in a b; do make -C $m test; done > log 2>&1 &",
		"if make test; then echo ok; fi &",
		"while true; do go test ./...; done &",
		"( ( make test ) ) &",
		"bin/mutate-verify --file x > log &",
		"mutate-verify-list list.sh &",
		"xcodebuild -scheme X test > log &",
		"swift test &",
		"serial-run -- make test > log &",
		"bin/skill-eval dupscan > log 2>&1 &",
		"bash tests/claude/test_x.sh > log &",
		"tests/claude/test_x.sh &",
		"bin/runtimeout 600 make test > log &",
		"env FOO=1 go test ./... &",
		"make test &>/dev/null &",
		"make test >&2 &",
		"( make test 2>&1 ) & wait",
	}
	for _, c := range cases {
		if backgroundDenial(c) == "" {
			t.Errorf("止めるべき形を通した: %q", c)
		}
	}
}

func TestBackgroundDenialLeavesOtherFormsAlone(t *testing.T) {
	cases := []string{
		"make test > log 2>&1",
		"make test && git push",
		"make test 2>&1 | tail -5",
		"make test |& tee log",
		"make test &>/dev/null",
		"make test > log 2>&1; sleep 1 &",
		"make test\nsleep 5 &",
		"( make test ); sleep 1 &",
		"{ make test; }; sleep 1 &",
		"sleep 10 &",
		"python3 -m http.server &",
		"make build &",
		"go build ./... &",
		"make -n test &",
		"echo 'make test &'",
		`git commit -m "make test & push"`,
		"nohup make test > log 2>&1 &",
		"( nohup make test > log 2>&1; echo $? > rc ) &",
		"make test > log 2>&1 & disown",
		"setsid make test > log &",
		"cat tests/claude/test_x.sh &",
		"bash -c 'make test &'",
		"cat <<'EOF' > x\nmake test &\nEOF",
	}
	for _, c := range cases {
		if r := backgroundDenial(c); r != "" {
			t.Errorf("止めない形を止めた: %q", c)
		}
	}
}

func TestHookDeniesBareBackgroundOfLongVerification(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"( make test > log 2>&1; echo $? > rc ) &"}}`
	var out bytes.Buffer
	if rc := runHook(strings.NewReader(in), &out); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var got struct {
		HSO struct {
			Decision string         `json:"permissionDecision"`
			Reason   string         `json:"permissionDecisionReason"`
			Updated  map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("出力が JSON でない: %q (%v)", out.String(), err)
	}
	if got.HSO.Decision != "deny" || !strings.Contains(got.HSO.Reason, "run_in_background") {
		t.Errorf("deny と run_in_background への案内を返すはず: %q", out.String())
	}
	if got.HSO.Updated != nil {
		t.Errorf("deny と書き換えを同時に返さない: %q", out.String())
	}
}
