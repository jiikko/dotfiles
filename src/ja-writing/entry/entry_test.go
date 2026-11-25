package entry

import (
	"bytes"
	"strings"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyfs"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := cmdenv.Env{Args: args, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Cwd: t.TempDir()}
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
	// 引数は本家の sys.argv と同じ字 (0xff は U+DCFF)。標準エラーには本家の _error と同じ backslashreplace で書く
	rc, _, stderr = run(t, "z"+string(pyfs.AppendSurrogate(nil, 0xDCFF)))
	if rc != cmdenv.ExitUsage || !strings.Contains(stderr, `です: z\udcff`+"\n") {
		t.Fatalf("rc=%d stderr=%q", rc, stderr)
	}
}

// 各 subcommand は自分の入口の答えを返す (別の subcommand の入口へ迷い込まない)。
func TestMainRoutesEachSubcommandToItsOwnEntry(t *testing.T) {
	rc, stdout, stderr := run(t, "hook", "--version")
	if rc != 0 || stdout != "yomiyasu_hook.py 3.1.0\n" || stderr != "" {
		t.Errorf("hook: rc=%d stdout=%q stderr=%q", rc, stdout, stderr)
	}
	rc, stdout, stderr = run(t, "check", "verify", "--json")
	if rc != cmdenv.ExitUsage || stdout != "" || !strings.Contains(stderr, "yomiyasu_check.py verify: error:") {
		t.Errorf("check: rc=%d stdout=%q stderr=%q", rc, stdout, stderr)
	}
	rc, stdout, stderr = run(t, "diff", "--json", "x.md")
	if rc != cmdenv.ExitUsage || stdout != "" || !strings.Contains(stderr, "yomiyasu_diff.py: error:") {
		t.Errorf("diff: rc=%d stdout=%q stderr=%q", rc, stdout, stderr)
	}
	rc, stdout, stderr = run(t, "lint", "--json", "x.md")
	if rc != cmdenv.ExitUsage || stdout != "" || !strings.Contains(stderr, "Error opening file x.md") {
		t.Errorf("lint: rc=%d stdout=%q stderr=%q", rc, stdout, stderr)
	}
	if len(subcommands) != 4 {
		t.Errorf("subcommand が %d 個 (期待 4)", len(subcommands))
	}
}
