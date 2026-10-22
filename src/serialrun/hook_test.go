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
		{"GOPATH=/x go test ./...", "GOPATH=/x serial-run -- go test ./..."},
		{"PKG_CONFIG_PATH=/a make test", "PKG_CONFIG_PATH=/a serial-run -- make test"},
		{`make test && git commit -m "serial-run を直す"`, `serial-run -- make test && git commit -m "serial-run を直す"`},
		{"go -C src/x test ./...", "serial-run -- go -C src/x test ./..."},
		{"make test | tee log", "serial-run -- make test | tee log"},
		{"cat x | make test", "cat x | serial-run -- make test"},
		{"make lint | make test", "serial-run -- make lint | make test"},
		{"make lint || make test", "serial-run -- make lint || serial-run -- make test"},
		{"make test 2>&1 | grep PATH=", "serial-run -- make test 2>&1 | grep PATH="},
		{`git commit -m "PATH= の扱い"; make test`, `git commit -m "PATH= の扱い"; serial-run -- make test`},
		{"make test PATH=/x", "serial-run -- make test PATH=/x"},
		{"make test \\\n  -j4", "serial-run -- make test \\\n  -j4"},
		{"(make lint) && make test", "(serial-run -- make lint) && serial-run -- make test"},
		{"true && \\\n  make test", "true && \\\n  serial-run -- make test"},
		{"for d in a b; do go test ./$d/... 2>&1 | tail -5; done", "for d in a b; do serial-run -- go test ./$d/... 2>&1 | tail -5; done"},
		{"(cd src/x && go build) && make test 2>&1 | tail -20", "(cd src/x && go build) && serial-run -- make test 2>&1 | tail -20"},
		{"make test 2>&1 | tail -20; for f in a; do echo $f; done", "serial-run -- make test 2>&1 | tail -20; for f in a; do echo $f; done"},
		{"if ! make test 2>&1 | tail -20; then echo ng; fi", "if ! serial-run -- make test 2>&1 | tail -20; then echo ng; fi"},
		{"make test |\\\n  tail", "serial-run -- make test |\\\n  tail"},
		{"2>&1 make test | cat", "2>&1 serial-run -- make test | cat"},
		{"make test > log 2>&1; make lint", "serial-run -- make test > log 2>&1; serial-run -- make lint"},
		{"env PATH=/x make test", "serial-run -- env PATH=/x make test"},
		{"make -Cscripts test", "serial-run -- make -Cscripts test"},
		{"go test ./... -args -h", "serial-run -- go test ./... -args -h"},
		{"go -C=src/x test ./...", "serial-run -- go -C=src/x test ./..."},
	}
	for _, c := range cases {
		got, changed, _ := wrapCommand(c.in)
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
		"PATH+=:/x make test",
		"serial-run -- make lint | make test",
		"serial-run -- make test && make lint",
		"make -kC test-fixtures build",
		"make test | # c\nmake lint",
		"make test |\n  make lint",
		"exec serial-run -- make test | make lint",
		"make test | xargs serial-run -- make lint",
		"make test | sh -c 'serial-run -- make lint'",
		"make test | tee \"$(serial-run -- make lint)\"",
		"make test | while read l; do echo $l; done",
		"(make test) 2>&1 | go test ./...",
		"{ make test; } 2>&1 | go test ./...",
		"(make test) |& go test ./...",
		"( (make test) 2>&1 | go test ./... )",
		"(go test ./...) 2>&1 | tee log | make lint",
		"make test | time (go test ./...)",
		"make lint | tail; make test | tail",
		"make lint |& tee l; make test",
		"(make test) | serial-run -- tee x",
		"make test | (serial-run -- make lint)",
		"{ make test; } | serial-run -- make lint",
		"if make test; then :; fi | serial-run -- cat",
		"for i in 1; do make test; done | serial-run -- cat",
		"make test | while read l; do serial-run -- make lint; done",
		"(cd x && make test) | serial-run -- tee log",
		"make test |\nserial-run -- cat",
		"make test |\\\n  serial-run -- cat",
		"(make lint) | make test",
		"builtin export PATH=/x; make test",
		"command export PATH=/x; make test",
		"unset PATH; make test",
		"eval x; make test",
		"path=/x; make test",
		"make test 2>&1 | serial-run -- make lint",
		"git commit -m $'fix\\'s ; make test ; done' \\'",
		"noglob make test",
		"make -t test",
		"declare -x PATH=/x; go test ./...",
		"make -n test",
		"make --dry-run test",
		"make -q test",
		"make -p test-lint",
		"make -nk test",
		"make test --help",
		"mutate-verify --help",
		"go test -h",
		"swift test --help",
		"serial-run -- make test",
	}
	for _, in := range cases {
		if got, changed, _ := wrapCommand(in); changed {
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

func TestWrapCommandSkipReasons(t *testing.T) {
	for in, want := range map[string]string{
		"git commit -F - <<'M'\nmake test\nM":       "heredoc",
		"for i in 1; do (( i++ )); make test; done": "arith",
		"echo 'x; make test":                        "parse",
		"export PATH=/x:$PATH; make test":           "path",
		"git status":                                "",
	} {
		if _, changed, skip := wrapCommand(in); changed || skip != want {
			t.Errorf("%q: changed=%v skip=%q want %q", in, changed, skip, want)
		}
	}
}

// hook の判断は置き場の hook.log に残る (差し込んだとき・重いコマンドを含むのに見送ったときだけ)
func TestHookLogsDecisions(t *testing.T) {
	state := t.TempDir()
	t.Setenv("SERIAL_RUN_STATE_DIR", state)
	if _, err := openQueue(filepath.Join(state, "default")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"make test", "git status", "git commit -F - <<'M'\nmake test\nM", "ls"} {
		b, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": c}})
		var out strings.Builder
		_ = runHook(strings.NewReader(string(b)), &out)
	}
	log, err := os.ReadFile(filepath.Join(state, "default", "hook.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "\twrapped\tmake test") || !strings.Contains(lines[1], "\tskipped:heredoc\t") {
		t.Fatalf("hook.log:\n%s", log)
	}
}
