package main

import (
	"bytes"
	"strings"
	"testing"

	"yomiyasu/cmdenv"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := cmdenv.Env{Args: args, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Cwd: t.TempDir(), Getenv: func(string) string { return "" }}
	return Main(t.Context(), env), out.String(), errOut.String()
}

func TestMainWithoutSubcommandIsUsageError(t *testing.T) {
	rc, _, stderr := run(t)
	if rc != cmdenv.ExitUsage || !strings.Contains(stderr, "使い方") {
		t.Fatalf("rc=%d stderr=%q", rc, stderr)
	}
}

func TestMainUnknownSubcommandIsUsageError(t *testing.T) {
	rc, _, stderr := run(t, "nope")
	if rc != cmdenv.ExitUsage || !strings.Contains(stderr, "nope") {
		t.Fatalf("rc=%d stderr=%q", rc, stderr)
	}
}

// 各 subcommand は未実装の間 rc=2 を返し、どの subcommand が未実装かを標準エラーに出す (別の subcommand の入口へ迷い込まない)。
func TestMainRoutesEachSubcommandToItsOwnEntry(t *testing.T) {
	for _, name := range []string{"lint", "diff", "check", "hook"} {
		rc, stdout, stderr := run(t, name, "--json", "x.md")
		if rc != cmdenv.ExitUsage || stdout != "" || !strings.Contains(stderr, "yomiyasu "+name+":") {
			t.Errorf("%s: rc=%d stdout=%q stderr=%q", name, rc, stdout, stderr)
		}
	}
	if len(subcommands) != 4 {
		t.Errorf("subcommand が %d 個 (期待 4)", len(subcommands))
	}
}
