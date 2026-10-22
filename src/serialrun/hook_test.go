package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWrapCommandInsertsBeforeHeavyCommands(t *testing.T) {
	cases := []struct{ in, want string }{
		{"make test", "serial-run -- make test"},
		{"make lint", "serial-run -- make lint"},
		{"make test-go", "serial-run -- make test-go"},
		{"make -C src/glogx test", "serial-run -- make -C src/glogx test"},
		{"make -j4 test", "serial-run -- make -j4 test"},
		{"make DRY_RUN=1 test-changed PATHS=x", "serial-run -- make DRY_RUN=1 test-changed PATHS=x"},
		{"go test ./...", "serial-run -- go test ./..."},
		{"GOFLAGS=-mod=readonly go test -race ./...", "GOFLAGS=-mod=readonly serial-run -- go test -race ./..."},
		{"xcodebuild -scheme X build", "serial-run -- xcodebuild -scheme X build"},
		{"swift test", "serial-run -- swift test"},
		{"bin/mutate-verify --file x", "serial-run -- bin/mutate-verify --file x"},
		{"cd src/x && make test && git push", "cd src/x && serial-run -- make test && git push"},
		{"make test > /tmp/x.log 2>&1; rc=$?", "serial-run -- make test > /tmp/x.log 2>&1; rc=$?"},
		{"make test 2>&1 | tail -5", "serial-run -- make test 2>&1 | tail -5"},
		{"(cd a && go test ./...)", "(cd a && serial-run -- go test ./...)"},
		{"if make test; then echo ok; fi", "if serial-run -- make test; then echo ok; fi"},
		{"time make test", "time serial-run -- make test"},
		{"make lint && make test", "serial-run -- make lint && serial-run -- make test"},
		{"echo start\nmake test", "echo start\nserial-run -- make test"},
		{"'make' test", "serial-run -- 'make' test"},
		{"env FOO=1 make test", "serial-run -- env FOO=1 make test"},
		{"nice -n 10 go test ./...", "serial-run -- nice -n 10 go test ./..."},
		{"bin/runtimeout 600 make test", "serial-run -- bin/runtimeout 600 make test"},
		{"make -j test", "serial-run -- make -j test"},
		{"make -j 8 test", "serial-run -- make -j 8 test"},
		{"make test &>/dev/null", "serial-run -- make test &>/dev/null"},
	}
	for _, c := range cases {
		got, changed := wrapCommand(c.in)
		if !changed || got != c.want {
			t.Errorf("%q:\n got %q (changed=%v)\nwant %q", c.in, got, changed, c.want)
		}
	}
}

func TestWrapCommandLeavesOtherCommandsAlone(t *testing.T) {
	cases := []string{
		"git status",
		"make build",
		"make -C test build",
		"make",
		"go build ./...",
		"go vet ./...",
		"swift run",
		`grep -rn "make test" issues/`,
		"grep -rn make test",
		`git commit -m "make test を回した"`,
		"echo make test",
		"echo $(make test)",
		"echo `go test ./...`",
		"x=\"$(go test ./...)\"",
		"git commit -F - <<'M'\nmake test\nM",
		"case $x in a) make test;; esac",
		"for i in $(seq 3); do (( i++ )); make test; done",
		"serial-run -- make test",
		"echo 'unterminated",
		"# make test",
		"ls # make test",
		"cat > f <<EOF\ngo test\nEOF",
		"make test)",
		"xcodebuild -version",
		"xcodebuild -list -project X.xcodeproj",
		"PATH=/usr/bin:/bin make test",
		"export PATH=/x:$PATH; make test",
		"echo foo &>/dev/null make test",
		"arr=(go test ./...)",
		"f() { make test; }",
		"make -j 8 build",
	}
	for _, in := range cases {
		if got, changed := wrapCommand(in); changed {
			t.Errorf("%q を書き換えた: %q", in, got)
		}
	}
}

func TestHookOutputsUpdatedInputKeepingOtherFields(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"make test","timeout":600000,"description":"テスト","run_in_background":true}}`
	var out strings.Builder
	if rc := runHook(strings.NewReader(in), &out); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName string         `json:"hookEventName"`
			UpdatedInput  map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("JSON でない: %q", out.String())
	}
	u := got.HookSpecificOutput.UpdatedInput
	if got.HookSpecificOutput.HookEventName != "PreToolUse" || u["command"] != "serial-run -- make test" ||
		u["timeout"] != float64(600000) || u["description"] != "テスト" || u["run_in_background"] != true {
		t.Fatalf("updatedInput: %#v", got)
	}
	if strings.Contains(out.String(), "permissionDecision") {
		t.Fatal("許可の判断を出している (書き換えだけにする。許可は Claude Code が書き換え後のコマンドで判定する)")
	}
}

func TestHookIsSilentWhenNothingToWrap(t *testing.T) {
	for _, in := range []string{
		`{"tool_name":"Bash","tool_input":{"command":"git status"}}`,
		`{"tool_name":"Read","tool_input":{"file_path":"make test"}}`,
		`not json`,
		`{"tool_name":"Bash","tool_input":{"command":42}}`,
	} {
		var out strings.Builder
		if rc := runHook(strings.NewReader(in), &out); rc != 0 || out.Len() != 0 {
			t.Errorf("%s: rc=%d out=%q", in, rc, out.String())
		}
	}
}

func hookContext(t *testing.T) (string, bool) {
	t.Helper()
	var out strings.Builder
	if rc := runHook(strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"make test"}}`), &out); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var got struct {
		HookSpecificOutput map[string]any `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("JSON でない: %q", out.String())
	}
	if got.HookSpecificOutput["updatedInput"] == nil {
		t.Fatalf("書き換えていない: %q", out.String())
	}
	c, ok := got.HookSpecificOutput["additionalContext"].(string)
	return c, ok
}

// 前に誰かいるときだけ、順番待ちになることを LLM 向けに添える
func TestHookAddsQueueNoteOnlyWhenSomeoneIsAhead(t *testing.T) {
	state := t.TempDir()
	t.Setenv("SERIAL_RUN_STATE_DIR", state)
	if c, ok := hookContext(t); ok {
		t.Fatalf("置き場が無いのに添えた: %q", c)
	}
	q, err := openQueue(filepath.Join(state, "default"))
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := hookContext(t); ok {
		t.Fatalf("誰も並んでいないのに添えた: %q", c)
	}
	// この test の中で札の flock を持つ (別の fd から見ると生きている札に見える)
	f, _, err := q.enqueue(ticketInfo{Pid: os.Getpid(), Start: time.Now(), Cwd: "/x/repo", Cmd: "make test"}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, ok := hookContext(t)
	if !ok {
		t.Fatal("前に 1 本あるのに順番待ちを知らせていない")
	}
	for _, want := range []string{"前に 1 本", "/x/repo の make test", "serial-run status", "rc=125"} {
		if !strings.Contains(c, want) {
			t.Errorf("知らせに %q が無い: %s", want, c)
		}
	}
}
